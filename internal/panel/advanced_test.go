package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGroupRoutingKeepsSharedActiveBeforeDisabled(t *testing.T) {
	now := time.Now().Unix()
	st := State{Policy: DevicePolicy{Mode: "include"}, Groups: []Group{{ID: "youtube", Enabled: true, Domains: []string{"googlevideo.com"}}, {ID: "other", Enabled: false, Domains: []string{"shared.example"}}}}
	reg := map[string]string{"youtube": groupSet("youtube"), "other": groupSet("other")}
	nat, udp, input := buildPolicyRules(st, reg, privateRanges)
	flat := func(r []firewallRule) string {
		lines := []string{}
		for _, x := range r {
			lines = append(lines, strings.Join(x, " "))
		}
		return strings.Join(lines, "\n")
	}
	for _, rules := range []string{flat(nat), flat(udp)} {
		active := strings.Index(rules, groupSet("youtube")+" dst -j ")
		off := strings.Index(rules, groupSet("other")+" dst -j RETURN")
		union := strings.LastIndex(rules, "darkls_vpn dst")
		if active < 0 || off < active || union < off {
			t.Fatal("shared CDN and disabled group precedence lost", rules)
		}
		if !strings.HasPrefix(rules, "-m set ! --match-set darkls_devices src -j RETURN") {
			t.Fatal("device guard must be first")
		}
	}
	if !strings.Contains(flat(input), groupSet("youtube")+" dst -j REJECT") {
		t.Fatal("YouTube default QUIC bypass missing")
	}
	if st.Groups[1].quicBlocked(now) {
		t.Fatal("QUIC blocked by default outside YouTube")
	}
	dns := string(dnsConfigForState(st))
	if !strings.Contains(dns, "darkls_vpn,darkls_yt,"+groupSet("youtube")) || strings.Contains(dns, groupSet("other")) {
		t.Fatal("DNS group ownership", dns)
	}
}
func TestExpiryRestoresPreviousAndChangesRuntimePolicy(t *testing.T) {
	now := time.Now().Unix()
	old := State{Groups: []Group{{ID: "one", Enabled: true, EnabledUntil: now - 1, TimedPrevious: false, BlockQUIC: boolRef(true), Domains: []string{"one.example"}}, {ID: "two", Enabled: true, BlockQUIC: boolRef(false), QUICUntil: now - 1, QUICPrevious: true}}}
	before, _ := json.Marshal(old)
	var copy State
	_ = json.Unmarshal(before, &copy)
	next, changed := expireState(copy, now)
	if !changed || next.Groups[0].Enabled || !next.Groups[1].quicBlocked(now) || next.Groups[1].QUICUntil != 0 {
		t.Fatal("expired choice not restored")
	}
	if !policyChanged(old, next) {
		t.Fatal("expiry must rebuild actual stored policy even though logical time has passed")
	}
	if _, changed = expireState(next, now); changed {
		t.Fatal("expiry not idempotent")
	}
}
func TestPreviewDoesNotWriteOrRunRouterCommands(t *testing.T) {
	s, runner := fixture(t)
	old, _ := s.state()
	next := cloneState(t, old)
	next.Groups[0].Enabled = false
	before := s.revision()
	data := previewChanges(old, next)
	if len(data["removed_domains"].([]string)) == 0 || data["xray_restart"] != false {
		t.Fatal(data)
	}
	if before != s.revision() || len(runner.calls) != 0 {
		t.Fatal("preview mutated runtime")
	}
}
func cloneState(t *testing.T, st State) State {
	t.Helper()
	var out State
	if e := json.Unmarshal(jsonBytes(st), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestReportScrubHidesConfiguredSecretsIncludingEscapes(t *testing.T) {
	s, _ := fixture(t)
	secret := "opaque-key_123"
	escaped := "secret\"with\\slashes"
	cfg := map[string]any{"outbounds": []any{map[string]any{"tag": "vpn", "settings": map[string]any{"password": secret, "address": "private-endpoint.example", "path": escaped}}}}
	if e := atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	raw := string(jsonBytes(map[string]string{"log": secret + " private-endpoint.example " + escaped + " vless://private-link"}))
	result := s.scrub(raw)
	if strings.Contains(result, secret) || strings.Contains(result, "private-endpoint.example") || strings.Contains(result, "secret\\\"with") {
		t.Fatal("secret leaked", result)
	}
	if !json.Valid([]byte(result)) {
		t.Fatal("sanitization broke JSON", result)
	}
}
func TestStorageRejectsTraversalSymlinkAndPendingJournal(t *testing.T) {
	s, _ := fixture(t)
	victim := filepath.Join(s.dir, "auth.json")
	b, _ := os.ReadFile(victim)
	if e := s.deleteStorage("settings:../../auth.json"); e == nil {
		t.Fatal("traversal accepted")
	}
	if e := os.Symlink(victim, filepath.Join(s.dir, "darkls-panel.previous")); e != nil {
		t.Fatal(e)
	}
	for _, x := range s.storageItems() {
		if x.ID == "panel-previous" {
			t.Fatal("symlink advertised")
		}
	}
	os.Remove(filepath.Join(s.dir, "darkls-panel.previous"))
	atomicWrite(filepath.Join(s.dir, "darkls-panel.previous"), []byte("old"))
	atomicWrite(filepath.Join(s.dir, "profile-switch-journal.json"), []byte("pending"))
	if e := s.deleteStorage("panel-previous"); e == nil {
		t.Fatal("deletion during pending transaction")
	}
	after, _ := os.ReadFile(victim)
	if string(b) != string(after) {
		t.Fatal("credentials changed")
	}
}
func TestBackupParserAndTuningPreserveIdentity(t *testing.T) {
	out, e := parseBackup("vless://00000000-0000-4000-8000-000000000000@backup.example:443?type=xhttp&security=reality&sni=cover.example&pbk=public-example&sid=aa&path=%2Fkeep&mode=packet-up&extra=%7B%22xmux%22%3A%7B%22maxConnections%22%3A%223%22%7D%7D")
	if e != nil {
		t.Fatal(e)
	}
	cfg := map[string]any{"outbounds": []any{out}}
	host, port := outboundEndpoint(out)
	if host != "backup.example" || port != 443 {
		t.Fatal(host, port)
	}
	before := cloneConfig(out)
	if e = setXMUX(cfg, "reuse4"); e != nil {
		t.Fatal(e)
	}
	extra, _ := muxSettings(cfg)
	if extra["xmux"].(map[string]any)["maxConnections"] != "4" {
		t.Fatal("preset not applied")
	}
	oldStream := before["streamSettings"].(map[string]any)
	newStream := out["streamSettings"].(map[string]any)
	if string(jsonBytes(oldStream["realitySettings"])) != string(jsonBytes(newStream["realitySettings"])) || oldStream["xhttpSettings"].(map[string]any)["path"] != newStream["xhttpSettings"].(map[string]any)["path"] {
		t.Fatal("tuning replaced path or keys")
	}
	if e = setXMUX(cfg, "unknown"); e == nil {
		t.Fatal("unknown tuning accepted")
	}
	for _, raw := range []string{"vless://id@example.com:0", "{}", "[]", "vless://example.com:443"} {
		if _, e := parseBackup(raw); e == nil {
			t.Fatal("invalid backup accepted", raw)
		}
	}
}
func TestCandidateDoesNotUseOccupiedPort(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	s, runner := fixture(t)
	e = s.candidate(context.Background(), map[string]any{}, port, nil)
	if e == nil || !strings.Contains(e.Error(), "занят") || len(runner.calls) > 0 {
		t.Fatal("occupied port could probe unrelated process", e)
	}
}
func TestHistoryIsBoundedAndKeepsOutageDuration(t *testing.T) {
	s, _ := fixture(t)
	start := time.Now().Unix() - 125
	atomicWrite(s.runtimePath("service-current.json"), jsonBytes(map[string]ServiceHistory{"chatgpt": {Service: "chatgpt", State: "timeout", Since: start}}))
	s.recordService("chatgpt", ServiceResult{State: "responded", DurationMS: 11})
	data := s.serviceHistory()
	events := data["events"].([]ServiceHistory)
	if len(events) != 1 || events[0].Duration < 125 || events[0].Previous != "timeout" {
		t.Fatal(events)
	}
	p := s.runtimePath("service-history.jsonl")
	atomicWrite(p, []byte(strings.Repeat("{}\n", 100000)))
	s.recordService("chatgpt", ServiceResult{State: "challenge"})
	st, _ := os.Stat(p)
	if st.Size() > 257<<10 {
		t.Fatal("history unbounded", st.Size())
	}
}

// Stateful iptables emulator verifies the swap itself, not just command strings.
type tableRunner struct {
	chains   map[string][][]string
	failBody bool
}

func (r *tableRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "iptables" || len(args) < 4 {
		return nil, errors.New("unexpected command")
	}
	table, op, chain := args[1], args[2], args[3]
	key := table + ":" + chain
	rules, exists := r.chains[key]
	rest := args[4:]
	bad := errors.New("missing rule")
	switch op {
	case "-N":
		if exists {
			return nil, bad
		}
		r.chains[key] = nil
	case "-S":
		if !exists {
			return nil, bad
		}
		lines := []string{"-N " + chain}
		for _, rule := range rules {
			lines = append(lines, "-A "+chain+" "+strings.Join(rule, " "))
		}
		return []byte(strings.Join(lines, "\n")), nil
	case "-F":
		if !exists {
			return nil, bad
		}
		r.chains[key] = nil
	case "-A":
		if r.failBody && strings.HasSuffix(chain, "B") {
			return nil, errors.New("simulated allocation failure")
		}
		r.chains[key] = append(rules, append([]string{}, rest...))
	case "-I":
		i, e := strconv.Atoi(rest[0])
		if e != nil || i < 1 || i > len(rules)+1 {
			return nil, bad
		}
		rule := append([]string{}, rest[1:]...)
		rules = append(rules, nil)
		copy(rules[i:], rules[i-1:])
		rules[i-1] = rule
		r.chains[key] = rules
	case "-D":
		if len(rest) == 1 {
			i, _ := strconv.Atoi(rest[0])
			if i < 1 || i > len(rules) {
				return nil, bad
			}
			r.chains[key] = append(rules[:i-1], rules[i:]...)
			return nil, nil
		}
		for i, v := range rules {
			if strings.Join(v, " ") == strings.Join(rest, " ") {
				r.chains[key] = append(rules[:i], rules[i+1:]...)
				return nil, nil
			}
		}
		return nil, bad
	default:
		return nil, fmt.Errorf("unexpected %s", op)
	}
	return nil, nil
}
func TestRuleSwapFailureKeepsOldPolicyThenCommitsNew(t *testing.T) {
	r := &tableRunner{chains: map[string][][]string{"nat:DV_NAT": {{"-j", "DV_NA"}}, "nat:DV_NA": {{"-p", "tcp", "-j", "RETURN"}}}, failBody: true}
	s := &Server{runner: r}
	newRules := []firewallRule{{"-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", "10535"}}
	if e := s.installBody("nat", "DV_NAT", "DV_NA", "DV_NB", newRules); e == nil {
		t.Fatal("failed build accepted")
	}
	if got := strings.Join(r.chains["nat:DV_NAT"][0], " "); got != "-j DV_NA" {
		t.Fatal("old live chain lost", got)
	}
	if len(r.chains["nat:DV_NA"]) != 1 {
		t.Fatal("old body flushed")
	}
	r.failBody = false
	if e := s.installBody("nat", "DV_NAT", "DV_NA", "DV_NB", newRules); e != nil {
		t.Fatal(e)
	}
	if len(r.chains["nat:DV_NAT"]) != 1 || strings.Join(r.chains["nat:DV_NAT"][0], " ") != "-j DV_NB" {
		t.Fatal("new body not committed exactly once", r.chains)
	}
}

func TestProfileJournalRestoresIdentityAndTuningAfterInterruptedCommit(t *testing.T) {
	s, _ := fixture(t)
	atomicWrite(filepath.Join(s.root, "autostart.disabled"), nil)
	os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700)
	old := map[string]any{"outbounds": []any{map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"address": "old.example", "port": 443}}}}
	current := map[string]any{"outbounds": []any{map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"address": "new.example", "port": 443}}}}
	oldMeta := jsonBytes(map[string]any{"name": "primary", "at": 1})
	atomicWrite(filepath.Join(s.dir, "active-profile.json"), oldMeta)
	out, _ := vpnOutbound(old)
	atomicWrite(s.profilePath("primary"), jsonBytes(out))
	if e := s.writeProfileJournal(jsonBytes(old)); e != nil {
		t.Fatal(e)
	}
	atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(current))
	atomicWrite(filepath.Join(s.dir, "active-profile.json"), jsonBytes(map[string]any{"name": "backup", "at": 2}))
	out, _ = vpnOutbound(current)
	atomicWrite(s.profilePath("primary"), jsonBytes(out))
	if e := s.recoverProfileSwitch(); e != nil {
		t.Fatal(e)
	}
	cfg, _ := s.readVPNConfig()
	out, _ = vpnOutbound(cfg)
	host, _ := outboundEndpoint(out)
	if host != "old.example" || s.profileStatus()["active"] != "primary" {
		t.Fatal("interrupted commit did not restore identity", cfg, s.profileStatus())
	}
	var restored map[string]any
	readJSON(s.profilePath("primary"), &restored)
	host, _ = outboundEndpoint(restored)
	if host != "old.example" {
		t.Fatal("persisted tuning profile not rolled back")
	}
}
func TestScrubKeepsLargeReportComplete(t *testing.T) {
	s, _ := fixture(t)
	text := string(jsonBytes(map[string]string{"large": strings.Repeat("x", 12000)}))
	if got := s.scrub(text); !json.Valid([]byte(got)) || len(got) < 12000 {
		t.Fatal("report truncated")
	}
}
func TestDeepMonitorDetectsOrderingNotJustRulePresence(t *testing.T) {
	want := []firewallRule{{"-p", "udp", "--dport", "53", "-j", "RETURN"}, {"-p", "udp", "-j", "TPROXY", "--on-port", "12346"}}
	good := "-N DV_UA\n-A DV_UA -p udp -m udp --dport 53 -j RETURN\n-A DV_UA -p udp -j TPROXY --on-port 12346\n"
	bad := "-A DV_UA -p udp -j TPROXY --on-port 12346\n-A DV_UA -p udp --dport 53 -j RETURN\n"
	if !orderedPolicyMatches(good, "DV_UA", want) || orderedPolicyMatches(bad, "DV_UA", want) {
		t.Fatal("rule order not verified")
	}
}
func TestRecoveryDoesNotRestartWorkingTunnelOrUnconfirmedWAN(t *testing.T) {
	good := Connectivity{Clock: true, WAN: true, ServerTCP: true, TunnelHTTPS: true, TunnelDNS: true}
	tests := []struct {
		name string
		c    Connectivity
		want bool
	}{{"healthy", good, false}, {"tunnel failed", Connectivity{Clock: true, WAN: true, ServerTCP: true, TunnelDNS: true}, true}, {"only DNS inlet failed", Connectivity{Clock: true, WAN: true, ServerTCP: true, TunnelHTTPS: true}, true}, {"WAN unconfirmed", Connectivity{Clock: true, ServerTCP: true}, false}, {"server unreachable", Connectivity{Clock: true, WAN: true}, false}, {"clock not ready", Connectivity{WAN: true, ServerTCP: true}, false}}
	for _, x := range tests {
		t.Run(x.name, func(t *testing.T) {
			if got := shouldRestartTunnel(x.c); got != x.want {
				t.Fatal(got, x.want)
			}
		})
	}
}
