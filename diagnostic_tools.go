package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type ServiceHistory struct {
	Service  string `json:"service"`
	At       int64  `json:"at"`
	State    string `json:"state"`
	Previous string `json:"previous,omitempty"`
	Since    int64  `json:"since"`
	Duration int64  `json:"previous_duration_seconds,omitempty"`
	TunnelOK *bool  `json:"tunnel_ok"`
	MS       int64  `json:"duration_ms"`
}

func (s *Server) recordService(id string, r ServiceResult) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	p := s.runtimePath("service-current.json")
	all := map[string]ServiceHistory{}
	_ = readJSON(p, &all)
	old := all[id]
	now := time.Now().Unix()
	entry := ServiceHistory{Service: id, At: now, State: r.State, Since: old.Since, MS: r.DurationMS}
	probe := readProbeState()
	if probeFresh(probe, time.Unix(now, 0)) {
		entry.TunnelOK = boolRef(probe.OK)
	}
	if old.State != r.State {
		entry.Previous = old.State
		if old.Since > 0 {
			entry.Duration = now - old.Since
		}
		entry.Since = now
		if old.State != "" {
			appendEvent("service_change", id+": "+old.State+" → "+r.State)
		}
	}
	all[id] = entry
	_ = atomicWrite(p, jsonBytes(all))
	log := s.runtimePath("service-history.jsonl")
	if st, e := os.Stat(log); e == nil && st.Size() > 256<<10 {
		_ = trimLog(log, 256<<10, 128<<10)
	}
	f, e := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		b, _ := json.Marshal(entry)
		_, _ = f.Write(append(b, '\n'))
		f.Close()
	}
}
func (s *Server) serviceHistory() map[string]any {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	cur := map[string]ServiceHistory{}
	_ = readJSON(s.runtimePath("service-current.json"), &cur)
	b, _ := os.ReadFile(s.runtimePath("service-history.jsonl"))
	events := []ServiceHistory{}
	// jsonBytes writes multiline objects; Decoder handles adjacent JSON values.
	for _, line := range strings.Split(string(b), "\n") {
		var e ServiceHistory
		if json.Unmarshal([]byte(line), &e) == nil && e.At >= time.Now().Add(-48*time.Hour).Unix() {
			events = append(events, e)
		}
	}
	if len(events) > 250 {
		events = events[len(events)-250:]
	}
	return map[string]any{"current": cur, "events": events, "retention": "48 часов, максимум 256 КиБ; данные в RAM и сбрасываются после перезагрузки"}
}

var dependencyHosts = map[string][]string{
	"instagram": {"www.instagram.com", "www.fbsbx.com", "static.cdninstagram.com"},
	"chatgpt":   {"chatgpt.com", "auth.openai.com", "oaistatic.com", "oaiusercontent.com"},
	"youtube":   {"www.youtube.com", "i.ytimg.com", "youtubei.googleapis.com", "googlevideo.com"},
	"discord":   {"discord.com", "gateway.discord.gg", "cdn.discordapp.com"},
	"gemini":    {"gemini.google.com", "accounts.google.com", "generativelanguage.googleapis.com"},
	"twitter":   {"x.com", "pbs.twimg.com", "abs.twimg.com"},
	"telegram":  {"telegram.org", "t.me", "web.telegram.org"},
}

