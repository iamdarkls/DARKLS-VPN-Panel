package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const xrayTargetVersion = "26.10.10"

// Official Xray-linux-arm64-v8a.zip verified against GitHub release SHA256
// 58e339e67827220175b3a1c6f66f48ccd086b79209f30ba1c0bcb47885d4c22a.
const xrayTargetHash = "fadf8d792889536a9410686f3d5c4df76ec7865b7aa2297495365759ffeb424c"

type coreJournal struct {
	Backup   string `json:"backup"`
	Hash     string `json:"hash"`
	Disabled bool   `json:"disabled"`
	Version  string `json:"version,omitempty"`
}

func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyFileChecked(src, dst string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func (s *Server) corePath(name string) string { return filepath.Join(s.dir, name) }
func (s *Server) stopXrayProcess() error {
	for _, p := range s.mainXrayPIDs() {
		if _, e := s.run("kill", p); e != nil {
			return e
		}
	}
	for i := 0; i < 30; i++ {
		if len(s.mainXrayPIDs()) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Xray не остановился; замена отменена")
}
func (s *Server) stopCore() error {
	if out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "stop"); e != nil {
		return fmt.Errorf("stop routing: %s", limited(out))
	}
	_, _ = s.run("killall", "xray")
	for i := 0; i < 20; i++ {
		if _, e := s.run("pidof", "xray"); e != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("Xray did not stop; binary was not removed")
}
func (s *Server) restoreCore(j coreJournal) error {
	if e := validateCoreBackup(j); e != nil {
		return e
	}
	var e error
	if e = s.stopCore(); e != nil {
		return e
	}
	target := filepath.Join(s.root, "xray")
	// Only a verified persistent backup authorizes removal of the current binary.
	if e = os.Remove(target); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = copyFileChecked(j.Backup, target+".new", 0700); e != nil {
		return e
	}
	if e = os.Rename(target+".new", target); e != nil {
		return e
	}
	if !j.Disabled {
		if out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "start"); e != nil {
			return fmt.Errorf("previous Xray startup: %s", limited(out))
		}
	}
	return os.Remove(s.corePath("xray-update-journal.json"))
}
func (s *Server) recoverCoreJournal() error {
	var j coreJournal
	if e := readJSON(s.corePath("xray-update-journal.json"), &j); e != nil {
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}
	release, e := s.pause()
	if e != nil {
		return e
	}
	defer release()
	if e = readJSON(s.corePath("xray-update-journal.json"), &j); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	if e = s.restoreCore(j); e != nil {
		return e
	}
	appendEvent("xray", "Прерванное обновление Xray отменено; прежнее ядро восстановлено")
	return nil
}
func (s *Server) upgradeCore(candidate string) error {
	return s.installCore(candidate, xrayTargetVersion, xrayTargetHash)
}
func (s *Server) installCore(candidate, targetVersion, targetHash string) (err error) {
	release, e := s.pause()
	if e != nil {
		return e
	}
	defer release()
	if updateBusy() {
		return errors.New("panel update is active")
	}
	if s.pendingRecovery() {
		return errors.New("сначала завершите восстановление предыдущей операции")
	}

	h, e := fileHash(candidate)
	if e != nil {
		return e
	}
	if h != targetHash {
		return errors.New("Xray SHA256 не совпала с проверенным кандидатом")
	}
	if e = checkELF(candidate, "arm64"); e != nil {
		return e
	}
	if e = os.Chmod(candidate, 0700); e != nil {
		return e
	}
	out, e := s.run(candidate, "version")
	if e != nil || !strings.HasPrefix(string(out), "Xray "+targetVersion+" ") {
		return errors.New("unexpected Xray version")
	}
	if current := s.currentCoreVersion(); current != "" && !releaseAtLeast(targetVersion, current) {
		return errors.New("понижение версии Xray запрещено")
	}
	out, e = s.run(candidate, "run", "-test", "-config", filepath.Join(s.root, "config-vpn.json"))
	if e != nil {
		return fmt.Errorf("new core rejects configuration: %s", limited(out))
	}
	const usb = "/tmp/mnt/WINDOWS_10"
	// A different st_dev proves this is a mounted volume, not a RAM directory.
	var us, parent syscall.Stat_t
	if syscall.Stat(usb, &us) != nil || syscall.Stat(filepath.Dir(usb), &parent) != nil || us.Dev == parent.Dev {
		return errors.New("USB WINDOWS_10 must be mounted for persistent rollback")
	}
	target := filepath.Join(s.root, "xray")
	old, e := os.Stat(target)
	if e != nil {
		return e
	}
	next, e := os.Stat(candidate)
	if e != nil {
		return e
	}
	var fs syscall.Statfs_t
	if e = syscall.Statfs(s.root, &fs); e != nil {
		return e
	}
	if old.Size() <= 0 || next.Size() <= 0 || int64(fs.Bavail)*int64(fs.Bsize) < 4<<20 {
		return errors.New("нужно минимум 4 МиБ свободного JFFS; резервная копия хранится на USB")
	}

	backupDir := filepath.Join(usb, "darkls-vpn-backups")
	if e = os.MkdirAll(backupDir, 0700); e != nil {
		return e
	}
	backup := filepath.Join(backupDir, fmt.Sprintf("xray-before-%s-%d", targetVersion, time.Now().UnixNano()))
	hash, e := fileHash(target)
	if e != nil {
		return e
	}
	if e = copyFileChecked(target, backup, 0600); e != nil {
		return e
	}
	bh, e := fileHash(backup)
	if e != nil || bh != hash {
		return errors.New("USB backup verification failed")
	}
	if d, e := os.Open(backupDir); e == nil {
		_ = d.Sync()
		d.Close()
	}
	j := coreJournal{Backup: backup, Hash: hash, Disabled: s.disabled(), Version: s.currentCoreVersion()}
	journal := s.corePath("xray-update-journal.json")
	if e = atomicWrite(journal, jsonBytes(j)); e != nil {
		return e
	}
	changed := false
	defer func() {
		if err != nil {
			if !changed {
				_ = os.Remove(journal)
				return
			}
			if re := s.restoreCore(j); re != nil {
				err = fmt.Errorf("%v; automatic rollback failed: %v; journal retained", err, re)
			} else {
				err = fmt.Errorf("%v; previous Xray restored", err)
			}
		}
	}()
	if e = s.stopXrayProcess(); e != nil {
		return e
	}
	changed = true
	if e = os.Remove(target); e != nil {
		return e
	}
	if e = copyFileChecked(candidate, target+".new", 0700); e != nil {
		return e
	}
	if got, e := fileHash(target + ".new"); e != nil || got != targetHash {
		return errors.New("installed Xray integrity check failed")
	}
	if e = os.Rename(target+".new", target); e != nil {
		return e
	}

	if !j.Disabled {
		if e = s.startMainXray(); e != nil {
			return e
		}
		if e = s.checkRouter(); e != nil {
			return fmt.Errorf("после обновления DNS/правила не прошли проверку: %w", e)
		}
		started := time.Now()
		if e = checkVPNConnection(); e != nil {
			time.Sleep(2 * time.Second)
			if e = checkVPNConnection(); e != nil {
				recordProbe(e, time.Since(started))
				return e
			}
		}
		recordProbe(nil, time.Since(started))
	}
	if e = syscall.Statfs(s.root, &fs); e != nil || int64(fs.Bavail)*int64(fs.Bsize) < 2<<20 {
		return errors.New("после записи ядра осталось меньше 2 МиБ; выполняем откат")
	}

	if e = atomicWrite(s.corePath("xray-previous.json"), jsonBytes(j)); e != nil {
		return e
	}
	if e = os.Remove(journal); e != nil {
		return e
	}
	appendEvent("xray", "Xray обновлён до "+targetVersion+"; прежнее ядро сохранено на USB")
	fmt.Println("Xray " + targetVersion + " installed; previous binary: " + backup)
	return nil
}

func validateCoreBackup(j coreJournal) error {
	h, e := fileHash(j.Backup)
	if e != nil || h != j.Hash {
		return errors.New("previous Xray backup unavailable or corrupt; keep USB connected")
	}
	return nil
}
