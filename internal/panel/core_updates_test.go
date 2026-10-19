package panel

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func coreReleaseFixture() ghRelease {
	v := "26.10.17"
	return ghRelease{Tag: "v" + v, Assets: []ghAsset{{Name: coreAssetName, URL: "https://github.com/" + coreRepository + "/releases/download/v" + v + "/" + coreAssetName, Size: 20 << 20, Digest: "sha256:" + strings.Repeat("a", 64)}}}
}
func TestOfficialCoreDiscoveryPinsArchitectureOriginAndChecksum(t *testing.T) {
	good := coreReleaseFixture()
	tests := []struct {
		name    string
		change  func(*ghRelease)
		wantErr bool
	}{{"valid", func(*ghRelease) {}, false}, {"other owner", func(r *ghRelease) { r.Assets[0].URL = strings.Replace(r.Assets[0].URL, "XTLS", "evil", 1) }, true}, {"wrong platform", func(r *ghRelease) { r.Assets[0].Name = "Xray-linux-64.zip" }, true}, {"prerelease", func(r *ghRelease) { r.Prerelease = true }, false}, {"draft", func(r *ghRelease) { r.Draft = true }, true}, {"bad hash", func(r *ghRelease) { r.Assets[0].Digest = "sha256:00" }, true}, {"oversized", func(r *ghRelease) { r.Assets[0].Size = coreMaxArchive + 1 }, true}, {"duplicate", func(r *ghRelease) { r.Assets = append(r.Assets, r.Assets[0]) }, true}, {"bad version", func(r *ghRelease) { r.Tag = "v26.10.17/../../latest" }, true}}
	for _, x := range tests {
		t.Run(x.name, func(t *testing.T) {
			var rel ghRelease
			_ = json.Unmarshal(jsonBytes(good), &rel)
			x.change(&rel)
			get := func(_ context.Context, u string, _ int64) ([]byte, error) {
				if u != "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=30" {
					t.Fatal("unexpected URL", u)
				}
				return jsonBytes([]ghRelease{rel}), nil
			}
			r, e := fetchCoreRelease(context.Background(), get)
			if (e != nil) != x.wantErr {
				t.Fatal(r, e)
			}
			if e == nil && (r.Version != "26.10.17" || r.Hash != strings.Repeat("a", 64)) {
				t.Fatal(r)
			}
		})
	}
}
func TestCoreChecksumFallbackMatchesOfficialDigestFormat(t *testing.T) {
	r := coreReleaseFixture()
	r.Assets[0].Digest = ""
	r.Assets = append(r.Assets, ghAsset{Name: coreAssetName + ".dgst", URL: r.Assets[0].URL + ".dgst"})
	get := func(_ context.Context, u string, _ int64) ([]byte, error) {
		if strings.HasSuffix(u, ".dgst") {
			return []byte("MD5= 123\nSHA2-256= " + strings.Repeat("b", 64) + "\nSHA2-512= unused\n"), nil
		}
		return jsonBytes([]ghRelease{r}), nil
	}
	out, e := fetchCoreRelease(context.Background(), get)
	if e != nil || out.Hash != strings.Repeat("b", 64) {
		t.Fatal(out, e)
	}
}
func TestCoreSchedulingUsesMSKOneAttemptPerDayAndFailureBlock(t *testing.T) {
	p := CoreSettings{Check: true, AutoInstall: true, Hour: 4, Blocked: map[string]int64{}}
	st := CoreStatus{Current: "26.10.10", Latest: "26.10.17", Available: true}
	at := time.Date(2026, 10, 18, 1, 15, 0, 0, time.UTC)
	if !autoCoreDue(p, st, at, true) {
		t.Fatal("04:15 MSK not due")
	}
	if autoCoreDue(p, st, at, false) || autoCoreDue(p, st, at.Add(time.Hour), true) {
		t.Fatal("unsafe or wrong-hour automatic install")
	}
	p.LastAutoDay = "2026-10-18"
	if autoCoreDue(p, st, at, true) {
		t.Fatal("second daily install allowed")
	}
	p.LastAutoDay = ""
	p.Blocked[st.Latest] = at.Unix()
	if autoCoreDue(p, st, at, true) {
		t.Fatal("failed release retried automatically")
	}
	p.Blocked = nil
	p.AutoInstall = false
	if autoCoreDue(p, st, at, true) {
		t.Fatal("default/manual mode auto-installed")
	}
	if newerRelease("26.3.27", "26.10.10") || newerRelease("26.10.10", "26.10.10") || !newerRelease("26.10.17", "26.10.10") {
		t.Fatal("version ordering")
	}
}
func zipCore(t *testing.T, names []string, symlink bool) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, n := range names {
		h := &zip.FileHeader{Name: n, Method: zip.Store}
		h.SetMode(0700)
		if symlink {
			h.SetMode(os.ModeSymlink | 0777)
		}
		f, e := w.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte("test core payload"))
	}
	w.Close()
	return b.Bytes()
}
func TestCoreExtractionIgnoresTraversalAndRejectsDuplicatesAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		bad  bool
	}{{"valid", zipCore(t, []string{"xray", "../outside"}, false), false}, {"duplicate", zipCore(t, []string{"xray", "xray"}, false), true}, {"symlink", zipCore(t, []string{"xray"}, true), true}, {"missing", zipCore(t, []string{"../xray"}, false), true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			stage := filepath.Join(root, "stage")
			os.Mkdir(stage, 0700)
			p := filepath.Join(root, "core.zip")
			os.WriteFile(p, tc.data, 0600)
			out, e := extractCore(p, stage)
			if (e != nil) != tc.bad {
				t.Fatal(out, e)
			}
			if _, e = os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(e) {
				t.Fatal("archive escaped staging")
			}
		})
	}
	b := zipCore(t, []string{"xray"}, false)
	i := bytes.Index(b, []byte{'P', 'K', 1, 2})
	if i < 0 {
		t.Fatal("missing central directory")
	}
	binary.LittleEndian.PutUint32(b[i+24:i+28], coreMaxBinary+1)
	root := t.TempDir()
	p := filepath.Join(root, "big.zip")
	os.WriteFile(p, b, 0600)
	if _, e := extractCore(p, root); e == nil {
		t.Fatal("oversized raw core accepted")
	}
}
func TestCoreSettingsRequirePasswordAndPreserveBlockedVersions(t *testing.T) {
	s, _ := fixture(t)
	cookie, csrf := loginTest(t, s)
	p := s.coreSettings()
	p.Blocked["26.10.17"] = 1
	p.LastAutoDay = "2026-10-18"
	atomicWrite(s.corePath("xray-settings.json"), jsonBytes(p))
	body := `{"password":"old-password-123","settings":{"check":true,"auto_install":true,"hour_msk":4,"blocked_versions":{},"last_auto_day":""}}`
	w := request(s, "POST", "/api/xray-settings", body, cookie, csrf, "http://"+s.host, s.host)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p = s.coreSettings()
	if !p.AutoInstall || p.Blocked["26.10.17"] != 1 || p.LastAutoDay != "2026-10-18" {
		t.Fatal("client overwrote internal safety state", p)
	}
	bad := strings.Replace(body, "old-password-123", "wrong", 1)
	if w = request(s, "POST", "/api/xray-settings", bad, cookie, csrf, "http://"+s.host, s.host); w.Code != 403 {
		t.Fatal(w.Code)
	}
	body = strings.Replace(body, `"hour_msk":4`, `"hour_msk":24`, 1)
	if w = request(s, "POST", "/api/xray-settings", body, cookie, csrf, "http://"+s.host, s.host); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestSharedRootsParsedOnceAndFailedListBounded(t *testing.T) {
	if sharedRoots() != sharedRoots() {
		t.Fatal("CA pool reparsed")
	}
	m := map[string]int64{}
	for i := int64(1); i <= 20; i++ {
		m[time.Unix(i, 0).String()] = i
	}
	pruneBlocked(m)
	if len(m) != 12 {
		t.Fatal(len(m))
	}
}
func TestCoreDiscoveryErrorDoesNotExecuteRouterCommands(t *testing.T) {
	s, r := fixture(t)
	_, e := fetchCoreRelease(context.Background(), func(context.Context, string, int64) ([]byte, error) { return nil, errors.New("offline") })
	if e == nil || len(r.calls) != 0 {
		t.Fatal("discovery touched router")
	}
	_ = s
}

type incompatibleCoreRunner struct {
	candidate string
	calls     []string
}

func (r *incompatibleCoreRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, line)
	if len(args) > 0 && args[0] == "version" {
		if name == r.candidate {
			return []byte("Xray 26.10.17 (Xray, Penetrates Everything.)"), nil
		}
		return []byte("Xray 26.10.10 (Xray, Penetrates Everything.)"), nil
	}
	if strings.Contains(line, "-test") {
		return []byte("incompatible config"), errors.New("rejected")
	}
	return []byte("OK"), nil
}
func TestIncompatibleNewCoreNeverStopsCurrentTunnel(t *testing.T) {
	s, _ := fixture(t)
	p := filepath.Join(t.TempDir(), "candidate")
	header := make([]byte, 64)
	copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1})
	header[18] = 183
	os.WriteFile(p, header, 0700)
	hash, _ := fileHash(p)
	r := &incompatibleCoreRunner{candidate: p}
	s.runner = r
	e := s.installCore(p, "26.10.17", hash)
	if e == nil || !strings.Contains(e.Error(), "rejects configuration") {
		t.Fatal(e)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "kill") || strings.Contains(c, "vpn.sh") || strings.Contains(c, "start") {
			t.Fatal("incompatible config disrupted current tunnel", c)
		}
	}
}
func TestCoreAPIMutationsRequireSessionAndCSRF(t *testing.T) {
	s, r := fixture(t)
	for _, p := range []string{"xray-check", "xray-settings", "xray-install", "xray-rollback", "cleanup-settings", "cleanup-now"} {
		w := request(s, "POST", "/api/"+p, `{}`, nil, "", "http://"+s.host, s.host)
		if w.Code != 401 {
			t.Fatal(p, w.Code)
		}
	}
	cookie, _ := loginTest(t, s)
	before := len(r.calls)
	w := request(s, "POST", "/api/xray-settings", `{}`, cookie, "", "http://"+s.host, s.host)
	if w.Code != 403 || len(r.calls) != before {
		t.Fatal("CSRF failure reached core", w.Code)
	}
}

