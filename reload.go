package main

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

// Build the Telegram set off to the side, then swap atomically. Domain-based
// sets are deliberately preserved: clients can cache DNS longer than dnsmasq.
func (s *Server) syncTelegramSet() error {
	data, err := os.ReadFile(filepath.Join(s.root, "telegram-networks.txt"))
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"create", "darkls_tg", "hash:net", "family", "inet", "-exist"},
		{"create", "darkls_tg_next", "hash:net", "family", "inet", "-exist"},
		{"flush", "darkls_tg_next"},
	} {
		if out, e := s.run("ipset", args...); e != nil {
			return fmt.Errorf("Telegram IP set: %s", limited(out))
		}
	}
	defer s.run("ipset", "destroy", "darkls_tg_next")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, e := netip.ParsePrefix(line)
		if e != nil || !prefix.Addr().Is4() {
			return fmt.Errorf("invalid Telegram IPv4 network: %s", line)
		}
		if out, e := s.run("ipset", "add", "darkls_tg_next", prefix.Masked().String(), "-exist"); e != nil {
			return fmt.Errorf("Telegram network: %s", limited(out))
		}
	}
	if out, e := s.run("ipset", "swap", "darkls_tg_next", "darkls_tg"); e != nil {
		return fmt.Errorf("Telegram set swap: %s", limited(out))
	}
	return nil
}

func (s *Server) reloadRouting(dnsChanged, telegramChanged bool) error {
	if s.disabled() {
		return nil
	}
	if telegramChanged {
		if e := s.syncTelegramSet(); e != nil {
			return e
		}
	}
	if dnsChanged {
		if out, e := s.run("sh", filepath.Join(s.dir, "reload-dns.sh")); e != nil {
			return fmt.Errorf("перезагрузка DNS: %s", limited(out))
		}
	}
	return nil
}
