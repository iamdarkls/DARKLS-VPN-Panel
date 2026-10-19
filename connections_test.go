package main

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exampleOutbound(t *testing.T, address string) map[string]any {
	t.Helper()
	out, e := parseBackup("vless://00000000-0000-4000-8000-000000000001@" + address + ":443?type=xhttp&security=reality&pbk=test-public-key&sid=abcd&sni=cover.example&path=%2Ftest&mode=packet-up&extra=" + url.QueryEscape(`{"uplinkHTTPMethod":"GET","xmux":{"maxConnections":"3"},"custom":true}`))
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func configFixture(t *testing.T, s *Server) map[string]any {
	cfg := map[string]any{"inbounds": []any{map[string]any{"tag": "keep-inbound"}}, "routing": map[string]any{"domainStrategy": "AsIs"}, "outbounds": []any{exampleOutbound(t, "old.example"), map[string]any{"tag": "direct", "protocol": "freedom"}}}
	if e := atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	return cfg
}
func TestVLESSPreservesExtraAndRejectsAmbiguity(t *testing.T) {
	out := exampleOutbound(t, "new.example")
	stream := out["streamSettings"].(map[string]any)
	extra := stream["xhttpSettings"].(map[string]any)["extra"].(map[string]any)
	if extra["uplinkHTTPMethod"] != "GET" || extra["custom"] != true {
		t.Fatal(extra)
	}
	for _, q := range []string{"type=xhttp&type=tcp", "unknown=secret", "type=xhttp&extra=null", "type=xhttp&extra=%5B%5D", "security=unknown", "type=kcp"} {
		if _, e := parseVLESSLink("vless://test@example.com:443?" + q); e == nil {
			t.Fatal("accepted", q)
		}
	}
	if _, e := parseVLESSLink("vless://test@example.com:443/ignored?type=tcp"); e == nil {
		t.Fatal("ignored URL path")
	}
}
func TestPrimaryCommitPreservesOtherSettingsAndKeepsRollback(t *testing.T) {
	s, _ := fixture(t)
	old := configFixture(t, s)
	out := exampleOutbound(t, "new.example")
	n := 0
	if e := s.commitPrimary(out, func() error { n++; return nil }, func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	cfg, _ := s.readVPNConfig()
	got, _ := vpnOutbound(cfg)
	if outboundHash(got) != outboundHash(out) || string(jsonBytes(cfg["inbounds"])) != string(jsonBytes(old["inbounds"])) || len(cfg["outbounds"].([]any)) != 2 || n != 1 {
		t.Fatal(cfg, n)
	}
	prior, e := s.connectionOutbound("previous")
	oldVPN, _ := vpnOutbound(old)
	if e != nil || outboundHash(prior) != outboundHash(oldVPN) {
		t.Fatal(prior, e)
	}
	if s.pendingRecovery() {
		t.Fatal("journal left after success")
	}
}
func TestPrimaryFailureRestoresAllSlotsAndRetainsFailedRollback(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "journal retained"}[rollbackFails], func(t *testing.T) {
			s, _ := fixture(t)
			old := configFixture(t, s)
			os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700)
			oldPrimary := exampleOutbound(t, "primary.example")
			oldBackup := exampleOutbound(t, "backup.example")
			atomicWrite(s.profilePath("primary"), jsonBytes(oldPrimary))
			atomicWrite(s.profilePath("backup"), jsonBytes(oldBackup))
			atomicWrite(filepath.Join(s.dir, "active-profile.json"), []byte(`{"name":"backup"}`))
			probes := 0
			err := s.commitPrimary(exampleOutbound(t, "new.example"), func() error { return nil }, func() error {
				probes++
				if probes == 1 || rollbackFails {
					return errors.New("probe failed")
				}
				return nil
			})
			if err == nil {
				t.Fatal("accepted failed connection")
			}
			cfg, _ := s.readVPNConfig()
			if string(jsonBytes(cfg)) != string(jsonBytes(old)) {
				t.Fatal("config not restored")
			}
			for name, want := range map[string]map[string]any{"primary": oldPrimary, "backup": oldBackup} {
				var got map[string]any
				readJSON(s.profilePath(name), &got)
				if outboundHash(got) != outboundHash(want) {
					t.Fatal("slot changed", name)
				}
			}
			var active map[string]any
			readJSON(filepath.Join(s.dir, "active-profile.json"), &active)
			if active["name"] != "backup" {
				t.Fatal(active)
			}
			if s.pendingRecovery() != rollbackFails {
				t.Fatal("incorrect journal lifecycle")
			}
		})
	}
}
func TestConnectionMetadataNeverRevealsCredentials(t *testing.T) {
	s, _ := fixture(t)
	configFixture(t, s)
	out := exampleOutbound(t, "private-server.example")
	if e := s.saveConnection("", "My server", out); e != nil {
		t.Fatal(e)
	}
	cookie, csrf := loginTest(t, s)
	w := request(s, "GET", "/api/connections", "", cookie, "", "", s.host)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, secret := range []string{"private-server.example", "test-public-key", "00000000-0000", "cover.example"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret in metadata", secret)
		}
	}
	w = request(s, "POST", "/api/connection-reveal", `{"id":"current","password":"wrong"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = request(s, "POST", "/api/connection-reveal", `{"id":"current","password":"old-password-123"}`, cookie, "", "http://"+s.host, s.host)
	if w.Code != 403 {
		t.Fatal("CSRF missing", w.Code)
	}
	w = request(s, "POST", "/api/connection-reveal", `{"id":"current","password":"old-password-123"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "old.example") {
		t.Fatal("authenticated reveal failed", w.Code, w.Body.String())
	}
	// Bank secrets must also be scrubbed from reports and errors.
	if v := s.scrub("private-server.example test-public-key cover.example"); strings.Contains(v, "private-server") || strings.Contains(v, "test-public-key") || strings.Contains(v, "cover.example") {
		t.Fatal(v)
	}
	list, _ := s.connections()
	info, _ := os.Stat(filepath.Join(s.dir, "connections.json"))
	if info.Mode().Perm() != 0600 || len(list) != 1 {
		t.Fatal(info, list)
	}
	if _, e := s.connectionOutbound("../../auth"); e == nil {
		t.Fatal("invalid id accepted")
	}
	if e := s.saveConnection("missing", "x", out); e == nil {
		t.Fatal("unknown update accepted")
	}
	for len(list) < 20 {
		list = append(list, list[0])
	}
	s.writeConnections(list)
	if e := s.saveConnection("", "extra", out); e == nil {
		t.Fatal("bank limit bypassed")
	}
}
func TestProfileJournalRestoresOriginallyMissingPrimary(t *testing.T) {
	s, _ := fixture(t)
	cfg := configFixture(t, s)
	os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700)
	if e := s.writeProfileJournal(jsonBytes(cfg)); e != nil {
		t.Fatal(e)
	}
	atomicWrite(s.profilePath("primary"), []byte(`{}`))
	if e := s.restoreProfileFiles(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.profilePath("primary")); !os.IsNotExist(e) {
		t.Fatal("new slot survived rollback")
	}
}
func TestConnectionsAPIRequiresLogin(t *testing.T) {
	s, _ := fixture(t)
	for _, p := range []string{"connections", "activity"} {
		w := request(s, "GET", "/api/"+p, "", nil, "", "", s.host)
		if w.Code != 401 {
			t.Fatal(p, w.Code)
		}
	}
}
