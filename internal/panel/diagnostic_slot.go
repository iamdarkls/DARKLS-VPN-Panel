package panel

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const diagnosticHandoffTimeout = 3 * time.Second

type diagnosticLease struct {
	label  string
	done   chan struct{}
	cancel context.CancelFunc
}

// The slot serializes diagnostics with VPN changes. Only background read-only
// checks can be interrupted. A manual caller reserves the next turn while the
// canceled check unwinds; the worker must release before anything else starts.
type diagnosticSlot struct {
	mu      sync.Mutex
	active  *diagnosticLease
	waiting string
}

func (s *diagnosticSlot) start(label string, cancel context.CancelFunc) func() {
	op := &diagnosticLease{label: label, done: make(chan struct{}), cancel: cancel}
	s.active = op
	return func() {
		if cancel != nil {
			cancel()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active = nil
		close(op.done)
	}
}

func (s *diagnosticSlot) tryExclusive(label string) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.waiting != "" {
		return nil
	}
	return s.start(label, nil)
}

func (s *diagnosticSlot) tryProbe(parent context.Context, label string) (context.Context, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.waiting != "" || parent.Err() != nil {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(parent)
	return ctx, s.start(label, cancel)
}

func diagnosticBusy(label string) error {
	return fmt.Errorf("Сейчас выполняется «%s». Повторите после завершения", label)
}

func (s *diagnosticSlot) manual(ctx context.Context, label string) (func(), error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.waiting != "" {
		name := s.waiting
		s.mu.Unlock()
		return nil, fmt.Errorf("Действие «%s» уже ожидает завершения фоновой проверки", name)
	}
	if s.active == nil {
		release := s.start(label, nil)
		s.mu.Unlock()
		return release, nil
	}
	active := s.active
	if active.cancel == nil {
		s.mu.Unlock()
		return nil, diagnosticBusy(active.label)
	}
	s.waiting = label
	s.mu.Unlock()

	active.cancel()
	timer := time.NewTimer(diagnosticHandoffTimeout)
	defer timer.Stop()
	select {
	case <-active.done:
	case <-ctx.Done():
	case <-timer.C:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting = ""
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.active != nil {
		return nil, diagnosticBusy(active.label)
	}
	return s.start(label, nil), nil
}

func diagnosticLabel(path string) string {
	switch path {
	case "/api/report":
		return "диагностический отчёт"
	case "/api/connectivity":
		return "проверка подключения"
	case "/api/dependencies":
		return "проверка зависимостей сервиса"
	case "/api/xhttp-benchmark":
		return "сравнение настроек XHTTP"
	case "/api/load-test":
		return "тест под нагрузкой"
	case "/api/profile-save":
		return "проверка резервного профиля"
	case "/api/profile-switch":
		return "переключение профиля"
	case "/api/xhttp-apply":
		return "применение настроек XHTTP"
	case "/api/recover-part":
		return "ручное восстановление VPN"
	case "/api/connection-use", "/api/connection-apply":
		return "переключение сервера"
	case "/api/connection-rollback":
		return "возврат предыдущего сервера"
	default:
		return "проверка профиля сервера"
	}
}
