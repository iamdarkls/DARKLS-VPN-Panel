package panel

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGRPCLinks(t *testing.T) {
	prefix := "vless://00000000-0000-4000-8000-000000000001@server.example:443?type=grpc&security=tls&serviceName=%2Fcustom%2FTun&alpn=h2&"
	for _, tc := range []struct {
		query, authority string
		multi            bool
	}{
		{"authority=", "", false}, {"authority=edge.example&mode=gun", "edge.example", false},
		{"authority=edge.example&mode=multi", "edge.example", true}, {"multiMode=true", "", true}, {"multiMode=false", "", false},
	} {
		out, e := parseVLESSLink(prefix + tc.query)
		if e != nil {
			t.Fatal(tc, e)
		}
		stream := out["streamSettings"].(map[string]any)
		g := stream["grpcSettings"].(map[string]any)
		if g["serviceName"] != "/custom/Tun" || g["multiMode"] != tc.multi {
			t.Fatal(tc, g)
		}
		if tc.authority != "" && g["authority"] != tc.authority {
			t.Fatal(tc, g)
		}
		if stream["tlsSettings"].(map[string]any)["alpn"].([]string)[0] != "h2" {
			t.Fatal(stream)
		}
	}
	for _, q := range []string{"multiMode=invalid", "mode=gun&multiMode=true", "mode=multi&multiMode=false", "mode=unknown"} {
		if _, e := parseVLESSLink(prefix + q); e == nil {
			t.Fatal("accepted conflicting mode", q)
		}
	}
}

// Optional end-to-end check with the official Xray executable. Both sides and
// the HTTP destination are on loopback; no subscription or VPN keys are used.
// TLS explicitly trusts the synthetic certificate for the local test server;
// certificate verification remains enabled.
func TestGRPCNativeLoopback(t *testing.T) {
	binary := os.Getenv("DARKLS_XRAY_TEST_BINARY")
	if binary == "" {
		t.Skip("set DARKLS_XRAY_TEST_BINARY to run native gRPC loopback")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "grpc-loopback-ok") }))
	defer target.Close()
	freePort := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	tlsSample := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := tlsSample.TLS.Certificates[0]
	tlsSample.Close()
	certPath, keyPath := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	keyDER, e := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); e != nil {
		t.Fatal(e)
	}
	_, targetPort, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	for _, tc := range []struct{ mode, security string }{{"gun", "none"}, {"multi", "none"}, {"gun", "tls"}, {"multi", "tls"}} {
		t.Run(tc.mode+"/"+tc.security, func(t *testing.T) {
			mode := tc.mode
			serverPort, socksPort := freePort(), freePort()
			id := "00000000-0000-4000-8000-000000000001"
			out, e := parseVLESSLink(fmt.Sprintf("vless://%s@127.0.0.1:%d?type=grpc&security=%s&serviceName=panel-test&authority=grpc.test&mode=%s&alpn=h2&sni=example.com", id, serverPort, tc.security, mode))
			if e != nil {
				t.Fatal(e)
			}
			if tc.security == "tls" {
				out["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = []any{map[string]any{"certificateFile": certPath, "usage": "verify"}}
			}
			server := map[string]any{"log": map[string]any{"loglevel": "error"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": serverPort, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"}, "streamSettings": map[string]any{"network": "grpc", "security": "none", "grpcSettings": map[string]any{"serviceName": "panel-test"}}}}, "outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1/32"}, "port": targetPort}}}}}}
			if tc.security == "tls" {
				stream := server["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)
				stream["security"] = "tls"
				stream["tlsSettings"] = map[string]any{"alpn": []string{"h2"}, "certificates": []any{map[string]any{"certificateFile": certPath, "keyFile": keyPath}}}
			}
			client := map[string]any{"log": map[string]any{"loglevel": "error"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{out}}
			start := func(name string, cfg map[string]any, port int) {
				path := filepath.Join(t.TempDir(), name+".json")
				if e := os.WriteFile(path, jsonBytes(cfg), 0600); e != nil {
					t.Fatal(e)
				}
				cmd := exec.Command(binary, "run", "-config", path)
				cmd.Stdout = io.Discard
				cmd.Stderr = io.Discard
				if e := cmd.Start(); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
					if e == nil {
						c.Close()
						return
					}
					time.Sleep(30 * time.Millisecond)
				}
				t.Fatal("Xray listener did not start", name)
			}
			start("server", server, serverPort)
			start("client", client, socksPort)
			c := transportClient(fmt.Sprintf("127.0.0.1:%d", socksPort))
			defer c.CloseIdleConnections()
			resp, e := c.Get(target.URL)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "grpc-loopback-ok") {
				t.Fatal(string(body))
			}
		})
	}
}

func TestGRPCParseAPIReportsCauseWithoutCredentials(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	body := `{"password":"old-password-123","outbound":"vless://00000000-0000-4000-8000-000000000001@private.example:443?type=grpc&authority=edge.example&mode=gun&multiMode=true"}`
	w := request(s, "POST", "/api/connection-parse", body, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "multiMode") {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "00000000") || strings.Contains(w.Body.String(), "private.example") {
		t.Fatal("credentials in parse failure")
	}
	body = strings.ReplaceAll(body, "&multiMode=true", "")
	w = request(s, "POST", "/api/connection-parse", body, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"authority":"edge.example"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestGRPCTLSValidationHintDoesNotExposeCandidate(t *testing.T) {
	hint := connectionConfigError([]byte(`candidate-private-key The feature "allowInsecure" has been removed`)).Error()
	if !strings.Contains(hint, "allowInsecure") || strings.Contains(hint, "candidate-private-key") {
		t.Fatal(hint)
	}
	out, e := parseVLESSLink("vless://test@example.com:443?type=grpc&security=tls&allowInsecure=0")
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := out["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["allowInsecure"]; ok {
		t.Fatal("redundant deprecated false flag retained")
	}
}
