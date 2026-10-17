package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) authSnapshot() Auth { s.authMu.RLock(); defer s.authMu.RUnlock(); return s.auth }
func (s *Server) currentPassword(r *http.Request, password string) error {
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
		return errors.New("слишком много попыток; повторите через 5 минут")
	}
	s.attempts[ip] = append(recent, now)
	s.loginMu.Unlock()
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
func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика уже выполняется"))
		return
	}
	defer s.diagnosticMu.Unlock()
	var req struct {
		Domain string `json:"domain"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	domain, e := normalizeDomain(req.Domain)
	if e != nil {
		fail(w, 400, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	dnsAddress := "127.0.0.1:10535"
	if s.disabled() {
		dnsAddress = "127.0.0.1:53"
	}
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, dnsAddress)
	}}
	ips, e := resolver.LookupIP(ctx, "ip4", domain)
	if e != nil {
		fail(w, 502, fmt.Errorf("DNS роутера не ответил: %v", e))
		return
	}
	st, e := s.state()
	if e != nil {
		fail(w, 500, e)
		return
	}
	matches := []map[string]any{}
	for _, g := range st.Groups {
		for _, suffix := range g.Domains {
			if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
				matches = append(matches, map[string]any{"name": g.Name, "enabled": g.Enabled, "category": g.Category})
				break
			}
		}
	}
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	eligible := true
	if st.Policy.Mode != "all" {
		_, e := s.run("ipset", "test", "darkls_devices", client)
		eligible = e == nil
		if st.Policy.Mode == "exclude" {
			eligible = !eligible
		}
	}
	_, hookErr := s.run("iptables", "-t", "nat", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_NAT")
	natRules := []byte(s.expandedChain("nat", "DV_NAT"))
	routesActive := hookErr == nil
	rows := []map[string]any{}
	for _, ip := range ips {
		cached := false
		for _, set := range []string{"darkls_vpn", "darkls_tg"} {
			if _, e := s.run("ipset", "test", set, ip.String()); e == nil {
				cached = true
				break
			}
		}
		rows = append(rows, map[string]any{"ip": ip.String(), "in_vpn_set": cached, "via_vpn": cached && eligible && routesActive && !natAddressBypass(ip.String(), string(natRules)) && !s.disabled(), "bypass": natAddressBypass(ip.String(), string(natRules))})
	}
	appendEvent("diagnostic", "Проверен домен "+domain)
	respond(w, 200, map[string]any{"domain": domain, "groups": matches, "addresses": rows, "client_ip": client, "device_eligible": eligible, "policy_mode": st.Policy.Mode, "disabled": s.disabled(), "routes_active": routesActive})
}
func (s *Server) extraAPI(w http.ResponseWriter, r *http.Request) bool {
	method := func(want string) bool {
		if r.Method != want {
			w.WriteHeader(405)
			return false
		}
		return true
	}
	switch r.URL.Path {
	case "/api/github":
		if method("GET") {
			respond(w, 200, s.githubStatus())
		}
	case "/api/github-check":
		if method("POST") {
			s.githubAPI(w, r, false)
		}
	case "/api/github-install":
		if method("POST") {
			s.githubAPI(w, r, true)
		}
	case "/api/devices":
		if method("GET") {
			respond(w, 200, s.devicePayload())
		}
	case "/api/metrics":
		if method("GET") {
			respond(w, 200, s.metrics())
		}
	case "/api/logs":
		if method("GET") {
			respond(w, 200, map[string]any{"events": readEvents(), "watch": tailLog("/tmp/darkls-watch.log"), "panel": tailLog("/tmp/darkls-panel.log"), "update": tailLog("/tmp/darkls-update-worker.log")})
		}
	case "/api/password":
		if method("POST") {
			s.changePassword(w, r)
		}
	case "/api/service-probe":
		if method("POST") {
			s.serviceProbe(w, r)
		}
	case "/api/diagnose":
		if method("POST") {
			s.diagnose(w, r)
		}
	case "/api/probe":
		if method("POST") {
			if !s.diagnosticMu.TryLock() {
				fail(w, 409, errors.New("диагностика уже выполняется"))
				return true
			}
			defer s.diagnosticMu.Unlock()
			start := time.Now()
			e := checkVPNConnection()
			recordProbe(e, time.Since(start))
			respond(w, 200, readProbeState())
		}
	case "/api/import-preview":
		if method("POST") {
			var st State
			if e := decode(w, r, &st); e != nil {
				fail(w, 400, e)
				return true
			}
			st, e := validate(st)
			if e != nil {
				fail(w, 400, e)
				return true
			}
			respond(w, 200, map[string]any{"state": st, "groups": len(st.Groups), "domains": len(activeDomains(st))})
		}
	case "/api/update":
		if method("POST") {
			s.uploadUpdate(w, r)
		}
	default:
		return false
	}
	return true
}

func routeBypass(ip string) bool {
	a, e := netip.ParseAddr(ip)
	if e != nil {
		return true
	}
	for _, raw := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4"} {
		if netip.MustParsePrefix(raw).Contains(a) {
			return true
		}
	}
	return false
}

// Read the actual router exclusions; no personal server IP is compiled into
// the public project. Only unconditional destination RETURN rules count.
func natAddressBypass(ip, rules string) bool {
	if routeBypass(ip) {
		return true
	}
	a, e := netip.ParseAddr(ip)
	if e != nil {
		return true
	}
	for _, line := range strings.Split(rules, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "-A" || f[1] != "DV_NAT" || f[2] != "-d" || f[4] != "-j" || f[5] != "RETURN" {
			continue
		}
		p, e := netip.ParsePrefix(f[3])
		if e == nil && p.Contains(a) {
			return true
		}
	}
	return false
}
