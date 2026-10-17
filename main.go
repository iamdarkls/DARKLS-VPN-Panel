package main

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

//go:embed web/*
var webFiles embed.FS

var version = "1.4.1"

type Group struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Domains       []string `json:"domains"`
	Category      string   `json:"category,omitempty"`
	BlockQUIC     *bool    `json:"block_quic,omitempty"`
	EnabledUntil  int64    `json:"enabled_until,omitempty"`
	TimedPrevious bool     `json:"timed_previous,omitempty"`
	QUICUntil     int64    `json:"quic_until,omitempty"`
	QUICPrevious  bool     `json:"quic_previous,omitempty"`
}
type State struct {
	Version int          `json:"version"`
	Groups  []Group      `json:"groups"`
	Policy  DevicePolicy `json:"policy"`
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
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type realRunner struct{}

func (realRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), "PATH=/usr/sbin:/usr/bin:/sbin:/bin")
	return c.CombinedOutput()
}

type Server struct {
	root, dir, host string
	runner          Runner
	auth            Auth
	sessions        map[string]Session
	sessionMu       sync.Mutex
	mutation        sync.Mutex
	loginMu         sync.Mutex
	attempts        map[string][]time.Time
	authMu          sync.RWMutex
	diagnosticMu    sync.Mutex
	metricsMu       sync.Mutex
	githubMu        sync.Mutex
	coreMu          sync.Mutex
	historyMu       sync.Mutex
	lastCPU         cpuSample
}

func newServer(root, host string, runner Runner) *Server {
	return &Server{root: root, dir: filepath.Join(root, "panel"), host: host, runner: runner, sessions: map[string]Session{}, attempts: map[string][]time.Time{}}
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
func atomicWrite(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".panel-write-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func jsonBytes(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *Server) run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeDomain(raw string) (string, error) {
	d := strings.TrimSpace(raw)
	if strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") {
		u, e := url.Parse(d)
		if e != nil || u.User != nil || u.Hostname() == "" {
			return "", errors.New("неверный адрес сайта")
		}
		d = u.Hostname()
	}
	d = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(d, "*."), "."))
	if len(d) > 253 || len(d) < 3 || !strings.Contains(d, ".") {
		return "", fmt.Errorf("неверный домен: %q", raw)
	}
	if _, e := netip.ParseAddr(d); e == nil {
		return "", errors.New("введите домен сайта, а не IP-адрес")
	}
	for _, p := range strings.Split(d, ".") {
		if !labelPattern.MatchString(p) {
			return "", fmt.Errorf("неверный домен %q; для кириллицы используйте punycode", raw)
		}
	}
	return d, nil
}
func validate(st State) (State, error) {
	if len(st.Groups) > 100 {
		return st, errors.New("не более 100 групп")
	}
	st.Version = 1
	if err := validatePolicy(&st.Policy); err != nil {
		return st, err
	}
	ids := map[string]bool{}
	total := 0
	for i := range st.Groups {
		g := &st.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		if g.BlockQUIC == nil {
			g.BlockQUIC = boolRef(g.ID == "youtube")
		}
		if g.EnabledUntil < 0 || g.QUICUntil < 0 || (time.Now().Year() >= 2025 && (g.EnabledUntil > time.Now().Add(8*24*time.Hour).Unix() || g.QUICUntil > time.Now().Add(8*24*time.Hour).Unix())) {
			return st, errors.New("неверный срок временной настройки")
		}
		if !idPattern.MatchString(g.ID) || ids[g.ID] {
			return st, errors.New("неверный или повторяющийся идентификатор группы")
		}
		ids[g.ID] = true
		if g.Category == "" {
			g.Category = inferCategory(g.ID)
		}
		if !validCategory(g.Category) {
			return st, errors.New("неверный раздел группы")
		}
		if !utf8.ValidString(g.Name) || utf8.RuneCountInString(g.Name) < 1 || utf8.RuneCountInString(g.Name) > 60 || strings.ContainsAny(g.Name, "\x00\r\n") {
			return st, errors.New("название группы: от 1 до 60 символов")
		}
		seen := map[string]bool{}
		out := []string{}
		for _, raw := range g.Domains {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			d, e := normalizeDomain(raw)
			if e != nil {
				return st, e
			}
			if !seen[d] {
				out = append(out, d)
				seen[d] = true
				total++
			}
		}
		sort.Strings(out)
		g.Domains = out
	}
	if total > 3000 {
		return st, errors.New("не более 3000 доменов")
	}
	return st, nil
}

