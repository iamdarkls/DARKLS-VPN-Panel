package main

import (
	"context"
	"time"
)

// runPeriodic waits after completion, so a slow operation never builds a queue
// of overdue ticks. Each task has one owner and stops scheduling on cancellation.
func runPeriodic(ctx context.Context, initial, interval time.Duration, task func()) {
	timer := time.NewTimer(initial)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if ctx.Err() != nil {
				return
			}
			task()
			timer.Reset(interval)
		}
	}
}

func (s *Server) startBackground(ctx context.Context) {
	go runPeriodic(ctx, 15*time.Second, 15*time.Second, s.maintenanceTick)
	go s.serviceMonitorLoop(ctx)
	go s.githubLoop(ctx)
	go s.coreLoop(ctx)
	go s.cleanupLoop(ctx)
}

// Timer expiry and DHCP refresh share one state read and one scheduling loop.
// Keep the order mutation -> pause; apply acquires pause internally.
func (s *Server) maintenanceTick() {
	if updateBusy() || s.pendingRecovery() || !s.mutation.TryLock() {
		return
	}
	defer s.mutation.Unlock()
	st, err := s.state()
	if err != nil {
		return
	}
	now := time.Now()
	if now.Year() >= 2025 {
		if next, changed := expireState(st, now.Unix()); changed {
			if _, err = s.apply(next, s.revision()); err != nil {
				appendEvent("timer_error", s.scrub(err.Error()))
			} else {
				appendEvent("timer", "Временные настройки возвращены")
			}
			return
		}
	}
	if s.disabled() {
		return
	}
	if _, _, changed := s.devicePlan(st.Policy); !changed {
		return
	}
	release, err := s.pause()
	if err != nil {
		return
	}
	defer release()
	if err = s.syncDevices(false); err != nil {
		appendEvent("devices_error", s.scrub(err.Error()))
	}
}

// Select the oldest due service, not always the first group in the catalogue.
// Spreading probes prevents one cycle from monopolizing diagnostics for minutes.
func dueService(st State, last map[string]time.Time, now time.Time, interval time.Duration) string {
	selected := ""
	for _, g := range st.Groups {
		if _, known := serviceDomains[g.ID]; !known || !g.active(now.Unix()) {
			continue
		}
		at := last[g.ID]
		if !at.IsZero() && now.Sub(at) >= 0 && now.Sub(at) < interval {
			continue
		}
		if selected == "" || at.Before(last[selected]) {
			selected = g.ID
		}
	}
	return selected
}

func (s *Server) serviceMonitorLoop(ctx context.Context) {
	last := map[string]time.Time{}
	runPeriodic(ctx, 15*time.Second, 15*time.Second, func() {
		now := time.Now()
		p := s.preferences()
		if now.Year() < 2025 || updateBusy() || s.pendingRecovery() || s.disabled() || !p.MonitorServices {
			return
		}
		st, err := s.state()
		if err != nil {
			return
		}
		id := dueService(st, last, now, time.Duration(p.Interval)*time.Second)
		if id == "" || !s.diagnosticMu.TryLock() {
			return
		}
		defer s.diagnosticMu.Unlock()
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result := checkService(probeCtx, serviceDomains[id])
		if ctx.Err() != nil {
			return
		}
		last[id] = time.Now()
		s.recordService(id, result)
	})
}

// Recheck failed discovery hourly; a clock correction also invalidates a cached
// timestamp in the future. Successful checks remain limited to once per day.
func releaseCheckDue(now time.Time, checkedAt int64, failure string) bool {
	delay := 24 * time.Hour
	if failure != "" {
		delay = time.Hour
	}
	age := now.Sub(time.Unix(checkedAt, 0))
	return checkedAt <= 0 || age < 0 || age >= delay
}
