package panel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const cleanupAge = 7 * 24 * time.Hour

var managedTempName = regexp.MustCompile(`^(?:darkls-update-[0-9]+|darkls-github-[0-9]+\.zip|darkls-candidate-[0-9]+\.json)$`)
var coreBackupName = regexp.MustCompile(`^xray-before-[0-9.]+-[0-9]+$`)
var managedCoreStage = regexp.MustCompile(`^\.core-stage-[0-9]+$`)

type CleanupSettings struct {
	Enabled bool `json:"enabled"`
}
type CleanupResult struct {
	At      int64  `json:"at"`
	Removed int    `json:"removed"`
	Bytes   int64  `json:"bytes"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) cleanupSettings() CleanupSettings {
	p := CleanupSettings{Enabled: true}
	_ = readJSON(s.corePath("cleanup-settings.json"), &p)
	return p
}
func (s *Server) pendingRecovery() bool {
	for _, n := range []string{"apply-journal.json", "xray-update-journal.json", "profile-switch-journal.json"} {
		if _, e := os.Stat(s.corePath(n)); e == nil || !os.IsNotExist(e) {
			return true
		}
	}
	return false
}
func newestItem(items []StorageItem, kind string) string {
	name := ""
	last := time.Time{}
	for _, item := range items {
		if item.Kind != kind {
			continue
		}
		st, e := os.Lstat(item.path)
		if e == nil && (name == "" || st.ModTime().After(last) || (st.ModTime().Equal(last) && item.path > name)) {
			name = item.path
			last = st.ModTime()
		}
	}
	return name
}
func cleanupProtected(items []StorageItem, previous string) map[string]bool {
	out := map[string]bool{}
	for _, kind := range []string{"settings", "xray"} {
		if n := newestItem(items, kind); n != "" {
			out[n] = true
		}
	}
	if previous != "" {
		out[previous] = true
	}
	for _, item := range items {
		if item.Kind == "panel" {
			out[item.path] = true
		}
	}
	return out
}
func expiredItem(path string, now time.Time) bool {
	st, e := os.Lstat(path)
	if e != nil || st.Mode()&os.ModeSymlink != 0 || (!st.Mode().IsRegular() && !st.IsDir()) {
		return false
	}
	if now.Year() < 2025 || st.ModTime().Year() < 2025 || st.ModTime().After(now) {
		return false
	}
	if now.Sub(st.ModTime()) < cleanupAge {
		return false
	}
	fresh := false
	if st.IsDir() {
		_ = filepath.Walk(path, func(_ string, info os.FileInfo, e error) error {
			if e != nil {
				fresh = true
				return filepath.SkipAll
			}
			if info.ModTime().After(now.Add(-cleanupAge)) || info.ModTime().Year() < 2025 {
				fresh = true
				return filepath.SkipAll
			}
			return nil
		})
	}
	return !fresh
}
func (s *Server) cleanupOld(now time.Time) (CleanupResult, error) {
	result := CleanupResult{At: now.Unix()}
	if now.Year() < 2025 {
		return result, errors.New("часы ещё не синхронизированы")
	}
	if updateBusy() || s.pendingRecovery() || coreJobActive(s.coreJob()) {
		return result, errors.New("очистка отложена: выполняется операция или нужен откат")
	}
	items := s.storageItems()
	var previous coreJournal
	_ = readJSON(s.corePath("xray-previous.json"), &previous)
	protected := cleanupProtected(items, previous.Backup)
	for _, p := range []string{filepath.Join(s.dir, "update-rollback")} {
		items = append(items, StorageItem{Kind: "temporary", path: p})
	}
	if s.root == "/jffs/xray" {
		entries, _ := os.ReadDir("/tmp")
		for _, e := range entries {
			if managedTempName.MatchString(e.Name()) {
				items = append(items, StorageItem{Kind: "temporary", path: filepath.Join("/tmp", e.Name())})
			}
		}
	}
	dir := filepath.Join(coreUSB, "darkls-vpn-backups")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if managedCoreStage.MatchString(e.Name()) {
			items = append(items, StorageItem{Kind: "temporary", path: filepath.Join(dir, e.Name())})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	for _, item := range items {
		if protected[item.path] || !expiredItem(item.path, now) {
			continue
		}
		size := diskBytes(item.path)
		if e := os.RemoveAll(item.path); e != nil {
			result.Error = "Не все старые файлы удалось удалить"
			continue
		}
		result.Removed++
		result.Bytes += size
	}
	_ = atomicWrite(s.runtimePath("cleanup-status.json"), jsonBytes(result))
	if result.Removed > 0 {
		appendEvent("cleanup_auto", "Удалены известные лишние файлы старше недели; последние копии сохранены")
	}
	return result, nil
}
func (s *Server) cleanupLoop(ctx context.Context) {
	runPeriodic(ctx, 2*time.Minute, 6*time.Hour, func() {
		if s.cleanupSettings().Enabled && s.mutation.TryLock() {
			release, e := s.pause()
			if e == nil {
				_, _ = s.cleanupOld(time.Now())
				release()
			}
			s.mutation.Unlock()
		}
	})
}

func (s *Server) storageStatus() map[string]any {
	var last CleanupResult
	_ = readJSON(s.runtimePath("cleanup-status.json"), &last)
	return map[string]any{"items": s.storageItems(), "cleanup": s.cleanupSettings(), "last_cleanup": last, "retention_days": 7, "note": "Сохраняются последняя панель, последняя копия ядра и последний снимок настроек. Неизвестные файлы не удаляются. Обход каждые 6 часов после синхронизации времени."}
}
