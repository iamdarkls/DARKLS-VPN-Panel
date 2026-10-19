package main

import (
	"net/http/httptest"
	"regexp"
	"testing"
)

// Extraction of frontend modules must not produce an unreachable script, or
// accidentally open access to arbitrary embedded paths.
func TestHTMLAssetsServedThroughExplicitAllowlist(t *testing.T) {
	s, _ := fixture(t)
	handler := s.handler()
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+s.host+path, nil)
		r.RemoteAddr = "192.168.50.224:12345"
		if method == "POST" {
			r.Header.Set("Origin", "http://"+s.host)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	index := request("GET", "/")
	if index.Code != 200 {
		t.Fatal(index.Code)
	}
	scripts := regexp.MustCompile(`src="([^"]+)"`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(scripts) < 2 {
		t.Fatal("frontend scripts missing")
	}
	for _, script := range scripts {
		for _, method := range []string{"GET", "HEAD"} {
			if w := request(method, script[1]); w.Code != 200 {
				t.Errorf("%s %s: %d", method, script[1], w.Code)
			}
		}
	}
	if w := request("POST", "/ui.js"); w.Code != 405 {
		t.Errorf("asset POST: %d", w.Code)
	}
	for _, path := range []string{"/index.html", "/auth.json", "/main.go", "/missing.js"} {
		if w := request("GET", path); w.Code != 404 {
			t.Errorf("unexpected public file %s: %d", path, w.Code)
		}
	}
}