func (s *Server) dependencies(ctx context.Context, id string) (map[string]any, error) {
	hosts, ok := dependencyHosts[id]
	if !ok {
		return nil, errors.New("выберите известный сервис")
	}
	st, e := s.state()
	if e != nil {
		return nil, e
	}
	rows := []map[string]any{}
	for _, host := range hosts {
		matches := []string{}
		for _, g := range st.Groups {
			if g.active(time.Now().Unix()) {
				for _, d := range g.Domains {
					if host == d || strings.HasSuffix(host, "."+d) {
						matches = append(matches, g.Name)
						break
					}
				}
			}
		}
		dnsCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ips, e := lookupRouterDNS(dnsCtx, "127.0.0.1:10535", host)
		cancel()
		addresses := []string{}
		inSet := false
		for _, ip := range ips {
			addresses = append(addresses, ip.String())
			if _, x := s.run("ipset", "test", "darkls_vpn", ip.String()); x == nil {
				inSet = true
			}
		}
		state := "DNS ответил"
		if e != nil {
			state = "Нет IPv4-ответа: " + s.scrub(e.Error())
		}
		https := ServiceResult{State: "not_checked", Message: "HTTPS не проверен: нет IPv4-адреса"}
		if len(ips) > 0 {
			probeCtx, probeCancel := context.WithTimeout(ctx, 12*time.Second)
			https = checkService(probeCtx, host)
			probeCancel()
		}
		rows = append(rows, map[string]any{"https": https, "domain": host, "groups": matches, "addresses": addresses, "in_vpn_set": inSet, "dns": state, "configured": len(matches) > 0})
	}
	result := checkService(ctx, serviceDomains[id])
	s.recordService(id, result)
	return map[string]any{"service": id, "rows": rows, "https": result, "note": "Проверка с роутера. Корневой CDN-домен может не иметь A-записи — это не доказательство сбоя. Список зависимостей ограничен известными доменами; фактический запрос браузера может использовать другой хост."}, nil
}
func previewChanges(old, next State) map[string]any {
	before, after := map[string]bool{}, map[string]bool{}
	for _, d := range activeDomains(old) {
		before[d] = true
	}
	for _, d := range activeDomains(next) {
		after[d] = true
	}
	added, removed := []string{}, []string{}
	for d := range after {
		if !before[d] {
			added = append(added, d)
		}
	}
	for d := range before {
		if !after[d] {
			removed = append(removed, d)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	changes := []string{}
	byID := map[string]Group{}
	for _, g := range old.Groups {
		byID[g.ID] = g
	}
	for _, g := range next.Groups {
		prev, ok := byID[g.ID]
		if !ok {
			changes = append(changes, "Добавлена группа: "+g.Name)
		} else {
			if prev.Enabled != g.Enabled {
				changes = append(changes, g.Name+": изменено включение")
			}
			if prev.quicBlocked(time.Now().Unix()) != g.quicBlocked(time.Now().Unix()) {
				changes = append(changes, g.Name+": изменён запрет QUIC")
			}
			if prev.EnabledUntil != g.EnabledUntil || prev.QUICUntil != g.QUICUntil {
				changes = append(changes, g.Name+": изменён таймер")
			}
		}
		delete(byID, g.ID)
	}
	for _, g := range byID {
		changes = append(changes, "Удалена группа: "+g.Name)
	}
	policy := string(jsonBytes(old.Policy)) != string(jsonBytes(next.Policy))
	return map[string]any{"added_domains": added, "removed_domains": removed, "groups": changes, "device_policy_changed": policy, "old_device_mode": old.Policy.Mode, "new_device_mode": next.Policy.Mode, "selected_devices": len(next.Policy.Devices), "affected_devices": deviceScope(next.Policy), "xray_restart": false, "dns_reload": string(dnsConfigForState(old)) != string(dnsConfigForState(next)), "note": "Применение меняет правила для новых соединений. Общие CDN-IP остаются в VPN, если нужны другой включённой группе. Старые соединения сохраняют прежний маршрут; ранее определённые IP удалённых доменов могут сохраняться до 24 часов."}
}
func expireState(st State, now int64) (State, bool) {
	changed := false
	for i := range st.Groups {
		g := &st.Groups[i]
		if g.EnabledUntil > 0 && now >= g.EnabledUntil {
			g.Enabled = g.TimedPrevious
			g.EnabledUntil = 0
			g.TimedPrevious = false
			changed = true
		}
		if g.QUICUntil > 0 && now >= g.QUICUntil {
			g.BlockQUIC = boolRef(g.QUICPrevious)
			g.QUICUntil = 0
			g.QUICPrevious = false
			changed = true
		}
	}
	return st, changed
}

type StorageItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Bytes       int64  `json:"bytes"`
	Consequence string `json:"consequence"`
	path        string
}

func diskBytes(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, e error) error {
		if e == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
func (s *Server) storageItems() []StorageItem {
	out := []StorageItem{}
	add := func(id, name, kind, p, consequence string) {
		st, e := os.Lstat(p)
		if e == nil && (st.Mode().IsRegular() || st.IsDir()) && st.Mode()&os.ModeSymlink == 0 {
			out = append(out, StorageItem{id, name, kind, diskBytes(p), consequence, p})
		}
	}
	add("panel-previous", "Предыдущая панель", "panel", filepath.Join(s.dir, "darkls-panel.previous"), "Пропадёт локальная копия предыдущего бинарника панели; автоматический откат следующего обновления создаёт свою копию заново.")
	entries, _ := os.ReadDir(filepath.Join(s.dir, "backups"))
	for _, e := range entries {
		if e.IsDir() && idPattern.MatchString(e.Name()) {
			add("settings:"+e.Name(), e.Name(), "settings", filepath.Join(s.dir, "backups", e.Name()), "Эту версию настроек больше нельзя будет восстановить из истории.")
		}
	}
	for _, n := range []string{"xray-arm64.zip", "asusware-usbmount.zip"} {
		add("archive:"+n, n, "installer", filepath.Join("/tmp", n), "Удаляется установочный архив в RAM. Работающие программы и настройки не меняются.")
	}
	dir := "/tmp/mnt/WINDOWS_10/darkls-vpn-backups"
	entries, _ = os.ReadDir(dir)

	for _, e := range entries {
		if coreBackupName.MatchString(e.Name()) {
			add("core:"+e.Name(), e.Name(), "xray", filepath.Join(dir, e.Name()), "Откат к этой копии ядра станет недоступен. Действующее ядро в JFFS не удаляется.")
		}
	}
	return out
}
func (s *Server) deleteStorage(id string) error {
	if updateBusy() {
		return errors.New("выполняется обновление")
	}
	if s.pendingRecovery() {
		return errors.New("есть незавершённая операция; сначала восстановите её")
	}

	for _, item := range s.storageItems() {
		if item.ID == id {
			if e := os.RemoveAll(item.path); e != nil {
				return e
			}
			if item.Kind == "xray" {
				var j coreJournal
				if readJSON(s.corePath("xray-previous.json"), &j) == nil && j.Backup == item.path {
					_ = os.Remove(s.corePath("xray-previous.json"))
				}
			}
			appendEvent("cleanup", "Удалена выбранная копия: "+item.Kind)
			return nil
		}
	}
	return errors.New("элемент не найден или его удаление запрещено")
}

var reportIP = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
var reportMAC = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b`)

func pseudonym(s string) string {
	h := sha256.Sum256([]byte(s))
	return "addr-" + hex.EncodeToString(h[:4])
}
func (s *Server) report() []byte {
	c := s.collectConnectivity(true)
	s.saveConnectivity(c)
	cfg, _ := s.readVPNConfig()
	o, _ := vpnOutbound(cfg)
	protocol := "unknown"
	network := "unknown"
	if o != nil {
		protocol, _ = o["protocol"].(string)
		stream, _ := o["streamSettings"].(map[string]any)
		network, _ = stream["network"].(string)
	}
	v, _ := s.run(filepath.Join(s.root, "xray"), "version")
	rules := map[string]string{}
	for _, x := range [][2]string{{"nat", "DV_NAT"}, {"mangle", "DV_UDP"}, {"filter", "DV_IN"}} {
		rules[x[1]] = s.expandedChain(x[0], x[1])
	}
	out := map[string]any{"format": 1, "panel_version": version, "xray_version": string(v), "created_at": time.Now().UTC(), "connectivity": c, "probe": readProbeState(), "metrics": s.metrics(), "profile": s.profileStatus(), "transport": map[string]string{"protocol": protocol, "network": network}, "rules": rules, "services": s.serviceHistory(), "events": readEvents(), "logs": map[string]string{"watch": tailLog("/tmp/darkls-watch.log"), "xray": errorLines(tailLog("/tmp/xray.log")), "dns": errorLines(tailLog("/tmp/dnsmasq-vpn.log"))}, "privacy": "Конфиг, пароли, VPN-ключи и cookies не включены. IP/MAC псевдонимизированы. Список посещённых сайтов не выгружается; выбранные проверки сервисов остаются."}
	data := s.scrub(string(jsonBytes(out)))
	data = reportMAC.ReplaceAllStringFunc(data, pseudonym)
	data = reportIP.ReplaceAllStringFunc(data, pseudonym)
	// Ensure sanitization did not break JSON; values are string replacements.
	if !json.Valid([]byte(data)) {
		return jsonBytes(map[string]string{"error": "Не удалось безопасно сформировать отчёт"})
	}
	return []byte(data)
}
func (s *Server) temporaryGroup(id string, minutes int, quic *bool, expected string) error {
	if time.Now().Year() < 2025 {
		return errors.New("дождитесь синхронизации часов")
	}
	if minutes < 1 || minutes > 1440 {
		return errors.New("срок от 1 минуты до 24 часов")
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	found := false
	for i := range st.Groups {
		g := &st.Groups[i]
		if g.ID != id {
			continue
		}
		found = true
		if quic == nil {
			if g.EnabledUntil == 0 {
				g.TimedPrevious = g.Enabled
			}
			g.Enabled = true
			g.EnabledUntil = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
		} else {
			if g.QUICUntil == 0 {
				g.QUICPrevious = g.quicBlocked(time.Now().Unix())
			}
			g.BlockQUIC = boolRef(*quic)
			g.QUICUntil = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
		}
	}
	if !found {
		return errors.New("группа не найдена")
	}
	_, e = s.apply(st, expected)
	return e
}

func errorLines(text string) string {
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		for _, key := range []string{"error", "failed", "warning", "fatal", "panic", "timeout", "refused", "ошиб", "не удалось"} {
			if strings.Contains(lower, key) {
				out = append(out, line)
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

func deviceScope(p DevicePolicy) string {
	if p.Mode == "all" || p.Mode == "" {
		return "Все устройства домашней сети"
	}
	names := []string{}
	for _, d := range p.Devices {
		n := d.Name
		if n == "" {
			n = d.MAC
		}
		names = append(names, n)
	}
	prefix := "Только: "
	if p.Mode == "exclude" {
		prefix = "Все, кроме: "
	}
	if len(names) == 0 {
		return prefix + "список пуст"
	}
	return prefix + strings.Join(names, ", ")
}
