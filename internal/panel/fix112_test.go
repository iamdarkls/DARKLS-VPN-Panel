package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstMetricsRequestHasCPUPercentage(t *testing.T) {
	s, _ := fixture(t)
	v := s.metrics()["cpu_percent"]
	n, ok := v.(float64)
	if !ok || n < 0 || n > 100 {
		t.Fatalf("first request CPU: %#v", v)
	}
}
func TestInstagramRepairAddsDependencyAndPreservesSelection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		s, r := fixture(t)
		st, _ := s.state()
		for i := range st.Groups {
			if st.Groups[i].ID == "instagram" {
				st.Groups = append(st.Groups[:i], st.Groups[i+1:]...)
				break
			}
		}
		st.Groups = append(st.Groups, Group{ID: "instagram", Name: "Мой Instagram", Enabled: enabled, Domains: []string{"instagram.com"}})
		atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st))
		if e := s.repairInstagram(); e != nil {
			t.Fatal(e)
		}
		got, _ := s.state()
		g := got.Groups[len(got.Groups)-1]
		if g.Name != "Мой Instagram" || g.Enabled != enabled || !strings.Contains(strings.Join(g.Domains, " "), "fbsbx.com") {
			t.Fatal(g)
		}
		conf, _ := os.ReadFile(filepath.Join(s.root, "dnsmasq-vpn.conf"))
		if strings.Contains(string(conf), "server=/fbsbx.com/") != enabled {
			t.Fatal("enabled state changed", string(conf))
		}
		for _, c := range r.calls {
			if strings.Contains(c, "vpn.sh start") || strings.Contains(c, "killall") {
				t.Fatal("unnecessary Xray restart", c)
			}
		}
		// Once applied, a deliberate later removal is respected.
		got.Groups[len(got.Groups)-1].Domains = []string{"instagram.com"}
		atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(got))
		if e := s.repairInstagram(); e != nil {
			t.Fatal(e)
		}
		got, _ = s.state()
		if len(got.Groups[len(got.Groups)-1].Domains) != 1 {
			t.Fatal("user removal undone")
		}
	}
}
func TestInstagramRepairRollbackRetainsRetry(t *testing.T) {
	s, r := fixture(t)
	st, _ := s.state()
	for i := range st.Groups {
		if st.Groups[i].ID == "instagram" {
			st.Groups = append(st.Groups[:i], st.Groups[i+1:]...)
			break
		}
	}
	st.Groups = append(st.Groups, Group{ID: "instagram", Name: "Instagram", Enabled: true, Domains: []string{"instagram.com"}})
	atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st))
	r.failReload = true
	if e := s.repairInstagram(); e == nil {
		t.Fatal("failure hidden")
	}
	if _, e := os.Stat(filepath.Join(s.dir, "instagram-fbsbx-migrated")); !os.IsNotExist(e) {
		t.Fatal("marker committed despite failed reload")
	}
	got, _ := s.state()
	if len(got.Groups[len(got.Groups)-1].Domains) != 1 {
		t.Fatal("failed migration not rolled back")
	}
}
