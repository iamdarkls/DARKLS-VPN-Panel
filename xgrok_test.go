package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestedXGrokActivationPreservesCustomDataAndRunsOnce(t *testing.T) {
	s, r := fixture(t)
	st, _ := s.state()
	st.Groups = []Group{
		{ID: "my-x", Name: "Мой X", Enabled: false, Category: "custom", BlockQUIC: boolRef(true), EnabledUntil: 1, Domains: []string{"twitter.com", "private.example"}},
		{ID: "my-grok", Name: "Мой Grok", Enabled: false, Category: "custom", Domains: []string{"grok.com", "another.example"}},
		{ID: "other", Name: "Other", Enabled: false, Domains: []string{"other.example"}},
	}
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); err != nil {
		t.Fatal(err)
	}
	if err := s.enableRequestedXGrok(); err != nil {
		t.Fatal(err)
	}
	got, err := s.state()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 3 || got.Groups[0].Name != "Мой X" || got.Groups[1].Name != "Мой Grok" || !got.Groups[0].Enabled || !got.Groups[1].Enabled || got.Groups[2].Enabled || !*got.Groups[0].BlockQUIC || got.Groups[0].EnabledUntil != 0 {
		t.Fatal(got)
	}
	conf, _ := os.ReadFile(filepath.Join(s.root, "dnsmasq-vpn.conf"))
	for _, d := range []string{"private.example", "another.example", "twimg.com", "twitteroauth.com", "grok.com", "x.ai", "challenges.cloudflare.com"} {
		if !strings.Contains(string(conf), "server=/"+d+"/") {
			t.Fatal("missing live dependency", d)
		}
	}
	for _, line := range r.calls {
		if strings.Contains(line, "vpn.sh start") || strings.Contains(line, "killall") {
			t.Fatal("Xray restarted", line)
		}
	}
	got.Groups[0].Enabled = false
	got.Groups[1].Enabled = false
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(got)); err != nil {
		t.Fatal(err)
	}
	rev := s.revision()
	r.calls = nil
	if err := s.enableRequestedXGrok(); err != nil {
		t.Fatal(err)
	}
	if s.revision() != rev || len(r.calls) != 0 {
		t.Fatal("later user choice overwritten")
	}
}

func TestRequestedXGrokActivationRollbackLeavesMigrationRetryable(t *testing.T) {
	s, r := fixture(t)
	before, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	r.failReload = true
	if err := s.enableRequestedXGrok(); err == nil {
		t.Fatal("DNS reload failure hidden")
	}
	after, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("previous state not restored")
	}
	if _, err := os.Stat(filepath.Join(s.dir, xGrokMarker)); !os.IsNotExist(err) {
		t.Fatal("migration marked complete despite failure")
	}
	if err := s.enableRequestedXGrok(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.state()
	enabled := map[string]bool{}
	for _, g := range got.Groups {
		enabled[g.ID] = g.Enabled
	}
	if !enabled["twitter"] || !enabled["grok"] {
		t.Fatal("retry did not enable both groups")
	}
	if serviceDomains["grok"] != "grok.com" || len(dependencyHosts["grok"]) < 4 {
		t.Fatal("diagnostics missing")
	}
}
