package panel

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func loginTest(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	s.auth = Auth{User: "admin", Salt: "salt", Hash: passwordHash("old-password-123", "salt")}
	w := request(s, "POST", "/api/login", `{"user":"admin","password":"old-password-123"}`, nil, "", "http://"+s.host, s.host)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var d map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	return w.Result().Cookies()[0], d["csrf"]
}
func TestPasswordChangeRevokesEveryOldSession(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	origin := "http://" + s.host
	bad := request(s, "POST", "/api/password", `{"current":"wrong","password":"new-password-123"}`, cookie, csrf, origin, s.host)
	if bad.Code != 403 {
		t.Fatal(bad.Code)
	}
	good := request(s, "POST", "/api/password", `{"current":"old-password-123","password":"new-password-123"}`, cookie, csrf, origin, s.host)
	if good.Code != 200 {
		t.Fatal(good.Body.String())
	}
	if w := request(s, "GET", "/api/state", "", cookie, "", "", s.host); w.Code != 401 {
		t.Fatal("old session survived")
	}
	if w := request(s, "POST", "/api/login", `{"user":"admin","password":"old-password-123"}`, nil, "", origin, s.host); w.Code != 401 {
		t.Fatal("old password accepted")
	}
	if w := request(s, "POST", "/api/login", `{"user":"admin","password":"new-password-123"}`, nil, "", origin, s.host); w.Code != 200 {
		t.Fatal("new password rejected", w.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(s.dir, "auth.json"))
	if bytes.Contains(data, []byte("new-password-123")) {
		t.Fatal("plaintext password persisted")
	}
	// A login that verified the old hash just before the change must not revive it.
	s.sessions["late-old"] = Session{CSRF: "late", AuthSalt: "salt", Expires: time.Now().Add(time.Hour)}
	if w := request(s, "GET", "/api/state", "", &http.Cookie{Name: "darkls_session", Value: "late-old"}, "", "", s.host); w.Code != 401 {
		t.Fatal("stale auth generation accepted")
	}
}
func TestImportPreviewDoesNotApplyAndValidatesPolicy(t *testing.T) {
	s, runner := fixture(t)
	cookie, csrf := loginTest(t, s)
	before := s.revision()
	body := `{"version":1,"groups":[{"id":"mine","name":"Группа","enabled":true,"domains":["https://EXAMPLE.com/test"],"category":"tools"}],"policy":{"mode":"exclude","devices":[{"mac":"02:11:22:33:44:55","name":"ПК"}]}}`
	w := request(s, "POST", "/api/import-preview", body, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("example.com")) {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.revision() != before || len(runner.calls) != 0 {
		t.Fatal("preview mutated runtime")
	}
	body = strings.Replace(body, "02:11:22:33:44:55", "$(reboot)", 1)
	if w := request(s, "POST", "/api/import-preview", body, cookie, csrf, "http://"+s.host, s.host); w.Code != 400 {
		t.Fatal("unsafe MAC accepted")
	}
}
func TestDHCPAndARPOwnership(t *testing.T) {
	leases := "100 02:11:22:33:44:55 192.168.50.20 Old *\n100 02:11:22:33:44:66 192.168.50.21 Phone *\n"
	arp := "192.168.50.20 0x1 0x6 02:11:22:33:44:77 * br0\n192.168.60.1 0x1 0x2 02:11:22:33:44:88 * br0\n"
	d := parseLANDevices(leases, arp)
	for _, v := range d {
		if v.MAC == "02:11:22:33:44:55" && v.IP != "" {
			t.Fatal("stale lease owns reassigned IP")
		}
		if v.MAC == "02:11:22:33:44:77" && (!v.Online || v.IP != "192.168.50.20") {
			t.Fatal("permanent completed ARP ignored")
		}
		if strings.HasPrefix(v.IP, "192.168.60.") {
			t.Fatal("outside LAN accepted")
		}
	}
}
func TestDevicePolicyRulesAndRollbackCleanup(t *testing.T) {
	s, r := fixture(t)
	st, _ := s.state()
	st.Policy = DevicePolicy{Mode: "include", Devices: []DeviceRef{}}
	_ = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st))
	if e := s.syncDevices(true); e != nil {
		t.Fatal(e)
	}
	calls := strings.Join(r.calls, "\n")
	if !strings.Contains(calls, "-I DV_NAT 1 -m set ! --match-set darkls_devices src -j RETURN") || !strings.Contains(calls, "-I DV_UDP 1 -m set ! --match-set darkls_devices src -j RETURN") {
		t.Fatal(calls)
	}
	r.calls = nil
	st.Policy.Mode = "all"
	_ = atomicWrite(filepath.Join(s.dir, "state.json"), jsonBytes(st))
	_ = os.Remove(s.runtimePath("device-digest"))
	if e := s.syncDevices(true); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(r.calls, "\n"), "-D DV_NAT") {
		t.Fatal("partial failed-policy rule not cleaned")
	}
}
func TestCategoryInferenceAndMetricsParsing(t *testing.T) {
	st, e := validate(State{Groups: []Group{{ID: "youtube", Name: "YouTube"}, {ID: "adult-one", Name: "18+"}, {ID: "mine", Name: "Моя"}}})
	if e != nil {
		t.Fatal(e)
	}
	if st.Groups[0].Category != "video" || st.Groups[1].Category != "adult" || st.Groups[2].Category != "custom" {
		t.Fatal(st)
	}
	c := parseCPU("cpu 10 1 2 100 5 6 7 8 50 20\n")
	if c.total != 139 || c.idle != 105 {
		t.Fatal(c)
	}
	m := parseMem("MemTotal: 1000 kB\nMemAvailable: 400 kB\n")
	if m["MemAvailable"] != 400 {
		t.Fatal(m)
	}
}
func samplePackage(t *testing.T, variant string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	m := PackageManifest{Format: 1, Version: version, Arch: runtime.GOARCH, Files: map[string]PackageFile{}}
	contents := map[string][]byte{}
	for _, name := range packageNames {
		data := []byte("#!/bin/sh\nexit 0\n")
		if name == "darkls-panel" {
			data = make([]byte, 64)
			copy(data, "\x7fELF")
			data[4] = 2
			data[5] = 1
			if runtime.GOARCH == "amd64" {
				data[18] = 62
			} else {
				data[18] = 183
			}
		}
		sum := sha256.Sum256(data)
		m.Files[name] = PackageFile{hex.EncodeToString(sum[:]), int64(len(data))}
		contents[name] = data
	}
	if variant == "corrupt" {
		contents["darkls-panel"][6] = 1
	}
	if variant == "old" {
		m.Version = "0.1.0"
	}
	for name, data := range contents {
		f, _ := z.Create("darkls-panel-update/" + name)
		_, _ = f.Write(data)
	}
	f, _ := z.Create("darkls-panel-update/manifest.json")
	_, _ = f.Write(jsonBytes(m))
	if variant == "traversal" {
		f, _ := z.Create("darkls-panel-update/../escape")
		_, _ = f.Write([]byte("bad"))
	}
	_ = z.Close()
	return b.Bytes()
}
func TestUpdatePackageIntegrityAndPaths(t *testing.T) {
	for _, variant := range []string{"good", "corrupt", "old", "traversal"} {
		data := samplePackage(t, variant)
		dir, m, e := stagePackage(bytes.NewReader(data), int64(len(data)))
		if variant == "good" {
			if e != nil {
				t.Fatal(e)
			}
			if m.Arch != runtime.GOARCH {
				t.Fatal(m)
			}
			if _, e = verifyDirectory(dir); e != nil {
				t.Fatal(e)
			}
			os.RemoveAll(dir)
		} else if e == nil {
			os.RemoveAll(dir)
			t.Fatal("accepted " + variant)
		}
	}
}
