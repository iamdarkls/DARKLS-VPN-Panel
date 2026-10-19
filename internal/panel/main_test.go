package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeRunner struct {
	mu         sync.Mutex
	failStart  bool
	failReload bool
	calls      []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if name == "sh" && len(args) > 0 && strings.HasSuffix(args[0], "/reload-dns.sh") && f.failReload {
		f.failReload = false
		return []byte("simulated DNS reload failure"), errors.New("failed")
	}
	if name == "sh" && len(args) > 1 && args[1] == "start" && f.failStart {
		f.failStart = false
		return []byte("simulated startup failure"), errors.New("failed")
	}
	return []byte("OK"), nil
}
func fixture(t *testing.T) (*Server, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	for name, data := range map[string]string{"domains.txt": "youtube.com\ngooglevideo.com\ntelegram.org\nexample.com\n", "dnsmasq-vpn.conf": "port=10535\n", "telegram-networks.txt": "149.154.160.0/20\n"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	runner := &fakeRunner{}
	s := &Server{root: root, dir: filepath.Join(root, "panel"), host: "192.168.50.1:8088", runner: runner, AuthService: newAuthService()}
	if e := s.init(); e != nil {
		t.Fatal(e)
	}
	return s, runner
}

func TestDomainValidation(t *testing.T) {
	for raw, want := range map[string]string{"https://EXAMPLE.com/path?q=1": "example.com", "*.Example.COM.": "example.com", "cdn.example.net": "cdn.example.net"} {
		got, e := normalizeDomain(raw)
		if e != nil || got != want {
			t.Fatalf("%q => %q %v", raw, got, e)
		}
	}
	for _, raw := range []string{"example.com;reboot", "a.com\nserver=1.2.3.4", "$(reboot).com", "192.168.50.1", "-bad.com", "a..com", "https://user:password@example.com", "localhost"} {
		if _, e := normalizeDomain(raw); e == nil {
			t.Errorf("accepted unsafe domain %q", raw)
		}
	}
}
func TestSharedDomainsAndYouTube(t *testing.T) {
	st := State{Groups: []Group{{ID: "one", Name: "One", Enabled: false, Domains: []string{"shared.com"}}, {ID: "two", Name: "Two", Enabled: true, Domains: []string{"shared.com", "googlevideo.com"}}}}
	config := string(dnsConfig(activeDomains(st)))
	if !strings.Contains(config, "ipset=/googlevideo.com/darkls_vpn,darkls_yt\n") {
		t.Fatal("YouTube exception missing")
	}
	if !strings.Contains(config, "server=/shared.com/") {
		t.Fatal("shared dependency removed")
	}
	if strings.Contains(config, "log-queries") {
		t.Fatal("unbounded debug DNS logging enabled")
	}
}
func TestApplyAndTelegramDisable(t *testing.T) {
	s, runner := fixture(t)
	st, _ := s.state()
	for i := range st.Groups {
		if st.Groups[i].ID == "telegram" {
			st.Groups[i].Enabled = false
		}
		if st.Groups[i].ID == "custom" {
			st.Groups[i].Domains = []string{"https://new.example/path"}
		}
	}
	old := s.revision()
	id, e := s.apply(st, old)
	if e != nil {
		t.Fatal(e)
	}
	if id == "" || s.revision() == old {
		t.Fatal("state not committed")
	}
	tg, _ := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
	if len(tg) != 0 {
		t.Fatal("Telegram ranges remain enabled")
	}
	dom, _ := os.ReadFile(filepath.Join(s.root, "domains.txt"))
	if strings.Contains(string(dom), "example.com") || !strings.Contains(string(dom), "new.example") {
		t.Fatalf("domain deletion not applied: %s", dom)
	}
	if !strings.Contains(strings.Join(runner.calls, "\n"), "ipset swap darkls_tg_next darkls_tg") {
		t.Fatal("Telegram ranges not swapped atomically")
	}
	if _, e = os.Stat(filepath.Join(s.dir, "apply-journal.json")); !os.IsNotExist(e) {
		t.Fatal("journal left after success")
	}
}
func TestFailedApplyRollsBack(t *testing.T) {
	s, runner := fixture(t)
	old := s.revision()
	st, _ := s.state()
	st.Groups = append(st.Groups, Group{ID: "added", Name: "Added", Enabled: true, Domains: []string{"new.example"}})
	runner.failReload = true
	_, e := s.apply(st, old)
	if e == nil || !strings.Contains(e.Error(), "прежние настройки восстановлены") {
		t.Fatalf("missing rollback: %v", e)
	}
	if s.revision() != old {
		t.Fatal("files differ after rollback")
	}
	if _, e = os.Stat(filepath.Join(s.root, ".panel-apply.lock")); !os.IsNotExist(e) {
		t.Fatal("operation lock leaked")
	}
}
func TestConflictAndInvalidStateNeverMutate(t *testing.T) {
	s, runner := fixture(t)
	st, _ := s.state()
	old := s.revision()
	if _, e := s.apply(st, "stale"); e == nil {
		t.Fatal("accepted stale revision")
	}
	st.Groups[0].Domains = []string{"bad.com;reboot"}
	if _, e := s.apply(st, old); e == nil {
		t.Fatal("accepted invalid state")
	}
	if s.revision() != old || len(runner.calls) > 0 {
		t.Fatal("invalid input executed commands")
	}
}

func TestApplyLockExcludesConcurrentWatchdog(t *testing.T) {
	s, _ := fixture(t)
	release, e := s.pause()
	if e != nil {
		t.Fatal(e)
	}
	if second, e := s.pause(); e == nil {
		second()
		release()
		t.Fatal("concurrent operation acquired lock")
	}
	release()
	again, e := s.pause()
	if e != nil {
		t.Fatal("lock not released:", e)
	}
	again()
}
func TestRecoveryAfterInterruptedWrite(t *testing.T) {
	s, _ := fixture(t)
	old := s.revision()
	id, e := s.backup()
	if e != nil {
		t.Fatal(e)
	}
	_ = atomicWrite(filepath.Join(s.dir, "apply-journal.json"), jsonBytes(map[string]string{"backup": id}))
	_ = atomicWrite(filepath.Join(s.root, "domains.txt"), []byte("broken.example\n"))
	if e = s.recoverJournal(); e != nil {
		t.Fatal(e)
	}
	if s.revision() != old {
		t.Fatal("interrupted write was not restored")
	}
}
func request(s *Server, method, path, body string, cookie *http.Cookie, csrf, origin, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
	r.Host = host
	r.RemoteAddr = "192.168.50.224:12345"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}
func TestAuthenticationCSRFAndHostGuard(t *testing.T) {
	s, _ := fixture(t)
	s.auth = Auth{User: "admin", Salt: "test-salt", Hash: passwordHash("correct-password", "test-salt")}
	origin := "http://" + s.host
	if w := request(s, "GET", "/api/state", "", nil, "", "", s.host); w.Code != 401 {
		t.Fatalf("unauth status %d", w.Code)
	}
	w := request(s, "POST", "/api/login", `{"user":"admin","password":"correct-password"}`, nil, "", origin, s.host)
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	var login map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &login)
	if w = request(s, "POST", "/api/power", `{"enabled":false}`, cookies[0], "", origin, s.host); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w = request(s, "POST", "/api/power", `{"enabled":false}`, cookies[0], login["csrf"], "http://evil.example", s.host); w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	if w = request(s, "GET", "/api/state", "", cookies[0], "", "", "evil.example"); w.Code != 403 {
		t.Fatal("DNS rebinding accepted")
	}
	if w = request(s, "POST", "/api/power", `{"enabled":false}`, cookies[0], login["csrf"], origin, s.host); w.Code != 200 {
		t.Fatalf("power status %d: %s", w.Code, w.Body.String())
	}
	if !s.disabled() {
		t.Fatal("disabled flag missing")
	}
	w = request(s, "GET", "/api/state", "", cookies[0], "", "", s.host)
	body, _ := io.ReadAll(w.Result().Body)
	if bytes.Contains(body, []byte("correct-password")) || bytes.Contains(body, []byte(s.auth.Hash)) {
		t.Fatal("credentials leaked")
	}
}

func TestBrowserHarness(t *testing.T) {
	if os.Getenv("DARKLS_BROWSER_PREVIEW") != "1" {
		t.Skip("manual browser harness")
	}
	s, _ := fixture(t)
	s.host = "127.0.0.1:18088"
	st := State{Version: 1, Groups: append([]Group{}, presets...)}
	st.Groups = append(st.Groups, Group{ID: "custom", Name: "Мои сайты", Enabled: true, Domains: []string{"api.ipify.org"}})
	st, _ = addCatalogGroups(st)
	_ = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st))
	s.auth = Auth{User: "admin", Salt: "browser-test", Hash: passwordHash("preview-password", "browser-test")}
	t.Log("browser harness ready")
	if e := http.ListenAndServe(s.host, s.handler()); e != nil {
		t.Fatal(e)
	}
}
