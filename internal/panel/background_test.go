package panel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceSkipsProcessScanForUnchangedDevicePolicy(t *testing.T) {
	s, r := fixture(t)
	// Default all-device mode never needs a lock or an external process on idle ticks.
	s.maintenanceTick()
	if len(r.calls) != 0 {
		t.Fatal(r.calls)
	}
	lock := filepath.Join(s.dir, "apply.lock")
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("idle tick entered pause", err)
	}
	st, _ := s.state()
	st.Policy.Mode = "include"
	if err := atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st)); err != nil {
		t.Fatal(err)
	}
	s.maintenanceTick()
	if len(r.calls) == 0 {
		t.Fatal("changed policy was not applied")
	}
	before := len(r.calls)
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	s.maintenanceTick()
	if len(r.calls) != before {
		t.Fatal("unchanged policy spawned commands")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("unchanged policy scanned processes")
	}
}

func TestPeriodicCancellationDoesNotQueueAnotherTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	go func() {
		defer close(done)
		runPeriodic(ctx, 0, time.Millisecond, func() { calls++; close(entered); <-release })
	}()
	<-entered
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	runPeriodic(ctx, 0, time.Millisecond, func() { t.Fatal("canceled scheduler ran") })
}

func TestServiceSchedulingFairnessAndDisabledGroups(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	st := State{Groups: []Group{{ID: "youtube", Enabled: true}, {ID: "chatgpt", Enabled: true}, {ID: "instagram", Enabled: false}}}
	last := map[string]time.Time{}
	if got := dueService(st, last, now, time.Minute); got != "youtube" {
		t.Fatal(got)
	}
	last["youtube"] = now
	if got := dueService(st, last, now, time.Minute); got != "chatgpt" {
		t.Fatal(got)
	}
	last["chatgpt"] = now
	if got := dueService(st, last, now, time.Minute); got != "" {
		t.Fatal(got)
	}
	if got := dueService(st, last, now.Add(2*time.Minute), time.Minute); got != "youtube" {
		t.Fatal(got)
	}
	last["youtube"] = now.Add(time.Hour)
	if got := dueService(st, last, now, time.Minute); got != "youtube" {
		t.Fatal("future clock sample suppressed monitoring", got)
	}
}

func TestFreshnessAndDiscoveryAfterClockCorrection(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, age := range []int64{-10, 100, 200} {
		if probeFresh(ProbeState{CheckedAt: now.Unix() - age}, now) {
			t.Fatal(age)
		}
	}
	if !probeFresh(ProbeState{CheckedAt: now.Unix() - 99}, now) {
		t.Fatal("recent sample rejected")
	}
	for _, x := range []struct {
		age time.Duration
		err string
		due bool
	}{{23 * time.Hour, "", false}, {24 * time.Hour, "", true}, {time.Hour, "offline", true}, {59 * time.Minute, "offline", false}, {-time.Hour, "", true}} {
		if got := releaseCheckDue(now, now.Add(-x.age).Unix(), x.err); got != x.due {
			t.Fatal(x, got)
		}
	}
}
