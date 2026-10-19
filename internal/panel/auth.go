package panel

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

// AuthService owns credentials, sessions and the shared password-attempt budget.
// Server composes it with routing and persistence instead of sharing its locks.
type AuthService struct {
	auth      Auth
	sessions  map[string]Session
	sessionMu sync.Mutex
	loginMu   sync.Mutex
	attempts  map[string][]time.Time
	authMu    sync.RWMutex
}

func newAuthService() AuthService {
	return AuthService{sessions: map[string]Session{}, attempts: map[string][]time.Time{}}
}

type Auth struct {
	User string `json:"user"`
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

type Session struct {
	CSRF     string
	Expires  time.Time
	AuthSalt string
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}

func passwordHash(password, salt string) string {
	key, e := pbkdf2.Key(sha256.New, password, []byte(salt), 150000, 32)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(key)
}

func (s *AuthService) session(r *http.Request) (Session, bool) {
	cookie, e := r.Cookie("darkls_session")
	if e != nil {
		return Session{}, false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	v, ok := s.sessions[cookie.Value]
	if ok && v.AuthSalt != s.authSnapshot().Salt {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	if ok && time.Now().After(v.Expires) {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	return v, ok
}

func (s *AuthService) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	if err := s.reservePasswordAttempt(ip, now); err != nil {
		fail(w, 429, err)
		return
	}
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	auth := s.authSnapshot()
	h := passwordHash(req.Password, auth.Salt)
	if subtle.ConstantTimeCompare([]byte(req.User), []byte(auth.User)) != 1 || subtle.ConstantTimeCompare([]byte(h), []byte(auth.Hash)) != 1 {
		fail(w, 401, errors.New("неверный логин или пароль"))
		return
	}
	s.loginMu.Lock()
	delete(s.attempts, ip)
	s.loginMu.Unlock()
	key := token()
	sess := Session{CSRF: token(), Expires: now.Add(12 * time.Hour), AuthSalt: auth.Salt}
	s.sessionMu.Lock()
	for k, v := range s.sessions {
		if now.After(v.Expires) {
			delete(s.sessions, k)
		}
	}
	s.sessions[key] = sess
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "darkls_session", Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	respond(w, 200, map[string]string{"csrf": sess.CSRF})
}

func (s *AuthService) authSnapshot() Auth { s.authMu.RLock(); defer s.authMu.RUnlock(); return s.auth }
func (s *AuthService) currentPassword(r *http.Request, password string) error {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if err := s.reservePasswordAttempt(ip, time.Now()); err != nil {
		return err
	}
	a := s.authSnapshot()
	h := passwordHash(password, a.Salt)
	if subtle.ConstantTimeCompare([]byte(h), []byte(a.Hash)) != 1 {
		return errors.New("неверный текущий пароль")
	}
	s.loginMu.Lock()
	delete(s.attempts, ip)
	s.loginMu.Unlock()
	return nil
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция уже выполняется"))
		return
	}
	defer s.mutation.Unlock()
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	if !utf8.ValidString(req.Password) || utf8.RuneCountInString(req.Password) < 12 || utf8.RuneCountInString(req.Password) > 128 {
		fail(w, 400, errors.New("новый пароль: от 12 до 128 символов"))
		return
	}
	release, e := s.pause()
	if e != nil {
		fail(w, 409, e)
		return
	}
	defer release()
	if e = s.currentPassword(r, req.Current); e != nil {
		fail(w, 403, e)
		return
	}
	a := s.authSnapshot()
	a.Salt = token()
	a.Hash = passwordHash(req.Password, a.Salt)
	if e = atomicWrite(filepath.Join(s.dir, "auth.json"), jsonBytes(a)); e != nil {
		fail(w, 500, e)
		return
	}
	s.authMu.Lock()
	s.auth = a
	s.authMu.Unlock()
	s.sessionMu.Lock()
	s.sessions = map[string]Session{}
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "darkls_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	appendEvent("security", "Пароль панели изменён; прежние сеансы завершены")
	respond(w, 200, map[string]bool{"ok": true})
}

// reservePasswordAttempt is shared by login and sensitive actions. A successful
// check clears the IP bucket; failures share the same five-minute budget.
func (s *AuthService) reservePasswordAttempt(ip string, now time.Time) error {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	recent := []time.Time{}
	for _, at := range s.attempts[ip] {
		if now.Sub(at) < 5*time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= 10 {
		return errors.New("слишком много попыток; повторите через 5 минут")
	}
	s.attempts[ip] = append(recent, now)
	return nil
}
