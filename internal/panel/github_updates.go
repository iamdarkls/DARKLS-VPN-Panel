package panel

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"darkls-panel/assets"
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
	Configured bool   `json:"configured"`
	CheckedAt  int64  `json:"checked_at"`
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Available  bool   `json:"available"`
	Signed     bool   `json:"signed"`
	Notes      string `json:"notes,omitempty"`
	URL        string `json:"url"`
	Error      string `json:"error,omitempty"`
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
		p, _ := url.Parse("socks5h://127.0.0.1:10808")
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
	var last error
	for _, proxy := range []bool{false, true} {
		client := githubHTTPClient(proxy)
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if e != nil {
			return e
		}
		req.Header.Set("User-Agent", "DARKLS-VPN-Panel/"+version)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, e := client.Do(req)
		if e != nil {
			last = e
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
			last = fmt.Errorf("GitHub HTTP %d", resp.StatusCode)
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
			last = e
			continue
		}
		return nil
	}
	return fmt.Errorf("GitHub недоступен напрямую и через VPN: %v", last)
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
	status := GitHubStatus{Configured: len(key) > 0, CheckedAt: time.Now().Unix(), Current: version, URL: "https://github.com/" + githubRepository + "/releases"}
	var manifest SignedRelease
	if !status.Configured {
		return status, manifest, "", errors.New("источник ещё не настроен: установите первый Release из своего репозитория после Publish-GitHub.ps1")
	}
	data, e := get(ctx, "https://api.github.com/repos/"+githubRepository+"/releases/latest", 256<<10)
	if e != nil {
		return status, manifest, "", e
	}
	var rel ghRelease
	if json.Unmarshal(data, &rel) != nil || rel.Draft || rel.Prerelease {
		return status, manifest, "", errors.New("нет подходящего стабильного выпуска")
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
	manifest, e = verifySignedRelease(data, sig, key, version, runtime.GOARCH)
	if e != nil {
		return status, manifest, "", e
	}
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
	status.Available = manifest.Version != version
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
	st.Current = version
	st.Configured = len(trustedReleaseKey()) > 0
	if st.URL == "" {
		st.URL = "https://github.com/" + githubRepository + "/releases"
	}
	if st.Latest != "" {
		st.Available = st.Latest != version && releaseAtLeast(st.Latest, version)
	}
	return st
}
func (s *Server) checkGitHub(ctx context.Context) GitHubStatus {
	st, _, _, e := fetchGitHubRelease(ctx)
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
	st, m, asset, e := fetchGitHubRelease(ctx)
	if e != nil {
		fail(w, 502, e)
		return
	}
	if !st.Available || req.Version != m.Version {
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
	dir, pm, e := stagePackage(f, m.Size)
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
	job, e := s.launchUpdate(dir, m.Version)
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
	var last error
	for _, proxy := range []bool{false, true} {
		client := githubHTTPClient(proxy)
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if e != nil {
			return "", e
		}
		req.Header.Set("User-Agent", "DARKLS-VPN-Panel/"+version)
		resp, e := client.Do(req)
		if e != nil {
			last = e
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			last = fmt.Errorf("загрузка HTTP %d", resp.StatusCode)
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
			last = errors.New("размер или SHA-256 загруженного пакета не совпадает с проверенными метаданными")
			continue
		}
		return name, nil
	}
	return "", last
}
func (s *Server) launchUpdate(dir, target string) (UpdateJob, error) {
	job := UpdateJob{ID: token()[:16], Phase: "staged", Target: target, Message: "Пакет проверен, начинается установка", Started: time.Now().Unix()}
	if e := atomicWrite(updateJobPath, jsonBytes(job)); e != nil {
		return job, e
	}
	cmd := exec.Command(filepath.Join(s.dir, "darkls-panel"), "--root", s.root, "--install-staged", dir)
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
