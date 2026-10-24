package panel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const probeStatePath = "/tmp/darkls-probe-state.json"
const eventsPath = "/tmp/darkls-events.jsonl"

type RecoveryConfirmation struct {
	At    int64  `json:"at"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func recoveryVerified(p ProbeState) bool { return p.Confirmation == nil || p.Confirmation.OK }

type ProbeState struct {
	Confirmation   *RecoveryConfirmation `json:"confirmation,omitempty"`
	OK             bool                  `json:"ok"`
	CheckedAt      int64                 `json:"checked_at"`
	LastSuccess    int64                 `json:"last_success"`
	LastFailure    int64                 `json:"last_failure"`
	Failures       int                   `json:"failures"`
	Recoveries     int                   `json:"recoveries"`
	LastRecovery   int64                 `json:"last_recovery"`
	RecoveryReason string                `json:"recovery_reason"`
	DurationMS     int64                 `json:"duration_ms"`
	Error          string                `json:"error,omitempty"`
}
type Event struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func tmpLock(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		f.Close()
		return nil, e
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func readProbeState() ProbeState { var p ProbeState; _ = readJSON(probeStatePath, &p); return p }

// Router clocks can jump during NTP synchronization. Future-dated samples must
// never authorize an automatic update or be displayed as fresh connectivity.
func probeFresh(p ProbeState, now time.Time) bool {
	age := now.Unix() - p.CheckedAt
	return p.CheckedAt > 0 && age >= 0 && age < 100
}

func recordProbe(err error, d time.Duration) {
	release, e := tmpLock("/tmp/darkls-probe-state.lock")
	if e != nil {
		return
	}
	defer release()
	p := readProbeState()
	wasOK := p.OK
	previousError := p.Error
	hadSample := p.CheckedAt > 0
	p.CheckedAt = time.Now().Unix()
	p.OK = err == nil
	p.DurationMS = d.Milliseconds()
	p.Error = ""
	if err == nil {
		p.LastSuccess = p.CheckedAt
		p.Failures = 0
		if !wasOK && hadSample {
			appendEvent("vpn", "HTTPS через VPN восстановлен")
		}
	} else {
		p.LastFailure = p.CheckedAt
		p.Failures++
		p.Error = redact(err.Error())
		// Preserve every probe in history, but notify only a new failure or cause.
		if !hadSample || wasOK || p.Error != previousError {
			appendEvent("vpn_error", p.Error)
		}
	}
	_ = atomicWrite(probeStatePath, jsonBytes(p))
	if time.Now().Year() >= 2025 {
		appendHistory(probeHistoryPath, ProbeSample{At: p.CheckedAt, Kind: "probe", OK: p.OK, MS: p.DurationMS})
	}
}
func recordRecovery(reason string) {
	release, e := tmpLock("/tmp/darkls-probe-state.lock")
	if e != nil {
		return
	}
	defer release()
	p := readProbeState()
	p.Recoveries++
	p.LastRecovery = time.Now().Unix()
	p.RecoveryReason = redact(reason)
	p.Confirmation = &RecoveryConfirmation{At: p.LastRecovery, Error: "Проверяем DNS, правила и HTTPS после восстановления"}
	_ = atomicWrite(probeStatePath, jsonBytes(p))
	if time.Now().Year() >= 2025 {
		appendHistory(probeHistoryPath, ProbeSample{At: p.LastRecovery, Kind: "recovery", Reason: p.RecoveryReason})
	}
	appendEvent("recovery", "Восстановление VPN: "+redact(reason))
}

// A fresh HTTPS result alone cannot erase a failed DNS/rules postcondition.
// Legacy state without a confirmation remains compatible after an upgrade.
func recordRecoveryConfirmation(err error) bool {
	release, e := tmpLock("/tmp/darkls-probe-state.lock")
	if e != nil {
		return false
	}
	defer release()
	p := readProbeState()
	if p.Confirmation == nil {
		return false
	}
	next := &RecoveryConfirmation{At: time.Now().Unix(), OK: err == nil}
	if err != nil {
		next.Error = redact(err.Error())
	}
	changed := p.Confirmation.OK != next.OK || p.Confirmation.Error != next.Error
	p.Confirmation = next
	if e := atomicWrite(probeStatePath, jsonBytes(p)); e != nil {
		return false
	}
	return changed
}

var secretUUID = regexp.MustCompile(`(?i)[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)
var secretURL = regexp.MustCompile(`(?i)(vless|vmess|trojan|ss|hysteria2?)://[^\s"'\\<>]+`)

func redact(s string) string {
	s = secretURL.ReplaceAllString(s, "[VPN URI скрыт]")
	s = secretUUID.ReplaceAllString(s, "[UUID скрыт]")
	if len(s) > 4000 {
		s = s[:4000]
	}
	return strings.ToValidUTF8(s, "?")
}
func appendEvent(kind, message string) {
	release, e := tmpLock("/tmp/darkls-events.lock")
	if e != nil {
		return
	}
	defer release()
	if st, e := os.Stat(eventsPath); e == nil && st.Size() > 256<<10 {
		_ = os.Rename(eventsPath, eventsPath+".previous")
	}
	f, e := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(Event{time.Now().Unix(), kind, redact(message)})
}
func readEvents() []Event {
	f, e := os.Open(eventsPath)
	if e != nil {
		return []Event{}
	}
	defer f.Close()
	events := []Event{}
	scanner := bufio.NewScanner(io.LimitReader(f, 300<<10))
	scanner.Buffer(make([]byte, 8192), 16384)
	for scanner.Scan() {
		var v Event
		if json.Unmarshal(scanner.Bytes(), &v) == nil {
			events = append(events, v)
			if len(events) > 150 {
				events = events[1:]
			}
		}
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events
}
func tailLog(path string) string {
	f, e := os.Open(filepath.Clean(path))
	if e != nil {
		return ""
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return ""
	}
	if st.Size() > 32<<10 {
		_, _ = f.Seek(st.Size()-(32<<10), io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, 32<<10))
	lines := strings.Split(string(b), "\n")
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	return redact(strings.Join(lines, "\n"))
}
func probeDescription() string {
	p := readProbeState()
	return fmt.Sprintf("ok=%v failures=%d", p.OK, p.Failures)
}