func TestCoreDiscoverySelectsNewestNumericReleaseIncludingPrereleases(t *testing.T) {
	makeRelease := func(v string, pre bool) ghRelease {
		r := coreReleaseFixture()
		r.Tag = "v" + v
		r.Prerelease = pre
		r.Assets[0].URL = "https://github.com/" + coreRepository + "/releases/download/" + r.Tag + "/" + coreAssetName
		return r
	}
	stable := makeRelease("26.3.27", false)
	preview := makeRelease("26.10.17", true)
	draft := makeRelease("26.12.31", true)
	draft.Draft = true
	missing := makeRelease("26.11.1", true)
	missing.Assets = nil
	for _, list := range [][]ghRelease{{stable, preview, draft, missing}, {draft, missing, preview, stable}} {
		out, err := fetchCoreRelease(context.Background(), func(context.Context, string, int64) ([]byte, error) {
			return jsonBytes(list), nil
		})
		if err != nil || out.Version != "26.10.17" || !out.Prerelease {
			t.Fatalf("wrong newest release: %+v %v", out, err)
		}
	}
	preview.Assets[0].Digest = "sha256:invalid"
	if _, err := fetchCoreRelease(context.Background(), func(context.Context, string, int64) ([]byte, error) {
		return jsonBytes([]ghRelease{stable, preview}), nil
	}); err == nil {
		t.Fatal("invalid newest checksum silently fell back to older stable release")
	}
	if newerRelease("26.3.27", "26.10.10") {
		t.Fatal("older stable version offered as upgrade")
	}
}

