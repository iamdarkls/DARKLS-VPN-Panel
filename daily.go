package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const probeHistoryPath = "/tmp/darkls-probe-history.jsonl"
const historyCap = 512 << 10

type ProbeSample struct {
	At     int64  `json:"at"`
	Kind   string `json:"kind,omitempty"`
	OK     bool   `json:"ok,omitempty"`
	MS     int64  `json:"ms,omitempty"`
	Reason string `json:"reason,omitempty"`
}
type ResourceSample struct {
	At    int64  `json:"at"`
	Used  uint64 `json:"used_kib"`
	Panel uint64 `json:"panel_kib"`
	Xray  uint64 `json:"xray_kib"`
}

// Writers serialize through the probe-state or resource lock. Logs live in RAM,
// are size bounded and survive panel restarts, but do not survive router reboots.
func appendHistory(path string, value any) {
	_ = trimLog(path, historyCap, 384<<10)
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(value)
}
func readHistory[T any](path string) []T {
	out := []T{}
	f, e := os.Open(path)
	if e != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, historyCap+16384))
	sc.Buffer(make([]byte, 4096), 16384)
	for sc.Scan() {
		var x T
		if json.Unmarshal(sc.Bytes(), &x) == nil {
			out = append(out, x)
		}
	}
	return out
}

type DailySummary struct {
	LastProbe   int64  `json:"last_probe_at"`
	ProbeFresh  bool   `json:"probe_fresh"`
	ProbeOK     bool   `json:"probe_ok"`
	From        int64  `json:"from"`
	Until       int64  `json:"until"`
	FirstSample int64  `json:"first_sample"`
	Observed    int64  `json:"observed_seconds"`
	Unavailable int64  `json:"unavailable_seconds"`
	Unknown     int64  `json:"unknown_seconds"`
	Failures    int    `json:"failure_episodes"`
	Recoveries  int    `json:"recoveries"`
	Ongoing     bool   `json:"ongoing_failure"`
	PeakUsed    uint64 `json:"peak_used_kib"`
	PeakPanel   uint64 `json:"peak_panel_kib"`
	PeakXray    uint64 `json:"peak_xray_kib"`
	Resources   int    `json:"resource_samples"`
	Note        string `json:"note"`
}

func summarizeDay(samples []ProbeSample, resources []ResourceSample, now int64) DailySummary {
	start := now - 86400
	out := DailySummary{From: start, Until: now, Unknown: 86400, Note: "По HTTPS-пробам, с точностью их интервала. Пропуски более 100 секунд считаются неизвестными. История в RAM с момента запуска сбора, максимум 512 КиБ на журнал; после перезагрузки роутера начинается заново."}
	probes := []ProbeSample{}
	for _, s := range samples {
		if s.At > now || s.At < start-100 {
			continue
		}
		if s.Kind == "recovery" {
			if s.At >= start {
				out.Recoveries++
			}
			continue
		}
		if s.Kind != "" && s.Kind != "probe" {
			continue
		}
		probes = append(probes, s)
	}
	sort.SliceStable(probes, func(i, j int) bool { return probes[i].At < probes[j].At })
	// At most one observation per second; the final observation wins.
	unique := probes[:0]
	for _, s := range probes {
		if len(unique) > 0 && unique[len(unique)-1].At == s.At {
			unique[len(unique)-1] = s
		} else {
			unique = append(unique, s)
		}
	}
	probes = unique
	for i, s := range probes {
		if out.FirstSample == 0 {
			out.FirstSample = s.At
		}
		end := now
		if i+1 < len(probes) {
			end = probes[i+1].At
		}
		if end > s.At+100 {
			end = s.At + 100
		}
		from := s.At
		if from < start {
			from = start
		}
		if !s.OK && s.At >= start && (i == 0 || probes[i-1].OK || s.At-probes[i-1].At > 100) {
			out.Failures++
		}
		if end <= from {
			continue
		}
		out.Observed += end - from
		if !s.OK {
			out.Unavailable += end - from
		}
	}
	out.Unknown = 86400 - out.Observed
	if len(probes) > 0 {
		last := probes[len(probes)-1]
		out.LastProbe = last.At
		out.ProbeFresh = now-last.At < 100
		out.ProbeOK = last.OK
		out.Ongoing = !last.OK && out.ProbeFresh
	}
	for _, s := range resources {
		if s.At < start || s.At > now {
			continue
		}
		out.Resources++
		if s.Used > out.PeakUsed {
			out.PeakUsed = s.Used
		}
		if s.Panel > out.PeakPanel {
			out.PeakPanel = s.Panel
		}
		if s.Xray > out.PeakXray {
			out.PeakXray = s.Xray
		}
	}
	return out
}
func (s *Server) sampleResources() {
	if updateBusy() {
		return
	}
	b, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return
	}
	m := parseMem(string(b))
	available := m["MemAvailable"]
	if available == 0 {
		available = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	sample := ResourceSample{At: time.Now().Unix(), Panel: processMemory("/proc/self/status")["rss_kib"]}
	if m["MemTotal"] >= available {
		sample.Used = m["MemTotal"] - available
	}
	if p := s.mainXrayPIDs(); len(p) > 0 {
		sample.Xray = processMemory(filepath.Join("/proc", p[0], "status"))["rss_kib"]
	}
	if time.Now().Year() < 2025 {
		return
	}
	release, e := tmpLock(s.runtimePath("resources.lock"))
	if e != nil {
		return
	}
	defer release()
	appendHistory(s.runtimePath("resources.jsonl"), sample)
}
func (s *Server) activity() map[string]any {
	summary := summarizeDay(readHistory[ProbeSample](probeHistoryPath), readHistory[ResourceSample](s.runtimePath("resources.jsonl")), time.Now().Unix())
	relevant := map[string]bool{"vpn": true, "vpn_error": true, "recovery": true, "manual_repair": true, "profile": true, "connection": true, "update_error": true}
	events := []map[string]any{}
	lastKey := ""
	for _, e := range readEvents() {
		if !relevant[e.Kind] || e.At < time.Now().Add(-24*time.Hour).Unix() || e.At > time.Now().Unix() {
			continue
		}
		key := e.Kind + e.Message
		if key == lastKey {
			continue
		}
		lastKey = key
		events = append(events, map[string]any{"id": outboundHash(map[string]any{"at": e.At, "kind": e.Kind, "message": e.Message})[:16], "at": e.At, "kind": e.Kind, "message": e.Message})
		if len(events) == 20 {
			break
		}
	}
	// Scrub the batch once: avoid rereading credential files for every event.
	var safe []map[string]any
	if json.Unmarshal([]byte(s.scrub(string(jsonBytes(events)))), &safe) != nil {
		safe = []map[string]any{}
	}
	return map[string]any{"summary": summary, "notifications": safe}
}
func (s *Server) activityAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/activity" {
		return false
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return true
	}
	if time.Now().Year() < 2025 {
		fail(w, 409, errors.New("часы роутера ещё не синхронизированы"))
		return true
	}
	respond(w, 200, s.activity())
	return true
}
