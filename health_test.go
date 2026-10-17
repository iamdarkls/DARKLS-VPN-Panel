package main

import (
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProbeFallbackAndInvalidResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/trace" {
			fmt.Fprint(w, "h=example\nip=192.0.2.1\n")
			return
		}
		fmt.Fprint(w, "captive portal")
	}))
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	if err := probeTargets(client, []probeTarget{{server.URL, 204, ""}, {server.URL + "/ok", 204, ""}}); err != nil {
		t.Fatal(err)
	}
	if err := probeTargets(client, []probeTarget{{server.URL, 200, "\nip="}}); err == nil {
		t.Fatal("accepted unrelated response")
	}
	if err := probeTargets(client, []probeTarget{{server.URL + "/trace", 200, "\nip="}}); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(probeRoots) {
		t.Fatal("invalid CA roots")
	}
}

func TestProbeUsesSOCKSRemoteDNS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			result <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		var greeting [2]byte
		if _, e = io.ReadFull(conn, greeting[:]); e != nil {
			result <- e
			return
		}
		methods := make([]byte, int(greeting[1]))
		io.ReadFull(conn, methods)
		conn.Write([]byte{5, 0})
		var request [5]byte
		if _, e = io.ReadFull(conn, request[:]); e != nil {
			result <- e
			return
		}
		if request[3] != 3 {
			result <- fmt.Errorf("SOCKS must use remote domain DNS")
			return
		}
		domain := make([]byte, int(request[4]))
		io.ReadFull(conn, domain)
		if string(domain) != "vpn-check.invalid" {
			result <- fmt.Errorf("unexpected destination: %s", domain)
			return
		}
		var port [2]byte
		io.ReadFull(conn, port[:])
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
		buf := make([]byte, 4096)
		if _, e = conn.Read(buf); e != nil {
			result <- e
			return
		}
		_, e = fmt.Fprint(conn, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
		result <- e
	}()
	proxy, _ := url.Parse("socks5h://" + listener.Addr().String())
	client, transport := newProbeClient(proxy, x509.NewCertPool())
	defer transport.CloseIdleConnections()
	if e := probeTargets(client, []probeTarget{{"http://vpn-check.invalid/", 204, ""}}); e != nil {
		t.Fatal(e)
	}
	if e := <-result; e != nil {
		t.Fatal(e)
	}
}
