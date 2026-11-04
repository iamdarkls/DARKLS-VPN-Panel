package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyMigrationPreservesChoicesAndRunsOnce(t *testing.T) {
	s, r := fixture(t)
	st, _ := s.state()
	st.Groups = []Group{
		{ID: "my-gemini", Name: "Мой Gemini", Category: "custom", Enabled: false, BlockQUIC: boolRef(true), Domains: []string{"gemini.google.com", "custom.example"}},
		{ID: "instagram", Name: "Фото", Enabled: true, Domains: []string{"instagram.com"}},
	}
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); err != nil {
		t.Fatal(err)
	}
	if err := s.upgradeServiceDependencies(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.state()
	if len(got.Groups) != 2 || got.Groups[0].Enabled || got.Groups[0].Name != "Мой Gemini" || got.Groups[0].Category != "custom" || !*got.Groups[0].BlockQUIC {
		t.Fatal("choices changed", got)
	}
	joined := strings.Join(got.Groups[0].Domains, " ")
	for _, d := range []string{"custom.example", "generativelanguage.googleapis.com", "geminiweb-pa.clients6.google.com"} {
		if !strings.Contains(joined, d) {
			t.Fatal("missing dependency", d)
		}
	}
	for _, call := range r.calls {
		if strings.Contains(call, "vpn.sh start") || call == "ipset flush darkls_vpn" || call == "ipset flush darkls_yt" {
			t.Fatal("disrupted VPN", call)
		}
	}
	got.Groups[0].Domains = []string{"gemini.google.com"}
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(got)); err != nil {
		t.Fatal(err)
	}
	revision := s.revision()
	r.calls = nil
	if err := s.upgradeServiceDependencies(); err != nil {
		t.Fatal(err)
	}
	if s.revision() != revision || len(r.calls) != 0 {
		t.Fatal("overwrote later user edit")
	}
}

func TestDependencyMigrationRollsBackAndCanRetry(t *testing.T) {
	s, r := fixture(t)
	before, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	r.failReload = true
	if err := s.upgradeServiceDependencies(); err == nil {
		t.Fatal("DNS failure hidden")
	}
	after, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("state not rolled back")
	}
	if _, err := os.Stat(filepath.Join(s.dir, dependenciesMarker)); !os.IsNotExist(err) {
		t.Fatal("failed migration marked complete")
	}
	if err := s.upgradeServiceDependencies(); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyCatalogValidationAndGeminiInsertion(t *testing.T) {
	st, _ := addCatalogGroups(State{Version: 1})
	st = requestedDependenciesState(st)
	if _, err := validate(st); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range st.Groups {
		if g.ID == "gemini" {
			found = g.Enabled
		}
		if strings.HasPrefix(g.ID, "adult-") && g.Enabled {
			t.Fatal("new group enabled", g.ID)
		}
		for _, d := range g.Domains {
			for _, broad := range []string{"google.com", "googleapis.com", "cloudflare.com", "cloudfront.net", "amazonaws.com", "akamaized.net", "azureedge.net"} {
				if d == broad {
					t.Fatal("whole shared CDN routed", d)
				}
			}
		}
	}
	if !found {
		t.Fatal("missing Gemini")
	}
	for _, g := range presets {
		if _, ok := catalogDependencies[g.ID]; !ok {
			t.Fatal("core service not reviewed", g.ID)
		}
	}
	for _, g := range additionalPresets {
		if len(g.Domains) == 0 {
			t.Fatal("empty group", g.ID)
		}
	}
}
