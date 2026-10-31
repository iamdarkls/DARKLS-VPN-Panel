package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

// Set by the release builder, never by a browser request or a settings file.
var panelReleaseChannel = "stable"

func validPanelChannel(channel string) bool { return channel == "stable" || channel == "preview" }
func (s *Server) panelUpdateChannel() string {
	var settings struct {
		Channel string `json:"channel"`
	}
	if readJSON(filepath.Join(s.dir, "panel-updates.json"), &settings) == nil && validPanelChannel(settings.Channel) {
		return settings.Channel
	}
	return "stable"
}
func signedPanelChannel(data []byte) string {
	var v struct {
		Channel string `json:"channel"`
	}
	if json.Unmarshal(data, &v) != nil {
		return "invalid"
	}
	if v.Channel == "" {
		return "stable"
	}
	return v.Channel
}
func (s *Server) panelChannelAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Password string `json:"password"`
		Channel  string `json:"channel"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	if e := s.currentPassword(r, req.Password); e != nil {
		fail(w, 403, e)
		return
	}
	if !validPanelChannel(req.Channel) {
		fail(w, 400, errors.New("неверный канал обновлений"))
		return
	}
	if !s.githubMu.TryLock() {
		fail(w, 409, errors.New("запрос к GitHub выполняется"))
		return
	}
	defer s.githubMu.Unlock()
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция выполняется"))
		return
	}
	defer s.mutation.Unlock()
	release, e := s.pause()
	if e != nil {
		fail(w, 409, e)
		return
	}
	defer release()
	if e = atomicWrite(filepath.Join(s.dir, "panel-updates.json"), jsonBytes(map[string]string{"channel": req.Channel})); e != nil {
		fail(w, 500, e)
		return
	}
	_ = os.Remove("/tmp/darkls-github-status.json")
	respond(w, 200, map[string]bool{"ok": true})
}
