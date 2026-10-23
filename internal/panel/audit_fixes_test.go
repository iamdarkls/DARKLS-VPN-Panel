package panel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProfileRollbackRetainsJournalOnWriteOrProbeFailure(t *testing.T) {
	for _, fault := range []string{"none", "write", "probe"} {
		t.Run(fault, func(t *testing.T) {
			s, _ := fixture(t)
			old := configFixture(t, s)
			next := cloneConfig(old)
			if err := replaceVPN(next, exampleOutbound(t, "backup.example")); err != nil {
				t.Fatal(err)
			}
			calls := 0
			restart := func() error {
				calls++
				if calls == 1 {
					if fault == "write" {
						p := filepath.Join(s.root, "config-vpn.json")
						os.Remove(p)
						os.Mkdir(p, 0700)
						os.WriteFile(filepath.Join(p, "block"), []byte("x"), 0600)
					}
					return errors.New("new channel failed")
				}
				return nil
			}
			probe := func() error {
				if fault == "probe" {
					return errors.New("old tunnel not responding")
				}
				return nil
			}
			err := s.commitProfileSwitch(next, jsonBytes(old), "backup", restart, probe)
			if err == nil {
				t.Fatal("failed switch reported success")
			}
			if s.pendingRecovery() != (fault != "none") {
				t.Fatal("incorrect journal retention", fault)
			}
			if fault == "none" {
				restored, e := s.readVPNConfig()
				if e != nil || string(jsonBytes(restored)) != string(jsonBytes(old)) {
					t.Fatal("old config not restored")
				}
			}
		})
	}
}

func TestCorePreflightAccountsForLargerAndCompressedBinary(t *testing.T) {
	const mib = int64(1 << 20)
	for _, tc := range []struct {
		free, reclaim, next int64
		want                bool
	}{
		{5 * mib, 10 * mib, 20 * mib, false}, {5 * mib, 10 * mib, 10 * mib, true},
		{3 * mib, 40 * mib, 10 * mib, false}, {5 * mib, 0, 10 * mib, false},
		{20 * mib, 0, 10 * mib, true}, {20 * mib, 0, 0, false},
	} {
		if coreSpaceEnough(tc.free, tc.reclaim, tc.next) != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestDNSHelperSkipsRestartAfterAnySuccessfulRetry(t *testing.T) {
	for success := 0; success <= 3; success++ {
		calls, waits := 0, 0
		got := dnsFailureConfirmed(func() error {
			calls++
			if calls == success+1 {
				return nil
			}
			return errors.New("timeout")
		}, func() { waits++ })
		if got != (success == 3) || waits != calls-1 {
			t.Fatal(success, got, calls, waits)
		}
	}
}

func TestRecoveryModeAllowsLoginAndReadsButBlocksMutation(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	origin := "http://" + s.host
	journal := s.corePath("xray-update-journal.json")
	if err := atomicWrite(journal, jsonBytes(coreJournal{Backup: filepath.Join(s.root, "missing-usb"), Hash: "bad"})); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/power", "/api/github-install", "/api/cleanup-now", "/api/profile-switch"} {
		w := request(s, "POST", path, `{}`, cookie, csrf, origin, s.host)
		if w.Code != 409 {
			t.Fatal(path, w.Code)
		}
	}
	if w := request(s, "GET", "/api/state", "", cookie, "", "", s.host); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", "/api/recovery", `{"password":"old-password-123"}`, cookie, csrf, origin, s.host); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !s.pendingRecovery() {
		t.Fatal("failed recovery discarded journal")
	}
	os.Remove(journal)
	if w := request(s, "POST", "/api/recovery", `{"password":"old-password-123"}`, cookie, csrf, origin, s.host); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestGitHubRetriesTransientResponsesAndStopsOnCancel(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504, 403, 404} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls, waits := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(code)
				} else {
					w.WriteHeader(200)
				}
			}))
			defer server.Close()
			resp, err := githubRequestWith(context.Background(), server.Client(), server.URL, func(context.Context) error { waits++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if code >= 500 {
				if calls != 2 || waits != 1 || resp.StatusCode != 200 {
					t.Fatal(calls, waits, resp.StatusCode)
				}
			} else if calls != 1 || waits != 0 {
				t.Fatal("retried permanent error")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	_, err := githubRequestWith(ctx, server.Client(), server.URL, func(context.Context) error { cancel(); return ctx.Err() })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestInterruptedProfileRecoveryKeepsJournalUntilHTTPSWorks(t *testing.T) {
	s, _ := fixture(t)
	cfg := configFixture(t, s)
	if err := s.writeProfileJournal(jsonBytes(cfg)); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverProfileSwitchWith(func() error { return nil }, func() error { return errors.New("timeout") }); err == nil || !s.pendingRecovery() {
		t.Fatal("unverified recovery discarded journal")
	}
	if err := s.recoverProfileSwitchWith(func() error { return nil }, func() error { return nil }); err != nil || s.pendingRecovery() {
		t.Fatal("verified recovery did not finish", err)
	}
}
