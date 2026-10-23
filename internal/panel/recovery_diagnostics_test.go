package panel

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestDNSRecoveryRequiresPersistentFailureAndWorkingHTTPS(t *testing.T) {
	failed := errors.New("DNS timed out")
	for _, tc := range []struct {
		name  string
		dns   []error
		https error
		want  bool
	}{
		{"first retry recovers", []error{nil}, nil, false},
		{"second retry recovers", []error{failed, nil}, nil, false},
		{"persistent DNS failure", []error{failed, failed}, nil, true},
		{"whole tunnel now fails", []error{failed, failed}, failed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, waits, httpsCalls := 0, 0, 0
			got, failures := confirmTunnelDNSFailure(func() error {
				e := tc.dns[calls]
				calls++
				return e
			}, func() error { httpsCalls++; return tc.https }, func() { waits++ })
			if got != tc.want || calls != len(tc.dns) || waits != calls {
				t.Fatalf("confirmed=%t DNS=%d waits=%d", got, calls, waits)
			}
			if (httpsCalls == 1) != (len(failures) == 2) {
				t.Fatal("HTTPS must be rechecked only after both DNS retries fail")
			}
		})
	}
}

func TestRecoverySnapshotReplacesPreviousAndScrubsSecrets(t *testing.T) {
	s, _ := fixture(t)
	c := Connectivity{Errors: map[string]string{"dns": "vless://secret@vpn.example:443"}}
	s.captureRecovery(c, "first")
	s.captureRecovery(c, "latest")
	b, err := os.ReadFile(s.runtimePath("last-recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret@") || strings.Contains(string(b), `"reason":"first"`) {
		t.Fatal("snapshot leaked secret or retained previous sample")
	}
	var saved recoverySnapshot
	if err := readJSON(s.runtimePath("last-recovery.json"), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Reason != "latest" || saved.At == 0 || saved.Resources["meminfo"] == "" {
		t.Fatal("missing pre-recovery diagnostics")
	}
}
