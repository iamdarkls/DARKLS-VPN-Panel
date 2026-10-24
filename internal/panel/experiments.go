package panel

import (
	"context"
	"darkls-panel/internal/routerenv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type TrialResult struct {
	Preset        string  `json:"preset"`
	Samples       int     `json:"samples"`
	Failures      int     `json:"failures"`
	MedianMS      float64 `json:"median_ms"`
	DownloadBytes int64   `json:"download_bytes"`
	DownloadMbps  float64 `json:"download_mbps"`
	Error         string  `json:"error,omitempty"`
}

func latencySample(ctx context.Context, c *http.Client) (float64, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
	start := time.Now()
	r, e := c.Do(req)
	if e != nil {
		return 0, e
	}
	r.Body.Close()
	if r.StatusCode != 204 {
		return 0, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return float64(time.Since(start).Microseconds()) / 1000, nil
}
func boundedDownload(ctx context.Context, c *http.Client, bytes int64) (int64, float64, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", bytes), nil)
	start := time.Now()
	r, e := c.Do(req)
	if e != nil {
		return 0, 0, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return 0, 0, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	n, e := io.Copy(io.Discard, io.LimitReader(r.Body, bytes))
	seconds := time.Since(start).Seconds()
	return n, float64(n) * 8 / seconds / 1e6, e
}
func setXMUX(cfg map[string]any, preset string) error {
	out, e := vpnOutbound(cfg)
	if e != nil {
		return e
	}
	stream, _ := out["streamSettings"].(map[string]any)
	if stream["network"] != "xhttp" {
		return errors.New("этот активный канал не использует XHTTP")
	}
	if preset == "current" {
		return nil
	}
	connections := ""
	switch preset {
	case "reuse2":
		connections = "2"
	case "reuse4":
		connections = "4"
	default:
		return errors.New("неверный профиль XHTTP")
	}
	x, _ := stream["xhttpSettings"].(map[string]any)
	if x == nil {
		x = map[string]any{}
		stream["xhttpSettings"] = x
	}
	extra, _ := x["extra"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
		x["extra"] = extra
	}
	mux, _ := extra["xmux"].(map[string]any)
	if mux == nil {
		mux = map[string]any{}
		extra["xmux"] = mux
	}
	mux["maxConnections"] = connections
	mux["maxConcurrency"] = "0"
	return nil
}
func (s *Server) benchmarkXHTTP(ctx context.Context) ([]TrialResult, error) {
	b, _ := os.ReadFile("/proc/meminfo")
	m := parseMem(string(b))
	available := m["MemAvailable"]
	if available == 0 {
		available = m["MemFree"] + m["Cached"] + m["Buffers"]
	}
	if available < 96*1024 {
		return nil, errors.New("для изолированного теста нужно минимум 96 МиБ доступной RAM")
	}
	cfg, e := s.readVPNConfig()
	if e != nil {
		return nil, e
	}
	if e = setXMUX(cloneConfig(cfg), "current"); e != nil {
		return nil, e
	}
	results := []TrialResult{}
	for _, preset := range []string{"current", "reuse2", "reuse4"} {
		candidate := cloneConfig(cfg)
		_ = setXMUX(candidate, preset)
		r := TrialResult{Preset: preset}
		e = s.candidate(ctx, candidate, 10819, func(client *http.Client) error {
			times := []float64{}
			for i := 0; i < 3; i++ {
				ms, e := latencySample(ctx, client)
				r.Samples++
				if e != nil {
					r.Failures++
				} else {
					times = append(times, ms)
				}
			}
			if len(times) > 0 {
				sort.Float64s(times)
				r.MedianMS = times[len(times)/2]
			}
			n, mbps, e := boundedDownload(ctx, client, 1<<20)
			r.DownloadBytes = n
			r.DownloadMbps = mbps
			return e
		})
		if e != nil {
			r.Error = "Тест не прошёл; рабочий канал не менялся"
		}
		results = append(results, r)
		if ctx.Err() != nil {
			break
		}
	}
	_ = atomicWrite(s.runtimePath("xhttp-results.json"), jsonBytes(map[string]any{"at": time.Now().Unix(), "results": results}))
	return results, nil
}

type muxSnapshot struct {
	Present bool `json:"present"`
	Value   any  `json:"value"`
}

func muxSettings(cfg map[string]any) (map[string]any, error) {
	out, e := vpnOutbound(cfg)
	if e != nil {
		return nil, e
	}
	stream, _ := out["streamSettings"].(map[string]any)
	if stream["network"] != "xhttp" {
		return nil, errors.New("активный канал не XHTTP")
	}
	x, _ := stream["xhttpSettings"].(map[string]any)
	if x == nil {
		x = map[string]any{}
		stream["xhttpSettings"] = x
	}
	extra, _ := x["extra"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
		x["extra"] = extra
	}
	return extra, nil
}
func (s *Server) applyTuning(ctx context.Context, preset string) error {
	cfg, e := s.readVPNConfig()
	if e != nil {
		return e
	}
	original := jsonBytes(cfg)
	extra, e := muxSettings(cfg)
	if e != nil {
		return e
	}
	profile, _ := s.profileStatus()["active"].(string)
	snapshotPath := s.profilePath("tuning-" + profile)
	var snapshot muxSnapshot
	if preset == "restore" {
		if e = readJSON(snapshotPath, &snapshot); e != nil {
			return errors.New("исходные параметры этого канала ещё не сохранены")
		}
		if snapshot.Present {
			extra["xmux"] = snapshot.Value
		} else {
			delete(extra, "xmux")
		}
	} else {
		if preset != "reuse2" && preset != "reuse4" {
			return errors.New("выберите проверенный профиль")
		}
		value, present := extra["xmux"]
		var copied any
		b, _ := json.Marshal(value)
		_ = json.Unmarshal(b, &copied)
		snapshot = muxSnapshot{present, copied}
		if e = setXMUX(cfg, preset); e != nil {
			return e
		}
	}
	out, _ := vpnOutbound(cfg)
	if e = s.testOutbound(ctx, out); e != nil {
		return errors.New("профиль не прошёл проверку; рабочая конфигурация сохранена")
	}
	if preset != "restore" {
		if e = os.MkdirAll(filepath.Join(s.dir, "profiles"), 0700); e != nil {
			return e
		}
		if _, e = os.Stat(snapshotPath); os.IsNotExist(e) {
			if e = atomicWrite(snapshotPath, jsonBytes(snapshot)); e != nil {
				return e
			}
		}
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
		if s.restartMainXray() == nil {
			_ = os.Remove(journal)
		}
		return errors.New("профиль не заработал; выполнена попытка отката")
	}
	// Persist the active channel's tuned outbound without replacing endpoint or TLS.
	if _, err := os.Stat(s.profilePath(profile)); err == nil {
		if e = atomicWrite(s.profilePath(profile), jsonBytes(out)); e != nil {
			if s.restoreProfileFiles() == nil && s.restartMainXray() == nil {
				_ = os.Remove(journal)
			}
			return errors.New("не удалось сохранить параметры; выполнена попытка отката")
		}
	}
	_ = os.Remove(journal)
	appendEvent("tuning", "Применён профиль XHTTP "+preset)
	return nil
}
func (s *Server) loadExperiment(ctx context.Context, via string) (map[string]any, error) {
	if via != "direct" && via != "vpn" {
		return nil, errors.New("выберите direct или vpn")
	}
	address := ""
	if via == "vpn" {
		address = routerenv.SOCKSAddress
	}
	client := transportClient(address)
	defer client.CloseIdleConnections()
	baseline := []float64{}
	failures := 0
	for i := 0; i < 3; i++ {
		ms, e := latencySample(ctx, client)
		if e == nil {
			baseline = append(baseline, ms)
		} else {
			failures++
		}
	}
	loadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	done := make(chan struct{})
	var total int64
	start := time.Now()
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			n, _, e := boundedDownload(loadCtx, client, 1<<20)
			total += n
			if e != nil {
				return
			}
			if loadCtx.Err() != nil {
				return
			}
		}
	}()
	loaded := []float64{}
	for i := 0; i < 20; i++ {
		select {
		case <-done:
			goto finish
		default:
		}
		ms, e := latencySample(ctx, client)
		if e == nil {
			loaded = append(loaded, ms)
		} else {
			failures++
		}
		select {
		case <-done:
			goto finish
		case <-time.After(200 * time.Millisecond):
		}
	}
finish:
	cancel()
	<-done
	duration := time.Since(start).Seconds()
	mean := func(v []float64) float64 {
		var n float64
		for _, x := range v {
			n += x
		}
		if len(v) == 0 {
			return 0
		}
		return n / float64(len(v))
	}
	reliable := duration >= 2 && len(baseline) >= 2 && len(loaded) >= 3
	note := "Ограниченная HTTP-нагрузка до 8 МиБ и 8 секунд; это не тест максимальной скорости тарифа и не измерение Wi-Fi-потерь. Повторите во время обычной загрузки на других устройствах."
	if !reliable {
		note += " Нагрузка оказалась слишком короткой или мало успешных проб — вывод о задержках делать рано."
	} else if mean(loaded) > mean(baseline)*2 && mean(loaded)-mean(baseline) > 50 {
		note += " Задержка заметно выросла. Нужна отдельная проверка очередей/QoS ASUS на вашей линии; настройки роутера автоматически не менялись."
	}
	out := map[string]any{"at": time.Now().Unix(), "via": via, "idle_ms": mean(baseline), "load_ms": mean(loaded), "failures": failures, "bytes": total, "seconds": duration, "enough_samples": reliable, "note": note}
	_ = atomicWrite(s.runtimePath("load-results.json"), jsonBytes(out))
	return out, nil
}
func (s *Server) saveQuicObservation(id, verdict string) error {
	if verdict != "good" && verdict != "slow" && verdict != "failed" {
		return errors.New("неверная оценка")
	}
	st, e := s.state()
	if e != nil {
		return e
	}
	for _, g := range st.Groups {
		if g.ID == id {
			mode := "QUIC разрешён"
			if g.quicBlocked(time.Now().Unix()) {
				mode = "QUIC запрещён"
			}
			s.recordService("quic:"+id, ServiceResult{State: mode + ": " + verdict})
			return nil
		}
	}
	return errors.New("группа не найдена")
}
