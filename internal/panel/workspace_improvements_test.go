package panel

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestApplyProgressConfirmsPersistenceAndRollback(t *testing.T) {
	for _, failure := range []bool{false, true} {
		s, r := fixture(t)
		before := s.revision()
		state, _ := s.state()
		state.Groups = append(state.Groups, Group{ID: "progress", Name: "Progress", Enabled: true, Domains: []string{"progress.example"}})
		r.failReload = failure
		_, err := s.apply(state, before, "operation-progress")
		p := s.readApplyProgress()
		if p.ID != "operation-progress" || p.Finished == 0 || p.Backup == "" {
			t.Fatal(p)
		}
		if failure {
			if err == nil || !p.RolledBack || p.Saved || p.Verified || p.Phase != "rolled_back" || s.revision() != before {
				t.Fatal(p, err)
			}
		} else {
			if err != nil || !p.Saved || !p.Verified || p.Phase != "succeeded" {
				t.Fatal(p, err)
			}
		}
		p.Steps[0].State = "changed"
		if s.readApplyProgress().Steps[0].State == "changed" {
			t.Fatal("status leaked mutable slice")
		}
	}
}

type brokenReload struct{ Runner }

func (r brokenReload) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "sh" && len(args) > 0 && strings.HasSuffix(args[0], "reload-dns.sh") {
		return []byte("failure"), errors.New("failure")
	}
	return r.Runner.Run(ctx, name, args...)
}
func TestApplyProgressRetainsFailedRecovery(t *testing.T) {
	s, r := fixture(t)
	s.runner = brokenReload{r}
	state, _ := s.state()
	state.Groups = append(state.Groups, Group{ID: "progress", Name: "Progress", Enabled: true, Domains: []string{"progress.example"}})
	if _, e := s.apply(state, s.revision()); e == nil {
		t.Fatal("failure accepted")
	}
	p := s.readApplyProgress()
	if !p.RecoveryPending || p.RolledBack || p.Verified || p.Phase != "error" {
		t.Fatal(p)
	}
}
func TestApplyProgressRequiresSessionButDoesNotWaitForMutation(t *testing.T) {
	s, _ := fixture(t)
	if w := request(s, "GET", "/api/apply-status", "", nil, "", "", s.host); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cookie, csrf := loginTest(t, s)
	s.beginApply("in-flight")
	s.mutation.Lock()
	defer s.mutation.Unlock()
	done := make(chan int, 1)
	go func() { done <- request(s, "GET", "/api/apply-status", "", cookie, csrf, "", s.host).Code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("progress blocked on apply mutation")
	}
}
func TestSwitchHistoryIsBoundedPrivateAndDistinguishesDisabled(t *testing.T) {
	s, _ := fixture(t)
	out := exampleOutbound(t, "secret-endpoint.example")
	out["private-key"] = "do-not-persist-this"
	for i := 0; i < 105; i++ {
		s.recordSwitch("server-old", out, "automatic", nil)
	}
	entries := s.switchHistory()
	if len(entries) != 100 || !entries[0].Verified {
		t.Fatal(entries)
	}
	path := filepath.Join(s.dir, "switch-history.json")
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	for _, secret := range []string{"secret-endpoint", "do-not-persist-this", "test-public-key"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("history leaked", secret)
		}
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	atomicWrite(filepath.Join(s.root, "autostart.disabled"), []byte{})
	s.recordSwitch("server-old", out, "manual", nil)
	last := s.switchHistory()[99]
	if last.Verified || last.Result != "configured_off" {
		t.Fatal(last)
	}
	s.recordSwitch("server-old", out, "manual", errors.New("sensitive-error"))
	last = s.switchHistory()[99]
	if last.Verified || last.Result != "not_applied" {
		t.Fatal(last)
	}
}
func TestPanelChannelRequiresPasswordAndPersists(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	for _, test := range []struct {
		body string
		code int
	}{{`{"channel":"preview","password":"wrong"}`, 403}, {`{"channel":"invalid","password":"old-password-123"}`, 400}} {
		w := request(s, "POST", "/api/github-channel", test.body, cookie, csrf, "http://"+s.host, s.host)
		if w.Code != test.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}

	if s.panelUpdateChannel() != "stable" {
		t.Fatal("default changed")
	}
	if _, e := os.Stat(filepath.Join(s.dir, "panel-updates.json")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	w := request(s, "POST", "/api/github-channel", `{"channel":"preview","password":"old-password-123"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 || s.panelUpdateChannel() != "preview" {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened := newServer(s.root, s.host, s.runner)
	if reopened.panelUpdateChannel() != "preview" {
		t.Fatal("channel not persisted")
	}
}
func TestPreviewDiscoveryRequiresMatchingSignedChannelAndNoImplicitDowngrade(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	channel := "preview"
	makeRelease := func(v string, preview bool) (ghRelease, []byte, []byte) {
		m := map[string]any{"format": 1, "kind": "panel", "repository": githubRepository, "version": v, "arch": runtime.GOARCH, "asset": "DARKLS-VPN-Panel-v" + v + ".zip", "sha256": strings.Repeat("a", 64), "size": 100, "channel": channel}
		data, _ := json.Marshal(m)
		sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
		rel := ghRelease{Tag: "v" + v, Prerelease: preview, Assets: []ghAsset{{Name: "panel-release.json", URL: "https://github.com/mf"}, {Name: "panel-release.sig", URL: "https://github.com/sig"}, {Name: m["asset"].(string), URL: "https://github.com/" + githubRepository + "/releases/download/v" + v + "/" + m["asset"].(string), Size: 100}}}
		return rel, data, sig
	}
	rel, data, sig := makeRelease("9.0.0", true)
	get := func(_ context.Context, u string, _ int64) ([]byte, error) {
		switch u {
		case "https://github.com/mf":
			return data, nil
		case "https://github.com/sig":
			return sig, nil
		default:
			if strings.Contains(u, "?per_page") {
				return json.Marshal([]ghRelease{rel})
			}
			return json.Marshal(rel)
		}
	}
	st, _, _, e := fetchPanelChannelWith(context.Background(), pub, get, "preview", false)
	if e != nil || !st.Prerelease || !st.Available {
		t.Fatal(st, e)
	}
	if _, _, _, e = fetchPanelChannelWith(context.Background(), pub, get, "stable", false); e == nil {
		t.Fatal("stable accepted preview")
	}
	channel = "stable"
	rel, data, sig = makeRelease("9.0.0", true)
	if _, _, _, e = fetchPanelChannelWith(context.Background(), pub, get, "preview", false); e == nil {
		t.Fatal("unsigned channel classification accepted")
	}
	rel, data, sig = makeRelease("2.1.3", false)
	if _, _, _, e = fetchPanelChannelWith(context.Background(), pub, get, "stable", false); e == nil {
		t.Fatal("ordinary downgrade accepted")
	}
	st, _, _, e = fetchPanelChannelWith(context.Background(), pub, get, "stable", true)
	if e != nil || !st.RollbackAvailable || st.Available {
		t.Fatal(st, e)
	}
}

func TestOlderPackageNeedsExplicitVerifiedReturnPath(t *testing.T) {
	data := samplePackage(t, "good")
	r, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, f := range r.File {
		src, _ := f.Open()
		content, _ := io.ReadAll(src)
		src.Close()
		if strings.HasSuffix(f.Name, "manifest.json") {
			var m PackageManifest
			json.Unmarshal(content, &m)
			m.Version = "1.1.0"
			content = jsonBytes(m)
		}
		dst, _ := w.Create(f.Name)
		dst.Write(content)
	}
	w.Close()
	if _, _, e := stagePackage(bytes.NewReader(b.Bytes()), int64(b.Len())); e == nil {
		t.Fatal("manual downgrade accepted")
	}
	dir, m, e := stagePackageFor(bytes.NewReader(b.Bytes()), int64(b.Len()), true)
	if e != nil || m.Version != "1.1.0" {
		t.Fatal(m, e)
	}
	defer os.RemoveAll(dir)
}

func TestDisabledApplyProgressDoesNotClaimLiveRules(t *testing.T) {
	s, _ := fixture(t)
	atomicWrite(filepath.Join(s.root, "autostart.disabled"), []byte{})
	state, _ := s.state()
	if _, e := s.apply(state, s.revision()); e != nil {
		t.Fatal(e)
	}
	p := s.readApplyProgress()
	if !p.Disabled || !p.Verified || !strings.Contains(p.Message, "VPN выключен") {
		t.Fatal(p)
	}
	for _, step := range p.Steps {
		if step.Name == "routing" && step.State != "skipped" {
			t.Fatal(step)
		}
	}
}

func TestStablePanelRejectsRollbackBeforeNetwork(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	w := request(s, "POST", "/api/github-install", `{"password":"old-password-123","version":"2.1.3","rollback":true}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "возврат доступен только") {
		t.Fatal(w.Code, w.Body.String())
	}
}