var presets = []Group{
	{ID: "youtube", Name: "YouTube", Enabled: true, Domains: strings.Fields("youtube.com youtu.be youtube-nocookie.com googlevideo.com ytimg.com youtubei.googleapis.com youtube.googleapis.com yt3.ggpht.com accounts.google.com oauth2.googleapis.com")},
	{ID: "chatgpt", Name: "ChatGPT", Enabled: true, Domains: strings.Fields("chatgpt.com openai.com oaistatic.com oaiusercontent.com")},
	{ID: "instagram", Name: "Instagram", Enabled: true, Domains: strings.Fields("instagram.com cdninstagram.com ig.me fbcdn.net fbsbx.com")},
	{ID: "discord", Name: "Discord", Enabled: true, Domains: strings.Fields("discord.com discord.gg discordapp.com discordapp.net discord.media discordcdn.com dis.gd")},
	{ID: "gemini", Name: "Gemini", Enabled: true, Domains: strings.Fields("gemini.google.com gemini.google aistudio.google.com generativelanguage.googleapis.com accounts.google.com oauth2.googleapis.com gstatic.com googleusercontent.com")},
	{ID: "twitter", Name: "X / Twitter", Enabled: true, Domains: strings.Fields("x.com twitter.com t.co twimg.com twittercommunity.com")},
	{ID: "telegram", Name: "Telegram", Enabled: true, Domains: strings.Fields("telegram.org telegram.me telegram.dog t.me telesco.pe telegra.ph telegram-cdn.org cdn-telegram.org fragment.com")},
}

