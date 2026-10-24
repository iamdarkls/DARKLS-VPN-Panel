package panel

import (
	"context"
	"darkls-panel/internal/routerenv"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var errDiagnosticIncomplete = errors.New("проверка не завершена за отведённое время")

// Monitor owns failure streaks and monotonic cooldowns inside the serving panel.
// The shell watchdog supervises only its process and heartbeat.
type Monitor struct {
	server                                             *Server
	lastDeep, lastProbe, lastMaintenance, lastRecovery time.Time
	httpsFailures, dnsFailures                         int
}

func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func bootSeconds() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return int64(seconds)
}

func (m *Monitor) heartbeat() {
	_ = atomicWrite(m.server.runtimePath("monitor-heartbeat"), []byte(strconv.Itoa(os.Getpid())+" "+strconv.FormatInt(bootSeconds(), 10)+"\n"))
}
func (m *Monitor) run(ctx context.Context) {
	runPeriodic(ctx, 0, 15*time.Second, func() { m.tick(ctx) })
}

// quickHealth proves structural absence only; a utility timeout is unknown,
// never evidence that interrupting working connections will repair anything.
func (s *Server) quickHealth(ctx context.Context) error {
	scoped := s.networkOperation(ctx)
	if len(scoped.mainXrayPIDsContext(ctx)) == 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("процесс Xray отсутствует")
	}
	if !pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf") {
		return errors.New("процесс VPN DNS отсутствует")
	}
	for _, rule := range []struct{ table, chain, hook string }{{"nat", "DV_NAT", "PREROUTING"}, {"mangle", "DV_UDP", "PREROUTING"}, {"filter", "DV_IN", "INPUT"}} {
		if _, err := scoped.run("iptables", "-t", rule.table, "-C", rule.hook, "-i", routerenv.LANInterface, "-s", routerenv.LANPrefix, "-j", rule.chain); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("отсутствует подключение %s: %w", rule.chain, err)
		}
	}
	return nil
}

func due(now, previous time.Time, interval time.Duration) bool {
	return previous.IsZero() || now.Sub(previous) >= interval
}
func failureStreak(previous int, err error) int {
	if err == nil {
		return 0
	}
	return previous + 1
}

func (m *Monitor) tick(parent context.Context) {
	s := m.server
	m.heartbeat()
	defer m.heartbeat()
	now := time.Now()
	if due(now, m.lastMaintenance, time.Minute) {
		maintainLogs()
		m.lastMaintenance = now
	}
	if updateBusy() || s.pendingRecovery() || s.disabled() || coreJobActive(s.coreJob()) {
		m.httpsFailures = 0
		m.dnsFailures = 0
		return
	}
	// Avoid competing with user changes, benchmarks and service diagnostics.
	if !s.mutation.TryLock() {
		return
	}
	s.mutation.Unlock()
	if !s.diagnosticMu.TryLock() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	scoped := s.networkOperation(ctx)
	routerVerified, httpsVerified := false, false
	reason, structural := m.observe(ctx, now, scoped.quickHealth, func() error {
		err := scoped.checkRouter()
		routerVerified = err == nil
		return err
	}, func() error {
		started := time.Now()
		err := checkVPNConnectionContext(ctx)
		httpsVerified = err == nil
		if ctx.Err() == nil {
			recordProbe(err, time.Since(started))
		}
		return err
	})
	if ctx.Err() == nil && routerVerified && httpsVerified && recordRecoveryConfirmation(nil) {
		appendEvent("recovery_confirmed", "DNS, правила и HTTPS снова подтверждены автоконтролем")
	}
	cancel()
	s.diagnosticMu.Unlock()
	cooldown := 180 * time.Second
	if structural {
		cooldown = 15 * time.Second
	}
	if reason == "" || !due(time.Now(), m.lastRecovery, cooldown) || parent.Err() != nil {
		return
	}
	m.recover(parent, reason)
}

// observe is the monitor policy boundary: socket and firewall faults can be
// exercised without mutating the host or waiting for production intervals.
func (m *Monitor) observe(ctx context.Context, now time.Time, quick func(context.Context) error, router, https func() error) (string, bool) {
	quickCtx, stopQuick := context.WithTimeout(ctx, 8*time.Second)
	quickErr := quick(quickCtx)
	stopQuick()
	reason := ""
	structural := false
	if quickErr != nil && !errors.Is(quickErr, context.DeadlineExceeded) && !errors.Is(quickErr, context.Canceled) {
		reason = quickErr.Error()
		structural = true
	} else if quickErr == nil {
		if due(now, m.lastDeep, time.Minute) {
			m.lastDeep = now
			err := router()
			// An aggregate deadline is an incomplete check, not a proven DNS outage.
			if ctx.Err() == nil && !errors.Is(err, errDiagnosticIncomplete) && !errors.Is(err, context.Canceled) {
				m.dnsFailures = failureStreak(m.dnsFailures, err)
				if m.dnsFailures >= 2 {
					reason = "Повторный отказ правил или VPN DNS: " + err.Error()
				}
			}
		}
		if now.Year() >= 2025 && due(now, m.lastProbe, 30*time.Second) && ctx.Err() == nil {
			m.lastProbe = now
			err := https()
			if ctx.Err() == nil {
				m.httpsFailures = failureStreak(m.httpsFailures, err)
				if m.httpsFailures >= 2 {
					reason = "HTTPS через VPN не ответил на обеих целях дважды"
				}
			}
		}
	}
	return reason, structural
}

func (m *Monitor) recover(parent context.Context, reason string) {
	s := m.server
	if !s.mutation.TryLock() {
		return
	}
	defer s.mutation.Unlock()
	if !s.diagnosticMu.TryLock() {
		return
	}
	defer s.diagnosticMu.Unlock()
	if updateBusy() || s.pendingRecovery() || s.disabled() || coreJobActive(s.coreJob()) {
		return
	}
	release, err := s.pause()
	if err != nil {
		return
	}
	defer release()
	m.lastRecovery = time.Now()
	log.Printf("Checking failed VPN components: %s", s.scrub(reason))
	err = s.networkOperation(parent).recoverComponents(reason)
	if err != nil {
		appendEvent("recovery_failed", s.scrub(err.Error()))
		log.Print("Recovery failed: ", s.scrub(err.Error()))
	}
	m.httpsFailures = 0
	m.dnsFailures = 0
}

// Heartbeats use boot time and PID, so NTP jumps and old process samples cannot
// authorize a healthy state for a new panel process.
func readMonitorHeartbeat(path string) map[string]any {
	b, err := os.ReadFile(path)
	fields := strings.Fields(string(b))
	fresh := false
	if err == nil && len(fields) == 2 && fields[0] == strconv.Itoa(os.Getpid()) {
		completed, e := strconv.ParseInt(fields[1], 10, 64)
		age := bootSeconds() - completed
		fresh = e == nil && completed > 0 && age >= 0 && age <= 240
	}
	return map[string]any{"fresh": fresh, "grace_seconds": 240}
}
