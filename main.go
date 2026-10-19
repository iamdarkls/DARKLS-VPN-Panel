package main

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

var version = "1.7.0"

type Auth struct {
	User string `json:"user"`
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

type Session struct {
	CSRF     string
	Expires  time.Time
	AuthSalt string
}

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type realRunner struct{}

func (realRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), "PATH=/usr/sbin:/usr/bin:/sbin:/bin")
	return c.CombinedOutput()
}

type Server struct {
	root, dir, host string
	runner          Runner
	auth            Auth
	sessions        map[string]Session
	sessionMu       sync.Mutex
	mutation        sync.Mutex
	loginMu         sync.Mutex
	attempts        map[string][]time.Time
	authMu          sync.RWMutex
	diagnosticMu    sync.Mutex
	metricsMu       sync.Mutex
	githubMu        sync.Mutex
	coreMu          sync.Mutex
	historyMu       sync.Mutex
	lastCPU         cpuSample
}

func newServer(root, host string, runner Runner) *Server {
	return &Server{root: root, dir: filepath.Join(root, "panel"), host: host, runner: runner, sessions: map[string]Session{}, attempts: map[string][]time.Time{}}
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}

func passwordHash(password, salt string) string {
	key, e := pbkdf2.Key(sha256.New, password, []byte(salt), 150000, 32)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(key)
}

