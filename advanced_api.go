package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"
)

func (s *Server) advancedAPI(w http.ResponseWriter, r *http.Request) bool {
	paths := map[string]bool{"/api/report": true, "/api/connectivity": true, "/api/recover-part": true, "/api/dependencies": true, "/api/service-history": true, "/api/preview": true, "/api/timer": true, "/api/quic-observation": true, "/api/storage": true, "/api/storage-delete": true, "/api/preferences": true, "/api/profiles": true, "/api/profile-save": true, "/api/profile-switch": true, "/api/xhttp-benchmark": true, "/api/xhttp-apply": true, "/api/load-test": true}
	if !paths[r.URL.Path] {
		return false
	}
	get := r.URL.Path == "/api/report" || r.URL.Path == "/api/service-history" || r.URL.Path == "/api/storage" || r.URL.Path == "/api/profiles" || (r.URL.Path == "/api/preferences" && r.Method == "GET")
	if (get && r.Method != "GET") || (!get && r.Method != "POST") {
		w.WriteHeader(405)
		return true
	}
	if get {
		switch r.URL.Path {
		case "/api/report":
			if !s.diagnosticMu.TryLock() {
				fail(w, 409, errors.New("диагностика занята"))
				break
			}
			defer s.diagnosticMu.Unlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="darkls-diagnostic-report.json"`)
			_, _ = w.Write(s.report())
		case "/api/service-history":
			respond(w, 200, s.serviceHistory())
		case "/api/storage":
			respond(w, 200, map[string]any{"items": s.storageItems()})
		case "/api/profiles":
			respond(w, 200, s.profileStatus())
		case "/api/preferences":
			respond(w, 200, s.preferences())
		}
		return true
	}
	if r.URL.Path == "/api/preview" {
		var req struct {
			State    State  `json:"state"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return true
		}
		s.mutation.Lock()
		defer s.mutation.Unlock()
		if req.Revision != s.revision() {
			fail(w, 409, errors.New("настройки изменились; обновите страницу"))
			return true
		}
		next, e := validate(req.State)
		if e != nil {
			fail(w, 400, e)
			return true
		}
		old, e := s.state()
		if e != nil {
			fail(w, 500, e)
			return true
		}
		respond(w, 200, previewChanges(old, next))
		return true
	}
	// Read-only network diagnostics share one slot; they cannot stack processes.
	if r.URL.Path == "/api/connectivity" || r.URL.Path == "/api/dependencies" || r.URL.Path == "/api/xhttp-benchmark" || r.URL.Path == "/api/load-test" {
		if !s.diagnosticMu.TryLock() {
			fail(w, 409, errors.New("другая диагностика уже выполняется"))
			return true
		}
		defer s.diagnosticMu.Unlock()
		switch r.URL.Path {
		case "/api/connectivity":
			c := s.collectConnectivity(true)
			s.saveConnectivity(c)
			respond(w, 200, c)
		case "/api/dependencies":
			var req struct {
				Service string `json:"service"`
			}
			if e := decode(w, r, &req); e != nil {
				fail(w, 400, e)
				break
			}
			ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
			defer cancel()
			d, e := s.dependencies(ctx, req.Service)
			if e != nil {
				fail(w, 400, e)
			} else {
				respond(w, 200, d)
			}
		case "/api/xhttp-benchmark":
			release, e := s.pause()
			if e != nil {
				fail(w, 409, e)
				break
			}
			defer release()
			ctx, cancel := context.WithTimeout(r.Context(), 130*time.Second)
			defer cancel()
			d, e := s.benchmarkXHTTP(ctx)
			if e != nil {
				fail(w, 400, e)
			} else {
				respond(w, 200, map[string]any{"results": d, "note": "Три коротких последовательных теста на отдельном процессе Xray. Рабочая конфигурация не менялась. Сравнение не доказывает стабильность за длительное время; повторите на своей линии в разное время суток."})
			}
		case "/api/load-test":
			var req struct {
				Via string `json:"via"`
			}
			if e := decode(w, r, &req); e != nil {
				fail(w, 400, e)
				break
			}
			ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
			defer cancel()
			d, e := s.loadExperiment(ctx, req.Via)
			if e != nil {
				fail(w, 400, e)
			} else {
				respond(w, 200, d)
			}
		}
		return true
	}
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция уже выполняется"))
		return true
	}
	defer s.mutation.Unlock()
	// State applications take their own shared process lock.
	if r.URL.Path == "/api/timer" {
		var req struct {
			Group    string `json:"group"`
			Minutes  int    `json:"minutes"`
			QUIC     *bool  `json:"quic"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return true
		}
		if e := s.temporaryGroup(req.Group, req.Minutes, req.QUIC, req.Revision); e != nil {
			fail(w, 400, e)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	if r.URL.Path == "/api/quic-observation" {
		var req struct {
			Group   string `json:"group"`
			Verdict string `json:"verdict"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return true
		}
		if e := s.saveQuicObservation(req.Group, req.Verdict); e != nil {
			fail(w, 400, e)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	release, e := s.pause()
	if e != nil {
		fail(w, 409, e)
		return true
	}
	defer release()
	switch r.URL.Path {
	case "/api/preferences":
		var req struct {
			Preferences Preferences `json:"preferences"`
			Password    string      `json:"password"`
		}
		if e = decode(w, r, &req); e == nil {
			e = s.currentPassword(r, req.Password)
		}
		if e == nil {
			p := req.Preferences
			if p.Interval < 120 || p.Interval > 3600 {
				e = errors.New("интервал от 120 до 3600 секунд")
			} else if p.AutoFailover && !s.backupConfigured() {
				e = errors.New("сначала добавьте и проверьте резервный канал")
			} else {
				e = atomicWrite(filepath.Join(s.dir, "preferences.json"), jsonBytes(p))
			}
		}
	case "/api/storage-delete":
		var req struct {
			ID          string `json:"id"`
			Password    string `json:"password"`
			Acknowledge bool   `json:"acknowledge"`
		}
		if e = decode(w, r, &req); e == nil {
			e = s.currentPassword(r, req.Password)
		}
		if e == nil && !req.Acknowledge {
			e = errors.New("подтвердите потерю выбранной копии")
		}
		if e == nil {
			e = s.deleteStorage(req.ID)
		}
	case "/api/recover-part":
		var req struct {
			Component string `json:"component"`
			Password  string `json:"password"`
		}
		if e = decode(w, r, &req); e != nil {
			break
		}
		if req.Component == "xray" {
			e = s.currentPassword(r, req.Password)
		}
		if e != nil {
			break
		}
		if s.disabled() {
			e = errors.New("VPN выключен")
			break
		}
		switch req.Component {
		case "auto":
			e = s.recoverComponents("Ручная диагностика")
		case "dns":
			_, e = s.run("sh", filepath.Join(s.dir, "reload-dns.sh"))
		case "rules":
			e = s.syncGroupRouting()
		case "xray":
			e = s.restartMainXray()
		default:
			e = errors.New("неверный компонент")
		}
		if e == nil {
			appendEvent("manual_repair", "Восстановлен компонент: "+req.Component)
		}
	case "/api/profile-save", "/api/profile-switch", "/api/xhttp-apply":
		var req struct {
			Password string `json:"password"`
			Outbound string `json:"outbound"`
			Profile  string `json:"profile"`
			Preset   string `json:"preset"`
		}
		if e = decode(w, r, &req); e == nil {
			e = s.currentPassword(r, req.Password)
		}
		if e != nil {
			break
		}
		if !s.diagnosticMu.TryLock() {
			e = errors.New("диагностика занята")
			break
		}
		defer s.diagnosticMu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
		defer cancel()
		switch r.URL.Path {
		case "/api/profile-save":
			e = s.saveBackup(ctx, req.Outbound)
		case "/api/profile-switch":
			e = s.switchProfile(ctx, req.Profile)
		case "/api/xhttp-apply":
			e = s.applyTuning(ctx, req.Preset)
		}
	}
	if e != nil {
		fail(w, 400, errors.New(s.scrub(e.Error())))
	} else {
		respond(w, 200, map[string]bool{"ok": true})
	}
	return true
}
