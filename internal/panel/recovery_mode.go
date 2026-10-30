package panel

import (
	"errors"
	"net/http"
)

// A missing USB backup must not hide the UI needed to diagnose the failure.
// Journals remain authoritative: mutations stay blocked until recovery succeeds.
func (s *Server) recoverPending() error {
	if err := s.recoverCoreJournal(); err != nil {
		return err
	}
	if err := s.recoverProfileSwitch(); err != nil {
		return err
	}
	return s.recoverJournal()
}

func recoveryRequestAllowed(path string) bool {
	switch path {
	case "/api/recovery", "/api/logout", "/api/connectivity", "/api/diagnose", "/api/service-probe", "/api/dependencies", "/api/probe":
		return true
	}
	return false
}

func (s *Server) recoveryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Password string `json:"password"`
		Offline  bool   `json:"offline"`
	}
	if err := decode(w, r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.currentPassword(r, req.Password); err != nil {
		fail(w, 403, err)
		return
	}
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция выполняется"))
		return
	}
	defer s.mutation.Unlock()
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика выполняется; повторите восстановление после её завершения"))
		return
	}
	defer s.diagnosticMu.Unlock()
	if req.Offline {
		backup, err := s.recoverProfileOffline()
		if err != nil {
			fail(w, 409, errors.New("Восстановление не завершено: "+s.scrub(err.Error())))
			return
		}
		respond(w, 200, map[string]any{"ok": true, "offline": true, "backup": backup})
		return
	}
	if err := s.recoverPending(); err != nil {
		fail(w, 409, errors.New("Восстановление не завершено: "+s.scrub(err.Error())))
		return
	}
	appendEvent("manual_repair", "Незавершённые операции восстановлены")
	respond(w, 200, map[string]bool{"ok": true})
}
