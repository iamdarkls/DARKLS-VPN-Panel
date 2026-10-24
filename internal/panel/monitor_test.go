package panel

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestWholeDiagnosticSharesCancellationBudget(t *testing.T) {
	s, _ := fixture(t)
	s.runner = blockingRunner{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	scoped := s.networkOperation(ctx)
	if err := scoped.checkRoutingOnly(); !errors.Is(err, errDiagnosticIncomplete) {
		t.Fatal(err)
	}
	c := scoped.collectConnectivity(true)
	if c.Complete || c.Errors["incomplete"] == "" {
		t.Fatal(c)
	}
	if time.Since(started) > time.Second {
		t.Fatal("deadline restarted for another component")
	}
	if err := scoped.quickHealth(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout misclassified as missing process", err)
	}
}

func TestMonitorRequiresRepeatedFailuresAndSkipsUnknownChecks(t *testing.T) {
	now := time.Now()
	good := func() error { return nil }
	quick := func(context.Context) error { return nil }
	failed := errors.New("upstream unavailable")
	m := Monitor{}
	for i, err := range []error{failed, nil, failed, failed} {
		reason, structural := m.observe(context.Background(), now.Add(time.Duration(i)*time.Minute), quick, good, func() error { return err })
		if (reason != "") != (i == 3) || structural {
			t.Fatalf("cycle %d: %q %t", i, reason, structural)
		}
	}
	m = Monitor{}
	unknown := func(context.Context) error { return context.DeadlineExceeded }
	if reason, _ := m.observe(context.Background(), now, unknown, good, good); reason != "" {
		t.Fatal(reason)
	}
	for i := 0; i < 3; i++ {
		reason, _ := m.observe(context.Background(), now.Add(time.Duration(i)*time.Minute), quick, func() error { return errDiagnosticIncomplete }, good)
		if reason != "" {
			t.Fatal("incomplete check triggered recovery", reason)
		}
	}
	m = Monitor{}
	for i := 0; i < 2; i++ {
		reason, _ := m.observe(context.Background(), now.Add(time.Duration(i)*time.Minute), quick, func() error { return failed }, good)
		if (reason != "") != (i == 1) {
			t.Fatal(reason)
		}
	}
	reason, structural := m.observe(context.Background(), now, func(context.Context) error { return errors.New("hook missing") }, good, good)
	if reason == "" || !structural {
		t.Fatal("structural repair deferred")
	}
}

func TestRepairMustVerifyTraffic(t *testing.T) {
	err := confirmRepair(func() error { return errors.New("new PID exists but DNS timed out") }, "DNS restart")
	if err == nil || !strings.Contains(err.Error(), "не подтверждено") {
		t.Fatal(err)
	}
	if err := confirmRepair(func() error { return nil }, "DNS restart"); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatRejectsPreviousPIDAndStaleUptime(t *testing.T) {
	s, _ := fixture(t)
	m := Monitor{server: s}
	m.heartbeat()
	path := s.runtimePath("monitor-heartbeat")
	if readMonitorHeartbeat(path)["fresh"] != true {
		t.Fatal("fresh heartbeat rejected")
	}
	for _, data := range []string{"1 1\n", strconv.Itoa(os.Getpid()) + " 999999999999\n", strconv.Itoa(os.Getpid()) + " 0\n", "bad data\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if readMonitorHeartbeat(path)["fresh"] != false {
			t.Fatal(data)
		}
	}
}

func TestRuleComparisonPreservesArgumentOwnership(t *testing.T) {
	want := "-p tcp -s 192.0.2.1 -d 192.0.2.2 -j RETURN"
	swapped := "-p tcp -s 192.0.2.2 -d 192.0.2.1 -j RETURN"
	if canonicalRule(want) == canonicalRule(swapped) {
		t.Fatal("swapped endpoints accepted")
	}
	if canonicalRule(want) != canonicalRule("-A TEST -m tcp -d 192.0.2.2 -s 192.0.2.1 -p tcp -j RETURN") {
		t.Fatal("equivalent listing rejected")
	}
}

func TestFailedRepairCannotReuseOldGreenHTTPS(t *testing.T) {
	old, err := os.ReadFile(probeStatePath)
	t.Cleanup(func() {
		if err == nil {
			_ = os.WriteFile(probeStatePath, old, 0600)
		} else {
			_ = os.Remove(probeStatePath)
		}
	})
	if e := atomicWrite(probeStatePath, jsonBytes(ProbeState{OK: true, CheckedAt: time.Now().Unix()})); e != nil {
		t.Fatal(e)
	}
	if !recoveryVerified(readProbeState()) {
		t.Fatal("legacy probe blocked on upgrade")
	}
	recordRecovery("DNS helper restarted")
	if recoveryVerified(readProbeState()) {
		t.Fatal("pending verification accepted")
	}
	recordRecoveryConfirmation(errors.New("DNS not responding"))
	recordProbe(nil, time.Millisecond)
	if recoveryVerified(readProbeState()) {
		t.Fatal("HTTPS erased failed DNS verification")
	}
	if !recordRecoveryConfirmation(nil) || !recoveryVerified(readProbeState()) {
		t.Fatal("verified repair not accepted")
	}
}
