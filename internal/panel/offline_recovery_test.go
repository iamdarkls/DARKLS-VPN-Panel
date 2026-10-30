package panel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pendingProfileFixture(t *testing.T) (*Server, *fakeRunner, []byte, []byte) {
	t.Helper()
	s, r := fixture(t)
	old := configFixture(t, s)
	if err := os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(s.profilePath("primary"), jsonBytes(exampleOutbound(t, "old.example"))); err != nil {
		t.Fatal(err)
	}
	if err := s.writeProfileJournal(jsonBytes(old)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.corePath("profile-switch-journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	next := cloneConfig(old)
	if err = replaceVPN(next, exampleOutbound(t, "unreachable.example")); err != nil {
		t.Fatal(err)
	}
	if err = atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(next)); err != nil {
		t.Fatal(err)
	}
	return s, r, jsonBytes(old), before
}

func TestOfflineProfileRecoveryBreaksUnreachableRollbackLoop(t *testing.T) {
	s, r, old, journal := pendingProfileFixture(t)
	// The old server can also be unreachable: ordinary recovery keeps its journal.
	probes := 0
	if err := s.recoverProfileSwitchWith(func() error { return nil }, func() error { probes++; return errors.New("old server unavailable") }); err == nil {
		t.Fatal("unreachable server accepted")
	}
	if !s.pendingRecovery() || probes != 1 {
		t.Fatal("failed recovery lost journal")
	}
	name, err := s.recoverProfileOffline()
	if err != nil {
		t.Fatal(err)
	}
	if !s.disabled() || s.pendingRecovery() {
		t.Fatal("offline choice or journal completion lost")
	}
	got, _ := os.ReadFile(filepath.Join(s.root, "config-vpn.json"))
	var want, restored map[string]any
	_ = json.Unmarshal(old, &want)
	_ = json.Unmarshal(got, &restored)
	if string(jsonBytes(restored)) != string(jsonBytes(want)) {
		t.Fatal("config not restored")
	}
	saved := filepath.Join(s.corePath("recovery-snapshots"), name)
	b, err := os.ReadFile(filepath.Join(saved, "profile-switch-journal.json"))
	if err != nil || string(b) != string(journal) {
		t.Fatal("original recovery journal not backed up")
	}
	info, _ := os.Stat(saved)
	if info.Mode().Perm() != 0700 {
		t.Fatal("backup directory not private")
	}
	info, _ = os.Stat(filepath.Join(saved, "profile-switch-journal.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("credentials backup not private")
	}
	if len(r.calls) != 1 || !strings.HasSuffix(r.calls[0], "/vpn.sh stop") {
		t.Fatal("offline recovery started VPN or ran unexpected commands", r.calls)
	}
	if probes != 1 {
		t.Fatal("offline recovery probed old server")
	}
}

func TestOfflineRecoveryRefusesOtherPendingOperations(t *testing.T) {
	for _, other := range []string{"xray-update-journal.json", "apply-journal.json"} {
		t.Run(other, func(t *testing.T) {
			s, r, _, _ := pendingProfileFixture(t)
			atomicWrite(s.corePath(other), []byte(`{}`))
			if s.offlineRecoveryAvailable() {
				t.Fatal("unsafe recovery offered")
			}
			if _, err := s.recoverProfileOffline(); err == nil {
				t.Fatal("other operation bypassed")
			}
			if s.disabled() || len(r.calls) != 0 || !s.pendingRecovery() {
				t.Fatal("rejected request changed router")
			}
		})
	}
}

type failedOfflineStop struct{ Runner }

func (r failedOfflineStop) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "sh" && len(args) > 1 && args[1] == "stop" {
		return []byte("secret diagnostic"), errors.New("stop failed")
	}
	return r.Runner.Run(ctx, name, args...)
}
func TestOfflineRecoveryKeepsJournalAndSnapshotWhenStopFails(t *testing.T) {
	s, r, _, journal := pendingProfileFixture(t)
	s.runner = failedOfflineStop{r}
	before, _ := os.ReadFile(filepath.Join(s.root, "config-vpn.json"))
	backup, err := s.recoverProfileOffline()
	if err == nil || strings.Contains(err.Error(), "secret diagnostic") || backup == "" {
		t.Fatal("incorrect stop failure", backup, err)
	}
	after, _ := os.ReadFile(filepath.Join(s.root, "config-vpn.json"))
	b, _ := os.ReadFile(s.corePath("profile-switch-journal.json"))
	if !s.disabled() || !s.pendingRecovery() || string(before) != string(after) || string(b) != string(journal) {
		t.Fatal("failed stop modified recoverable state")
	}
}
func TestOfflineRecoveryBackupFailureMakesNoChanges(t *testing.T) {
	s, r, _, _ := pendingProfileFixture(t)
	atomicWrite(s.corePath("recovery-snapshots"), []byte("not a directory"))
	if _, err := s.recoverProfileOffline(); err == nil {
		t.Fatal("backup failure ignored")
	}
	if s.disabled() || len(r.calls) != 0 || !s.pendingRecovery() {
		t.Fatal("backup failure changed VPN")
	}
}
func TestOfflineRecoveryRequiresAuthenticationPasswordAndCSRF(t *testing.T) {
	s, _, _, _ := pendingProfileFixture(t)
	cookie, csrf := loginTest(t, s)
	origin := "http://" + s.host
	body := `{"password":"old-password-123","offline":true}`
	if w := request(s, "POST", "/api/recovery", body, nil, "", origin, s.host); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", "/api/recovery", body, cookie, "", origin, s.host); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", "/api/recovery", `{"password":"wrong","offline":true}`, cookie, csrf, origin, s.host); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if s.disabled() || !s.pendingRecovery() {
		t.Fatal("unauthorized mutation")
	}
	if w := request(s, "POST", "/api/recovery", body, cookie, csrf, origin, s.host); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !s.disabled() || s.pendingRecovery() {
		t.Fatal("offline recovery did not unlock server selection")
	}
}

func TestOfflineRecoveryRetainsCorruptJournalAndBackup(t *testing.T) {
	s, _, _, _ := pendingProfileFixture(t)
	atomicWrite(s.corePath("profile-switch-journal.json"), []byte("invalid journal"))
	backup, err := s.recoverProfileOffline()
	if err == nil || backup == "" || !s.pendingRecovery() || !s.disabled() {
		t.Fatal("invalid journal discarded", backup, err)
	}
	b, e := os.ReadFile(filepath.Join(s.corePath("recovery-snapshots"), backup, "profile-switch-journal.json"))
	if e != nil || string(b) != "invalid journal" {
		t.Fatal("recovery evidence lost")
	}
}

func TestDisabledProfileRecoveryFinishesInterruptedStop(t *testing.T) {
	s, r, _, _ := pendingProfileFixture(t)
	atomicWrite(filepath.Join(s.root, "autostart.disabled"), []byte("OFF persisted before crash"))
	if err := s.recoverProfileSwitchWith(func() error { t.Fatal("started disabled VPN"); return nil }, func() error { t.Fatal("probed offline server"); return nil }); err != nil {
		t.Fatal(err)
	}
	if !s.disabled() || s.pendingRecovery() || len(r.calls) != 1 || !strings.HasSuffix(r.calls[0], "/vpn.sh stop") {
		t.Fatal("interrupted OFF not completed", r.calls)
	}
}
