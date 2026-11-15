package panel

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionReviewShowsChangedFieldsWithoutSecrets(t *testing.T) {
	current := map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"address": "example.com", "port": 443, "id": "old-token"}}
	next := map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"address": "new.example.com", "port": 444, "id": "new-token"}}
	review := subscriptionReview{ID: "review-id", Changes: subscriptionChanges(current, next), out: next, config: subscriptionConfig{URL: "https://example.com/private-token"}}
	s := string(jsonBytes(review))
	if !strings.Contains(s, "Адрес сервера") || !strings.Contains(s, "Порт") ||
		strings.Contains(s, "token") || strings.Contains(s, "new.example.com") {
		t.Fatal(s)
	}
	if len(subscriptionChanges(current, current)) != 0 {
		t.Fatal("identical profiles differ")
	}
}

func TestSubscriptionApplyRejectsStaleReviewAndChangedBinding(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	out := map[string]any{"tag": "vpn", "protocol": "vless"}
	cfg := subscriptionConfig{URL: "https://example.com/private", Bound: outboundHash(out)}
	if err := atomicWrite(s.subscriptionPath(), jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	writeOutbound := func(next map[string]any) {
		t.Helper()
		if err := atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(map[string]any{"outbounds": []any{next}})); err != nil {
			t.Fatal(err)
		}
	}
	writeOutbound(out)
	s.pendingSubscription = subscriptionReview{ID: "id", config: cfg, out: out, expires: time.Now().Add(-time.Second)}
	apply := func() (int, string) {
		body := string(jsonBytes(map[string]string{"action": "apply", "review": "id", "password": "old-password-123"}))
		w := request(s, "POST", "/api/subscription", body, cookie, csrf, "http://"+s.host, s.host)
		return w.Code, w.Body.String()
	}
	if status, _ := apply(); status != 409 {
		t.Fatal(status)
	}
	s.pendingSubscription.expires = time.Now().Add(time.Minute)
	writeOutbound(map[string]any{"tag": "vpn", "protocol": "trojan"})
	if status, body := apply(); status != 400 || !strings.Contains(body, "изменилось") {
		t.Fatalf("apply accepted changed binding: %d %s", status, body)
	}
	writeOutbound(out)
	if status, body := apply(); status != 200 || !json.Valid([]byte(body)) || !strings.Contains(body, "unchanged") {
		t.Fatalf("no-change should be a no-op: %d %s", status, body)
	}
	if status, _ := apply(); status != 409 {
		t.Fatal("review reused")
	}
}
