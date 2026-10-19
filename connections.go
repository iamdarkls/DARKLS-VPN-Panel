package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type SavedConnection struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Outbound map[string]any `json:"outbound"`
	Updated  int64          `json:"updated_at"`
}

func (s *Server) connections() ([]SavedConnection, error) {
	list := []SavedConnection{}
	e := readJSON(filepath.Join(s.dir, "connections.json"), &list)
	if os.IsNotExist(e) {
		return list, nil
	}
	if e != nil || len(list) > 20 {
		return nil, errors.New("не удалось прочитать сохранённые подключения")
	}
	return list, nil
}
func (s *Server) writeConnections(list []SavedConnection) error {
	return atomicWrite(filepath.Join(s.dir, "connections.json"), jsonBytes(list))
}
func outboundHash(out map[string]any) string {
	sum := sha256.Sum256(jsonBytes(out))
	return hex.EncodeToString(sum[:])
}
func connectionMeta(out map[string]any) map[string]any {
	stream, _ := out["streamSettings"].(map[string]any)
	return map[string]any{"protocol": out["protocol"], "transport": stream["network"], "security": stream["security"]}
}
func (s *Server) connectionStatus() (map[string]any, error) {
	list, e := s.connections()
	if e != nil {
		return nil, e
	}
	cfg, e := s.readVPNConfig()
	current := map[string]any{}
	if e == nil {
		current, _ = vpnOutbound(cfg)
	}
	var primary map[string]any
	_ = readJSON(s.profilePath("primary"), &primary)
	rows := []map[string]any{}
	for _, item := range list {
		row := connectionMeta(item.Outbound)
		row["id"] = item.ID
		row["name"] = item.Name
		row["updated_at"] = item.Updated
		row["active"] = len(current) > 0 && outboundHash(item.Outbound) == outboundHash(current)
		row["primary"] = len(primary) > 0 && outboundHash(item.Outbound) == outboundHash(primary)
		rows = append(rows, row)
	}
	_, previousErr := os.Stat(filepath.Join(s.dir, "previous-connection.json"))
	return map[string]any{"rollback_available": previousErr == nil, "items": rows, "current": connectionMeta(current), "limit": 20, "profile": s.profileStatus()}, nil
}
func (s *Server) connectionOutbound(id string) (map[string]any, error) {
	if id == "previous" {
		var cfg map[string]any
		if e := readJSON(filepath.Join(s.dir, "previous-connection.json"), &cfg); e != nil {
			return nil, errors.New("предыдущее подключение отсутствует")
		}
		return vpnOutbound(cfg)
	}
	if id == "current" || id == "primary" {
		if id == "primary" {
			var out map[string]any
			if readJSON(s.profilePath("primary"), &out) == nil {
				return out, nil
			}
		}
		cfg, e := s.readVPNConfig()
		if e != nil {
			return nil, e
		}
		return vpnOutbound(cfg)
	}
	if !idPattern.MatchString(id) {
		return nil, errors.New("неверное подключение")
	}
	list, e := s.connections()
	if e != nil {
		return nil, e
	}
	for _, item := range list {
		if item.ID == id {
			return cloneConfig(item.Outbound), nil
		}
	}
	return nil, errors.New("подключение не найдено")
}
func (s *Server) saveConnection(id, name string, out map[string]any) error {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 60 || strings.ContainsAny(name, "\r\n\x00") || strings.Contains(name, "://") {
		return errors.New("название подключения: 1–60 символов без VPN-ссылки")
	}
	list, e := s.connections()
	if e != nil {
		return e
	}
	if id != "" && !idPattern.MatchString(id) {
		return errors.New("неверное подключение")
	}
	if id == "" {
		if len(list) >= 20 {
			return errors.New("не более 20 подключений")
		}
		list = append(list, SavedConnection{ID: "server-" + token()[:16], Name: name, Outbound: out, Updated: time.Now().Unix()})
	} else {
		found := false
		for i := range list {
			if list[i].ID == id {
				list[i].Name = name
				list[i].Outbound = out
				list[i].Updated = time.Now().Unix()
				found = true
				break
			}
		}
		if !found {
			return errors.New("подключение не найдено")
		}
	}
	return s.writeConnections(list)
}

