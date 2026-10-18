package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const coreRepository = "XTLS/Xray-core"
const coreAssetName = "Xray-linux-arm64-v8a.zip"
const coreUSB = "/tmp/mnt/WINDOWS_10"
const coreMaxArchive = 48 << 20
const coreMaxBinary = 96 << 20

var coreVersionLine = regexp.MustCompile(`^Xray (\d{1,4}\.\d{1,4}\.\d{1,4}) `)
var coreChecksumLine = regexp.MustCompile(`(?im)^SHA(?:2-)?256\s*(?:\([^\r\n]*\))?\s*[:=]\s*([a-f0-9]{64})\s*$`)

type CoreSettings struct {
	Check       bool             `json:"check"`
	AutoInstall bool             `json:"auto_install"`
	Hour        int              `json:"hour_msk"`
	LastAutoDay string           `json:"last_auto_day,omitempty"`
	Blocked     map[string]int64 `json:"blocked_versions"`
}
type CoreStatus struct {
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	CheckedAt  int64  `json:"checked_at"`
	Available  bool   `json:"available"`
	Prerelease bool   `json:"prerelease"`
	Notes      string `json:"notes,omitempty"`
	URL        string `json:"url"`
	Error      string `json:"error,omitempty"`
}
type CoreRelease struct {
	Prerelease bool
	Version    string
	URL        string
	Size       int64
	Hash       string
	Notes      string
}
type CoreJob struct {
	ID       string `json:"id"`
	Phase    string `json:"phase"`
	Target   string `json:"target"`
	Message  string `json:"message"`
	Started  int64  `json:"started"`
	Finished int64  `json:"finished"`
	Rollback bool   `json:"rollback"`
}

func (s *Server) coreSettings() CoreSettings {
	p := CoreSettings{Check: true, Hour: 4, Blocked: map[string]int64{}}
	_ = readJSON(s.corePath("xray-settings.json"), &p)
	if p.Blocked == nil {
		p.Blocked = map[string]int64{}
	}
	return p
}
func (s *Server) currentCoreVersion() string {
	b, e := s.run(filepath.Join(s.root, "xray"), "version")
	if e != nil {
		return ""
	}
	m := coreVersionLine.FindStringSubmatch(string(b))
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
func newerRelease(a, b string) bool {
	return releasePattern.MatchString(a) && releasePattern.MatchString(b) && releaseAtLeast(a, b) && !releaseAtLeast(b, a)
}

// The byte-getter adapter keeps discovery testable without a network. Production
// uses githubRead so the full release list is never retained in RAM.
func fetchCoreRelease(ctx context.Context, get func(context.Context, string, int64) ([]byte, error)) (CoreRelease, error) {
	return fetchCoreReleaseStream(ctx, func(ctx context.Context, url string, limit int64, consume func(io.Reader) error) error {
		b, e := get(ctx, url, limit)
		if e != nil {
			return e
		}
		return consume(bytes.NewReader(b))
	})
}

func selectCoreRelease(r io.Reader) (ghRelease, error) {
	dec := json.NewDecoder(r)
	start, e := dec.Token()
	if e != nil || start != json.Delim('[') {
		return ghRelease{}, errors.New("список официальных выпусков Xray не распознан")
	}
	var best ghRelease
	for dec.More() {
		var candidate ghRelease
		if e = dec.Decode(&candidate); e != nil {
			return ghRelease{}, e
		}
		v := strings.TrimPrefix(candidate.Tag, "v")
		if candidate.Draft || !strings.HasPrefix(candidate.Tag, "v") || !releasePattern.MatchString(v) {
			continue
		}
		for _, asset := range candidate.Assets {
			if asset.Name == coreAssetName && (best.Tag == "" || newerRelease(v, strings.TrimPrefix(best.Tag, "v"))) {
				// Only the selected release is retained; validation happens afterwards.
				if len(candidate.Body) > 8192 {
					candidate.Body = strings.Clone(candidate.Body[:8192])
				}
				best = candidate
				break
			}
		}
	}
	if end, err := dec.Token(); err != nil || end != json.Delim(']') {
		return ghRelease{}, errors.New("список выпусков Xray оборван")
	}
	// Reject a truncated list or trailing JSON rather than trusting a partial result.
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		return ghRelease{}, errors.New("лишние данные в списке выпусков Xray")
	}
	if best.Tag == "" {
		return best, errors.New("официальный выпуск Xray для Linux ARM64 не найден")
	}
	return best, nil
}

