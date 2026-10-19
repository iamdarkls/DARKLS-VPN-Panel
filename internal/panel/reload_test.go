package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddingGroupPreservesCachedVPNRoutesAndXray(t *testing.T) {
	s, runner := fixture(t)
	st, _ := s.state()
	st.Groups = append(st.Groups, Group{ID: "extra", Name: "Extra", Enabled: true, Domains: []string{"extra.example"}})
	if _, err := s.apply(st, s.revision()); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(runner.calls, "\n")
	if !strings.Contains(calls, "reload-dns.sh") {
		t.Fatal("new DNS list not activated")
	}
	if strings.Contains(calls, "vpn.sh") || strings.Contains(calls, "ipset flush darkls_vpn") || strings.Contains(calls, "ipset flush darkls_yt") {
		t.Fatalf("disrupted cached client routes or Xray: %s", calls)
	}
}

func TestDisabledGroupAndRenameDoNotTouchLiveVPN(t *testing.T) {
	s, runner := fixture(t)
	st, _ := s.state()
	// Bring the fixture's live DNS config in sync first.
	if _, err := s.apply(st, s.revision()); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	st.Groups[0].Name = "New name"
	st.Groups = append(st.Groups, Group{ID: "extra", Name: "Disabled", Enabled: false, Domains: []string{"extra.example"}})
	if _, err := s.apply(st, s.revision()); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(runner.calls, "\n")
	if strings.Contains(calls, "vpn.sh") || strings.Contains(calls, "reload-dns.sh") || strings.Contains(calls, "ipset ") {
		t.Fatalf("inactive edits touched runtime: %s", calls)
	}
}

func TestCatalogMigrationPreservesSelectionAndCustomGroups(t *testing.T) {
	s, runner := fixture(t)
	st, _ := s.state()
	st.Groups = []Group{{ID: "youtube", Name: "Мой YouTube", Enabled: false, Domains: []string{"youtube.com"}}, {ID: "my-facebook", Name: "Личная группа", Enabled: true, Domains: []string{"facebook.com"}}}
	if e := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(s.root, "domains.txt"))
	if e := s.updateCatalog(); e != nil {
		t.Fatal(e)
	}
	after, _ := s.state()
	if after.Groups[0].Enabled || after.Groups[0].Name != "Мой YouTube" || !after.Groups[1].Enabled {
		t.Fatal("existing selection changed")
	}
	for _, g := range after.Groups[2:] {
		if g.Enabled || g.ID == "facebook" {
			t.Fatal("catalog enabled new service or duplicated custom group")
		}
	}
	if len(runner.calls) != 0 {
		t.Fatal("catalog import touched live VPN")
	}
	live, _ := os.ReadFile(filepath.Join(s.root, "domains.txt"))
	if string(live) != string(before) {
		t.Fatal("live domains changed")
	}
	revision := s.revision()
	if e := s.updateCatalog(); e != nil {
		t.Fatal(e)
	}
	if s.revision() != revision {
		t.Fatal("migration is not idempotent")
	}
}

func TestAdultCatalogDefaultsOffAndKeepsUserChoice(t *testing.T) {
	st, _ := addCatalogGroups(State{Version: 1})
	count := 0
	for i := range st.Groups {
		if strings.HasPrefix(st.Groups[i].ID, "adult-") {
			count++
			if st.Groups[i].Enabled {
				t.Fatal("adult group enabled by default")
			}
		}
		if st.Groups[i].ID == "adult-pornhub" {
			st.Groups[i].Enabled = true
		}
	}
	if count != 28 {
		t.Fatalf("adult groups: %d", count)
	}
	updated, added := addCatalogGroups(st)
	if added != 0 {
		t.Fatal("duplicate groups added")
	}
	for _, g := range updated.Groups {
		if g.ID == "adult-pornhub" && !g.Enabled {
			t.Fatal("user choice reset")
		}
	}
}

func TestGroupNameCountsCharactersNotUTF8Bytes(t *testing.T) {
	st := State{Groups: []Group{{ID: "test", Name: strings.Repeat("Ж", 60)}}}
	if _, err := validate(st); err != nil {
		t.Fatal("valid Cyrillic name rejected:", err)
	}
	st.Groups[0].Name += "Ж"
	if _, err := validate(st); err == nil {
		t.Fatal("overlong name accepted")
	}
}

func TestProviderCatalogPreservesExistingGroupsAndDoesNotTouchLiveVPN(t *testing.T) {
	original := Group{ID: "my-tiktok", Name: "Мой TikTok", Enabled: true, Category: "custom", BlockQUIC: boolRef(true), Domains: []string{"tiktok.com", "my.example"}}
	st, added := addCatalogGroups(State{Groups: []Group{original}})
	if added == 0 {
		t.Fatal("catalogue was not expanded")
	}
	st, err := validate(st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Groups[0].Name != original.Name || !st.Groups[0].Enabled || !*st.Groups[0].BlockQUIC || st.Groups[0].Category != "custom" || len(st.Groups[0].Domains) != 2 {
		t.Fatal("custom service was overwritten")
	}
	want := map[string]string{"spotify": "music", "netflix": "video", "claude": "ai", "notion": "work", "miro": "work", "canva": "work", "adobe": "work", "autodesk": "work", "wrike": "work"}
	for _, g := range st.Groups[1:] {
		if g.ID == "tiktok" {
			t.Fatal("custom TikTok duplicated")
		}
		if g.Enabled {
			t.Fatal("new service automatically enabled", g.ID)
		}
		if category, ok := want[g.ID]; ok {
			if g.Category != category || catalogNotes[g.ID] == "" {
				t.Fatal("missing category or provider note", g.ID)
			}
			delete(want, g.ID)
		}
	}
	if len(want) != 0 {
		t.Fatal("missing services", want)
	}
	if again, n := addCatalogGroups(st); n != 0 || len(again.Groups) != len(st.Groups) {
		t.Fatal("catalogue not idempotent")
	}
}
