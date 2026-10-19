package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Login and a password-protected action must consume the same IP budget.
func TestPasswordAttemptBudgetSharedAndExpires(t *testing.T) {
	s, _ := fixture(t)
	s.auth = Auth{User: "admin", Salt: "test-salt", Hash: passwordHash("test-password", "test-salt")}
	ip := "192.168.50.224"
	login := func(password, remote string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"`+password+`"}`))
		r.RemoteAddr = remote + ":12345"
		w := httptest.NewRecorder()
		s.login(w, r)
		return w
	}
	for range 9 {
		if w := login("wrong", ip); w.Code != 401 {
			t.Fatalf("wrong password: %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/password", nil)
	r.RemoteAddr = ip + ":12345"
	if err := s.currentPassword(r, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if w := login("test-password", ip); w.Code != 429 {
		t.Fatalf("shared limit bypassed: %d", w.Code)
	}
	if w := login("test-password", "192.168.50.202"); w.Code != 200 {
		t.Fatalf("another IP blocked: %d", w.Code)
	}
	s.loginMu.Lock()
	for i := range s.attempts[ip] {
		s.attempts[ip][i] = time.Now().Add(-6 * time.Minute)
	}
	s.loginMu.Unlock()
	w := login("test-password", ip)
	if w.Code != 200 {
		t.Fatalf("expired budget still blocks login: %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protections changed")
	}
	if len(s.attempts[ip]) != 0 {
		t.Fatal("successful login did not clear failures")
	}
}
