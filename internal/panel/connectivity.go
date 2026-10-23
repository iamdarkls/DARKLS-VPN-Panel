package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Connectivity struct {
	At             int64             `json:"at"`
	Xray           bool              `json:"xray"`
	DNSProcess     bool              `json:"dns_process"`
	LocalDNS       bool              `json:"local_dns"`
	TunnelDNS      bool              `json:"tunnel_dns"`
	Rules          bool              `json:"rules"`
	WAN            bool              `json:"direct_internet"`
	ServerTCP      bool              `json:"server_tcp"`
	TunnelHTTPS    bool              `json:"tunnel_https"`
	Clock          bool              `json:"clock"`
	TrafficChecked bool              `json:"traffic_checked"`
	Errors         map[string]string `json:"errors"`
	Actions        []string          `json:"actions,omitempty"`
}

func (s *Server) mainXrayPIDs() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.mainXrayPIDsContext(ctx)
}

// Candidate Xray processes used by diagnostics must not count as the live VPN.
func (s *Server) mainXrayPIDsContext(ctx context.Context) []string {
	b, _ := s.runner.Run(ctx, "pidof", "xray")
	out := []string{}
	for _, p := range strings.Fields(string(b)) {
		if _, e := strconv.Atoi(p); e != nil {
			continue
		}
		cmd, _ := os.ReadFile(filepath.Join("/proc", p, "cmdline"))
		if strings.Contains(string(cmd), filepath.Join(s.root, "config-vpn.json")) {
			out = append(out, p)
		}
	}
	return out
}
func (s *Server) startMainXray() error {
	cfg := filepath.Join(s.root, "config-vpn.json")
	bin := filepath.Join(s.root, "xray")
	if out, e := s.run(bin, "run", "-test", "-config", cfg); e != nil {
		return fmt.Errorf("конфиг Xray не прошёл проверку: %s", s.scrub(string(out)))
	}
	if len(s.mainXrayPIDs()) > 0 {
		return nil
	}
	logFile, e := os.OpenFile("/tmp/xray.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer logFile.Close()
	cmd := exec.Command(bin, "run", "-config", cfg)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	time.Sleep(300 * time.Millisecond)
	if len(s.mainXrayPIDs()) == 0 {
		return errors.New("Xray не запустился; см. журнал")
	}
	return nil
}
func (s *Server) restartMainXray() error {
	for _, p := range s.mainXrayPIDs() {
		_, _ = s.run("kill", p)
	}
	for i := 0; i < 20 && len(s.mainXrayPIDs()) > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if len(s.mainXrayPIDs()) > 0 {
		return errors.New("Xray не остановился")
	}
	return s.startMainXray()
}
func transportClient(proxyAddress string) *http.Client {
	roots := sharedRoots()
	var proxy *url.URL
	if proxyAddress != "" {
		proxy, _ = url.Parse("socks5h://" + proxyAddress)
	}
	client, _ := newProbeClient(proxy, roots)
	client.Timeout = 6 * time.Second
	return client
}
func checkDirectInternet(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://1.1.1.1/cdn-cgi/trace", nil)
	c := transportClient("")
	defer c.CloseIdleConnections()
	resp, e := c.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func testDNS(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := lookupRouterDNS(ctx, address, token()[:12]+".example.com")
	if de, ok := e.(*net.DNSError); ok && de.IsNotFound {
		return nil
	}
	return e
}
func (s *Server) serverTCP() error {
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	out, e := vpnOutbound(cfg)
	if e != nil {
		return e
	}
	host, port := outboundEndpoint(out)
	if host == "" || port == 0 {
		return errors.New("адрес сервера не распознан")
	}
	c, e := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
	if e == nil {
		c.Close()
	}
	return e
}
func (s *Server) collectConnectivity(full bool) Connectivity {
	c := Connectivity{At: time.Now().Unix(), Clock: time.Now().Year() >= 2025, Errors: map[string]string{}}
	c.Xray = len(s.mainXrayPIDs()) > 0
	c.DNSProcess = pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf")
	if e := testDNS("127.0.0.1:10535"); e == nil {
		c.LocalDNS = true
	} else {
		c.Errors["local_dns"] = s.scrub(e.Error())
	}
	if e := testDNS("127.0.0.1:1053"); e == nil {
		c.TunnelDNS = true
	} else {
		c.Errors["tunnel_dns"] = s.scrub(e.Error())
	}
	if e := s.checkRoutingOnly(); e == nil {
		c.Rules = true
	} else {
		c.Errors["rules"] = s.scrub(e.Error())
	}
	if full && c.Clock {
		c.TrafficChecked = true
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		if e := checkDirectInternet(ctx); e == nil {
			c.WAN = true
		} else {
			c.Errors["direct_internet"] = s.scrub(e.Error())
		}
		cancel()
		if e := s.serverTCP(); e == nil {
			c.ServerTCP = true
		} else {
			c.Errors["server_tcp"] = s.scrub(e.Error())
		}
		if e := checkVPNConnection(); e == nil {
			c.TunnelHTTPS = true
		} else {
			c.Errors["tunnel_https"] = s.scrub(e.Error())
		}
	}
	return c
}
func (s *Server) saveConnectivity(c Connectivity) {
	_ = atomicWrite(s.runtimePath("connectivity.json"), jsonBytes(c))
	appendEvent("connectivity", fmt.Sprintf("Xray=%t DNS=%t правила=%t прямой HTTPS=%t сервер TCP=%t VPN HTTPS=%t", c.Xray, c.LocalDNS, c.Rules, c.WAN, c.ServerTCP, c.TunnelHTTPS))
}
func (s *Server) recoverComponents(reason string) error {
	if s.disabled() {
		return nil
	}
	c := Connectivity{At: time.Now().Unix(), Clock: time.Now().Year() >= 2025, Errors: map[string]string{}}
	defer func() { s.saveConnectivity(c) }()
	// Structural repairs do not depend on WAN or clock readiness at boot.
	if len(s.mainXrayPIDs()) == 0 {
		s.captureRecovery(c, "Процесс Xray отсутствует: "+reason)
		if e := s.startMainXray(); e != nil {
			c.Errors["xray"] = s.scrub(e.Error())
			return e
		}
		c.Actions = append(c.Actions, "Запущен Xray")
	}
	c.Xray = true
	if !pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf") || testDNS("127.0.0.1:10535") != nil {
		s.captureRecovery(c, "Восстановление DNS helper: "+reason)
		if out, e := s.run("sh", filepath.Join(s.dir, "reload-dns.sh")); e != nil {
			c.Errors["local_dns"] = s.scrub(string(out))
			return errors.New("не удалось восстановить DNS helper")
		}
		c.Actions = append(c.Actions, "Перезапущен только DNS helper")
	}
	c.DNSProcess = true
	c.LocalDNS = true
	if e := s.checkRoutingOnly(); e != nil {
		s.captureRecovery(c, "Восстановление правил: "+reason)
		if e = s.syncGroupRouting(); e != nil {
			c.Errors["rules"] = s.scrub(e.Error())
			return e
		}
		c.Actions = append(c.Actions, "Восстановлены только правила")
	}
	c.Rules = true
	if len(c.Actions) > 0 {
		recordRecovery(strings.Join(c.Actions, "; "))
		return nil
	}
	if !c.Clock {
		c.Errors["clock"] = "ожидается синхронизация часов"
		return nil
	}
	// A site-specific error never calls this path. Recheck the whole tunnel.
	c = s.collectConnectivity(true)
	if c.TunnelHTTPS {
		if shouldRestartTunnel(c) {
			// An upstream DNS delay is not proof that Xray is stuck. Give DNS
			// two fresh attempts, then recheck HTTPS before interrupting clients.
			confirmed, failures := confirmTunnelDNSFailure(func() error {
				return testDNS("127.0.0.1:1053")
			}, checkVPNConnection, func() { time.Sleep(2 * time.Second) })
			if !confirmed {
				if len(failures) < 2 {
					c.TunnelDNS = true
					delete(c.Errors, "tunnel_dns")
					appendEvent("dns_retry", "DNS через Xray ответил при повторной проверке; перезапуск отменён")
				} else {
					c.TunnelHTTPS = false
					c.Errors["tunnel_https"] = "Повторная HTTPS-проверка тоже не прошла"
					appendEvent("dns_retry", "Повторная HTTPS-проверка не прошла; отдельное восстановление DNS отложено до проверки туннеля")
				}
				return nil
			}
			c.Errors["tunnel_dns_confirmation"] = s.scrub(strings.Join(failures, "; "))
			s.captureRecovery(c, "Подтверждён повторный отказ DNS через Xray при рабочем HTTPS")
			if e := s.restartMainXray(); e != nil {
				return e
			}
			c.Actions = append(c.Actions, "Перезапущен Xray после повторного отказа DNS через туннель")
			recordRecovery("Повторно не отвечал DNS через Xray при рабочем HTTPS; Xray перезапущен")
		}
		return nil
	}
	if !c.WAN {
		appendEvent("wan_unconfirmed", "Прямая HTTPS-проверка не прошла; Xray не перезапускаем вслепую")
		return nil
	}
	prefs := s.preferences()
	s.captureRecovery(c, reason)
	if prefs.AutoFailover && s.backupConfigured() {
		if e := s.failover(); e == nil {
			c.Actions = append(c.Actions, "Переключено на проверенный резерв")
			recordRecovery("Переключение на резерв")
			return nil
		} else {
			c.Errors["backup"] = s.scrub(e.Error())
		}
	}
	if !shouldRestartTunnel(c) {
		appendEvent("server_unreachable", "TCP сервера недоступен; ожидание восстановления или резервного канала")
		return nil
	}
	if e := s.restartMainXray(); e != nil {
		return e
	}
	c.Actions = append(c.Actions, "Перезапущен только Xray")
	recordRecovery("Туннель не отвечал при доступном интернете и сервере: " + reason)
	return nil
}
func canonicalRule(line string) string {
	f := strings.Fields(line)
	if len(f) > 2 && f[0] == "-A" {
		f = f[2:]
	}
	out := []string{}
	for i := 0; i < len(f); i++ {
		if f[i] == "-m" && i+1 < len(f) && (f[i+1] == "tcp" || f[i+1] == "udp") {
			i++
			continue
		}
		out = append(out, f[i])
	}
	sortStrings(out)
	return strings.Join(out, " ")
}
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
func (s *Server) checkRoutingOnly() error {
	for _, v := range []struct{ table, hook, chain string }{{"nat", "PREROUTING", "DV_NAT"}, {"mangle", "PREROUTING", "DV_UDP"}, {"filter", "INPUT", "DV_IN"}} {
		if _, e := s.run("iptables", "-t", v.table, "-C", v.hook, "-i", "br0", "-s", "192.168.50.0/24", "-j", v.chain); e != nil {
			return fmt.Errorf("отсутствует подключение %s", v.chain)
		}
	}
	rules, _ := s.run("ip", "rule", "show")
	if !strings.Contains(string(rules), "fwmark 0x1000000/0x1000000 lookup 100") {
		return errors.New("нет policy rule")
	}
	route, _ := s.run("ip", "route", "show", "table", "100")
	if !strings.Contains(string(route), "local default") || !strings.Contains(string(route), "dev lo") {
		return errors.New("нет local route")
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	nat, udp, input := buildPolicyRules(st, s.groupRegistry(), s.exclusions())
	for _, v := range []struct {
		table, top string
		want       []firewallRule
	}{{"nat", "DV_NAT", nat}, {"mangle", "DV_UDP", udp}, {"filter", "DV_IN", input}} {

		listing, _ := s.run("iptables", "-t", v.table, "-S", v.top)
		body := ""
		topRules := []string{}
		allowed := map[string]bool{"DV_NA": true, "DV_NB": true, "DV_UA": true, "DV_UB": true, "DV_IA": true, "DV_IB": true}
		for _, line := range strings.Split(string(listing), "\n") {
			if !strings.HasPrefix(line, "-A "+v.top+" ") {
				continue
			}
			topRules = append(topRules, line)
			f := strings.Fields(line)
			if len(f) == 4 && f[2] == "-j" && allowed[f[3]] {
				body = f[3]
			}
		}
		if body == "" || len(topRules) < 1 || len(topRules) > 2 {
			return fmt.Errorf("нет активной цепочки группы %s", v.top)
		}
		if canonicalRule(topRules[len(topRules)-1]) != canonicalRule("-j "+body) {
			return fmt.Errorf("неверный порядок входа %s", v.top)
		}
		if len(topRules) == 2 {
			if v.top == "DV_IN" || st.Policy.Mode == "all" || canonicalRule(topRules[0]) != canonicalRule(strings.Join(v.want[0], " ")) {
				return fmt.Errorf("лишнее правило входа %s", v.top)
			}
		}
		listing, _ = s.run("iptables", "-t", v.table, "-S", body)
		if !orderedPolicyMatches(string(listing), body, v.want) {
			return fmt.Errorf("неполные правила или неверный порядок %s", v.top)
		}

	}
	return nil
}

// Reports never include raw config; this helper replaces endpoint names and
// opaque authentication material from logs, including user-defined variants.
func (s *Server) scrub(text string) string {
	text = secretURL.ReplaceAllString(text, "[VPN URI скрыт]")
	text = secretUUID.ReplaceAllString(text, "[UUID скрыт]")
	for _, name := range []string{"config-vpn.json", "panel/profiles/primary.json", "panel/profiles/backup.json", "panel/connections.json", "panel/previous-connection.json", "panel/profile-switch-journal.json"} {
		b, e := os.ReadFile(filepath.Join(s.root, name))
		if e != nil {
			continue
		}
		var value any
		if json.Unmarshal(b, &value) != nil {
			continue
		}
		var walk func(any, string)
		walk = func(v any, k string) {
			switch x := v.(type) {
			case map[string]any:
				for key, val := range x {
					walk(val, key)
				}
			case []any:
				for _, a := range x {
					walk(a, k)
				}
			case string:
				k = strings.ToLower(k)
				if len(x) >= 4 && (k == "id" || k == "password" || k == "publickey" || k == "privatekey" || k == "shortid" || k == "address" || k == "servername" || k == "email" || k == "path" || k == "servicename" || k == "spiderx" || k == "auth" || k == "token" || k == "secret" || k == "username" || k == "authorization" || k == "cookie" || k == "apikey" || k == "host" || k == "encryption") {
					text = strings.ReplaceAll(text, x, "[скрыто]")
					encoded, _ := json.Marshal(x)
					if len(encoded) > 2 {
						text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[скрыто]")
					}
				}
			}
		}
		walk(value, "")
	}
	return text
}

func orderedPolicyMatches(listing, chain string, want []firewallRule) bool {
	actual := []string{}
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, "-A "+chain+" ") {
			actual = append(actual, canonicalRule(line))
		}
	}
	if len(actual) != len(want) {
		return false
	}
	for i, r := range want {
		if actual[i] != canonicalRule(strings.Join(r, " ")) {
			return false
		}
	}
	return true
}

func shouldRestartTunnel(c Connectivity) bool {
	return c.Clock && c.WAN && c.ServerTCP && (!c.TunnelHTTPS || !c.TunnelDNS)
}
