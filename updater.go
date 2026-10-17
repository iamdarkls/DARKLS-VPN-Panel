package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updateJobPath = "/tmp/darkls-update-status.json"

type PackageFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type PackageManifest struct {
	Format  int                    `json:"format"`
	Version string                 `json:"version"`
	Arch    string                 `json:"arch"`
	Files   map[string]PackageFile `json:"files"`
}
type UpdateJob struct {
	ID       string `json:"id"`
	Phase    string `json:"phase"`
	Target   string `json:"target"`
	Message  string `json:"message"`
	Started  int64  `json:"started"`
	Finished int64  `json:"finished,omitempty"`
}

var packageNames = []string{"darkls-panel", "install-update.sh", "reload-dns.sh", "devices-sync.sh", "watch.sh"}
var releasePattern = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)

func readUpdateJob() UpdateJob { var j UpdateJob; _ = readJSON(updateJobPath, &j); return j }
func updateBusy() bool {
	j := readUpdateJob()
	return (j.Phase == "staged" || j.Phase == "installing") && time.Now().Unix()-j.Started < 600
}
func (s *Server) publicUpdateStatus(w http.ResponseWriter) {
	j := readUpdateJob()
	respond(w, 200, map[string]any{"version": version, "job": j})
}
func validateManifest(m PackageManifest) error {
	if !releaseAtLeast(m.Version, "1.1.0") || !releaseAtLeast(m.Version, version) {
		return errors.New("версия пакета старее установленной или несовместима")
	}
	if m.Format != 1 || m.Arch != runtime.GOARCH || !releasePattern.MatchString(m.Version) || len(m.Files) != len(packageNames) {
		return errors.New("пакет не подходит этой панели или архитектуре")
	}
	var total int64
	for _, name := range packageNames {
		f, ok := m.Files[name]
		if !ok || f.Size <= 0 || len(f.SHA256) != 64 {
			return errors.New("неполный манифест пакета")
		}
		if _, e := hex.DecodeString(f.SHA256); e != nil {
			return e
		}
		limit := int64(128 << 10)
		if name == "darkls-panel" {
			limit = 14 << 20
		}
		if f.Size > limit {
			return errors.New("слишком большой файл пакета")
		}
		total += f.Size
	}
	if total > 15<<20 {
		return errors.New("слишком большой распакованный пакет")
	}
	return nil
}
func stagePackage(reader io.ReaderAt, size int64) (dir string, m PackageManifest, err error) {
	z, e := zip.NewReader(reader, size)
	if e != nil {
		return "", m, errors.New("неверный ZIP-архив")
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			if f.Name != "darkls-panel-update/" {
				return "", m, errors.New("лишний каталог в пакете")
			}
			continue
		}
		if !strings.HasPrefix(f.Name, "darkls-panel-update/") || !f.Mode().IsRegular() {
			return "", m, errors.New("неверный путь или тип файла")
		}
		name := strings.TrimPrefix(f.Name, "darkls-panel-update/")
		if name == "" || strings.ContainsAny(name, "/\\") || files[name] != nil {
			return "", m, errors.New("неверный или повторяющийся файл")
		}
		files[name] = f
	}
	if len(files) != len(packageNames)+1 {
		return "", m, errors.New("неверный состав пакета")
	}
	mf := files["manifest.json"]
	if mf == nil || mf.UncompressedSize64 > 8192 {
		return "", m, errors.New("нет манифеста")
	}
	r, e := mf.Open()
	if e != nil {
		return "", m, e
	}
	data, e := io.ReadAll(io.LimitReader(r, 8193))
	r.Close()
	if e != nil || len(data) > 8192 {
		return "", m, errors.New("неверный манифест")
	}
	if e = json.Unmarshal(data, &m); e != nil {
		return "", m, e
	}
	if e = validateManifest(m); e != nil {
		return "", m, e
	}
	dir, e = os.MkdirTemp("/tmp", "darkls-update-")
	if e != nil {
		return "", m, e
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	for _, name := range packageNames {
		meta := m.Files[name]
		zf := files[name]
		if zf == nil || zf.UncompressedSize64 != uint64(meta.Size) {
			return dir, m, errors.New("размер файла не совпадает с манифестом")
		}
		src, e := zf.Open()
		if e != nil {
			return dir, m, e
		}
		dst, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if e != nil {
			src.Close()
			return dir, m, e
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, meta.Size+1))
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil || n != meta.Size || hex.EncodeToString(hash.Sum(nil)) != meta.SHA256 {
			return dir, m, errors.New("контрольная сумма пакета не совпадает")
		}
	}
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), jsonBytes(m)); err != nil {
		return dir, m, err
	}
	if err = checkELF(filepath.Join(dir, "darkls-panel"), m.Arch); err != nil {
		return dir, m, err
	}
	return dir, m, nil
}
func checkELF(path, arch string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	header := make([]byte, 64)
	if _, e = io.ReadFull(f, header); e != nil {
		return errors.New("неполный исполняемый файл")
	}
	machine := uint16(header[18]) | uint16(header[19])<<8
	expected := uint16(183)
	if arch == "amd64" {
		expected = 62
	}
	if string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || machine != expected {
		return errors.New("неверный ELF или архитектура")
	}
	return nil
}
func (s *Server) uploadUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("другая операция выполняется"))
		return
	}
	defer s.mutation.Unlock()
	if updateBusy() {
		fail(w, 409, errors.New("обновление уже выполняется"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if e := r.ParseMultipartForm(256 << 10); e != nil {
		fail(w, 400, errors.New("неверный пакет или размер больше 8 МиБ"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	if e := s.currentPassword(r, r.FormValue("password")); e != nil {
		fail(w, 403, e)
		return
	}
	file, header, e := r.FormFile("package")
	if e != nil {
		fail(w, 400, errors.New("выберите ZIP-пакет обновления"))
		return
	}
	defer file.Close()
	dir, m, e := stagePackage(file, header.Size)
	if e != nil {
		fail(w, 400, e)
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
func (s *Server) installStaged(dir string) (err error) {
	clean := filepath.Clean(dir)
	if filepath.Dir(clean) != "/tmp" || !strings.HasPrefix(filepath.Base(clean), "darkls-update-") {
		return errors.New("неверный каталог обновления")
	}
	defer os.RemoveAll(clean)
	defer func() {
		if err != nil {
			job := readUpdateJob()
			if job.Phase == "staged" || job.Phase == "installing" {
				job.Phase = "error"
				job.Message = "Проверка или установка пакета не завершена"
				job.Finished = time.Now().Unix()
				_ = atomicWrite(updateJobPath, jsonBytes(job))
				appendEvent("update_error", job.Message)
			}
		}
	}()
	var m PackageManifest
	if e := readJSON(filepath.Join(clean, "manifest.json"), &m); e != nil {
		return e
	}
	if e := validateManifest(m); e != nil {
		return e
	}
	for _, name := range packageNames {
		f, e := os.Open(filepath.Join(clean, name))
		if e != nil {
			return e
		}
		hash := sha256.New()
		n, e := io.Copy(hash, io.LimitReader(f, m.Files[name].Size+1))
		f.Close()
		if e != nil || n != m.Files[name].Size || hex.EncodeToString(hash.Sum(nil)) != m.Files[name].SHA256 {
			return errors.New("пакет изменился после проверки")
		}
	}
	job := readUpdateJob()
	job.Phase = "installing"
	job.Message = "Устанавливаем панель; соединение временно прервётся"
	_ = atomicWrite(updateJobPath, jsonBytes(job))
	cmd := exec.Command("sh", filepath.Join(clean, "install-update.sh"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	e := cmd.Run()
	job.Finished = time.Now().Unix()
	if e != nil {
		job.Phase = "error"
		job.Message = "Установка не завершена; проверьте журнал обновления"
		if _, re := os.Stat(filepath.Join(clean, "rollback.ok")); re == nil {
			job.Phase = "rolled_back"
			job.Message = "Обновление не удалось; предыдущая версия восстановлена"
		}
		appendEvent("update_error", job.Message)
	} else {
		job.Phase = "succeeded"
		job.Message = "Панель обновлена. Войдите снова."
		appendEvent("update", "Панель обновлена до "+m.Version)
	}
	_ = atomicWrite(updateJobPath, jsonBytes(job))
	return e
}

func verifyDirectory(dir string) (PackageManifest, error) {
	var m PackageManifest
	if e := readJSON(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return m, e
	}
	if e := validateManifest(m); e != nil {
		return m, e
	}
	if m.Version != version {
		return m, errors.New("версия программы не совпадает с манифестом")
	}
	for _, name := range packageNames {
		f, e := os.Open(filepath.Join(dir, name))
		if e != nil {
			return m, e
		}
		hash := sha256.New()
		n, e := io.Copy(hash, io.LimitReader(f, m.Files[name].Size+1))
		f.Close()
		if e != nil || n != m.Files[name].Size || hex.EncodeToString(hash.Sum(nil)) != m.Files[name].SHA256 {
			return m, errors.New("пакет изменился или повреждён")
		}
	}
	return m, checkELF(filepath.Join(dir, "darkls-panel"), m.Arch)
}

func releaseAtLeast(a, b string) bool {
	if !releasePattern.MatchString(a) || !releasePattern.MatchString(b) {
		return false
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return true
}