func (s *Server) run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, e error) {
	respond(w, code, map[string]string{"error": e.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("неверные данные запроса")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("лишние данные запроса")
	}
	return nil
}

func (s *Server) session(r *http.Request) (Session, bool) {
	cookie, e := r.Cookie("darkls_session")
	if e != nil {
		return Session{}, false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	v, ok := s.sessions[cookie.Value]
	if ok && v.AuthSalt != s.authSnapshot().Salt {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	if ok && time.Now().After(v.Expires) {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	return v, ok
}

func (s *Server) allowedIP(r *http.Request) bool {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return false
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return ip.IsLoopback() || netip.MustParsePrefix("192.168.50.0/24").Contains(ip.Unmap())
}

func (s *Server) handler() http.Handler {
	sub, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != s.host || !s.allowedIP(r) {
			fail(w, 403, errors.New("доступ разрешён только из домашней сети по адресу панели"))
			return
		}
		if r.Method == "POST" && r.Header.Get("Origin") != "http://"+s.host {
			fail(w, 403, errors.New("неверный источник запроса"))
			return
		}
		if r.URL.Path == "/api/login" {
			s.login(w, r)
			return
		}
		if r.URL.Path == "/api/update-status" && r.Method == "GET" {
			s.publicUpdateStatus(w)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				w.WriteHeader(405)
				return
			}
			if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/theme.js" && r.URL.Path != "/style.css" && r.URL.Path != "/advanced.js" && r.URL.Path != "/core.js" && r.URL.Path != "/manage.js" && r.URL.Path != "/notifications.js" {
				http.NotFound(w, r)
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		sess, ok := s.session(r)
		if !ok {
			fail(w, 401, errors.New("войдите в панель"))
			return
		}
		if r.Method == "POST" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(sess.CSRF)) != 1 {
			fail(w, 403, errors.New("обновите страницу и повторите вход"))
			return
		}
		if r.Method == "POST" && updateBusy() && r.URL.Path != "/api/logout" {
			fail(w, 409, errors.New("выполняется обновление панели"))
			return
		}
		if s.connectionAPI(w, r) {
			return
		}
		if s.activityAPI(w, r) {
			return
		}
		if s.coreAPI(w, r) {
			return
		}
		if s.advancedAPI(w, r) {
			return
		}
		if s.extraAPI(w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/session":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			respond(w, 200, map[string]any{"csrf": sess.CSRF, "version": version, "user": s.authSnapshot().User})
		case "/api/state":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			s.mutation.Lock()
			defer s.mutation.Unlock()
			st, e := s.state()
			if e != nil {
				fail(w, 500, e)
				return
			}
			respond(w, 200, map[string]any{"state": st, "revision": s.revision(), "catalog_notes": catalogNotes})
		case "/api/status":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			s.status(w)
		case "/api/backups":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			entries, _ := os.ReadDir(filepath.Join(s.dir, "backups"))
			ids := []string{}
			for _, e := range entries {
				if e.IsDir() && idPattern.MatchString(e.Name()) {
					ids = append(ids, e.Name())
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(ids)))
			respond(w, 200, map[string]any{"backups": ids})
		case "/api/export":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			st, e := s.state()
			if e != nil {
				fail(w, 500, e)
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="darkls-settings.json"`)
			respond(w, 200, st)
		case "/api/apply", "/api/restore", "/api/power":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			s.mutate(w, r)
		case "/api/logout":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			c, _ := r.Cookie("darkls_session")
			s.sessionMu.Lock()
			delete(s.sessions, c.Value)
			s.sessionMu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "darkls_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			respond(w, 200, map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	s.loginMu.Lock()
	recent := []time.Time{}
	for _, t := range s.attempts[ip] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		s.loginMu.Unlock()
		fail(w, 429, errors.New("слишком много попыток; повторите через 5 минут"))
		return
	}
	s.attempts[ip] = append(recent, now)
	s.loginMu.Unlock()
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	auth := s.authSnapshot()
	h := passwordHash(req.Password, auth.Salt)
	if subtle.ConstantTimeCompare([]byte(req.User), []byte(auth.User)) != 1 || subtle.ConstantTimeCompare([]byte(h), []byte(auth.Hash)) != 1 {
		fail(w, 401, errors.New("неверный логин или пароль"))
		return
	}
	s.loginMu.Lock()
	delete(s.attempts, ip)
	s.loginMu.Unlock()
	key := token()
	sess := Session{CSRF: token(), Expires: now.Add(12 * time.Hour), AuthSalt: auth.Salt}
	s.sessionMu.Lock()
	for k, v := range s.sessions {
		if now.After(v.Expires) {
			delete(s.sessions, k)
		}
	}
	s.sessions[key] = sess
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "darkls_session", Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	respond(w, 200, map[string]string{"csrf": sess.CSRF})
}

func (s *Server) mutate(w http.ResponseWriter, r *http.Request) {
	if !s.mutation.TryLock() {
		fail(w, 409, errors.New("настройки уже применяются"))
		return
	}
	defer s.mutation.Unlock()
	switch r.URL.Path {
	case "/api/apply":
		var req struct {
			State    State  `json:"state"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		id, e := s.apply(req.State, req.Revision)
		if e != nil {
			code := 400
			if strings.HasPrefix(e.Error(), "CONFLICT:") {
				code = 409
			}
			fail(w, code, e)
			return
		}
		respond(w, 200, map[string]any{"ok": true, "backup": id, "revision": s.revision(), "disabled": s.disabled()})
	case "/api/restore":
		var req struct {
			Backup   string `json:"backup"`
			Revision string `json:"revision"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		if !idPattern.MatchString(req.Backup) {
			fail(w, 400, errors.New("неверная копия"))
			return
		}
		var st State
		if e := readJSON(filepath.Join(s.dir, "backups", req.Backup, "state.json"), &st); e != nil {
			fail(w, 404, errors.New("копия не найдена"))
			return
		}
		id, e := s.apply(st, req.Revision)
		if e != nil {
			fail(w, 400, e)
			return
		}
		respond(w, 200, map[string]any{"ok": true, "backup": id})
	case "/api/power":
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		release, e := s.pause()
		if e != nil {
			fail(w, 409, e)
			return
		}
		defer release()
		path := filepath.Join(s.root, "autostart.disabled")
		wasDisabled := s.disabled()
		if req.Enabled {
			e = os.Remove(path)
			if os.IsNotExist(e) {
				e = nil
			}
		} else {
			e = atomicWrite(path, []byte("disabled by panel\n"))
		}
		if e == nil {
			e = s.restart()
		}
		if e != nil {
			if wasDisabled {
				_ = atomicWrite(path, []byte("disabled\n"))
			} else {
				_ = os.Remove(path)
			}
			_ = s.restart()
			fail(w, 500, e)
			return
		}
		message := "VPN выключен"
		if req.Enabled {
			message = "VPN включён"
		}
		appendEvent("power", message)
		respond(w, 200, map[string]bool{"ok": true})
	}
}

func (s *Server) status(w http.ResponseWriter) {
	// Keep status within the browser's timeout even if a router utility stalls.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := func(name string, args ...string) bool { _, e := s.runner.Run(ctx, name, args...); return e == nil }
	nat := check("iptables", "-t", "nat", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_NAT")
	udp := check("iptables", "-t", "mangle", "-C", "PREROUTING", "-i", "br0", "-s", "192.168.50.0/24", "-j", "DV_UDP")
	xray := len(s.mainXrayPIDsContext(ctx)) > 0
	dns := pidMatches("/tmp/dnsmasq-vpn.pid", "dnsmasq-vpn.conf")
	watch := pidMatches("/tmp/darkls-watch.pid", "/jffs/xray/watch.sh")
	probe := readProbeState()
	fresh := probeFresh(probe, time.Now())
	respond(w, 200, map[string]any{"active": nat && udp && xray && dns && !s.disabled() && probe.OK && fresh, "routes": nat && udp && xray && dns, "disabled": s.disabled(), "nat": nat, "udp": udp, "xray": xray, "dns": dns, "watch": watch, "version": version, "probe": probe, "probe_fresh": fresh})
}

func pidMatches(file, match string) bool {
	b, e := os.ReadFile(file)
	if e != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	if _, e = strconv.Atoi(pid); e != nil {
		return false
	}
	cmd, e := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	return e == nil && bytes.Contains(cmd, []byte(match))
}

func tuneRuntime() {
	// The local control panel has little parallel work. Bound Go's memory target
	// and collect sooner; this is a soft runtime budget, not a hard RSS limit.
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(24 << 20)
	debug.SetGCPercent(75)
}

func main() {
	tuneRuntime()
	root := flag.String("root", "/jffs/xray", "каталог VPN")
	listen := flag.String("listen", "192.168.50.1:8088", "адрес панели")
	initAuth := flag.Bool("init", false, "создать пароль и импортировать списки")
	reset := flag.Bool("reset-password", false, "создать новый пароль")
	watchRecover := flag.Bool("watch-recover", false, "восстановить маршрутизацию из автоконтроля")
	syncGroups := flag.Bool("sync-groups", false, "восстановить правила групп без перезапуска Xray")
	upgradePolicy := flag.Bool("upgrade-policy", false, "мигрировать правила групп")
	recoverParts := flag.Bool("recover-components", false, "точечное восстановление VPN")
	checkParts := flag.Bool("check-components", false, "проверить компоненты соединения")
	enableXGrok := flag.Bool("enable-x-grok", false, "однократно включить X и Grok с зависимостями")
	repairIG := flag.Bool("repair-instagram", false, "добавить недостающий домен проверки Instagram один раз")
	updateCatalog := flag.Bool("update-catalog", false, "добавить отсутствующие группы выключенными")
	checkVPN := flag.Bool("check-vpn", false, "проверить HTTPS через локальный SOCKS Xray")
	upgradeXray := flag.String("upgrade-xray", "", "установить официальное ядро Xray 26.10.10 arm64 с откатом")
	rollbackXray := flag.Bool("rollback-xray", false, "вернуть предыдущее ядро Xray с USB")
	checkRouter := flag.Bool("check-router", false, "проверить содержимое правил и VPN DNS")
	maintain := flag.Bool("maintain-logs", false, "ограничить журналы в RAM")
	showVersion := flag.Bool("version", false, "показать версию")
	syncDevices := flag.Bool("sync-devices", false, "обновить выбор устройств")
	forceDevices := flag.Bool("force-devices", false, "восстановить правила устройств")
	reason := flag.String("reason", "автоконтроль", "причина восстановления")
	installStaged := flag.String("install-staged", "", "установить проверенный пакет")
	verifyPackage := flag.String("verify-package", "", "проверить пакет")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *verifyPackage != "" {
		if _, e := verifyDirectory(*verifyPackage); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Package integrity OK")
		return
	}
	s := newServer(*root, *listen, realRunner{})
	if *maintain {
		maintainLogs()
		return
	}
	if *checkRouter {
		if e := s.checkRouter(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println("VPN rules and DNS OK")
		return
	}
	if *checkVPN {
		started := time.Now()
		e := checkVPNConnection()
		recordProbe(e, time.Since(started))
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println("VPN HTTPS OK")
		return
	}
	if *upgradeXray != "" {
		if e := s.upgradeCore(*upgradeXray); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *rollbackXray {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		var j coreJournal
		if e = readJSON(s.corePath("xray-previous.json"), &j); e != nil {
			log.Fatal(e)
		}
		if e := validateCoreBackup(j); e != nil {
			log.Fatal(e)
		}
		j.Disabled = s.disabled()
		if e = atomicWrite(s.corePath("xray-update-journal.json"), jsonBytes(j)); e != nil {
			log.Fatal(e)
		}
		if e = s.restoreCore(j); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Previous Xray restored")
		return
	}
	if *installStaged != "" {
		if e := s.installStaged(*installStaged); e != nil {
			log.Fatal(e)
		}
		return
	}
	if e := s.init(); e != nil {
		log.Fatal(e)
	}

	if *syncDevices {
		if e := s.syncDevices(*forceDevices); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *syncGroups {
		if e := s.syncGroupRouting(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *upgradePolicy {
		if e := s.upgradeGroupPolicy(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *checkParts {
		snap := s.collectConnectivity(true)
		s.saveConnectivity(snap)
		fmt.Println(string(jsonBytes(snap)))
		return
	}
	if *recoverParts {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		if e = s.recoverComponents(*reason); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *enableXGrok {
		if e := s.enableRequestedXGrok(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *repairIG {
		if e := s.repairInstagram(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *updateCatalog {
		if e := s.updateCatalog(); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *watchRecover {
		release, e := s.pause()
		if e != nil {
			log.Fatal(e)
		}
		defer release()
		if s.disabled() {
			return
		}
		var pending coreJournal
		if readJSON(s.corePath("xray-update-journal.json"), &pending) == nil {
			if e = s.restoreCore(pending); e != nil {
				log.Fatal(e)
			}
			return
		}
		if _, e = os.Stat(filepath.Join(s.dir, "apply-journal.json")); e == nil {
			log.Print("Незавершённое применение: ожидаем восстановления панелью")
			return
		}
		e = s.recoverComponents(*reason)
		fmt.Println("Component recovery checked")
		if e != nil {
			release()
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if e := s.recoverCoreJournal(); e != nil {
		log.Fatal(e)
	}
	if e := s.recoverProfileSwitch(); e != nil {
		log.Fatal(e)
	}
	authPath := filepath.Join(s.dir, "auth.json")
	if *initAuth || *reset {
		if _, e := os.Stat(authPath); e == nil && !*reset {
			fmt.Println("Пароль уже создан. Для сброса: --reset-password")
			return
		}
		password := token()[:24]
		s.auth = Auth{User: "admin", Salt: token()}
		s.auth.Hash = passwordHash(password, s.auth.Salt)
		if e := atomicWrite(authPath, jsonBytes(s.auth)); e != nil {
			log.Fatal(e)
		}
		fmt.Printf("Логин: admin\nПароль: %s\nАдрес: http://%s\n", password, *listen)
		return
	}
	if e := readJSON(authPath, &s.auth); e != nil {
		log.Fatal("сначала выполните darkls-panel --init")
	}
	if e := s.recoverJournal(); e != nil {
		log.Fatal("восстановление незавершённого применения: ", e)
	}
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Fatal(e)
	}
	pidPath := "/tmp/darkls-panel.pid"
	if e = atomicWrite(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n")); e != nil {
		log.Fatal(e)
	}
	defer os.Remove(pidPath)
	// Start workers only after binding the listener: a second panel process must
	// not mutate router state when its HTTP port is already occupied.
	background, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()
	s.startBackground(background)
	server := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	log.Printf("DARKLS panel %s: %s", version, *listen)
	if e = server.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Print(e)
	}
}
