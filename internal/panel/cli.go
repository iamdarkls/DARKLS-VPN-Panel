package panel

import (
	"context"
	"darkls-panel/internal/routerenv"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"
)

var version = "2.1.1"

func tuneRuntime() {
	// The local control panel has little parallel work. Bound Go's memory target
	// and collect sooner; this is a soft runtime budget, not a hard RSS limit.
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(24 << 20)
	debug.SetGCPercent(75)
}

// Run owns CLI startup; HTTP and background components share one Server instance.
func Run() {
	tuneRuntime()
	root := flag.String("root", "/jffs/xray", "каталог VPN")
	listen := flag.String("listen", routerenv.PanelAddress, "адрес панели")
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
		if s.pendingRecovery() {
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
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Fatal(e)
	}
	pidPath := "/tmp/darkls-panel.pid"
	if e = atomicWrite(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n")); e != nil {
		log.Fatal(e)
	}
	defer os.Remove(pidPath)
	if e := s.recoverPending(); e != nil {
		log.Print("Режим восстановления; изменения заблокированы: ", s.scrub(e.Error()))
	}
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