// The same journal covers config, primary slot and active-profile metadata.
// A failed rollback retains it for recovery at the next panel startup.
func (s *Server) commitPrimary(out map[string]any, restart, probe func() error) (err error) {
	if s.pendingRecovery() {
		return errors.New("сначала завершите восстановление предыдущей операции")
	}
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	original := jsonBytes(cfg)
	if e = replaceVPN(cfg, out); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700); e != nil {
		return e
	}
	journal := filepath.Join(s.dir, "profile-switch-journal.json")
	if e = s.writeProfileJournal(original); e != nil {
		return e
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		restore := s.restoreProfileFiles()
		if restore == nil && !s.disabled() {
			restore = restart()
			if restore == nil {
				restore = probe()
			}
		}
		if restore == nil {
			_ = os.Remove(journal)
		} else {
			err = errors.New("переключение не завершено; журнал отката сохранён для восстановления")
		}
	}()
	if e = atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(cfg)); e != nil {
		return e
	}
	if !s.disabled() {
		if e = restart(); e == nil {
			e = probe()
		}
		if e != nil {
			return errors.New("новое подключение не прошло проверку; выполнен откат")
		}
	}
	if e = atomicWrite(s.profilePath("primary"), jsonBytes(out)); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(s.dir, "active-profile.json"), jsonBytes(map[string]any{"name": "primary", "at": time.Now().Unix()})); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(s.dir, "previous-connection.json"), original); e != nil {
		return e
	}
	if e = os.Remove(journal); e != nil {
		return e
	}
	committed = true
	appendEvent("connection", "Основное подключение проверено и изменено")
	return nil
}
func (s *Server) activateConnection(ctx context.Context, out map[string]any) error {
	if e := s.testOutbound(ctx, out); e != nil {
		return errors.New("новое подключение не прошло изолированную HTTPS-проверку; рабочее подключение сохранено")
	}
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	if e = replaceVPN(cfg, out); e != nil {
		return e
	}
	// Validate the full config too: existing inbound/routing settings are preserved.
	f, e := os.CreateTemp("/tmp", "darkls-candidate-*.json")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	_, e = f.Write(jsonBytes(cfg))
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if _, e = s.runner.Run(ctx, filepath.Join(s.root, "xray"), "run", "-test", "-config", path); e != nil {
		return errors.New("полная конфигурация Xray не прошла проверку")
	}
	return s.commitPrimary(out, s.restartMainXray, func() error {
		started := time.Now()
		err := checkVPNConnection()
		recordProbe(err, time.Since(started))
		return err
	})
}

func (s *Server) connectionAPI(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path != "connections" && !strings.HasPrefix(path, "connection-") {
		return false
	}
	if path == "connections" {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return true
		}
		s.mutation.Lock()
		defer s.mutation.Unlock()
		data, e := s.connectionStatus()
		if e != nil {
			fail(w, 500, e)
		} else {
			respond(w, 200, data)
		}
		return true
	}
	allowed := map[string]bool{"connection-reveal": true, "connection-parse": true, "connection-test": true, "connection-save": true, "connection-apply": true, "connection-use": true, "connection-delete": true, "connection-rollback": true}
	if !allowed[path] {
		return false
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return true
	}
	var req struct {
		Password string `json:"password"`
		ID       string `json:"id"`
		Name     string `json:"name"`
		Outbound string `json:"outbound"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return true
	}
	if e := s.currentPassword(r, req.Password); e != nil {
		fail(w, 403, e)
		return true
	}
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция выполняется"))
		return true
	}
	defer s.mutation.Unlock()
	if coreJobActive(s.coreJob()) || s.pendingRecovery() {
		fail(w, 409, errors.New("дождитесь завершения операции или восстановления"))
		return true
	}
	if path == "connection-reveal" {
		out, e := s.connectionOutbound(req.ID)
		if e != nil {
			fail(w, 400, e)
		} else {
			respond(w, 200, map[string]any{"outbound": out})
		}
		return true
	}
	if path == "connection-delete" {
		list, e := s.connections()
		if e != nil {
			fail(w, 400, e)
			return true
		}
		found := false
		next := []SavedConnection{}
		for _, item := range list {
			if item.ID == req.ID {
				found = true
			} else {
				next = append(next, item)
			}
		}
		if !found {
			fail(w, 404, errors.New("подключение не найдено"))
			return true
		}
		release, e := s.pause()
		if e != nil {
			fail(w, 409, e)
			return true
		}
		defer release()
		if e = s.writeConnections(next); e != nil {
			fail(w, 500, e)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	var out map[string]any
	var e error
	if path == "connection-rollback" {
		out, e = s.connectionOutbound("previous")
	} else if path == "connection-use" {
		out, e = s.connectionOutbound(req.ID)
	} else {
		if len(req.Outbound) > 64<<10 {
			e = errors.New("подключение больше 64 КиБ")
		} else {
			out, e = parseBackup(req.Outbound)
		}
	}
	if e != nil {
		fail(w, 400, errors.New("параметры подключения не распознаны; проверьте ссылку или outbound JSON"))
		return true
	}
	if path == "connection-parse" {
		respond(w, 200, map[string]any{"outbound": out})
		return true
	}
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика занята"))
		return true
	}
	defer s.diagnosticMu.Unlock()
	release, e := s.pause()
	if e != nil {
		fail(w, 409, e)
		return true
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	if path == "connection-use" || path == "connection-apply" || path == "connection-rollback" {
		e = s.activateConnection(ctx, out)
	} else {
		if e = s.testOutbound(ctx, out); e != nil {
			e = errors.New("изолированная HTTPS-проверка не пройдена; изменения не сохранены")
		} else if path == "connection-save" {
			e = s.saveConnection(req.ID, req.Name, out)
		}
	}
	if e != nil {
		fail(w, 400, errors.New(s.scrub(e.Error())))
	} else {
		respond(w, 200, map[string]bool{"ok": true})
	}
	return true
}
