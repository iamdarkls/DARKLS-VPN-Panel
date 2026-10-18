package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DeviceRef struct {
	MAC  string `json:"mac"`
	Name string `json:"name,omitempty"`
}
type DevicePolicy struct {
	Mode    string      `json:"mode"`
	Devices []DeviceRef `json:"devices"`
}
type LANDevice struct {
	MAC    string `json:"mac"`
	IP     string `json:"ip"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

func normalizeMAC(raw string) (string, error) {
	m, e := net.ParseMAC(strings.TrimSpace(raw))
	if e != nil || len(m) != 6 || m[0]&1 != 0 || m.String() == "00:00:00:00:00:00" {
		return "", errors.New("неверный MAC-адрес")
	}
	return strings.ToLower(m.String()), nil
}
func validatePolicy(p *DevicePolicy) error {
	if p.Mode == "" {
		p.Mode = "all"
	}
	if p.Mode != "all" && p.Mode != "include" && p.Mode != "exclude" {
		return errors.New("неверный режим устройств")
	}
	if len(p.Devices) > 128 {
		return errors.New("не более 128 устройств")
	}
	out := []DeviceRef{}
	seen := map[string]bool{}
	for _, d := range p.Devices {
		mac, e := normalizeMAC(d.MAC)
		if e != nil {
			return e
		}
		if seen[mac] {
			continue
		}
		seen[mac] = true
		d.MAC = mac
		d.Name = strings.TrimSpace(d.Name)
		if len(d.Name) > 128 {
			d.Name = ""
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	p.Devices = out
	return nil
}
func validLANIP(ip string) bool {
	a, e := netip.ParseAddr(ip)
	return e == nil && a.Is4() && netip.MustParsePrefix("192.168.50.0/24").Contains(a) && ip != "192.168.50.0" && ip != "192.168.50.1" && ip != "192.168.50.255"
}
func parseLANDevices(leases, arp string) []LANDevice {
	byMAC := map[string]LANDevice{}
	for _, line := range strings.Split(leases, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !validLANIP(f[2]) {
			continue
		}
		mac, e := normalizeMAC(f[1])
		if e != nil {
			continue
		}
		name := f[3]
		if name == "*" {
			name = ""
		}
		if len(name) > 128 {
			name = ""
		}
		expiry, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		ip := f[2]
		if expiry > 0 && expiry < time.Now().Unix() {
			ip = ""
		}
		byMAC[mac] = LANDevice{MAC: mac, IP: ip, Name: name}
	}
	for _, line := range strings.Split(arp, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[5] != "br0" || !validLANIP(f[0]) {
			continue
		}
		flags, err := strconv.ParseUint(f[2], 0, 32)
		if err != nil || flags&2 == 0 {
			continue
		}
		mac, e := normalizeMAC(f[3])
		if e != nil {
			continue
		}
		d := byMAC[mac]
		d.MAC = mac
		d.IP = f[0]
		d.Online = true
		byMAC[mac] = d
	}
	owners := map[string]string{}
	for _, d := range byMAC {
		if d.Online {
			owners[d.IP] = d.MAC
		}
	}
	out := []LANDevice{}
	for _, d := range byMAC {
		if owner, ok := owners[d.IP]; ok && owner != d.MAC {
			d.IP = ""
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return out[i].IP < out[j].IP
	})
	return out
}
func (s *Server) lanDevices() []LANDevice {
	var leases []byte
	for _, p := range []string{"/var/lib/misc/dnsmasq.leases", "/tmp/var/lib/misc/dnsmasq.leases", "/tmp/dnsmasq.leases"} {
		if b, e := os.ReadFile(p); e == nil {
			leases = b
			break
		}
	}
	arp, _ := os.ReadFile("/proc/net/arp")
	return parseLANDevices(string(leases), string(arp))
}
func (s *Server) syncDevices(force bool) error {
	st, e := s.state()
	if e != nil {
		return e
	}
	p := st.Policy
	ips, digest, changed := s.devicePlan(p)
	if !force && !changed {
		return nil
	}
	digestPath := s.runtimePath("device-digest")
	if p.Mode != "all" {
		for _, args := range [][]string{{"create", "darkls_devices", "hash:ip", "family", "inet", "-exist"}, {"create", "darkls_dev_next", "hash:ip", "family", "inet", "-exist"}, {"flush", "darkls_dev_next"}} {
			if out, e := s.run("ipset", args...); e != nil {
				return fmt.Errorf("набор устройств: %s", limited(out))
			}
		}
		defer s.run("ipset", "destroy", "darkls_dev_next")
		for _, ip := range ips {
			if out, e := s.run("ipset", "add", "darkls_dev_next", ip, "-exist"); e != nil {
				return fmt.Errorf("IP устройства: %s", limited(out))
			}
		}
		if out, e := s.run("ipset", "swap", "darkls_dev_next", "darkls_devices"); e != nil {
			return fmt.Errorf("замена набора устройств: %s", limited(out))
		}
	}
	for _, spec := range [][2]string{{"nat", "DV_NAT"}, {"mangle", "DV_UDP"}} {
		// These chains already exist during vpn.sh startup, before LAN hooks attach.
		for _, negate := range []bool{false, true} {
			args := []string{"-t", spec[0], "-D", spec[1], "-m", "set"}
			if negate {
				args = append(args, "!")
			}
			args = append(args, "--match-set", "darkls_devices", "src", "-j", "RETURN")
			for i := 0; i < 4; i++ {
				if _, e := s.run("iptables", args...); e != nil {
					break
				}
			}
		}
		if p.Mode != "all" {
			args := []string{"-t", spec[0], "-I", spec[1], "1", "-m", "set"}
			if p.Mode == "include" {
				args = append(args, "!")
			}
			args = append(args, "--match-set", "darkls_devices", "src", "-j", "RETURN")
			if out, e := s.run("iptables", args...); e != nil {
				return fmt.Errorf("правила устройств: %s", limited(out))
			}
		}
	}
	return atomicWrite(digestPath, []byte(digest))
}

// devicePlan is read-only. The fast loop can skip flock and /proc scans when
// DHCP/ARP still produces the same source IP set. Recompute after taking pause.
func (s *Server) devicePlan(p DevicePolicy) (ips []string, digest string, changed bool) {
	previous, _ := os.ReadFile(s.runtimePath("device-digest"))

	selected := map[string]bool{}
	for _, d := range p.Devices {
		selected[d.MAC] = true
	}
	if p.Mode != "all" {
		for _, d := range s.lanDevices() {
			if selected[d.MAC] && validLANIP(d.IP) {
				ips = append(ips, d.IP)
			}
		}
	}
	sort.Strings(ips)
	sum := sha256.Sum256([]byte(p.Mode + "\n" + strings.Join(ips, "\n")))
	digest = hex.EncodeToString(sum[:])
	return ips, digest, string(previous) != digest && !(p.Mode == "all" && len(previous) == 0)
}
func (s *Server) devicePayload() map[string]any {
	devices := s.lanDevices()
	st, _ := s.state()
	found := map[string]bool{}
	for _, d := range devices {
		found[d.MAC] = true
	}
	for _, d := range st.Policy.Devices {
		if !found[d.MAC] {
			devices = append(devices, LANDevice{MAC: d.MAC, Name: d.Name})
		}
	}
	return map[string]any{"devices": devices, "policy": st.Policy, "refresh_seconds": 15}
}
func removeDeviceDigest() { _ = os.Remove(filepath.Clean("/tmp/darkls-device-digest")) }

func (s *Server) runtimePath(name string) string {
	if s.root != "/jffs/xray" {
		return filepath.Join(s.dir, "runtime-"+name)
	}
	return "/tmp/darkls-" + name
}
