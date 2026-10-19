package panel

import (
	"encoding/json"
	"net"
	"testing"
)

func TestDiagnoseQueriesRouterDNSAndIncludesDeviceRoute(t *testing.T) {
	conn, e := net.ListenPacket("udp", "127.0.0.1:10535")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, e := conn.ReadFrom(buf)
			if e != nil {
				return
			}
			if n < 17 {
				continue
			}
			end := 12
			for end < n && buf[end] != 0 {
				end += 1 + int(buf[end])
			}
			end += 5
			if end > n {
				continue
			}
			reply := append([]byte{}, buf[:end]...)
			reply[2] = 0x81
			reply[3] = 0x80
			reply[6] = 0
			reply[7] = 1
			reply[8] = 0
			reply[9] = 0
			reply[10] = 0
			reply[11] = 0
			reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 5)
			conn.WriteTo(reply, addr)
		}
	}()
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	w := request(s, "POST", "/api/diagnose", `{"domain":"https://www.youtube.com/watch"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d struct {
		Domain    string `json:"domain"`
		Addresses []struct {
			IP     string `json:"ip"`
			ViaVPN bool   `json:"via_vpn"`
		} `json:"addresses"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &d); e != nil {
		t.Fatal(e)
	}
	if d.Domain != "www.youtube.com" || len(d.Addresses) == 0 || d.Addresses[0].IP != "192.0.2.5" || !d.Addresses[0].ViaVPN {
		t.Fatal(w.Body.String())
	}
	if !routeBypass("192.168.50.1") || !natAddressBypass("198.51.100.20", "-A DV_NAT -d 198.51.100.20/32 -j RETURN") || routeBypass("192.0.2.5") {
		t.Fatal("bypass precedence differs from router script")
	}
}