func importState(domains []byte) (State, error) {
	existing := map[string]bool{}
	for _, line := range strings.Split(string(domains), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d, e := normalizeDomain(line)
		if e != nil {
			return State{}, e
		}
		existing[d] = true
	}
	used := map[string]bool{}
	st := State{Version: 1, Groups: []Group{}}
	for _, p := range presets {
		g := Group{ID: p.ID, Name: p.Name, Enabled: true, Domains: []string{}}
		for _, d := range p.Domains {
			if existing[d] {
				g.Domains = append(g.Domains, d)
				used[d] = true
			}
		}
		st.Groups = append(st.Groups, g)
	}
	other := Group{ID: "custom", Name: "Мои сайты", Enabled: true, Domains: []string{}}
	for d := range existing {
		if !used[d] {
			other.Domains = append(other.Domains, d)
		}
	}
	st.Groups = append(st.Groups, other)
	st, _ = addCatalogGroups(st)
	return validate(st)
}
func activeDomains(st State) []string {
	m := map[string]bool{}
	for _, g := range st.Groups {
		if g.active(time.Now().Unix()) {
			for _, d := range g.Domains {
				m[d] = true
			}
		}
	}
	out := []string{}
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
func youtubeDomain(d string) bool {
	for _, suffix := range []string{"youtube.com", "googlevideo.com", "youtube-nocookie.com", "ytimg.com"} {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return true
		}
	}
	return false
}
func dnsConfig(domains []string) []byte {
	var b strings.Builder
	b.WriteString("port=10535\nlisten-address=127.0.0.1,192.168.50.1\nbind-interfaces\npid-file=/tmp/dnsmasq-vpn.pid\nno-resolv\nno-hosts\ncache-size=1000\nfilter-AAAA\nserver=127.0.0.1#53\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "server=/%s/127.0.0.1#1053\nipset=/%s/darkls_vpn", d, d)
		if youtubeDomain(d) {
			b.WriteString(",darkls_yt")
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
func (s *Server) init() error {
	if e := os.MkdirAll(filepath.Join(s.dir, "backups"), 0700); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(s.dir, "state.json")); os.IsNotExist(e) {
		b, e := os.ReadFile(filepath.Join(s.root, "domains.txt"))
		if e != nil {
			return e
		}
		st, e := importState(b)
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); e != nil {
			return e
		}
	}
	tg := filepath.Join(s.dir, "telegram-original.txt")
	if _, e := os.Stat(tg); os.IsNotExist(e) {
		b, e := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
		if e != nil {
			return e
		}
		if e = atomicWrite(tg, b); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) state() (State, error) {
	var st State
	e := readJSON(filepath.Join(s.dir, "state.json"), &st)
	if e != nil {
		return st, e
	}
	return validate(st)
}

var managedFiles = []string{"domains.txt", "dnsmasq-vpn.conf", "telegram-networks.txt", "panel/state.json"}

func (s *Server) revision() string {
	h := sha256.New()
	for _, f := range managedFiles {
		b, _ := os.ReadFile(filepath.Join(s.root, f))
		h.Write([]byte(f))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) backup() (string, error) {
	now := time.Now()
	id := fmt.Sprintf("%s-%09d-%s", now.Format("20060102-150405"), now.Nanosecond(), token()[:8])
	dir := filepath.Join(s.dir, "backups", id)
	if e := os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	for _, f := range managedFiles {
		b, e := os.ReadFile(filepath.Join(s.root, f))
		if e != nil {
			return "", e
		}
		if e = atomicWrite(filepath.Join(dir, filepath.Base(f)), b); e != nil {
			return "", e
		}
	}
	return id, nil
}
func (s *Server) restoreFiles(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("неверная резервная копия")
	}
	for _, f := range managedFiles {
		b, e := os.ReadFile(filepath.Join(s.dir, "backups", id, filepath.Base(f)))
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(s.root, f), b); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) prune() {
	entries, _ := os.ReadDir(filepath.Join(s.dir, "backups"))
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && idPattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > 12 {
		_ = os.RemoveAll(filepath.Join(s.dir, "backups", names[0]))
		names = names[1:]
	}
}
func (s *Server) disabled() bool {
	_, e := os.Stat(filepath.Join(s.root, "autostart.disabled"))
	return e == nil
}

func (s *Server) pause() (func(), error) {
	lock, e := os.OpenFile(filepath.Join(s.dir, "apply.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		_ = lock.Close()
		return nil, errors.New("другая операция уже выполняется")
	}
	// Kernel-managed locks are released even if the process crashes or the router reboots.
	release := func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }
	deadline := time.Now().Add(45 * time.Second)
	for managerRunning(s.root) {
		if time.Now().After(deadline) {
			release()
			return nil, errors.New("скрипт VPN занят; повторите позже")
		}
		time.Sleep(250 * time.Millisecond)
	}
	return release, nil
}

func managerRunning(root string) bool {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if e != nil {
			continue
		}
		args := strings.Split(string(b), "\x00")
		if len(args) > 1 && args[1] == filepath.Join(root, "vpn.sh") {
			return true
		}
	}
	return false
}
func (s *Server) restart() error {
	if out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "stop"); e != nil {
		return fmt.Errorf("остановка маршрутизации: %s", limited(out))
	}
	if s.disabled() {
		return nil
	}
	// Cached client DNS answers remain in use across process restarts. Preserve
	// their VPN and YouTube address sets instead of silently bypassing the VPN.
	if e := s.syncTelegramSet(); e != nil {
		return e
	}
	out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "start")
	if e != nil {
		return fmt.Errorf("запуск VPN: %s", limited(out))
	}
	return nil
}
func limited(b []byte) string {
	if len(b) > 3000 {
		b = b[len(b)-3000:]
	}
	return string(b)
}
func (s *Server) apply(st State, expected string) (backup string, err error) {
	st, err = validate(st)
	if err != nil {
		return "", err
	}
	if expected != s.revision() {
		return "", errors.New("CONFLICT: настройки изменились; обновите страницу")
	}
	config := dnsConfigForState(st)
	tmp := filepath.Join(s.dir, "dnsmasq-candidate.conf")
	if err = atomicWrite(tmp, config); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	if out, e := s.run("dnsmasq", "--test", "--conf-file="+tmp); e != nil {
		return "", fmt.Errorf("проверка DNS: %s", limited(out))
	}
	release, e := s.pause()
	if e != nil {
		return "", e
	}
	defer release()
	// Recheck under the interprocess lock: another process may have committed
	// between validation and acquiring the lock.
	if expected != s.revision() {
		return "", errors.New("CONFLICT: настройки изменились; обновите страницу")
	}
	oldConfig, err := os.ReadFile(filepath.Join(s.root, "dnsmasq-vpn.conf"))
	if err != nil {
		return "", err
	}
	oldState, err := s.state()
	if err != nil {
		return "", err
	}
	oldTG, err := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
	if err != nil {
		return "", err
	}
	routingChanged := policyChanged(oldState, st)
	if !s.disabled() && (routingChanged || !bytes.Equal(oldConfig, config)) {
		if err = s.ensureGroupSets(st); err != nil {
			return "", err
		}
	}
	backup, err = s.backup()
	if err != nil {
		return "", err
	}
	journal := filepath.Join(s.dir, "apply-journal.json")
	if err = atomicWrite(journal, jsonBytes(map[string]string{"backup": backup})); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			re := s.restoreFiles(backup)
			rr := s.reloadRouting(true, true)
			if re == nil {
				if pe := s.syncGroupRouting(); pe != nil && rr == nil {
					rr = pe
				}
			}
			if de := s.syncDevices(true); de != nil && rr == nil {
				rr = de
			}
			if re != nil || rr != nil {
				err = fmt.Errorf("%v; восстановление: файлы=%v, VPN=%v", err, re, rr)
				return
			}
			err = fmt.Errorf("%v; прежние настройки восстановлены", err)
		}
		_ = os.Remove(journal)
	}()
	domains := strings.Join(activeDomains(st), "\n")
	if domains != "" {
		domains += "\n"
	}
	tg := []byte{}
	for _, g := range st.Groups {
		if g.ID == "telegram" && g.Enabled {
			tg, err = os.ReadFile(filepath.Join(s.dir, "telegram-original.txt"))
			if err != nil {
				return backup, err
			}
		}
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{"domains.txt", []byte(domains)}, {"dnsmasq-vpn.conf", config}, {"telegram-networks.txt", tg}, {"panel/state.json", jsonBytes(st)}} {
		if err = atomicWrite(filepath.Join(s.root, f.name), f.data); err != nil {
			return backup, err
		}
	}
	if err = s.reloadRouting(!bytes.Equal(oldConfig, config), !bytes.Equal(oldTG, tg)); err != nil {
		return backup, err
	}
	if !bytes.Equal(jsonBytes(oldState.Policy), jsonBytes(st.Policy)) {
		if err = s.syncDevices(true); err != nil {
			return backup, err
		}
	}
	if routingChanged || !bytes.Equal(oldConfig, config) {
		if err = s.syncGroupRouting(); err != nil {
			return backup, err
		}
	}
	appendEvent("settings", "Настройки сайтов и устройств применены")
	s.prune()
	return backup, nil
}
func (s *Server) recoverJournal() error {
	path := filepath.Join(s.dir, "apply-journal.json")
	var j map[string]string
	e := readJSON(path, &j)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	release, e := s.pause()
	if e != nil {
		return e
	}
	defer release()
	if e = s.restoreFiles(j["backup"]); e != nil {
		return e
	}
	if e = s.restart(); e != nil {
		return e
	}
	return os.Remove(path)
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, e error) {
	respond(w, code, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("неверные данные запроса")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("лишние данные запроса")
	}
	return nil
}
func (s *Server) session(r *http.Request) (Session, bool) {
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
func (s *Server) allowedIP(r *http.Request) bool {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return false
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return ip.IsLoopback() || netip.MustParsePrefix("192.168.50.0/24").Contains(ip.Unmap())
}
func (s *Server) handler() http.Handler {
	sub, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != s.host || !s.allowedIP(r) {
			fail(w, 403, errors.New("доступ разрешён только из домашней сети по адресу панели"))
			return
		}
		if r.Method == "POST" && r.Header.Get("Origin") != "http://"+s.host {
			fail(w, 403, errors.New("неверный источник запроса"))
			return
		}
		if r.URL.Path == "/api/login" {
			s.login(w, r)
			return
		}
		if r.URL.Path == "/api/update-status" && r.Method == "GET" {
			s.publicUpdateStatus(w)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				w.WriteHeader(405)
				return
			}
			if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/theme.js" && r.URL.Path != "/style.css" && r.URL.Path != "/advanced.js" && r.URL.Path != "/core.js" {
				http.NotFound(w, r)
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		sess, ok := s.session(r)
		if !ok {
			fail(w, 401, errors.New("войдите в панель"))
			return
		}
		if r.Method == "POST" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(sess.CSRF)) != 1 {
			fail(w, 403, errors.New("обновите страницу и повторите вход"))
			return
		}
		if r.Method == "POST" && updateBusy() && r.URL.Path != "/api/logout" {
			fail(w, 409, errors.New("выполняется обновление панели"))
			return
		}
		if s.coreAPI(w, r) {
			return
		}
		if s.advancedAPI(w, r) {
			return
		}
		if s.extraAPI(w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/session":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			respond(w, 200, map[string]any{"csrf": sess.CSRF, "version": version, "user": s.authSnapshot().User})
		case "/api/state":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			s.mutation.Lock()
			defer s.mutation.Unlock()
			st, e := s.state()
			if e != nil {
				fail(w, 500, e)
				return
			}
			respond(w, 200, map[string]any{"state": st, "revision": s.revision()})
		case "/api/status":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			s.status(w)
		case "/api/backups":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			entries, _ := os.ReadDir(filepath.Join(s.dir, "backups"))
			ids := []string{}
			for _, e := range entries {
				if e.IsDir() && idPattern.MatchString(e.Name()) {
					ids = append(ids, e.Name())
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(ids)))
			respond(w, 200, map[string]any{"backups": ids})
		case "/api/export":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			st, e := s.state()
			if e != nil {
				fail(w, 500, e)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="darkls-settings.json"`)
			respond(w, 200, st)
		case "/api/apply", "/api/restore", "/api/power":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			s.mutate(w, r)
		case "/api/logout":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			c, _ := r.Cookie("darkls_session")
			s.sessionMu.Lock()
			delete(s.sessions, c.Value)
			s.sessionMu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "darkls_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			respond(w, 200, map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	s.loginMu.Lock()
	recent := []time.Time{}
	for _, t := range s.attempts[ip] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		s.loginMu.Unlock()
		fail(w, 429, errors.New("слишком много попыток; повторите через 5 минут"))
		return
	}
	s.attempts[ip] = append(recent, now)
	s.loginMu.Unlock()
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
func (s *Server) mutate(w http.ResponseWriter, r *http.Request) {
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("настройки уже применяются"))
		return
	}
	defer s.mutation.Unlock()
	switch r.URL.Path {
	case "/api/apply":
		var req struct {
			State    State  `json:"state"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		id, e := s.apply(req.State, req.Revision)
		if e != nil {
			code := 400
			if strings.HasPrefix(e.Error(), "CONFLICT:") {
				code = 409
			}
			fail(w, code, e)
			return
		}
		respond(w, 200, map[string]any{"ok": true, "backup": id, "revision": s.revision(), "disabled": s.disabled()})
	case "/api/restore":
		var req struct {
			Backup   string `json:"backup"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		if !idPattern.MatchString(req.Backup) {
			fail(w, 400, errors.New("неверная копия"))
			return
		}
		var st State
		if e := readJSON(filepath.Join(s.dir, "backups", req.Backup, "state.json"), &st); e != nil {
			fail(w, 404, errors.New("копия не найдена"))
			return
		}
		id, e := s.apply(st, req.Revision)
		if e != nil {
			fail(w, 400, e)
			return
		}
		respond(w, 200, map[string]any{"ok": true, "backup": id})
	case "/api/power":
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		release, e := s.pause()
		if e != nil {
			fail(w, 409, e)
			return
		}
		defer release()
		path := filepath.Join(s.root, "autostart.disabled")
		wasDisabled := s.disabled()
		if req.Enabled {
			e = os.Remove(path)
			if os.IsNotExist(e) {
				e = nil
			}
		} else {
			e = atomicWrite(path, []byte("disabled by panel\n"))
		}
		if e == nil {
			e = s.restart()
		}
		if e != nil {
			if wasDisabled {
				_ = atomicWrite(path, []byte("disabled\n"))
			} else {
				_ = os.Remove(path)
			}
			_ = s.restart()
			fail(w, 500, e)
			return
		}
		message := "VPN выключен"
		if req.Enabled {
			message = "VPN включён"
		}
		appendEvent("power", message)
		respond(w, 200, map[string]bool{"ok": true})
	}
}
func (s *Server) status(w http.ResponseWriter) {
	check := func(name string, args ...string) bool { _, e := s.run(name, args...); return e == nil }
	nat := check("iptables", "-t", "nat", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_NAT")
	udp := check("iptables", "-t", "mangle", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_UDP")
	xray := check("pidof", "xray")
	dns := pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf")
	watch := pidMatches("/tmp/darkls-watch.pid", "/jffs/xray/watch.sh")
	probe := readProbeState()
	fresh := probe.CheckedAt > 0 && time.Since(time.Unix(probe.CheckedAt, 0)) >= 0 && time.Since(time.Unix(probe.CheckedAt, 0)) < 100*time.Second
	respond(w, 200, map[string]any{"active": nat && udp && xray && dns && !s.disabled() && probe.OK && fresh, "routes": nat && udp && xray && dns, "disabled": s.disabled(), "nat": nat, "udp": udp, "xray": xray, "dns": dns, "watch": watch, "version": version, "probe": probe, "probe_fresh": fresh})
}
func pidMatches(file, match string) bool {
	b, e := os.ReadFile(file)
	if e != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	if _, e = strconv.Atoi(pid); e != nil {
		return false
	}
	cmd, e := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	return e == nil && bytes.Contains(cmd, []byte(match))
}

func tuneRuntime() {
	// The local control panel has little parallel work. Bound Go's memory target
	// and collect sooner; this is a soft runtime budget, not a hard RSS limit.
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(24 << 20)
	debug.SetGCPercent(75)
}

func main() {
	tuneRuntime()
	root := flag.String("root", "/jffs/xray", "каталог VPN")
	listen := flag.String("listen", "192.168.50.1:8088", "адрес панели")
	initAuth := flag.Bool("init", false, "создать пароль и импортировать списки")
	reset := flag.Bool("reset-password", false, "создать новый пароль")
	watchRecover := flag.Bool("watch-recover", false, "восстановить маршрутизацию из автоконтроля")
	syncGroups := flag.Bool("sync-groups", false, "восстановить правила групп без перезапуска Xray")
	upgradePolicy := flag.Bool("upgrade-policy", false, "мигрировать правила групп")
	recoverParts := flag.Bool("recover-components", false, "точечное восстановление VPN")
	checkParts := flag.Bool("check-components", false, "проверить компоненты соединения")
	repairIG := flag.Bool("repair-instagram", false, "добавить недостающий домен проверки Instagram один раз")
	updateCatalog := flag.Bool("update-catalog", false, "добавить отсутствующие группы выключенными")
	checkVPN := flag.Bool("check-vpn", false, "проверить HTTPS через локальный SOCKS Xray")
	upgradeXray := flag.String("upgrade-xray", "", "установить официальное ядро Xray 26.10.10 arm64 с откатом")
	rollbackXray := flag.Bool("rollback-xray", false, "вернуть предыдущее ядро Xray с USB")
	checkRouter := flag.Bool("check-router", false, "проверить содержимое правил и VPN DNS")
	maintain := flag.Bool("maintain-logs", false, "ограничить журналы в RAM")
	showVersion := flag.Bool("version", false, "показать версию")
	syncDevices := flag.Bool("sync-devices", false, "обновить выбор устройств")
	forceDevices := flag.Bool("force-devices", false, "восстановить правила устройств")
	reason := flag.String("reason", "автоконтроль", "причина восстановления")
	installStaged := flag.String("install-staged", "", "установить проверенный пакет")
	verifyPackage := flag.String("verify-package", "", "проверить пакет")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *verifyPackage != "" {
		if _, e := verifyDirectory(*verifyPackage); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Package integrity OK")
		return
	}
	s := newServer(*root, *listen, realRunner{})
	if *maintain {
		maintainLogs()
		return
	}
	if *checkRouter {
		maintainLogs()
		if e := s.checkRouter(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println("VPN rules and DNS OK")
		return
	}
	if *checkVPN {
		started := time.Now()
		e := checkVPNConnection()
		recordProbe(e, time.Since(started))
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println("VPN HTTPS OK")
		return
	}
	if *upgradeXray != "" {
		if e := s.upgradeCore(*upgradeXray); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *rollbackXray {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		var j coreJournal
		if e = readJSON(s.corePath("xray-previous.json"), &j); e != nil {
			log.Fatal(e)
		}
		if e := validateCoreBackup(j); e != nil {
			log.Fatal(e)
		}
		j.Disabled = s.disabled()
		if e = atomicWrite(s.corePath("xray-update-journal.json"), jsonBytes(j)); e != nil {
			log.Fatal(e)
		}
		if e = s.restoreCore(j); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Previous Xray restored")
		return
	}
	if *installStaged != "" {
		if e := s.installStaged(*installStaged); e != nil {
			log.Fatal(e)
		}
		return
	}
	if e := s.init(); e != nil {
		log.Fatal(e)
	}

	if *syncDevices {
		if e := s.syncDevices(*forceDevices); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *syncGroups {
		if e := s.syncGroupRouting(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *upgradePolicy {
		if e := s.upgradeGroupPolicy(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *checkParts {
		snap := s.collectConnectivity(true)
		s.saveConnectivity(snap)
		fmt.Println(string(jsonBytes(snap)))
		return
	}
	if *recoverParts {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		if e = s.recoverComponents(*reason); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *repairIG {
		if e := s.repairInstagram(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *updateCatalog {
		if e := s.updateCatalog(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *watchRecover {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		if s.disabled() {
			return
		}
		var pending coreJournal
		if readJSON(s.corePath("xray-update-journal.json"), &pending) == nil {
			if e = s.restoreCore(pending); e != nil {
				log.Fatal(e)
			}
			return
		}
		if _, e = os.Stat(filepath.Join(s.dir, "apply-journal.json")); e == nil {
			log.Print("Незавершённое применение: ожидаем восстановления панелью")
			return
		}
		e = s.recoverComponents(*reason)
		fmt.Println("Component recovery checked")
		if e != nil {
			release()
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if e := s.recoverCoreJournal(); e != nil {
		log.Fatal(e)
	}
	if e := s.recoverProfileSwitch(); e != nil {
		log.Fatal(e)
	}
	authPath := filepath.Join(s.dir, "auth.json")
	if *initAuth || *reset {
		if _, e := os.Stat(authPath); e == nil && !*reset {
			fmt.Println("Пароль уже создан. Для сброса: --reset-password")
			return
		}
		password := token()[:24]
		s.auth = Auth{User: "admin", Salt: token()}
		s.auth.Hash = passwordHash(password, s.auth.Salt)
		if e := atomicWrite(authPath, jsonBytes(s.auth)); e != nil {
			log.Fatal(e)
		}
		fmt.Printf("Логин: admin\nПароль: %s\nАдрес: http://%s\n", password, *listen)
		return
	}
	if e := readJSON(authPath, &s.auth); e != nil {
		log.Fatal("сначала выполните darkls-panel --init")
	}
	if e := s.recoverJournal(); e != nil {
		log.Fatal("восстановление незавершённого применения: ", e)
	}
	go s.deviceLoop()
	go s.githubLoop()
	go s.advancedLoop()
	go s.coreLoop()
	go s.cleanupLoop()
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Fatal(e)
	}
	pidPath := "/tmp/darkls-panel.pid"
	if e = atomicWrite(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n")); e != nil {
		log.Fatal(e)
	}
	defer os.Remove(pidPath)
	server := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	log.Printf("DARKLS panel %s: %s", version, *listen)
	if e = server.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Print(e)
	}
}
