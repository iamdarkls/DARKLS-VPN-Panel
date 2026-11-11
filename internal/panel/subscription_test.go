package panel

import (
	"context"
	"encoding/base64"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelectSubscriptionProfile(t *testing.T) {
	const primary = "vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&type=ws#Main%20NL"
	const backup = "vless://00000000-0000-4000-8000-000000000002@other.example.com:443?security=tls#Backup"
	list := primary + "\r\n" + backup + "\r\n"
	for _, data := range []string{list, base64.StdEncoding.EncodeToString([]byte(list)), base64.RawURLEncoding.EncodeToString([]byte(list))} {
		out, err := selectSubscription([]byte(data), "Main NL")
		host, port := outboundEndpoint(out)
		if err != nil || host != "example.com" || port != 443 || out["tag"] != "vpn" {
			t.Fatalf("expected named primary, got %s:%d (%v)", host, port, err)
		}
	}
	for _, tc := range []struct{ body, name string }{
		{list, ""}, {list, "Missing"}, {primary + "\n" + primary, "Main NL"},
		{strings.Replace(primary, "security=tls", "security=tls&allowInsecure=true", 1), ""},
		{strings.Replace(primary, "type=ws", "type=xhttp&extra=%7B%22DownloadSettings%22%3A%7B%7D%7D", 1), ""},
		{"<html>not a subscription</html>", ""},
	} {
		if _, err := selectSubscription([]byte(tc.body), tc.name); err == nil {
			t.Fatal("invalid or ambiguous subscription accepted")
		}
	}
}

func TestSubscriptionPublicOnly(t *testing.T) {
	for _, ip := range []string{"::1", "127.0.0.1", "::ffff:192.168.1.2", "100.64.1.2", "169.254.169.254", "198.19.0.5", "64:ff9b::7f00:1"} {
		if subscriptionPublic(netip.MustParseAddr(ip)) {
			t.Errorf("non-public address accepted: %s", ip)
		}
	}
	for _, raw := range []string{"http://example.com/sub", "https://admin:token@example.com/sub", "https://example.com/sub#token", "https://router.local/sub", "https://192.168.50.1/sub", "https://example.com:65536/sub"} {
		if _, err := subscriptionURL(raw); err == nil {
			t.Errorf("URL accepted: %s", raw)
		}
	}
	if _, err := subscriptionURL("https://example.com:2096/sub/test?token=example"); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchSubscription(context.Background(), "https://127.0.0.1/private"); err == nil {
		t.Fatal("loopback fetch accepted")
	}
}

func TestSubscriptionBindingStopsAfterManualChange(t *testing.T) {
	root := t.TempDir()
	s := newServer(root, "", nil)
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"tag": "vpn", "protocol": "vless"}
	if err := atomicWrite(filepath.Join(root, "config-vpn.json"), jsonBytes(map[string]any{"outbounds": []any{out}})); err != nil {
		t.Fatal(err)
	}
	config := subscriptionConfig{URL: "https://example.com/secret", Bound: "different"}
	if code, err := s.refreshSubscription(context.Background(), config); err == nil || code != "" {
		t.Fatal("detached subscription should not be fetched or applied")
	}
	if err := atomicWrite(s.subscriptionPath(), jsonBytes(config)); err != nil {
		t.Fatal(err)
	}
	status := s.subscriptionStatus()
	if status["bound"] != false || strings.Contains(string(jsonBytes(status)), "secret") {
		t.Fatal("incorrect binding state or leaked subscription token")
	}
}

func TestSubscriptionSchedule(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		ago  time.Duration
		code string
		due  bool
	}{
		{2 * time.Hour, "updated", false}, {7 * time.Hour, "unchanged", true},
		{30 * time.Minute, "error", false}, {2 * time.Hour, "error", true},
		{-time.Hour, "updated", true},
	} {
		if subscriptionDue(subscriptionResult{At: now.Add(-tc.ago).Unix(), Code: tc.code}, now) != tc.due {
			t.Errorf("incorrect schedule for %v", tc)
		}
	}
}

func TestSubscriptionAPIProtectsSourceAndDoesNotModifyVPNOnSave(t *testing.T) {
	s, _ := fixture(t)
	cfg := []byte(`{"outbounds":[{"tag":"vpn","protocol":"vless"}]}`)
	configPath := filepath.Join(s.root, "config-vpn.json")
	if err := atomicWrite(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginTest(t, s)
	origin := "http://" + s.host
	const secret = "subscription-token-test"
	const body = `{"action":"save","password":"old-password-123","url":"https://example.com:2096/sub/` + secret + `","profile":"Main","auto":true}`
	if w := request(s, "POST", "/api/subscription", body, cookie, "", origin, s.host); w.Code != 403 {
		t.Fatalf("missing CSRF accepted: %d", w.Code)
	}
	if w := request(s, "POST", "/api/subscription", strings.Replace(body, "old-password-123", "wrong", 1), cookie, csrf, origin, s.host); w.Code != 403 {
		t.Fatalf("incorrect password accepted: %d", w.Code)
	}
	if w := request(s, "POST", "/api/subscription", body, cookie, csrf, origin, s.host); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	info, err := os.Stat(s.subscriptionPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("subscription file not private")
	}
	if w := request(s, "GET", "/api/connections", "", cookie, csrf, origin, s.host); w.Code != 200 ||
		strings.Contains(w.Body.String(), secret) || !strings.Contains(w.Body.String(), `"bound":true`) {
		t.Fatal("subscription was not bound or exposed its secret")
	}
	if w := request(s, "POST", "/api/subscription", `{"action":"remove","password":"old-password-123"}`, cookie, csrf, origin, s.host); w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	current, err := os.ReadFile(configPath)
	if err != nil || string(current) != string(cfg) {
		t.Fatal("save or removal changed the active VPN")
	}
}
