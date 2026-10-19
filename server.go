package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

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

func (s *Server) run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
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
