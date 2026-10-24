package panel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Callers hold mutation for in-process exclusion; pause adds the cross-process
// flock. Journal first, mutate second, and keep recovery data until success.
var managedFiles = []string{"domains.txt", "dnsmasq-vpn.conf", "telegram-networks.txt", "panel/state.json"}

func (s *Server) revision() string {
	h := sha256.New()
	for _, f := range managedFiles {
		b, _ := os.ReadFile(filepath.Join(s.root, f))
		h.Write([]byte(f))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) backup() (string, error) {
	now := time.Now()
	id := fmt.Sprintf("%s-%09d-%s", now.Format("20060102-150405"), now.Nanosecond(), token()[:8])
	dir := filepath.Join(s.dir, "backups", id)
	if e := os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	for _, f := range managedFiles {
		b, e := os.ReadFile(filepath.Join(s.root, f))
		if e != nil {
			return "", e
		}
		if e = atomicWrite(filepath.Join(dir, filepath.Base(f)), b); e != nil {
			return "", e
		}
	}
	return id, nil
}

func (s *Server) restoreFiles(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("неверная резервная копия")
	}
	for _, f := range managedFiles {
		b, e := os.ReadFile(filepath.Join(s.dir, "backups", id, filepath.Base(f)))
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(s.root, f), b); e != nil {
			return e
		}
	}
	return nil
}

func (s *Server) prune() {
	entries, _ := os.ReadDir(filepath.Join(s.dir, "backups"))
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && idPattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > 12 {
		_ = os.RemoveAll(filepath.Join(s.dir, "backups", names[0]))
		names = names[1:]
	}
}

func (s *Server) disabled() bool {
	_, e := os.Stat(filepath.Join(s.root, "autostart.disabled"))
	return e == nil
}

func (s *Server) pause() (func(), error) {
	lock, e := os.OpenFile(filepath.Join(s.dir, "apply.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		_ = lock.Close()
		return nil, errors.New("другая операция уже выполняется")
	}
	// Kernel-managed locks are released even if the process crashes or the router reboots.
	release := func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }
	deadline := time.Now().Add(45 * time.Second)
	for managerRunning(s.root) {
		if time.Now().After(deadline) {
			release()
			return nil, errors.New("скрипт VPN занят; повторите позже")
		}
		if err := waitContext(s.networkContext(), 250*time.Millisecond); err != nil {
			release()
			return nil, err
		}
	}
	return release, nil
}

func managerRunning(root string) bool {
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if e != nil {
			continue
		}
		args := strings.Split(string(b), "\x00")
		if len(args) > 1 && args[1] == filepath.Join(root, "vpn.sh") {
			return true
		}
	}
	return false
}

func (s *Server) restart() error {
	if out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "stop"); e != nil {
		return fmt.Errorf("остановка маршрутизации: %s", limited(out))
	}
	if s.disabled() {
		return nil
	}
	// Cached client DNS answers remain in use across process restarts. Preserve
	// their VPN and YouTube address sets instead of silently bypassing the VPN.
	if e := s.syncTelegramSet(); e != nil {
		return e
	}
	out, e := s.run("sh", filepath.Join(s.root, "vpn.sh"), "start")
	if e != nil {
		return fmt.Errorf("запуск VPN: %s", limited(out))
	}
	return nil
}

func (s *Server) apply(st State, expected string) (backup string, err error) {
	st, err = validate(st)
	if err != nil {
		return "", err
	}
	if expected != s.revision() {
		return "", errors.New("CONFLICT: настройки изменились; обновите страницу")
	}
	config := dnsConfigForState(st)
	tmp := filepath.Join(s.dir, "dnsmasq-candidate.conf")
	if err = atomicWrite(tmp, config); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	if out, e := s.run("dnsmasq", "--test", "--conf-file="+tmp); e != nil {
		return "", fmt.Errorf("проверка DNS: %s", limited(out))
	}
	release, e := s.pause()
	if e != nil {
		return "", e
	}
	defer release()
	// Recheck under the interprocess lock: another process may have committed
	// between validation and acquiring the lock.
	if expected != s.revision() {
		return "", errors.New("CONFLICT: настройки изменились; обновите страницу")
	}
	oldConfig, err := os.ReadFile(filepath.Join(s.root, "dnsmasq-vpn.conf"))
	if err != nil {
		return "", err
	}
	oldState, err := s.state()
	if err != nil {
		return "", err
	}
	oldTG, err := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
	if err != nil {
		return "", err
	}
	routingChanged := policyChanged(oldState, st)
	if !s.disabled() && (routingChanged || !bytes.Equal(oldConfig, config)) {
		if err = s.ensureGroupSets(st); err != nil {
			return "", err
		}
	}
	backup, err = s.backup()
	if err != nil {
		return "", err
	}
	journal := filepath.Join(s.dir, "apply-journal.json")
	if err = atomicWrite(journal, jsonBytes(map[string]string{"backup": backup})); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			re := s.restoreFiles(backup)
			rr := s.reloadRouting(true, true)
			if re == nil {
				if pe := s.syncGroupRouting(); pe != nil && rr == nil {
					rr = pe
				}
			}
			if de := s.syncDevices(true); de != nil && rr == nil {
				rr = de
			}
			if re != nil || rr != nil {
				err = fmt.Errorf("%v; восстановление: файлы=%v, VPN=%v", err, re, rr)
				return
			}
			err = fmt.Errorf("%v; прежние настройки восстановлены", err)
		}
		_ = os.Remove(journal)
	}()
	domains := strings.Join(activeDomains(st), "\n")
	if domains != "" {
		domains += "\n"
	}
	tg := []byte{}
	for _, g := range st.Groups {
		if g.ID == "telegram" && g.Enabled {
			tg, err = os.ReadFile(filepath.Join(s.dir, "telegram-original.txt"))
			if err != nil {
				return backup, err
			}
		}
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{"domains.txt", []byte(domains)}, {"dnsmasq-vpn.conf", config}, {"telegram-networks.txt", tg}, {"panel/state.json", jsonBytes(st)}} {
		if err = atomicWrite(filepath.Join(s.root, f.name), f.data); err != nil {
			return backup, err
		}
	}
	if err = s.reloadRouting(!bytes.Equal(oldConfig, config), !bytes.Equal(oldTG, tg)); err != nil {
		return backup, err
	}
	if !bytes.Equal(jsonBytes(oldState.Policy), jsonBytes(st.Policy)) {
		if err = s.syncDevices(true); err != nil {
			return backup, err
		}
	}
	if routingChanged || !bytes.Equal(oldConfig, config) {
		if err = s.syncGroupRouting(); err != nil {
			return backup, err
		}
	}
	appendEvent("settings", "Настройки сайтов и устройств применены")
	s.prune()
	return backup, nil
}

func (s *Server) recoverJournal() error {
	path := filepath.Join(s.dir, "apply-journal.json")
	var j map[string]string
	e := readJSON(path, &j)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	release, e := s.pause()
	if e != nil {
		return e
	}
	defer release()
	if e = s.restoreFiles(j["backup"]); e != nil {
		return e
	}
	if e = s.restart(); e != nil {
		return e
	}
	return os.Remove(path)
}
