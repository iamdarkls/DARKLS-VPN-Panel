package panel

import (
	"bytes"
	"context"
	"darkls-panel/internal/routerenv"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// Copy/truncate in place: daemon descriptors keep pointing to the same inode.
// Xray is launched with O_APPEND by the updated VPN script.
func trimLog(path string, maximum, keep int64) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0600)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return nil
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() <= maximum {
		return nil
	}
	if _, e = f.Seek(-keep, io.SeekEnd); e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(f, keep))
	if e != nil {
		return e
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	if e = f.Truncate(0); e != nil {
		return e
	}
	if _, e = f.WriteAt(b, 0); e != nil {
		return e
	}
	return nil
}
func maintainLogs() {
	// The watchdog uses an append descriptor too; retain its inode like Xray's.
	if e := trimLog("/tmp/darkls-watch.log", 512<<10, 64<<10); e != nil {
		fmt.Fprintln(os.Stderr, "log maintenance: darkls-watch.log", e)
	}
	for _, name := range []string{"xray.log", "dnsmasq-vpn.log", "darkls-panel.log", "darkls-update-worker.log", "darkls-candidate.log", "darkls-components.log"} {
		if e := trimLog("/tmp/"+name, 2<<20, 256<<10); e != nil {
			fmt.Fprintln(os.Stderr, "log maintenance:", name, e)
		}
	}
}
func lookupRouterDNS(ctx context.Context, address, domain string) ([]net.IP, error) {
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
	}}
	return resolver.LookupIP(ctx, "ip4", domain)
}

// Inspect the contents, not just the presence, of the transparent proxy chains.
func validateRouterRules(nat, udp, input string) error {
	required := []struct {
		text  string
		parts []string
	}{
		{nat, []string{"-p udp ", "--dport 53 ", "-j REDIRECT ", "--to-ports " + routerenv.DNSPort}},
		{nat, []string{"-p tcp ", "--dport 53 ", "-j REDIRECT ", "--to-ports " + routerenv.DNSPort}},
		{udp, []string{"-p udp ", "--dport 53 ", "-j RETURN"}},
		{input, []string{"-p udp ", "--dport 443 ", "--match-set darkls_yt dst ", "-j REJECT ", "--reject-with icmp-port-unreachable"}},
		{input, []string{"-p udp ", "--mark " + routerenv.Mark + " ", "-j ACCEPT"}},
		{input, []string{"-p udp ", "--dport " + routerenv.DNSPort + " ", "-j ACCEPT"}},
		{input, []string{"-p tcp ", "--dports " + routerenv.AcceptedTCPPorts + " ", "-j ACCEPT"}},
	}
	for _, set := range []string{"darkls_vpn", "darkls_tg"} {
		required = append(required, struct {
			text  string
			parts []string
		}{nat, []string{"-p tcp ", "--match-set " + set + " dst ", "-j REDIRECT ", "--to-ports " + routerenv.TCPPort}}, struct {
			text  string
			parts []string
		}{udp, []string{"-p udp ", "--match-set " + set + " dst ", "-j TPROXY ", "--on-port " + routerenv.UDPPort, "--on-ip 127.0.0.1", "--tproxy-mark " + routerenv.Mark}})
	}
	for _, r := range required {
		found := false
		for _, line := range strings.Split(r.text, "\n") {
			match := true
			for _, part := range r.parts {
				if !strings.Contains(line+" ", part) {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing VPN rule: %s", strings.Join(r.parts, " "))
		}
	}
	return nil
}
func (s *Server) checkRouter() error {
	ctx, cancel := context.WithTimeout(s.networkContext(), 20*time.Second)
	defer cancel()
	err := s.networkOperation(ctx).checkRouterWithin()
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", errDiagnosticIncomplete, ctx.Err())
	}
	return err
}
func (s *Server) checkRouterWithin() error {
	if e := s.checkRoutingOnly(); e != nil {
		return e
	}
	// Random names avoid a stale positive DNS-cache entry hiding a dead upstream.
	for _, address := range []string{routerenv.TunnelDNS, routerenv.HelperDNS} {
		// Each inlet gets its own budget; the second must not inherit a
		// nearly expired deadline from a slow response on the first.
		ctx, cancel := context.WithTimeout(s.networkContext(), 5*time.Second)
		_, e := lookupRouterDNS(ctx, address, token()[:12]+".example.com")
		cancel()
		if de, ok := e.(*net.DNSError); ok && de.IsNotFound {
			continue
		}
		if e != nil {
			return fmt.Errorf("VPN DNS %s: %w", address, e)
		}
	}
	return nil
}
