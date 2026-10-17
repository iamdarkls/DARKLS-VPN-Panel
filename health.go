package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Public CA roots are bundled because stock routers may lack a system CA store.
//
//go:embed certs/probe-roots.pem
var probeRoots []byte

type probeTarget struct {
	url    string
	status int
	marker string
}

func probeTargets(client *http.Client, targets []probeTarget) error {
	var failures []string
	for _, target := range targets {
		resp, err := client.Get(target.url)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.url, err))
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		if readErr == nil && resp.StatusCode == target.status && (target.marker == "" || strings.Contains(string(body), target.marker)) {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s: HTTP %d, invalid response (%v)", target.url, resp.StatusCode, readErr))
	}
	return fmt.Errorf("VPN HTTPS failed: %s", strings.Join(failures, "; "))
}

func checkVPNConnection() error {
	proxy, _ := url.Parse("socks5h://127.0.0.1:10808")
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(probeRoots) {
		return fmt.Errorf("invalid bundled CA roots")
	}
	client, transport := newProbeClient(proxy, roots)
	defer transport.CloseIdleConnections()
	return probeTargets(client, []probeTarget{
		{"https://www.gstatic.com/generate_204", 204, ""},
		{"https://www.cloudflare.com/cdn-cgi/trace", 200, "\nip="},
	})
}

func newProbeClient(proxy *url.URL, roots *x509.CertPool) (*http.Client, *http.Transport) {
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxy),
		DialContext:         (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 5 * time.Second,
		DisableKeepAlives:   true,
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return client, transport
}
