package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestSignedGitHubManifestRejectsTamperingAndWrongTargets(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	good := SignedRelease{1, "panel", githubRepository, "9.0.0", "arm64", "DARKLS-VPN-Panel-v9.0.0.zip", strings.Repeat("a", 64), 2700000}
	verify := func(m SignedRelease, mutate bool) error {
		b, _ := json.Marshal(m)
		sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, b)))
		if mutate {
			b = append(b, ' ')
		}
		_, e := verifySignedRelease(b, sig, pub, "1.2.0", "arm64")
		return e
	}
	if e := verify(good, false); e != nil {
		t.Fatal(e)
	}
	if e := verify(good, true); e == nil {
		t.Fatal("modified metadata accepted")
	}
	for _, bad := range []SignedRelease{{1, "panel", "attacker/repo", "9.0.0", "arm64", good.Asset, good.SHA256, good.Size}, {1, "panel", githubRepository, "0.1.0", "arm64", good.Asset, good.SHA256, good.Size}, {1, "panel", githubRepository, "9.0.0", "amd64", good.Asset, good.SHA256, good.Size}, {1, "panel", githubRepository, "9.0.0", "arm64", "../../bad", good.SHA256, good.Size}, {1, "panel", githubRepository, "9.0.0", "arm64", good.Asset, good.SHA256, 9 << 20}} {
		if verify(bad, false) == nil {
			t.Fatal("bad signed target accepted", bad)
		}
	}
}
func TestGitHubDownloadRejectsArbitraryAddresses(t *testing.T) {
	for _, u := range []string{"http://github.com/x", "https://github.com.evil.test/x", "https://127.0.0.1/x", "https://user:pass@github.com/x", "https://github.com:8088/x", "file:///etc/passwd"} {
		if allowedGitHubURL(u) {
			t.Fatal(u)
		}
		if _, e := githubGet(context.Background(), u, 10); e == nil {
			t.Fatal("unexpected network success")
		}
	}
	for _, u := range []string{"https://github.com/a/b", "https://api.github.com/repos/a/b/releases/latest", "https://release-assets.githubusercontent.com/a"} {
		if !allowedGitHubURL(u) {
			t.Fatal(u)
		}
	}
}
func TestGitHubInstallRequiresPasswordBeforeNetwork(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	w := request(s, "POST", "/api/github-install", `{"password":"wrong","version":"9.0.0"}`, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestGitHubStatusRequiresSession(t *testing.T) {
	s, _ := fixture(t)
	w := request(s, "GET", "/api/github", "", nil, "", "", s.host)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestGitHubClientRedirectRestriction(t *testing.T) {
	c := githubHTTPClient(false)
	req := httptest.NewRequest("GET", "https://evil.test/payload", nil)
	if c.CheckRedirect(req, nil) == nil {
		t.Fatal("external redirect accepted")
	}
}

func TestReleaseDiscoveryVerifiesSignatureTagAndExactAssetURL(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	assetURL := "https://github.com/" + githubRepository + "/releases/download/v9.0.0/DARKLS-VPN-Panel-v9.0.0.zip"
	m := SignedRelease{1, "panel", githubRepository, "9.0.0", runtime.GOARCH, "DARKLS-VPN-Panel-v9.0.0.zip", strings.Repeat("a", 64), 100}
	data, _ := json.Marshal(m)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
	rel := ghRelease{Tag: "v9.0.0", Body: "Release notes", Assets: []ghAsset{{"panel-release.json", "https://github.com/mf", int64(len(data)), ""}, {"panel-release.sig", "https://github.com/sig", int64(len(sig)), ""}, {m.Asset, assetURL, 100, ""}}}
	get := func(_ context.Context, u string, _ int64) ([]byte, error) {
		switch u {
		case "https://github.com/mf":
			return data, nil
		case "https://github.com/sig":
			return sig, nil
		default:
			return json.Marshal(rel)
		}
	}
	s, _, url, e := fetchGitHubReleaseWith(context.Background(), pub, get)
	if e != nil || !s.Signed || !s.Available || url != assetURL {
		t.Fatal(s, url, e)
	}
	rel.Tag = "v9.0.1"
	if _, _, _, e = fetchGitHubReleaseWith(context.Background(), pub, get); e == nil {
		t.Fatal("mismatched tag accepted")
	}
	rel.Tag = "v9.0.0"
	rel.Assets[2].URL = "https://github.com/attacker/repo/file.zip"
	if _, _, _, e = fetchGitHubReleaseWith(context.Background(), pub, get); e == nil {
		t.Fatal("foreign package URL accepted")
	}
}
