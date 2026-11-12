package panel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManualDiagnosticTakesPriorityAfterProbeExits(t *testing.T) {
	var slot diagnosticSlot
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	probe, stopProbe := slot.tryProbe(ctx, "проверка туннеля")
	type result struct {
		stop func()
		err  error
	}
	ready := make(chan result, 1)
	go func() {
		stop, err := slot.manual(ctx, "переключение сервера")
		ready <- result{stop, err}
	}()
	select {
	case <-probe.Done():
		if ctx.Err() != nil {
			t.Fatal("manual request did not cancel the probe")
		}
	case <-ctx.Done():
		t.Fatal("background probe was not canceled")
	}
	select {
	case <-ready:
		t.Fatal("manual request ran before the probe released the slot")
	default:
	}
	if _, other := slot.tryProbe(ctx, "проверка сервиса"); other != nil {
		t.Fatal("another probe overtook the waiting manual request")
	}
	if _, err := slot.manual(ctx, "проверка HTTPS"); err == nil || !strings.Contains(err.Error(), "переключение сервера") {
		t.Fatalf("expected the waiting operation name, got %v", err)
	}
	stopProbe()
	r := <-ready
	if r.err != nil {
		t.Fatal(r.err)
	}
	r.stop()
	if _, release := slot.tryProbe(ctx, "проверка сервиса"); release == nil {
		t.Fatal("background checks could not resume")
	} else {
		release()
	}
}

func TestManualDiagnosticCancellationDoesNotLeaveReservation(t *testing.T) {
	var slot diagnosticSlot
	probe, release := slot.tryProbe(context.Background(), "проверка туннеля")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-probe.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	if stop, err := slot.manual(ctx, "проверка профиля"); stop != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	release()
	if stop := slot.tryExclusive("восстановление"); stop == nil {
		t.Fatal("canceled request left the slot reserved")
	} else {
		stop()
	}
}

func TestDiagnosticConflictIdentifiesOngoingRecovery(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	stop := s.diagnostics.tryExclusive("автоматическое восстановление VPN")
	defer stop()
	w := request(s, "POST", "/api/probe", "{}", cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "автоматическое восстановление VPN") {
		t.Fatalf("expected specific 409: %d %s", w.Code, w.Body.String())
	}
	if s.diagnostics.tryExclusive("установка") != nil {
		t.Fatal("manual request interrupted an exclusive operation")
	}
}
