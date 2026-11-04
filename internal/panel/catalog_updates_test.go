package panel

import (
	"strings"
	"testing"
)

func TestCatalogProposalsPreserveGroupsAndRespectDecisions(t *testing.T) {
	st := State{Groups: []Group{{ID: "custom-gemini", Name: "Мой Gemini", Enabled: false, Domains: []string{"gemini.google.com", "private.example"}}}}
	proposals := catalogUpdates(st)
	var gemini CatalogUpdate
	for _, p := range proposals {
		if p.ID == "gemini" {
			gemini = p
		}
	}
	if gemini.New || gemini.Group.ID != "custom-gemini" || gemini.Group.Enabled || len(gemini.Missing) == 0 {
		t.Fatal(gemini)
	}
	if len(st.Groups) != 1 || len(st.Groups[0].Domains) != 2 {
		t.Fatal("preview changed state")
	}
	st.CatalogChoices = map[string]string{"gemini": gemini.Digest}
	if _, err := validate(st); err != nil {
		t.Fatal(err)
	}
	// A digest is tied to the complete sorted preset; later catalog additions
	// change it, even if this version was previously dismissed.
	old := append([]string{}, presets[4].Domains...)
	presets[4].Domains = append(append([]string{}, old...), "new-dependency.example")
	defer func() { presets[4].Domains = old }()
	for _, p := range catalogUpdates(st) {
		if p.ID == "gemini" && p.Digest == gemini.Digest {
			t.Fatal("new dependencies hidden by previous decision")
		}
	}
	st.CatalogChoices["gemini"] = strings.Repeat("x", 64)
	if _, err := validate(st); err == nil {
		t.Fatal("invalid decision accepted")
	}
}

func TestNewCatalogGroupsRemainDisabled(t *testing.T) {
	for _, p := range catalogUpdates(State{}) {
		if !p.New || p.Group.Enabled || len(p.Missing) == 0 {
			t.Fatal(p)
		}
	}
}

func TestCatalogDecisionPersistsWithoutChangingLiveRoutes(t *testing.T) {
	s, r := fixture(t)
	st, _ := s.state()
	if _, err := s.apply(st, s.revision()); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	st.CatalogChoices = map[string]string{"gemini": strings.Repeat("a", 64)}
	if _, err := s.apply(st, s.revision()); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.state()
	if saved.CatalogChoices["gemini"] != st.CatalogChoices["gemini"] {
		t.Fatal("decision lost")
	}
	for _, call := range r.calls {
		if strings.Contains(call, "reload-dns.sh") || strings.Contains(call, "vpn.sh start") {
			t.Fatal("decision changed routes", call)
		}
	}
}
