package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type firewallRule []string

func groupSet(id string) string {
	h := sha256.Sum256([]byte(id))
	return "dv_g_" + hex.EncodeToString(h[:6])
}
func (g Group) active(now int64) bool {
	if g.EnabledUntil > 0 && now >= g.EnabledUntil {
		return g.TimedPrevious
	}
	return g.Enabled
}
func (g Group) quicBlocked(now int64) bool {
	if g.QUICUntil > 0 && now >= g.QUICUntil {
		return g.QUICPrevious
	}
	if g.BlockQUIC == nil {
		return g.ID == "youtube"
	}
	return *g.BlockQUIC
}
func boolRef(b bool) *bool { return &b }
func dnsConfigForState(st State) []byte {
	data := string(dnsConfig(activeDomains(st)))
	owners := map[string][]string{}
	for _, g := range st.Groups {
		if g.active(time.Now().Unix()) {
			for _, d := range g.Domains {
				owners[d] = append(owners[d], groupSet(g.ID))
			}
		}
	}
	lines := strings.Split(data, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "ipset=/") {
			f := strings.SplitN(l, "/", 3)
			if len(f) == 3 {
				sets := owners[f[1]]
				sort.Strings(sets)
				if len(sets) > 0 {
					lines[i] += "," + strings.Join(sets, ",")
				}
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
func (s *Server) groupRegistry() map[string]string {
	m := map[string]string{}
	_ = readJSON(s.runtimePath("group-registry.json"), &m)
	return m
}
func (s *Server) ensureGroupSets(st State) error {
	reg := s.groupRegistry()
	for _, spec := range [][]string{{"create", "darkls_vpn", "hash:ip", "family", "inet", "timeout", "86400", "-exist"}, {"create", "darkls_yt", "hash:ip", "family", "inet", "timeout", "86400", "-exist"}, {"create", "darkls_tg", "hash:net", "family", "inet", "-exist"}} {
		if out, e := s.run("ipset", spec...); e != nil {
			return fmt.Errorf("IP-набор: %s", limited(out))
		}
	}
	for _, g := range st.Groups {
		name := groupSet(g.ID)
		if out, e := s.run("ipset", "create", name, "hash:ip", "family", "inet", "timeout", "86400", "-exist"); e != nil {
			return fmt.Errorf("IP-набор группы: %s", limited(out))
		}
		reg[g.ID] = name
	}
	return atomicWrite(s.runtimePath("group-registry.json"), jsonBytes(reg))
}

var privateRanges = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}

func (s *Server) exclusions() []string {
	found := map[string]bool{}
	for _, r := range privateRanges {
		found[r] = true
	}
	// Preserve existing endpoint exclusions, including after switching profiles.
	for _, chain := range []string{"DV_NAT", "DV_NA", "DV_NB"} {
		out, _ := s.run("iptables", "-t", "nat", "-S", chain)
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 6 && f[2] == "-d" && f[4] == "-j" && f[5] == "RETURN" {
				if p, e := netip.ParsePrefix(f[3]); e == nil && p.Addr().Is4() {
					found[p.String()] = true
				}
			}
		}
	}
	// Router-originated tunnel sockets are not intercepted; an unresolved endpoint
	// is not a reason to interrupt working LAN connections.
	out := make([]string, 0, len(found))
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
func buildPolicyRules(st State, reg map[string]string, excludes []string) (nat, udp, input []firewallRule) {
	now := time.Now().Unix()
	if st.Policy.Mode != "all" && st.Policy.Mode != "" {
		r := firewallRule{"-m", "set"}
		if st.Policy.Mode == "include" {
			r = append(r, "!")
		}
		r = append(r, "--match-set", "darkls_devices", "src", "-j", "RETURN")
		nat = append(nat, r)
		udp = append(udp, r)
	}
	nat = append(nat, firewallRule{"-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", "10535"}, firewallRule{"-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", "10535"})
	udp = append(udp, firewallRule{"-p", "udp", "--dport", "53", "-j", "RETURN"})
	for _, p := range excludes {
		r := firewallRule{"-d", p, "-j", "RETURN"}
		nat = append(nat, r)
		udp = append(udp, r)
	}
	redirect := func(set string) {
		nat = append(nat, firewallRule{"-p", "tcp", "-m", "set", "--match-set", set, "dst", "-j", "REDIRECT", "--to-ports", "12345"})
		udp = append(udp, firewallRule{"-p", "udp", "-m", "set", "--match-set", set, "dst", "-j", "TPROXY", "--on-ip", "127.0.0.1", "--on-port", "12346", "--tproxy-mark", "0x1000000/0x1000000"})
	}
	active := map[string]bool{}
	for _, g := range st.Groups {
		if !g.active(now) {
			continue
		}
		active[g.ID] = true
		redirect(groupSet(g.ID))
		if g.ID == "telegram" {
			redirect("darkls_tg")
		}
		if g.quicBlocked(now) {
			input = append(input, firewallRule{"-p", "udp", "--dport", "443", "-m", "set", "--match-set", groupSet(g.ID), "dst", "-j", "REJECT", "--reject-with", "icmp-port-unreachable"})
			if g.ID == "youtube" {
				input = append(input, firewallRule{"-p", "udp", "--dport", "443", "-m", "set", "--match-set", "darkls_yt", "dst", "-j", "REJECT", "--reject-with", "icmp-port-unreachable"})
			}
		}
	}
	// Active groups win shared CDN addresses. Inactive/removed groups then bypass
	// the legacy union so a newly enabled timed group can actually expire.
	ids := []string{}
	for id := range reg {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !active[id] {
			r := firewallRule{"-m", "set", "--match-set", reg[id], "dst", "-j", "RETURN"}
			nat = append(nat, r)
			udp = append(udp, r)
		}
	}
	redirect("darkls_vpn")
	input = append(input, firewallRule{"-p", "udp", "-m", "mark", "--mark", "0x1000000/0x1000000", "-j", "ACCEPT"}, firewallRule{"-p", "udp", "--dport", "10535", "-j", "ACCEPT"}, firewallRule{"-p", "tcp", "-m", "multiport", "--dports", "10535,12345", "-j", "ACCEPT"})
	return
}

// Build an unreferenced body chain, then insert its jump before removing old
// rules. No global flush, no Xray restart and no empty live-chain interval.
func (s *Server) installBody(table, top, a, b string, rules []firewallRule) error {
	_, _ = s.run("iptables", "-t", table, "-N", top)
	old, _ := s.run("iptables", "-t", table, "-S", top)
	candidate := a
	if strings.Contains(string(old), "-j "+a) {
		candidate = b
	}
	// A previous interrupted commit may have left the inactive body's jump.
	for i := 0; i < 3; i++ {
		if _, e := s.run("iptables", "-t", table, "-D", top, "-j", candidate); e != nil {
			break
		}
	}
	_, _ = s.run("iptables", "-t", table, "-N", candidate)
	if out, e := s.run("iptables", "-t", table, "-F", candidate); e != nil {
		return fmt.Errorf("подготовка правил: %s", limited(out))
	}
	for _, r := range rules {
		args := append([]string{"-t", table, "-A", candidate}, r...)
		if out, e := s.run("iptables", args...); e != nil {
			return fmt.Errorf("правило %s: %s", top, limited(out))
		}
	}
	if out, e := s.run("iptables", "-t", table, "-I", top, "1", "-j", candidate); e != nil {
		return fmt.Errorf("подключение правил: %s", limited(out))
	}
	// Deletion by index is bounded by the actual rules read before insertion.
	count := 0
	for _, l := range strings.Split(string(old), "\n") {
		if strings.HasPrefix(l, "-A "+top+" ") {
			count++
		}
	}
	for i := 0; i < count; i++ {
		if _, e := s.run("iptables", "-t", table, "-D", top, "2"); e != nil {
			break
		}
	}
	return nil
}
func (s *Server) syncGroupRouting() error {
	if s.disabled() {
		return nil
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	if e = s.ensureGroupSets(st); e != nil {
		return e
	}
	if e = s.syncDevices(true); e != nil {
		return e
	}
	if out, e := s.run("modprobe", "xt_TPROXY"); e != nil {
		return fmt.Errorf("TPROXY: %s", limited(out))
	}
	_, _ = s.run("modprobe", "ipt_REJECT")
	if out, e := s.run("ip", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", "100"); e != nil {
		return fmt.Errorf("маршрут: %s", limited(out))
	}
	if out, _ := s.run("ip", "rule", "show"); !strings.Contains(string(out), "fwmark 0x1000000/0x1000000 lookup 100") {
		if out, e := s.run("ip", "rule", "add", "pref", "10", "fwmark", "0x1000000/0x1000000", "table", "100"); e != nil {
			return fmt.Errorf("правило маршрута: %s", limited(out))
		}
	}
	nat, udp, input := buildPolicyRules(st, s.groupRegistry(), s.exclusions())
	for _, v := range []struct {
		table, top, a, b, hook string
		rules                  []firewallRule
	}{{"nat", "DV_NAT", "DV_NA", "DV_NB", "PREROUTING", nat}, {"mangle", "DV_UDP", "DV_UA", "DV_UB", "PREROUTING", udp}, {"filter", "DV_IN", "DV_IA", "DV_IB", "INPUT", input}} {
		if e = s.installBody(v.table, v.top, v.a, v.b, v.rules); e != nil {
			return e
		}
		args := []string{"-t", v.table, "-C", v.hook, "-i", "br0", "-s", "192.168.50.0/24", "-j", v.top}
		if _, e = s.run("iptables", args...); e != nil {
			args[2] = "-I"
			args = append(args[:4], append([]string{"1"}, args[4:]...)...)
			if out, e := s.run("iptables", args...); e != nil {
				return fmt.Errorf("LAN-hook: %s", limited(out))
			}
		}
	}
	return nil
}
func (s *Server) expandedChain(table, top string) string {
	out, _ := s.run("iptables", "-t", table, "-S", top)
	text := string(out)
	for _, name := range []string{"DV_NA", "DV_NB", "DV_UA", "DV_UB", "DV_IA", "DV_IB"} {
		if strings.Contains(text, "-j "+name) {
			b, _ := s.run("iptables", "-t", table, "-S", name)
			text += "\n" + string(b)
		}
	}
	return text
}
func (s *Server) upgradeGroupPolicy() error {
	marker := filepath.Join(s.dir, "policy-v130.ready")
	if _, e := os.Stat(marker); e == nil {
		return nil
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	if _, e = s.apply(st, s.revision()); e != nil {
		return e
	}
	return atomicWrite(marker, []byte("ready\n"))
}

// Labels and never-active disabled groups do not mutate live rules.
func policyChanged(a, b State) bool {
	if string(jsonBytes(a.Policy)) != string(jsonBytes(b.Policy)) {
		return true
	}
	signature := func(st State) string {
		m := map[string]any{}
		for _, g := range st.Groups {
			if g.Enabled {
				m[g.ID] = struct {
					Domains []string
					QUIC    bool
				}{g.Domains, (g.BlockQUIC == nil && g.ID == "youtube") || (g.BlockQUIC != nil && *g.BlockQUIC)}
			}
		}
		return string(jsonBytes(m))
	}
	return signature(a) != signature(b)
}
