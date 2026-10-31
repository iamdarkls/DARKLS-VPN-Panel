package panel

import (
	"os"
	"path/filepath"
	"time"
)

type SwitchRecord struct {
	At       int64  `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
	Reason   string `json:"reason"`
	Result   string `json:"result"`
	Verified bool   `json:"verified"`
}

func (s *Server) activeConnectionID() string {
	cfg, e := s.readVPNConfig()
	if e != nil {
		return "unknown"
	}
	out, e := vpnOutbound(cfg)
	if e != nil {
		return "unknown"
	}
	return s.connectionID(out)
}
func (s *Server) connectionID(out map[string]any) string {
	// Stable opaque identifiers avoid storing endpoint addresses, UUIDs or VPN keys.
	return "server-" + outboundHash(out)[:12]
}
func (s *Server) switchHistory() []SwitchRecord {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	return s.readSwitchHistory()
}
func (s *Server) readSwitchHistory() []SwitchRecord {
	entries := []SwitchRecord{}
	path := filepath.Join(s.dir, "switch-history.json")
	if st, e := os.Stat(path); e != nil || st.Size() > 128<<10 {
		return entries
	}
	if readJSON(path, &entries) != nil {
		return []SwitchRecord{}
	}
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}
	next := make([]SwitchRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.At >= time.Now().Add(-30*24*time.Hour).Unix() {
			next = append(next, entry)
		}
	}
	return next
}
func (s *Server) recordSwitch(from string, out map[string]any, reason string, err error) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	switch reason {
	case "automatic", "profile", "connection-use", "connection-apply", "connection-rollback":
	default:
		reason = "manual"
	}
	record := SwitchRecord{At: time.Now().Unix(), From: from, To: s.connectionID(out), Reason: reason, Result: "confirmed", Verified: err == nil && !s.disabled()}
	if err != nil {
		record.Result = "not_applied"
		if s.pendingRecovery() {
			record.Result = "recovery_pending"
		}
	} else if s.disabled() {
		record.Result = "configured_off"
	}
	entries := s.readSwitchHistory()
	next := make([]SwitchRecord, 0, 100)
	for _, entry := range entries {
		if entry.At >= time.Now().Add(-30*24*time.Hour).Unix() {
			next = append(next, entry)
		}
	}
	next = append(next, record)
	if len(next) > 100 {
		next = next[len(next)-100:]
	}
	if e := atomicWrite(filepath.Join(s.dir, "switch-history.json"), jsonBytes(next)); e != nil {
		appendEvent("history_error", "Не удалось сохранить историю переключений")
	}
}
