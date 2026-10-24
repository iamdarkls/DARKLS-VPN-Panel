package panel

import (
	"context"
	"crypto/subtle"
	"darkls-panel/internal/routerenv"
	"darkls-panel/web"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

func (s *Server) allowedIP(r *http.Request) bool {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return false
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return ip.IsLoopback() || netip.MustParsePrefix(routerenv.LANPrefix).Contains(ip.Unmap())
}

func (s *Server) handler() http.Handler {
	static := http.HandlerFunc(web.ServeAsset)
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
			if !publicAsset(r.URL.Path) {
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
		if r.Method == "POST" && s.pendingRecovery() && !recoveryRequestAllowed(r.URL.Path) {
			fail(w, 409, errors.New("Режим восстановления: завершите незавершённую операцию в разделе Контроль связи → Восстановление"))
			return
		}
		if r.URL.Path == "/api/recovery" {
			s.recoveryAPI(w, r)
			return
		}
		if s.connectionAPI(w, r) {
			return
		}
		if s.activityAPI(w, r) {
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
			respond(w, 200, map[string]any{"state": st, "revision": s.revision(), "catalog_notes": catalogNotes})
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
	// Keep status within the browser's timeout even if a router utility stalls.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := func(name string, args ...string) bool { _, e := s.runner.Run(ctx, name, args...); return e == nil }
	nat := check("iptables", "-t", "nat", "-C", "PREROUTING", "-i", routerenv.LANInterface, "-s", routerenv.LANPrefix, "-j", "DV_NAT")
	udp := check("iptables", "-t", "mangle", "-C", "PREROUTING", "-i", routerenv.LANInterface, "-s", routerenv.LANPrefix, "-j", "DV_UDP")
	xray := len(s.mainXrayPIDsContext(ctx)) > 0
	dns := pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf")
	watch := pidMatches("/tmp/darkls-watch.pid", "/jffs/xray/watch.sh")
	probe := readProbeState()
	fresh := probeFresh(probe, time.Now())
	respond(w, 200, map[string]any{"recovery_pending": s.pendingRecovery(), "active": nat && udp && xray && dns && !s.disabled() && probe.OK && fresh, "routes": nat && udp && xray && dns, "disabled": s.disabled(), "nat": nat, "udp": udp, "xray": xray, "dns": dns, "watch": watch, "monitor": readMonitorHeartbeat(s.runtimePath("monitor-heartbeat")), "version": version, "probe": probe, "probe_fresh": fresh})
}

// Keep the asset allowlist explicit even though the web package embeds asset directories.
func publicAsset(path string) bool {
	switch path {
	case "/", "/js/features/app.js", "/js/core/ui.js", "/js/components/navigation.js", "/js/core/theme.js", "/css/style.css", "/js/features/advanced.js", "/js/features/core.js", "/js/features/manage.js", "/js/components/notifications.js":
		return true
	default:
		return false
	}
}
