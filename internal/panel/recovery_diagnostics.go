package panel

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Called only after the initial DNS probe failed with working tunnel HTTPS.
// Any successful retry cancels recovery. If HTTPS also fails, let the normal
// tunnel-failure policy handle it on the next watchdog cycle.
func confirmTunnelDNSFailure(dns, https func() error, wait func()) (bool, []string) {
	failures := make([]string, 0, 2)
	for attempt := 0; attempt < 2; attempt++ {
		wait()
		if err := dns(); err != nil {
			failures = append(failures, err.Error())
		} else {
			return false, failures
		}
	}
	return https() == nil, failures
}

type recoverySnapshot struct {
	At           int64             `json:"at"`
	Reason       string            `json:"reason"`
	Connectivity Connectivity      `json:"connectivity"`
	Resources    map[string]string `json:"resources"`
	XrayErrors   string            `json:"xray_errors"`
}

// Keep only the most recent snapshot in RAM. Never include configuration,
// process command lines, full DNS/access logs or VPN credentials.
func (s *Server) captureRecovery(c Connectivity, reason string) {
	resources := map[string]string{}
	for _, name := range []string{"meminfo", "uptime", "sys/net/netfilter/nf_conntrack_count", "sys/net/netfilter/nf_conntrack_max"} {
		if b, err := os.ReadFile(filepath.Join("/proc", name)); err == nil && len(b) <= 16<<10 {
			resources[name] = string(b)
		}
	}
	for _, pid := range s.mainXrayPIDs() {
		b, _ := os.ReadFile(filepath.Join("/proc", pid, "status"))
		var lines []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmSwap:") || strings.HasPrefix(line, "Threads:") {
				lines = append(lines, line)
			}
		}
		resources["xray-"+pid] = strings.Join(lines, "\n")
	}
	snapshot := recoverySnapshot{time.Now().Unix(), reason, c, resources, errorLines(tailLog("/tmp/xray.log"))}
	_ = atomicWrite(s.runtimePath("last-recovery.json"), []byte(s.scrub(string(jsonBytes(snapshot)))))
}
