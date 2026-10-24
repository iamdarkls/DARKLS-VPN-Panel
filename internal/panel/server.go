package panel

import (
	"bytes"
	"context"
	"darkls-panel/internal/routerexec"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Runner = routerexec.Runner
type realRunner = routerexec.Real

type Server struct {
	AuthService
	root, dir, host  string
	operationContext context.Context
	runner           Runner
	mutation         sync.Mutex
	diagnosticMu     sync.Mutex
	metricsMu        sync.Mutex
	githubMu         sync.Mutex
	coreMu           sync.Mutex
	historyMu        sync.Mutex
	lastCPU          cpuSample
}

func newServer(root, host string, runner Runner) *Server {
	return &Server{root: root, dir: filepath.Join(root, "panel"), host: host, runner: runner, AuthService: newAuthService()}
}

func (s *Server) run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.networkContext(), 40*time.Second)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
}

// networkOperation creates an isolated command scope, never copies mutexes and
// never starts workers. The caller retains the owning server's mutation locks.
func (s *Server) networkOperation(ctx context.Context) *Server {
	return &Server{root: s.root, dir: s.dir, host: s.host, runner: s.runner, operationContext: ctx}
}
func (s *Server) networkContext() context.Context {
	if s.operationContext != nil {
		return s.operationContext
	}
	return context.Background()
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
