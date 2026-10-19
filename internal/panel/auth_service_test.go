package panel

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Composing two services with the same credentials must not share sessions or
// an IP's failed-password budget. No router/Runner is needed for this component.
func TestAuthServiceInstancesDoNotShareState(t *testing.T) {
	first, second := newAuthService(), newAuthService()
	auth := Auth{User: "admin", Salt: "isolated-test", Hash: passwordHash("test-password", "isolated-test")}
	first.auth, second.auth = auth, auth
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"user":"admin","password":"test-password"}`))
	req.RemoteAddr = "192.168.50.224:12345"
	result := httptest.NewRecorder()
	first.login(result, req)
	if result.Code != 200 {
		t.Fatal(result.Code)
	}
	check := httptest.NewRequest("GET", "/api/state", nil)
	check.AddCookie(result.Result().Cookies()[0])
	if _, ok := first.session(check); !ok {
		t.Fatal("owner rejected its session")
	}
	if _, ok := second.session(check); ok {
		t.Fatal("session crossed component instances")
	}
	now := time.Now()
	for range 10 {
		if err := first.reservePasswordAttempt("client", now); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.reservePasswordAttempt("client", now); err == nil {
		t.Fatal("limit was not enforced")
	}
	if err := second.reservePasswordAttempt("client", now); err != nil {
		t.Fatal("another service inherited failed attempts")
	}
}
