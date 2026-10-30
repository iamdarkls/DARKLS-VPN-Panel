package panel

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Offline recovery is explicit and limited to server-switch journals. A missing
// core backup or incomplete site transaction must retain its normal safeguards.
func (s *Server) offlineRecoveryAvailable() bool {
	if st, err := os.Stat(s.corePath("profile-switch-journal.json")); err != nil || !st.Mode().IsRegular() {
		return false
	}
	for _, name := range []string{"xray-update-journal.json", "apply-journal.json"} {
		if _, err := os.Stat(s.corePath(name)); !os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// Callers own mutation and diagnosticMu; hold the cross-process lock throughout
// backup, route removal and file restoration. Never run an HTTPS probe here.
func (s *Server) recoverProfileOffline() (string, error) {
	release, err := s.pause()
	if err != nil {
		return "", err
	}
	defer release()
	if !s.offlineRecoveryAvailable() {
		return "", errors.New("восстановление с выключенным VPN доступно только для незавершённого переключения сервера; обновление ядра и применение сайтов восстанавливаются отдельно")
	}
	backup, err := s.backupOfflineProfile()
	if err != nil {
		return "", fmt.Errorf("резервная копия не создана; настройки не изменены: %w", err)
	}
	// Persist the user's explicit OFF choice before stopping routes. Any later
	// failure retains both OFF and the original journal for another safe retry.
	if err = atomicWrite(filepath.Join(s.root, "autostart.disabled"), []byte("disabled for offline recovery\n")); err != nil {
		return backup, err
	}
	if _, err = s.run("sh", filepath.Join(s.root, "vpn.sh"), "stop"); err != nil {
		return backup, errors.New("не удалось остановить маршрутизацию; VPN отмечен выключенным, резервная копия и журнал сохранены")
	}
	if err = s.restoreProfileFiles(); err != nil {
		return backup, err
	}
	if err = os.Remove(s.corePath("profile-switch-journal.json")); err != nil {
		return backup, err
	}
	appendEvent("recovery_offline", "Файлы подключения восстановлены; VPN оставлен выключенным. Выберите и проверьте рабочий сервер перед включением. Копия: panel/recovery-snapshots/"+backup)
	return backup, nil
}

func (s *Server) backupOfflineProfile() (name string, err error) {
	parent := s.corePath("recovery-snapshots")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	if len(entries) >= 8 {
		return "", errors.New("уже сохранено восемь снимков восстановления; освободите место в panel/recovery-snapshots")
	}
	dir, err := os.MkdirTemp(parent, "profile-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	files := []struct {
		name, path string
		required   bool
	}{
		{"config-vpn.json", filepath.Join(s.root, "config-vpn.json"), true},
		{"profile-switch-journal.json", s.corePath("profile-switch-journal.json"), true},
		{"active-profile.json", s.corePath("active-profile.json"), false},
		{"primary.json", s.profilePath("primary"), false},
		{"backup.json", s.profilePath("backup"), false},
		{"autostart.disabled", filepath.Join(s.root, "autostart.disabled"), false},
	}
	present := map[string]bool{}
	for _, file := range files {
		f, e := os.Open(file.path)
		if os.IsNotExist(e) && !file.required {
			present[file.name] = false
			continue
		}
		if e != nil {
			return "", e
		}
		data, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
		_ = f.Close()
		if e != nil {
			return "", e
		}
		if len(data) > 2<<20 {
			return "", errors.New("файл восстановления превышает 2 МиБ")
		}
		if err = atomicWrite(filepath.Join(dir, file.name), data); err != nil {
			return "", err
		}
		present[file.name] = true
	}
	if err = atomicWrite(filepath.Join(dir, "present.json"), jsonBytes(present)); err != nil {
		return "", err
	}
	return filepath.Base(dir), nil
}
