package panel

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"darkls-panel/assets"
	"darkls-panel/internal/routerenv"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const githubRepository = "iamdarkls/DARKLS-VPN-Panel"

// A bootstrap build intentionally has no signing key. First install the release
// built by your own repository after Publish-GitHub.ps1 generates its key.
var githubPublicKey = assets.GitHubPublicKey

var githubRoots = assets.GitHubRoots

type GitHubStatus struct {
	Channel           string `json:"channel"`
	InstalledChannel  string `json:"installed_channel"`
	Prerelease        bool   `json:"prerelease"`
	RollbackAvailable bool   `json:"rollback_available"`
	Configured        bool   `json:"configured"`
	CheckedAt         int64  `json:"checked_at"`
	Current           string `json:"current"`
	Latest            string `json:"latest,omitempty"`
	Available         bool   `json:"available"`
	Signed            bool   `json:"signed"`
	Notes             string `json:"notes,omitempty"`
	URL               string `json:"url"`
	Error             string `json:"error,omitempty"`
}
type SignedRelease struct {
	Format     int    `json:"format"`
	Kind       string `json:"kind"`
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Arch       string `json:"arch"`
	Asset      string `json:"asset"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}
type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}
type ghRelease struct {
	PublishedAt string    `json:"published_at"`
	Tag         string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Body        string    `json:"body"`
	Assets      []ghAsset `json:"assets"`
}

func trustedReleaseKey() ed25519.PublicKey {
	b, e := base64.StdEncoding.DecodeString(strings.TrimSpace(githubPublicKey))
	if e != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}
func verifySignedRelease(data, sig []byte, key ed25519.PublicKey, current, arch string) (SignedRelease, error) {
	var m SignedRelease
	raw, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if e != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, data, raw) {
		return m, errors.New("подпись выпуска не подтверждена")
	}
	if json.Unmarshal(data, &m) != nil || m.Format != 1 || m.Kind != "panel" || m.Repository != githubRepository || m.Arch != arch || !releasePattern.MatchString(m.Version) || !releaseAtLeast(m.Version, current) {
		return m, errors.New("выпуск несовместим или старее установленной версии")
	}
	if m.Asset != "DARKLS-VPN-Panel-v"+m.Version+".zip" || m.Size <= 0 || m.Size > 8<<20 || len(m.SHA256) != 64 {
		return m, errors.New("неверные параметры подписанного пакета")
	}
	if _, e = hex.DecodeString(m.SHA256); e != nil {
		return m, e
	}
	return m, nil
}
func allowedGitHubURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func githubHTTPClient(proxy bool) *http.Client {
	roots := sharedRoots()
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", address)
	}, TLSHandshakeTimeout: 7 * time.Second, DisableKeepAlives: true}
	if proxy {
		p, _ := url.Parse("socks5h://" + routerenv.SOCKSAddress)
		tr.Proxy = http.ProxyURL(p)
	}
	return &http.Client{Transport: tr, Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || !allowedGitHubURL(r.URL.String()) {
			return errors.New("неразрешённый переход загрузки")
		}
		return nil
	}}
}

// githubRead keeps metadata bounded while allowing callers to decode a stream.
// Each fallback attempt gets a new reader; consumers must replace partial state.
func githubRead(ctx context.Context, raw string, limit int64, consume func(io.Reader) error) error {
	if !allowedGitHubURL(raw) {
		return errors.New("неразрешённый адрес GitHub")
	}
	var failures []string
	for _, proxy := range []bool{false, true} {
		client := githubHTTPClient(proxy)
		resp, e := githubRequest(ctx, client, raw)
		if e != nil {
			failures = append(failures, githubRouteError(proxy, e))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			client.CloseIdleConnections()
			if resp.StatusCode == 404 {
				return errors.New("репозиторий или опубликованный Release пока не найден")
			}
			if resp.StatusCode == 403 || resp.StatusCode == 429 {
				return errors.New("GitHub ограничил запросы; повторите позже")
			}
			failures = append(failures, githubRouteError(proxy, fmt.Errorf("GitHub HTTP %d", resp.StatusCode)))
			continue
		}
		reader := &io.LimitedReader{R: resp.Body, N: limit + 1}
		e = consume(reader)
		// Even a consumer that stops early must not bypass the response size cap.
		if e == nil {
			_, e = io.Copy(io.Discard, reader)
		}
		resp.Body.Close()
		client.CloseIdleConnections()
		if reader.N == 0 {
			return errors.New("ответ GitHub слишком большой")
		}
		if e != nil {
			failures = append(failures, githubRouteError(proxy, e))
			continue
		}
		return nil
	}
	return fmt.Errorf("GitHub недоступен: %s", strings.Join(failures, "; "))
}

func githubGet(ctx context.Context, raw string, limit int64) ([]byte, error) {
	var data []byte
	err := githubRead(ctx, raw, limit, func(r io.Reader) error {
		var e error
		data, e = io.ReadAll(r)
		return e
	})
	return data, err
}
func fetchGitHubRelease(ctx context.Context) (GitHubStatus, SignedRelease, string, error) {
	return fetchGitHubReleaseWith(ctx, trustedReleaseKey(), githubGet)
}
func fetchGitHubReleaseWith(ctx context.Context, key ed25519.PublicKey, get func(context.Context, string, int64) ([]byte, error)) (GitHubStatus, SignedRelease, string, error) {
	return fetchPanelChannelWith(ctx, key, get, "stable", false)
}
func fetchPanelChannelWith(ctx context.Context, key ed25519.PublicKey, get func(context.Context, string, int64) ([]byte, error), channel string, allowOlder bool) (GitHubStatus, SignedRelease, string, error) {
	status := GitHubStatus{Channel: channel, InstalledChannel: panelReleaseChannel, Configured: len(key) > 0, CheckedAt: time.Now().Unix(), Current: version, URL: "https://github.com/" + githubRepository + "/releases"}
	var manifest SignedRelease
	if !status.Configured {
		return status, manifest, "", errors.New("источник ещё не настроен: установите первый Release из своего репозитория после Publish-GitHub.ps1")
	}
	if !validPanelChannel(channel) {
		return status, manifest, "", errors.New("неверный канал")
	}
	endpoint := "https://api.github.com/repos/" + githubRepository + "/releases/latest"
	if channel == "preview" {
		endpoint = "https://api.github.com/repos/" + githubRepository + "/releases?per_page=30"
	}
	data, e := get(ctx, endpoint, 512<<10)
	if e != nil {
		return status, manifest, "", e
	}
	var rel ghRelease
	if channel == "stable" {
		if json.Unmarshal(data, &rel) != nil || rel.Draft || rel.Prerelease {
			return status, manifest, "", errors.New("нет подходящего стабильного выпуска")
		}
	} else {
		var releases []ghRelease
		if json.Unmarshal(data, &releases) != nil {
			return status, manifest, "", errors.New("неверный список выпусков")
		}
		for _, candidate := range releases {
			v := strings.TrimPrefix(candidate.Tag, "v")
			if candidate.Draft || !releasePattern.MatchString(v) {
				continue
			}
			if rel.Tag == "" || releaseAtLeast(v, strings.TrimPrefix(rel.Tag, "v")) {
				rel = candidate
			}
		}
		if rel.Tag == "" {
			return status, manifest, "", errors.New("подходящий выпуск не найден")
		}
	}
	assets := map[string]ghAsset{}
	for _, a := range rel.Assets {
		if _, ok := assets[a.Name]; ok {
			return status, manifest, "", errors.New("повторяющиеся файлы выпуска")
		}
		assets[a.Name] = a
	}
	mf, ok := assets["panel-release.json"]
	if !ok {
		return status, manifest, "", errors.New("Release не содержит подписанный манифест")
	}
	sf, ok := assets["panel-release.sig"]
	if !ok {
		return status, manifest, "", errors.New("Release не содержит подпись")
	}
	data, e = get(ctx, mf.URL, 8192)
	if e != nil {
		return status, manifest, "", e
	}
	sig, e := get(ctx, sf.URL, 1024)
	if e != nil {
		return status, manifest, "", e
	}
	floor := version
	if allowOlder && channel == "stable" {
		floor = "2.1.3"
	}
	manifest, e = verifySignedRelease(data, sig, key, floor, runtime.GOARCH)
	if e != nil {
		return status, manifest, "", e
	}
	signedChannel := signedPanelChannel(data)
	if !validPanelChannel(signedChannel) || (signedChannel == "preview") != rel.Prerelease {
		return status, manifest, "", errors.New("канал выпуска не совпадает с подписью")
	}
	status.Prerelease = rel.Prerelease
	if rel.Tag != "v"+manifest.Version {
		return status, manifest, "", errors.New("тег не совпадает с подписанной версией")
	}
	asset, ok := assets[manifest.Asset]
	if !ok || asset.Size != manifest.Size {
		return status, manifest, "", errors.New("пакет отсутствует или его размер неверен")
	}
	expected := "https://github.com/" + githubRepository + "/releases/download/" + url.PathEscape(rel.Tag) + "/" + manifest.Asset
	if asset.URL != expected {
		return status, manifest, "", errors.New("пакет принадлежит другому адресу выпуска")
	}
	status.Latest = manifest.Version
	status.Available = manifest.Version != version && releaseAtLeast(manifest.Version, version)
	status.RollbackAvailable = allowOlder && channel == "stable" && manifest.Version != version && !status.Available
	status.Signed = true
	status.Notes = rel.Body
	if len(status.Notes) > 8192 {
		status.Notes = status.Notes[:8192]
	}
	status.URL = "https://github.com/" + githubRepository + "/releases/tag/" + rel.Tag
	return status, manifest, asset.URL, nil
}
func (s *Server) githubStatus() GitHubStatus {
	var st GitHubStatus
	_ = readJSON("/tmp/darkls-github-status.json", &st)
	if st.Channel != s.panelUpdateChannel() {
		st = GitHubStatus{}
	}
	st.Channel = s.panelUpdateChannel()
	st.InstalledChannel = panelReleaseChannel
	st.Current = version
	st.Configured = len(trustedReleaseKey()) > 0
	if st.URL == "" {
		st.URL = "https://github.com/" + githubRepository + "/releases"
	}
	st.RollbackAvailable = panelReleaseChannel == "preview" && st.Channel == "stable" && st.Latest != "" && !releaseAtLeast(st.Latest, version)
	if st.Latest != "" {
		st.Available = st.Latest != version && releaseAtLeast(st.Latest, version)
	}
	return st
}
func (s *Server) checkGitHub(ctx context.Context) GitHubStatus {
	st, _, _, e := fetchPanelChannelWith(ctx, trustedReleaseKey(), githubGet, s.panelUpdateChannel(), panelReleaseChannel == "preview" && s.panelUpdateChannel() == "stable")
	if e != nil {
		st.Error = redact(e.Error())
	}
	_ = atomicWrite("/tmp/darkls-github-status.json", jsonBytes(st))
	return st
}
func (s *Server) githubLoop(ctx context.Context) {
	runPeriodic(ctx, 30*time.Second, 30*time.Minute, func() {
		if len(trustedReleaseKey()) > 0 && time.Now().Year() >= 2025 && !updateBusy() && s.githubMu.TryLock() {
			st := s.githubStatus()
			if releaseCheckDue(time.Now(), st.CheckedAt, st.Error) {
				ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
				s.checkGitHub(ctx)
				cancel()
			}
			s.githubMu.Unlock()
		}
	})
}
func (s *Server) githubAPI(w http.ResponseWriter, r *http.Request, install bool) {
	if !s.githubMu.TryLock() {
		fail(w, 409, errors.New("запрос к GitHub уже выполняется"))
		return
	}
	defer s.githubMu.Unlock()
	if !install {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		respond(w, 200, s.checkGitHub(ctx))
		return
	}
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция выполняется"))
		return
	}
	defer s.mutation.Unlock()
	var req struct {
		Password string `json:"password"`
		Version  string `json:"version"`
		Rollback bool   `json:"rollback"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	if e := s.currentPassword(r, req.Password); e != nil {
		fail(w, 403, e)
		return
	}
	if updateBusy() {
		fail(w, 409, errors.New("обновление уже выполняется"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	channel := s.panelUpdateChannel()
	allowOlder := req.Rollback && panelReleaseChannel == "preview" && channel == "stable"
	if req.Rollback && !allowOlder {
		fail(w, 400, errors.New("возврат доступен только с тестовой сборки на стабильный канал"))
		return
	}
	st, m, asset, e := fetchPanelChannelWith(ctx, trustedReleaseKey(), githubGet, channel, allowOlder)
	if e != nil {
		fail(w, 502, e)
		return
	}
	if (!st.Available && !(allowOlder && st.RollbackAvailable)) || req.Version != m.Version {
		fail(w, 409, errors.New("версия изменилась или уже установлена; проверьте обновления снова"))
		return
	}
	// Stream ZIP to RAM-backed disk, never keep the whole archive in Go heap.
	path, e := downloadGitHubPackage(ctx, asset, m)
	if e != nil {
		fail(w, 502, e)
		return
	}
	defer os.Remove(path)
	f, e := os.Open(path)
	if e != nil {
		fail(w, 500, e)
		return
	}
	defer f.Close()
	dir, pm, e := stagePackageFor(f, m.Size, allowOlder)
	if e != nil {
		fail(w, 400, e)
		return
	}
	if pm.Version != m.Version {
		os.RemoveAll(dir)
		fail(w, 400, errors.New("внутренняя версия пакета отличается от подписи"))
		return
	}
	release, e := s.pause()
	if e != nil {
		os.RemoveAll(dir)
		fail(w, 409, e)
		return
	}
	defer release()
	job, e := s.launchUpdateFor(dir, m.Version, allowOlder)
	if e != nil {
		os.RemoveAll(dir)
		fail(w, 500, e)
		return
	}
	respond(w, 202, map[string]any{"ok": true, "job": job})
}
func downloadGitHubPackage(ctx context.Context, raw string, m SignedRelease) (string, error) {
	return downloadVerifiedAsset(ctx, raw, m.Size, m.SHA256, "/tmp", "darkls-github-*.zip")
}
func downloadVerifiedAsset(ctx context.Context, raw string, size int64, hash, dir, pattern string) (string, error) {
	if !allowedGitHubURL(raw) {
		return "", errors.New("неразрешённый адрес пакета")
	}
	var failures []string
	route := func(proxy bool, err error) {
		name := "напрямую"
		if proxy {
			name = "через VPN"
		}
		failures = append(failures, name+": "+err.Error())
	}
	for _, proxy := range []bool{false, true} {
		client := githubHTTPClient(proxy)
		resp, e := githubRequest(ctx, client, raw)
		if e != nil {
			route(proxy, e)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			route(proxy, fmt.Errorf("загрузка HTTP %d", resp.StatusCode))
			continue
		}
		f, e := os.CreateTemp(dir, pattern)
		if e != nil {
			resp.Body.Close()
			return "", e
		}
		name := f.Name()
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1))
		resp.Body.Close()
		ce := f.Close()
		if copyErr != nil || ce != nil || n != size || hex.EncodeToString(h.Sum(nil)) != hash {
			os.Remove(name)
			route(proxy, errors.New("размер или SHA-256 загруженного пакета не совпадает с проверенными метаданными"))
			continue
		}
		return name, nil
	}
	return "", errors.New(strings.Join(failures, "; "))
}
func (s *Server) launchUpdate(dir, target string) (UpdateJob, error) {
	return s.launchUpdateFor(dir, target, false)
}
func (s *Server) launchUpdateFor(dir, target string, rollback bool) (UpdateJob, error) {
	job := UpdateJob{ID: token()[:16], Phase: "staged", Target: target, Message: "Пакет проверен, начинается установка", Started: time.Now().Unix()}
	if e := atomicWrite(updateJobPath, jsonBytes(job)); e != nil {
		return job, e
	}
	worker := filepath.Join(s.dir, "darkls-panel")
	// A verified stable target validates its own version during an explicit return.
	// Manual uploads and ordinary installs still use the running version's floor.
	if rollback {
		worker = filepath.Join(dir, "darkls-panel")
	}
	cmd := exec.Command(worker, "--root", s.root, "--install-staged", dir)
	f, e := os.OpenFile("/tmp/darkls-update-worker.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e == nil {
		cmd.Stdout = f
		cmd.Stderr = f
		e = cmd.Start()
		f.Close()
	}
	if e != nil {
		job.Phase = "error"
		job.Message = "Не удалось запустить установщик"
		_ = atomicWrite(updateJobPath, jsonBytes(job))
		return job, e
	}
	go cmd.Wait()
	appendEvent("update", "Начата установка панели "+target)
	return job, nil
}