func fetchCoreReleaseStream(ctx context.Context, read func(context.Context, string, int64, func(io.Reader) error) error) (CoreRelease, error) {
	var result CoreRelease
	var rel ghRelease
	e := read(ctx, "https://api.github.com/repos/"+coreRepository+"/releases?per_page=30", 4<<20, func(r io.Reader) error {
		var err error
		rel, err = selectCoreRelease(r)
		return err
	})
	if e != nil {
		return result, e
	}
	assets := map[string]ghAsset{}
	for _, a := range rel.Assets {
		if _, ok := assets[a.Name]; ok {
			return result, errors.New("повторяющиеся файлы Xray")
		}
		assets[a.Name] = a
	}
	asset, ok := assets[coreAssetName]
	expected := "https://github.com/" + coreRepository + "/releases/download/" + rel.Tag + "/" + coreAssetName
	if !ok || asset.URL != expected || asset.Size <= 0 || asset.Size > coreMaxArchive {
		return result, errors.New("нет подходящего официального Linux ARM64 архива")
	}
	hash := strings.TrimPrefix(asset.Digest, "sha256:")
	if asset.Digest == "" {
		dg, ok := assets[coreAssetName+".dgst"]
		if !ok || dg.URL != expected+".dgst" {
			return result, errors.New("официальная SHA-256 не найдена")
		}
		var b []byte
		e = read(ctx, dg.URL, 8192, func(r io.Reader) error { var err error; b, err = io.ReadAll(r); return err })
		if e != nil {
			return result, e
		}
		m := coreChecksumLine.FindStringSubmatch(string(b))
		if len(m) != 2 {
			return result, errors.New("формат официальной SHA-256 не распознан")
		}
		hash = strings.ToLower(m[1])
	}
	raw, e := hex.DecodeString(hash)
	if e != nil || len(raw) != 32 || strings.ToLower(hash) != hash {
		return result, errors.New("некорректная SHA-256 Xray")
	}
	notes := rel.Body
	if len(notes) > 8192 {
		notes = notes[:8192]
	}
	return CoreRelease{Version: strings.TrimPrefix(rel.Tag, "v"), URL: asset.URL, Size: asset.Size, Hash: hash, Notes: notes, Prerelease: rel.Prerelease}, nil
}
func (s *Server) coreStatus() CoreStatus {
	var st CoreStatus
	_ = readJSON(s.runtimePath("core-status.json"), &st)
	if st.Current == "" {
		st.Current = s.currentCoreVersion()
		if st.Current != "" {
			_ = atomicWrite(s.runtimePath("core-status.json"), jsonBytes(st))
		}
	}
	st.Available = newerRelease(st.Latest, st.Current)
	return st
}
func (s *Server) checkCore(ctx context.Context) (CoreStatus, CoreRelease, error) {
	st := CoreStatus{Current: s.currentCoreVersion(), CheckedAt: time.Now().Unix(), URL: "https://github.com/" + coreRepository + "/releases"}
	rel, e := fetchCoreReleaseStream(ctx, githubRead)
	if e != nil {
		st.Error = s.scrub(e.Error())
	} else {
		st.Latest = rel.Version
		st.Prerelease = rel.Prerelease
		st.Available = newerRelease(rel.Version, st.Current)
		st.Notes = rel.Notes
		st.URL += "/tag/v" + rel.Version
		if st.Current == "" {
			st.Error = "Версия установленного ядра не определена; установка запрещена"
		}
	}
	_ = atomicWrite(s.runtimePath("core-status.json"), jsonBytes(st))
	return st, rel, e
}
func (s *Server) coreJob() CoreJob {
	var j CoreJob
	_ = readJSON(s.runtimePath("core-job.json"), &j)
	return j
}
func coreJobActive(j CoreJob) bool { return j.ID != "" && j.Finished == 0 }
func (s *Server) coreProgress(j *CoreJob, phase, message string) {
	j.Phase = phase
	j.Message = message
	_ = atomicWrite(s.runtimePath("core-job.json"), jsonBytes(j))
}
func coreUSBMounted() bool {
	var us, parent syscall.Stat_t
	return syscall.Stat(coreUSB, &us) == nil && syscall.Stat(filepath.Dir(coreUSB), &parent) == nil && us.Dev != parent.Dev
}
func coreBackupDir() (string, error) {
	if !coreUSBMounted() {
		return "", errors.New("для отката нужна подключённая USB WINDOWS_10")
	}
	dir := filepath.Join(coreUSB, "darkls-vpn-backups")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	return dir, nil
}

