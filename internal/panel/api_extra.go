package panel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика уже выполняется"))
		return
	}
	defer s.diagnosticMu.Unlock()
	var req struct {
		Domain    string `json:"domain"`
		DeviceMAC string `json:"device_mac"`
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
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	selected := LANDevice{IP: client}
	if req.DeviceMAC != "" {
		mac, err := normalizeMAC(req.DeviceMAC)
		if err != nil {
			fail(w, 400, err)
			return
		}
		selected = LANDevice{}
		for _, d := range s.lanDevices() {
			if d.MAC == mac && validLANIP(d.IP) {
				selected = d
				break
			}
		}
		if selected.IP == "" {
			fail(w, 400, errors.New("устройство не найдено с действующим LAN-адресом; обновите список"))
			return
		}
		client = selected.IP
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
				matches = append(matches, map[string]any{"name": g.Name, "enabled": g.active(time.Now().Unix()), "category": g.Category})
				break
			}
		}
	}
	eligible := true
	if st.Policy.Mode != "all" {
		_, e := s.runner.Run(ctx, "ipset", "test", "darkls_devices", client)
		eligible = e == nil
		if st.Policy.Mode == "exclude" {
			eligible = !eligible
		}
	}
	_, hookErr := s.run("iptables", "-t", "nat", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_NAT")
	natRules := []byte(s.expandedChain("nat", "DV_NAT"))
	routesActive := hookErr == nil
	rows := []map[string]any{}
	reg := s.groupRegistry()
	for _, ip := range ips {
		if len(rows) == 16 {
			break
		}
		cache := map[string]bool{}
		contains := func(set string) bool {
			if v, ok := cache[set]; ok {
				return v
			}
			_, err := s.runner.Run(ctx, "ipset", "test", set, ip.String())
			cache[set] = err == nil
			return err == nil
		}
		matched, reason := routeGroupDecision(st, reg, contains)
		bypass := natAddressBypass(ip.String(), string(natRules))
		if bypass {
			reason = "Адрес исключён из перехвата"
		}
		if !eligible {
			reason = "Устройство исключено режимом устройств"
		}
		if !routesActive {
			reason = "Цепочка маршрутов не подключена"
		}
		if s.disabled() {
			reason = "VPN выключен"
		}
		rows = append(rows, map[string]any{"ip": ip.String(), "in_vpn_set": matched,
			"via_vpn": matched && eligible && routesActive && !bypass && !s.disabled(), "bypass": bypass, "reason": reason})
	}
	if ctx.Err() != nil {
		fail(w, 504, errors.New("проверка маршрутов превысила 10 секунд; результат неполный"))
		return
	}
	expected := eligible
	if selected.MAC != "" {
		expected = devicePolicyEligible(st.Policy, selected.MAC)
	}
	appendEvent("diagnostic", "Проверен домен "+domain)
	respond(w, 200, map[string]any{"device": selected, "configured_eligible": expected, "policy_mismatch": expected != eligible, "domain": domain, "groups": matches, "addresses": rows, "client_ip": client, "device_eligible": eligible, "policy_mode": st.Policy.Mode, "disabled": s.disabled(), "routes_active": routesActive})
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

// Mirrors buildPolicyRules: active groups win shared CDN IPs, inactive sets
// bypass the legacy union. This is a rule prediction, not traffic from the device.
func routeGroupDecision(st State, reg map[string]string, contains func(string) bool) (bool, string) {
	active := map[string]bool{}
	now := time.Now().Unix()
	for _, g := range st.Groups {
		if !g.active(now) {
			continue
		}
		active[g.ID] = true
		if contains(groupSet(g.ID)) || (g.ID == "telegram" && contains("darkls_tg")) {
			return true, "Активная группа: " + g.Name
		}
	}
	for id, set := range reg {
		if !active[id] && contains(set) {
			return false, "Адрес выключенной или удалённой группы"
		}
	}
	if contains("darkls_vpn") {
		return true, "Адрес сохранён в общем VPN-наборе"
	}
	return false, "Адрес отсутствует в активных VPN-наборах"
}
func devicePolicyEligible(p DevicePolicy, mac string) bool {
	member := false
	for _, d := range p.Devices {
		if d.MAC == mac {
			member = true
			break
		}
	}
	if p.Mode == "include" {
		return member
	}
	if p.Mode == "exclude" {
		return !member
	}
	return true
}