func TestStreamingCoreDiscoveryRejectsPartialOrTrailingMetadata(t *testing.T) {
	valid := string(jsonBytes([]ghRelease{coreReleaseFixture()}))
	for _, input := range []string{valid[:len(valid)/2], valid + "{}", valid + "garbage", "null", "{}", "[]", "[" + string(jsonBytes(coreReleaseFixture())) + ",broken]"} {
		if _, err := selectCoreRelease(strings.NewReader(input)); err == nil {
			t.Fatal("accepted invalid metadata", input)
		}
	}
}

// Same synthetic 30-release document for both paths; the buffered path models
// v1.4.1's full-response allocation and full release-list decoding.
func BenchmarkCoreMetadata(b *testing.B) {
	releases := make([]ghRelease, 30)
	for i := range releases {
		releases[i] = coreReleaseFixture()
		releases[i].Body = strings.Repeat("notes ", 10000)
	}
	data := jsonBytes(releases)
	b.Run("buffered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			raw, err := io.ReadAll(bytes.NewReader(data))
			if err != nil {
				b.Fatal(err)
			}
			var all []ghRelease
			if err = json.Unmarshal(raw, &all); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("streamed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := selectCoreRelease(bytes.NewReader(data)); err != nil {
				b.Fatal(err)
			}
		}
	})
}