func extractCore(zipPath, dir string) (string, error) {
	z, e := zip.OpenReader(zipPath)
	if e != nil {
		return "", e
	}
	defer z.Close()
	var entry *zip.File
	for _, f := range z.File {
		if f.Name == "xray" {
			if entry != nil {
				return "", errors.New("повторяющийся xray в архиве")
			}
			entry = f
		}
	}
	if entry == nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > coreMaxBinary {
		return "", errors.New("неверный бинарник xray в архиве")
	}
	r, e := entry.Open()
	if e != nil {
		return "", e
	}
	defer r.Close()
	p := filepath.Join(dir, "xray-candidate")
	w, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		return "", e
	}
	n, e := io.Copy(w, io.LimitReader(r, coreMaxBinary+1))
	if e == nil && uint64(n) != entry.UncompressedSize64 {
		e = errors.New("размер бинарника не совпал")
	}
	if e == nil {
		e = w.Sync()
	}
	ce := w.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(p)
		return "", e
	}
	return p, nil
}

// Caller hands over mutation + diagnostic locks. The shared process lock is
// acquired by installCore only after download, so traffic stays live meanwhile.
func (s *Server) runCoreJob(j CoreJob, retry bool) {
	defer s.mutation.Unlock()
	defer s.diagnosticMu.Unlock()
	var err error
	blocked := false
	defer func() {
		s.coreMu.Lock()
		defer s.coreMu.Unlock()
		j.Finished = time.Now().Unix()
		if err != nil {
			s.coreProgress(&j, "error", s.scrub(err.Error()))
			if blocked {
				p := s.coreSettings()
				p.Blocked[j.Target] = time.Now().Unix()
				pruneBlocked(p.Blocked)
				_ = atomicWrite(s.corePath("xray-settings.json"), jsonBytes(p))
			}
			appendEvent("xray_error", j.Message)
		} else {
			s.coreProgress(&j, "succeeded", "Xray проверен; операция завершена")
			st := s.coreStatus()
			st.Current = s.currentCoreVersion()
			st.Available = newerRelease(st.Latest, st.Current)
			_ = atomicWrite(s.runtimePath("core-status.json"), jsonBytes(st))
			appendEvent("xray", "Операция ядра завершена: "+j.Target)
		}
	}()
	if j.Rollback {
		release, e := s.pause()
		if e != nil {
			err = e
			return
		}
		defer release()
		var previous coreJournal
		if e = readJSON(s.corePath("xray-previous.json"), &previous); e != nil {
			err = errors.New("копия для отката не найдена")
			return
		}
		if !coreUSBMounted() {
			err = errors.New("для отката подключите USB WINDOWS_10")
			return
		}
		if verifyErr := validateCoreBackup(previous); verifyErr != nil {
			err = errors.New("копия для отката отсутствует или повреждена; рабочее ядро не менялось")
			return
		}
		previous.Disabled = s.disabled()
		if e = atomicWrite(s.corePath("xray-update-journal.json"), jsonBytes(previous)); e != nil {
			err = e
			return
		}
		s.coreProgress(&j, "rollback", "Восстанавливаем проверенную копию ядра с USB")
		err = s.restoreCore(previous)
		if err == nil && !previous.Disabled {
			if err = s.checkRouter(); err == nil {
				err = checkVPNConnection()
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s.coreProgress(&j, "checking", "Проверяем официальный выпуск Xray")
	st, rel, e := s.checkCore(ctx)
	if e != nil {
		err = e
		return
	}
	if !st.Available || rel.Version != j.Target {
		err = errors.New("выпуск изменился или уже установлен; проверьте обновления снова")
		return
	}
	s.coreMu.Lock()
	p := s.coreSettings()
	denied := p.Blocked[rel.Version] > 0 && !retry
	s.coreMu.Unlock()
	if denied {
		err = errors.New("эта версия ранее не прошла установку; нужен явный ручной повтор")
		return
	}
	dir, e := coreBackupDir()
	if e != nil {
		err = e
		return
	}
	stage, e := os.MkdirTemp(dir, ".core-stage-")
	if e != nil {
		err = e
		return
	}
	defer os.RemoveAll(stage)
	old, statErr := os.Stat(filepath.Join(s.root, "xray"))
	if statErr != nil {
		err = statErr
		return
	}
	var fs syscall.Statfs_t
	if e = syscall.Statfs(dir, &fs); e != nil || int64(fs.Bavail)*int64(fs.Bsize) < rel.Size+coreMaxBinary+old.Size()+(4<<20) {
		err = errors.New("на USB недостаточно места для архива, распаковки и запаса")
		return
	}
	s.coreProgress(&j, "downloading", "Загружаем официальный ARM64 архив на USB и проверяем SHA-256")
	archive, e := downloadVerifiedAsset(ctx, rel.URL, rel.Size, rel.Hash, stage, "core-*.zip")
	if e != nil {
		err = e
		return
	}
	candidate, e := extractCore(archive, stage)
	if e != nil {
		err = e
		return
	}
	hash, e := fileHash(candidate)
	if e != nil {
		err = e
		return
	}
	s.coreProgress(&j, "installing", "Проверяем конфиг, сохраняем старое ядро и обновляем Xray")
	blocked = true
	err = s.installCore(candidate, rel.Version, hash)
}
func pruneBlocked(m map[string]int64) {
	for len(m) > 12 {
		old := ""
		var at int64
		for k, v := range m {
			if old == "" || v < at {
				old, at = k, v
			}
		}
		delete(m, old)
	}
}
func (s *Server) launchCoreJob(target string, rollback, retry bool) CoreJob {
	j := CoreJob{ID: token()[:16], Target: target, Rollback: rollback, Started: time.Now().Unix()}
	s.coreProgress(&j, "queued", "Операция Xray запущена")
	go s.runCoreJob(j, retry)
	return j
}
func (s *Server) coreAPI(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path != "xray" && path != "xray-check" && path != "xray-settings" && path != "xray-install" && path != "xray-rollback" {
		return false
	}
	if path == "xray" {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return true
		}
		s.coreMu.Lock()
		defer s.coreMu.Unlock()
		var previous coreJournal
		has := readJSON(s.corePath("xray-previous.json"), &previous) == nil && coreUSBMounted()
		if st, e := os.Lstat(previous.Backup); e != nil || !st.Mode().IsRegular() {
			has = false
		}
		respond(w, 200, map[string]any{"status": s.coreStatus(), "settings": s.coreSettings(), "job": s.coreJob(), "rollback_available": has})
		return true
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return true
	}
	if !s.coreMu.TryLock() {
		fail(w, 409, errors.New("операция Xray уже выполняется"))
		return true
	}
	defer s.coreMu.Unlock()
	if coreJobActive(s.coreJob()) {
		fail(w, 409, errors.New("дождитесь завершения операции Xray"))
		return true
	}
	if path == "xray-check" {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		st, _, _ := s.checkCore(ctx)
		respond(w, 200, st)
		return true
	}
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция уже выполняется"))
		return true
	}
	handoff := false
	defer func() {
		if !handoff {
			s.mutation.Unlock()
		}
	}()
	var req struct {
		Password string        `json:"password"`
		Version  string        `json:"version"`
		Retry    bool          `json:"retry"`
		Settings *CoreSettings `json:"settings"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return true
	}
	if e := s.currentPassword(r, req.Password); e != nil {
		fail(w, 403, e)
		return true
	}
	if path == "xray-settings" {
		if req.Settings == nil || req.Settings.Hour < 0 || req.Settings.Hour > 23 || req.Settings.AutoInstall && !req.Settings.Check {
			fail(w, 400, errors.New("укажите час 0–23 МСК; автоустановка требует проверки обновлений"))
			return true
		}
		p := s.coreSettings()
		p.Check = req.Settings.Check
		p.AutoInstall = req.Settings.AutoInstall
		p.Hour = req.Settings.Hour
		if e := atomicWrite(s.corePath("xray-settings.json"), jsonBytes(p)); e != nil {
			fail(w, 500, e)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	if runtime.GOARCH != "arm64" {
		fail(w, 400, errors.New("обновление рассчитано на Linux ARM64 ASUS"))
		return true
	}
	rollback := path == "xray-rollback"
	if !rollback {
		st := s.coreStatus()
		if !st.Available || req.Version != st.Latest {
			fail(w, 409, errors.New("сначала проверьте актуальный выпуск Xray"))
			return true
		}
		if s.coreSettings().Blocked[req.Version] > 0 && !req.Retry {
			fail(w, 409, errors.New("версия заблокирована после ошибки; подтвердите ручной повтор"))
			return true
		}
	}
	if !s.diagnosticMu.TryLock() {
		fail(w, 409, errors.New("диагностика уже выполняется"))
		return true
	}
	handoff = true
	respond(w, 202, map[string]any{"job": s.launchCoreJob(req.Version, rollback, req.Retry)})
	return true
}
func autoCoreDue(p CoreSettings, st CoreStatus, now time.Time, healthy bool) bool {
	day := now.In(time.FixedZone("MSK", 3*3600))
	return p.Check && p.AutoInstall && now.Year() >= 2025 && day.Hour() == p.Hour && day.Format("2006-01-02") != p.LastAutoDay && st.Available && p.Blocked[st.Latest] == 0 && healthy
}
func (s *Server) coreLoop(ctx context.Context) {
	j := s.coreJob()
	if coreJobActive(j) {
		j.Finished = time.Now().Unix()
		s.coreProgress(&j, "interrupted", "Панель перезапущена; проверьте версию и журнал восстановления")
	}
	runPeriodic(ctx, 45*time.Second, 15*time.Minute, func() {
		if time.Now().Year() >= 2025 && !updateBusy() && s.coreMu.TryLock() {
			p := s.coreSettings()
			st := s.coreStatus()
			if p.Check && !coreJobActive(s.coreJob()) && releaseCheckDue(time.Now(), st.CheckedAt, st.Error) {
				ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
				st, _, _ = s.checkCore(ctx)
				cancel()
			}
			if ctx.Err() == nil {
				s.tryAutoCore(p, st, time.Now())
			}
			s.coreMu.Unlock()
		}
	})
}

// coreMu is held by the scheduler. Job ownership passes both operation locks
// to the worker only after the daily attempt has been durably recorded.
func (s *Server) tryAutoCore(p CoreSettings, st CoreStatus, now time.Time) {
	probe := readProbeState()
	healthy := probe.OK && probeFresh(probe, now) && !s.disabled()
	if !autoCoreDue(p, st, now, healthy) || coreJobActive(s.coreJob()) || runtime.GOARCH != "arm64" || !s.mutation.TryLock() {
		return
	}
	launched := false
	defer func() {
		if !launched {
			s.mutation.Unlock()
		}
	}()
	if !s.diagnosticMu.TryLock() {
		return
	}
	defer func() {
		if !launched {
			s.diagnosticMu.Unlock()
		}
	}()
	if _, e := coreBackupDir(); e != nil {
		return
	}
	p.LastAutoDay = now.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02")
	if atomicWrite(s.corePath("xray-settings.json"), jsonBytes(p)) != nil {
		return
	}
	s.launchCoreJob(st.Latest, false, false)
	launched = true
}
