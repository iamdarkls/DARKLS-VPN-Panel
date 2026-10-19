package panel

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type cpuSample struct{ total, idle uint64 }

func parseCPU(data string) cpuSample {
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var c cpuSample
		for i, raw := range f[1:] {
			if i >= 8 {
				break
			}
			v, _ := strconv.ParseUint(raw, 10, 64)
			c.total += v
			if i == 3 || i == 4 {
				c.idle += v
			}
		}
		return c
	}
	return cpuSample{}
}
func parseMem(data string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			v, _ := strconv.ParseUint(f[1], 10, 64)
			out[strings.TrimSuffix(f[0], ":")] = v
		}
	}
	return out
}
func processMemory(path string) map[string]uint64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return map[string]uint64{}
	}
	m := parseMem(string(b))
	return map[string]uint64{"rss_kib": m["VmRSS"], "swap_kib": m["VmSwap"]}
}
func (s *Server) metrics() map[string]any {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	b, _ := os.ReadFile("/proc/stat")
	cur := parseCPU(string(b))
	// The first request takes two small samples instead of waiting for a second
	// page refresh. The 200 ms delay happens once per panel process.
	if s.lastCPU.total == 0 && cur.total > 0 {
		s.lastCPU = cur
		time.Sleep(200 * time.Millisecond)
		b, _ = os.ReadFile("/proc/stat")
		cur = parseCPU(string(b))
	}
	var cpu any = nil
	if s.lastCPU.total > 0 && cur.total > s.lastCPU.total && cur.idle >= s.lastCPU.idle {
		total := cur.total - s.lastCPU.total
		idle := cur.idle - s.lastCPU.idle
		if idle <= total {
			cpu = 100 * float64(total-idle) / float64(total)
		}
	}
	s.lastCPU = cur
	b, _ = os.ReadFile("/proc/meminfo")
	m := parseMem(string(b))
	available := m["MemAvailable"]
	if available == 0 {
		available = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	b, _ = os.ReadFile("/proc/loadavg")
	load := strings.Fields(string(b))
	if len(load) > 3 {
		load = load[:3]
	}
	b, _ = os.ReadFile("/proc/uptime")
	var uptime float64
	_, _ = fmt.Sscanf(string(b), "%f", &uptime)
	var fs syscall.Statfs_t
	_ = syscall.Statfs(s.root, &fs)
	processes := map[string]any{"panel": processMemory("/proc/self/status")}
	if pids := s.mainXrayPIDs(); len(pids) > 0 {
		processes["xray"] = processMemory("/proc/" + pids[0] + "/status")
	}

	mounts, _ := os.ReadFile("/proc/mounts")
	usb := strings.Contains(string(mounts), " /tmp/mnt/")
	return map[string]any{"cpu_percent": cpu, "cpu_cores": runtime.NumCPU(), "load": load, "uptime_seconds": uptime, "memory_total_kib": m["MemTotal"], "memory_available_kib": available, "jffs_total_bytes": fs.Blocks * uint64(fs.Bsize), "jffs_free_bytes": fs.Bavail * uint64(fs.Bsize), "processes": processes, "usb": usb}
}
