package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticatedAPIResponsesAreNeverCachedOrStaticCompressed(t *testing.T) {
	s, _ := fixture(t)
	s.auth = Auth{Salt: "test-salt"}
	s.sessions["test"] = Session{CSRF: "token", AuthSalt: "test-salt", Expires: time.Now().Add(time.Hour)}
	for _, path := range []string{"/api/session", "/api/state"} {
		req := httptest.NewRequest("GET", "http://"+s.host+path, nil)
		req.RemoteAddr = "192.168.50.224:12345"
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("If-None-Match", "*")
		req.AddCookie(&http.Cookie{Name: "darkls_session", Value: "test"})
		result := httptest.NewRecorder()
		s.handler().ServeHTTP(result, req)
		if result.Code != 200 || result.Header().Get("Cache-Control") != "no-store" || result.Header().Get("ETag") != "" || result.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: %d %v", path, result.Code, result.Header())
		}
	}
}
