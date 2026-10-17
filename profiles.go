package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Preferences struct {
	MonitorServices bool `json:"monitor_services"`
	Interval        int  `json:"interval_seconds"`
	AutoFailover    bool `json:"auto_failover"`
}

func (s *Server) preferences() Preferences {
	p := Preferences{true, 300, false}
	_ = readJSON(filepath.Join(s.dir, "preferences.json"), &p)
	if p.Interval < 120 || p.Interval > 3600 {
		p.Interval = 300
	}
	return p
}
func (s *Server) readVPNConfig() (map[string]any, error) {
	var c map[string]any
	e := readJSON(filepath.Join(s.root, "config-vpn.json"), &c)
	return c, e
}
func cloneConfig(c map[string]any) map[string]any {
	b, _ := json.Marshal(c)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func vpnOutbound(c map[string]any) (map[string]any, error) {
	a, _ := c["outbounds"].([]any)
	for _, v := range a {
		m, _ := v.(map[string]any)
		if m["tag"] == "vpn" {
			return m, nil
		}
	}
	return nil, errors.New("не найден outbound с тегом vpn")
}
func replaceVPN(c map[string]any, out map[string]any) error {
	a, _ := c["outbounds"].([]any)
	for i, v := range a {
		m, _ := v.(map[string]any)
		if m["tag"] == "vpn" {
			copy := cloneConfig(out)
			copy["tag"] = "vpn"
			a[i] = copy
			c["outbounds"] = a
			return nil
		}
	}
	return errors.New("не найден outbound vpn")
}
func number(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		i, _ := strconv.Atoi(string(x))
		return i
	}
	return 0
}
func outboundEndpoint(out map[string]any) (string, int) {
	settings, _ := out["settings"].(map[string]any)
	for _, key := range []string{"vnext", "servers"} {
		a, _ := settings[key].([]any)
		if len(a) > 0 {
			m, _ := a[0].(map[string]any)
			host, _ := m["address"].(string)
			return host, number(m["port"])
		}
	}
	host, _ := settings["address"].(string)
	return host, number(settings["port"])
}
func parseBackup(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	var out map[string]any
	if strings.HasPrefix(raw, "vless://") {
		u, e := url.Parse(raw)
		if e != nil || u.User == nil || u.Hostname() == "" {
			return nil, errors.New("неверная ссылка VLESS")
		}
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return nil, errors.New("неверный порт")
		}
		q := u.Query()
		stream := map[string]any{"network": q.Get("type"), "security": q.Get("security")}
		if stream["network"] == "" {
			stream["network"] = "tcp"
		}
		if stream["security"] == "reality" {
			stream["realitySettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid"), "spiderX": q.Get("spx")}
		} else if stream["security"] == "tls" {
			stream["tlsSettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp")}
		}
		switch stream["network"] {
		case "xhttp":
			x := map[string]any{"path": q.Get("path"), "mode": q.Get("mode")}
			if q.Get("host") != "" {
				x["host"] = q.Get("host")
			}
			if q.Get("extra") != "" {
				var extra map[string]any
				if json.Unmarshal([]byte(q.Get("extra")), &extra) != nil {
					return nil, errors.New("неверный extra")
				}
				x["extra"] = extra
			}
			stream["xhttpSettings"] = x
		case "grpc":
			stream["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName")}
		case "ws":
			stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
		}
		user := map[string]any{"id": u.User.Username(), "encryption": "none"}
		if q.Get("flow") != "" {
			user["flow"] = q.Get("flow")
		}
		out = map[string]any{"tag": "vpn", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{user}}}}, "streamSettings": stream}
	} else if json.Unmarshal([]byte(raw), &out) != nil {
		return nil, errors.New("вставь VLESS-ссылку или один outbound JSON")
	}
	switch out["protocol"] {
	case "vless", "trojan", "shadowsocks", "hysteria":
	default:
		return nil, errors.New("поддерживаются outbound VLESS, Trojan, Shadowsocks или Hysteria; окончательную проверку выполнит Xray")
	}
	out["tag"] = "vpn"
	host, p := outboundEndpoint(out)
	if host == "" || p < 1 || p > 65535 {
		return nil, errors.New("не распознан адрес и порт outbound")
	}
	return out, nil
}
func (s *Server) profilePath(name string) string {
	return filepath.Join(s.dir, "profiles", name+".json")
}
func (s *Server) backupConfigured() bool {
	st, e := os.Stat(s.profilePath("backup"))
	return e == nil && st.Mode().IsRegular()
}
func (s *Server) profileStatus() map[string]any {
	var active map[string]any
	_ = readJSON(filepath.Join(s.dir, "active-profile.json"), &active)
	name := "primary"
	if v, ok := active["name"].(string); ok {
		name = v
	}
	return map[string]any{"active": name, "backup_configured": s.backupConfigured(), "auto_failover": s.preferences().AutoFailover, "last_switch": active["at"]}
}
func (s *Server) candidate(ctx context.Context, cfg map[string]any, port int, run func(*http.Client) error) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("порт изолированного теста занят")
	}
	listener.Close()
	memory, _ := os.ReadFile("/proc/meminfo")
	m := parseMem(string(memory))
	available := m["MemAvailable"]
	if available == 0 {
		available = m["MemFree"] + m["Cached"] + m["Buffers"]
	}
	if available < 96*1024 {
		return errors.New("для изолированной проверки нужно 96 МиБ доступной RAM")
	}
	c := cloneConfig(cfg)
	out, e := vpnOutbound(c)
	if e != nil {
		return e
	}
	c = map[string]any{"outbounds": []any{out}}
	c["inbounds"] = []any{map[string]any{"tag": "candidate-socks", "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}
	c["outbounds"] = []any{out}
	delete(c, "routing")
	c["log"] = map[string]any{"loglevel": "warning"}
	f, e := os.CreateTemp("/tmp", "darkls-candidate-*.json")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	if _, e = f.Write(jsonBytes(c)); e != nil {
		f.Close()
		return e
	}
	f.Close()
	bin := filepath.Join(s.root, "xray")
	if _, e = s.run(bin, "run", "-test", "-config", path); e != nil {
		return errors.New("конфигурация не прошла проверку Xray")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, bin, "run", "-config", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logFile, e := os.OpenFile("/tmp/darkls-candidate.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if e = cmd.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); <-done }()
	ready := false
	for i := 0; i < 30; i++ {
		c, e := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		select {
		case <-probeCtx.Done():
			return errors.New("тестовое соединение не запустилось")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		return errors.New("тестовое соединение не запустилось")
	}
	client := transportClient(address)
	defer client.CloseIdleConnections()
	return run(client)
}
func (s *Server) testOutbound(ctx context.Context, out map[string]any) error {
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	if e = replaceVPN(cfg, out); e != nil {
		return e
	}
	return s.candidate(ctx, cfg, 10818, func(c *http.Client) error {
		return probeTargets(c, []probeTarget{{"https://www.gstatic.com/generate_204", 204, ""}, {"https://www.cloudflare.com/cdn-cgi/trace", 200, "\nip="}})
	})
}
func (s *Server) saveBackup(ctx context.Context, raw string) error {
	out, e := parseBackup(raw)
	if e != nil {
		return e
	}
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	primary, e := vpnOutbound(cfg)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700); e != nil {
		return e
	}
	if _, e = os.Stat(s.profilePath("primary")); os.IsNotExist(e) {
		if e = atomicWrite(s.profilePath("primary"), jsonBytes(primary)); e != nil {
			return e
		}
	}
	if e = s.testOutbound(ctx, out); e != nil {
		return errors.New("резерв не прошёл HTTPS-проверку; подключение не сохранено")
	}
	return atomicWrite(s.profilePath("backup"), jsonBytes(out))
}
func (s *Server) switchProfile(ctx context.Context, name string) error {
	if name != "primary" && name != "backup" {
		return errors.New("неверный профиль")
	}
	var out map[string]any
	if e := readJSON(s.profilePath(name), &out); e != nil {
		return errors.New("профиль не настроен")
	}
	if e := s.testOutbound(ctx, out); e != nil {
		return errors.New("выбранный канал не прошёл проверку; рабочий канал сохранён")
	}
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	original := jsonBytes(cfg)
	if e = replaceVPN(cfg, out); e != nil {
		return e
	}
	journal := filepath.Join(s.dir, "profile-switch-journal.json")
	if e = s.writeProfileJournal(original); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(s.root, "config-vpn.json"), jsonBytes(cfg)); e != nil {
		return e
	}
	if e = s.restartMainXray(); e == nil {
		e = checkVPNConnection()
	}
	if e != nil {
		_ = atomicWrite(filepath.Join(s.root, "config-vpn.json"), original)
		re := s.restartMainXray()
		if re == nil {
			_ = os.Remove(journal)
		}
		return errors.New("переключение не удалось; выполнена попытка возврата прежнего канала")
	}
	if e = atomicWrite(filepath.Join(s.dir, "active-profile.json"), jsonBytes(map[string]any{"name": name, "at": time.Now().Unix()})); e != nil {
		if s.restoreProfileFiles() == nil && s.restartMainXray() == nil {
			_ = os.Remove(journal)
		}
		return errors.New("не удалось завершить запись профиля; выполнена попытка отката")
	}
	_ = os.Remove(journal)
	appendEvent("profile", "Активен канал "+name)
	return nil
}
func (s *Server) failover() error {
	st := s.profileStatus()
	if st["active"] == "backup" {
		return errors.New("резерв уже активен; автоматического возврата и циклических переключений нет")
	}
	b, _ := os.ReadFile(s.runtimePath("last-failover"))
	last, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	uptime, _ := os.ReadFile("/proc/uptime")
	fields := strings.Fields(string(uptime))
	var now float64
	if len(fields) > 0 {
		now, _ = strconv.ParseFloat(fields[0], 64)
	}
	if now-last < 300 && last > 0 {
		return errors.New("интервал между переключениями 5 минут")
	}
	_ = atomicWrite(s.runtimePath("last-failover"), []byte(fmt.Sprint(now)))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.switchProfile(ctx, "backup")
}

type profileJournal struct {
	Config json.RawMessage `json:"previous_config"`
	Active json.RawMessage `json:"previous_active,omitempty"`
}

func (s *Server) writeProfileJournal(config []byte) error {
	active, _ := os.ReadFile(filepath.Join(s.dir, "active-profile.json"))
	return atomicWrite(filepath.Join(s.dir, "profile-switch-journal.json"), jsonBytes(profileJournal{Config: config, Active: active}))
}
func (s *Server) restoreProfileFiles() error {
	path := filepath.Join(s.dir, "profile-switch-journal.json")
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var j profileJournal
	if e = json.Unmarshal(b, &j); e != nil {
		return e
	}
	if len(j.Config) == 0 {
		j.Config = b
	}
	var cfg map[string]any
	if json.Unmarshal(j.Config, &cfg) != nil {
		return errors.New("неверный журнал переключения")
	}
	if e = atomicWrite(filepath.Join(s.root, "config-vpn.json"), j.Config); e != nil {
		return e
	}
	activePath := filepath.Join(s.dir, "active-profile.json")
	name := "primary"
	if len(j.Active) > 0 {
		var a map[string]any
		_ = json.Unmarshal(j.Active, &a)
		if a["name"] == "backup" {
			name = "backup"
		}
		if e = atomicWrite(activePath, j.Active); e != nil {
			return e
		}
	} else {
		_ = os.Remove(activePath)
	}
	if _, e = os.Stat(s.profilePath(name)); e == nil {
		out, e := vpnOutbound(cfg)
		if e != nil {
			return e
		}
		if e = atomicWrite(s.profilePath(name), jsonBytes(out)); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) recoverProfileSwitch() error {
	path := filepath.Join(s.dir, "profile-switch-journal.json")
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	release, e := s.pause()
	if e != nil {
		return e
	}
	defer release()
	if e = s.restoreProfileFiles(); e != nil {
		return e
	}
	if !s.disabled() {
		if e = s.restartMainXray(); e != nil {
			return e
		}
	}
	return os.Remove(path)
}
