package panel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func agedFile(t *testing.T, path string, at time.Time) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("known backup"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chtimes(path, at, at); e != nil {
		t.Fatal(e)
	}
}
func TestWeeklyCleanupKeepsLatestSnapshotsUnknownFilesAndPanel(t *testing.T) {
	s, _ := fixture(t)
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	older := now.Add(-10 * 24 * time.Hour)
	for _, id := range []string{"old-snapshot", "last-snapshot"} {
		p := filepath.Join(s.dir, "backups", id, "state.json")
		at := older
		if id == "last-snapshot" {
			at = old
		}
		agedFile(t, p, at)
		os.Chtimes(filepath.Dir(p), at, at)
	}
	atomicWrite(s.corePath("auth.json"), []byte("private credentials"))
	panel := filepath.Join(s.dir, "darkls-panel.previous")
	agedFile(t, panel, older)
	unknown := filepath.Join(s.dir, "do-not-delete.zip")
	agedFile(t, unknown, older)
	result, e := s.cleanupOld(now)
	if e != nil {
		t.Fatal(e)
	}
	if result.Removed != 1 {
		t.Fatal(result)
	}
	for _, p := range []string{panel, unknown, filepath.Join(s.dir, "backups", "last-snapshot", "state.json"), s.corePath("auth.json"), filepath.Join(s.root, "dnsmasq-vpn.conf")} {
		if _, e := os.Stat(p); e != nil {
			t.Fatal("protected file removed", p, e)
		}
	}
	if _, e := os.Stat(filepath.Join(s.dir, "backups", "old-snapshot")); !os.IsNotExist(e) {
		t.Fatal("old redundant snapshot retained")
	}
}
func TestCleanupRejectsUntrustedClockFreshChildAndSymlink(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	p := filepath.Join(root, "old")
	agedFile(t, p, now.Add(-8*24*time.Hour))
	if expiredItem(p, now.Add(-25*time.Hour)) {
		t.Fatal("seven-day boundary too early")
	}
	if !expiredItem(p, now) {
		t.Fatal("old file retained")
	}
	if expiredItem(p, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("unsynchronized clock accepted")
	}
	link := filepath.Join(root, "link")
	os.Symlink(p, link)
	if expiredItem(link, now) {
		t.Fatal("symlink accepted")
	}
	dir := filepath.Join(root, "old-dir")
	agedFile(t, filepath.Join(dir, "fresh-child"), now)
	os.Chtimes(dir, now.Add(-9*24*time.Hour), now.Add(-9*24*time.Hour))
	if expiredItem(dir, now) {
		t.Fatal("fresh nested data would be deleted")
	}
}
func TestCleanupPendingJournalPreventsDeletion(t *testing.T) {
	s, _ := fixture(t)
	atomicWrite(s.corePath("profile-switch-journal.json"), []byte("pending"))
	if _, e := s.cleanupOld(time.Now()); e == nil {
		t.Fatal("cleaned during recovery")
	}
	os.Remove(s.corePath("profile-switch-journal.json"))
	atomicWrite(s.runtimePath("core-job.json"), jsonBytes(CoreJob{ID: "running", Phase: "downloading"}))
	if _, e := s.cleanupOld(time.Now()); e == nil {
		t.Fatal("cleaned active core staging")
	}
}
func TestCoreCleanupProtectsJournalPointerAndNewestCopy(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	items := []StorageItem{{Kind: "xray", path: filepath.Join(root, "older")}, {Kind: "xray", path: filepath.Join(root, "newer")}, {Kind: "xray", path: filepath.Join(root, "unreferenced")}}
	for i, item := range items {
		agedFile(t, item.path, now.Add(time.Duration(i-10)*24*time.Hour))
	}
	protected := cleanupProtected(items, items[0].path)
	if !protected[items[0].path] || !protected[items[2].path] || protected[items[1].path] {
		t.Fatal(protected)
	}
}
