package main

import (
	"testing"
	"time"
)

func TestDailyCoverageGapsAndPeaks(t *testing.T) {
	now := int64(1800000000)
	p := []ProbeSample{{At: now - 300, OK: true}, {At: now - 270, OK: false}, {At: now - 240, OK: true}, {At: now - 20, OK: false}, {At: now - 10, Kind: "recovery"}, {At: now + 1, OK: true}}
	r := []ResourceSample{{At: now - 1, Used: 300, Panel: 20, Xray: 50}, {At: now - 86401, Used: 9999}}
	s := summarizeDay(p, r, now)
	if s.Observed != 180 || s.Unavailable != 50 || s.Unknown != 86220 || s.Failures != 2 || s.Recoveries != 1 || !s.Ongoing || s.PeakUsed != 300 || s.Resources != 1 {
		t.Fatal(s)
	}
}
func TestDailyLatestFailureAndDuplicate(t *testing.T) {
	now := int64(1800000000)
	s := summarizeDay([]ProbeSample{{At: now, OK: true}, {At: now, OK: false}}, nil, now)
	if s.Failures != 1 || !s.Ongoing || s.Observed != 0 || s.Unknown != 86400 {
		t.Fatal(s)
	}
}
func TestDailyClipsWindowAndDoesNotInventRecovery(t *testing.T) {
	now := int64(1800000000)
	s := summarizeDay([]ProbeSample{{At: now - 86420, OK: false}}, nil, now)
	if s.Observed != 80 || s.Unavailable != 80 || s.Failures != 0 || s.Recoveries != 0 || s.Ongoing {
		t.Fatal(s)
	}
}
func TestCorePublicationDeferral(t *testing.T) {
	now := time.Unix(1800000000, 0)
	p := CoreSettings{DelayDays: 3}
	for _, x := range []struct {
		at   int64
		want bool
	}{{now.Unix() - 3*86400, true}, {now.Unix() - 3*86400 + 1, false}, {0, false}, {now.Unix() + 1, false}} {
		if got := coreReleaseMature(p, CoreStatus{PublishedAt: x.at}, now); got != x.want {
			t.Fatal(x, got)
		}
	}
	p.DelayDays = 0
	if !coreReleaseMature(p, CoreStatus{}, now) {
		t.Fatal("manual/no-delay blocked")
	}
}
func TestDevicePolicyAndGroupPrecedence(t *testing.T) {
	mac := "02:00:00:00:00:01"
	for _, mode := range []string{"all", "include", "exclude"} {
		p := DevicePolicy{Mode: mode, Devices: []DeviceRef{{MAC: mac}}}
		if got := devicePolicyEligible(p, mac); got != (mode != "exclude") {
			t.Fatal(mode, got)
		}
	}
	g := Group{ID: "site", Name: "Site", Enabled: false}
	st := State{Groups: []Group{g}}
	reg := map[string]string{g.ID: groupSet(g.ID)}
	contains := func(string) bool { return true }
	if got, _ := routeGroupDecision(st, reg, contains); got {
		t.Fatal("inactive bypass lost to legacy union")
	}
	st.Groups[0].Enabled = true
	if got, _ := routeGroupDecision(st, reg, contains); !got {
		t.Fatal("active group lost shared IP")
	}
	st.Groups = nil
	if got, _ := routeGroupDecision(st, reg, contains); got {
		t.Fatal("removed group routed")
	}
}

func TestInvalidDeviceFailsBeforeDNS(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	for _, mac := range []string{"invalid", "02:ff:ff:ff:ff:ff"} {
		w := request(s, "POST", "/api/diagnose", `{"domain":"example.com","device_mac":"`+mac+`"}`, cookie, csrf, "http://"+s.host, s.host)
		if w.Code != 400 {
			t.Fatal(mac, w.Code, w.Body.String())
		}
	}
}
