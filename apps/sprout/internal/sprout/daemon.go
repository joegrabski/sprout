package sprout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	daemonDialTimeout       = 180 * time.Millisecond
	daemonReadWriteTimeout  = 2 * time.Second
	daemonServerIdleTimeout = 30 * time.Second
)

type daemonState struct {
	RepoRoot        string     `json:"repo_root"`
	FetchedAtUnixMs int64      `json:"fetched_at_unix_ms"`
	StateVersion    int64      `json:"state_version"`
	Source          string     `json:"source"`
	Worktrees       []Worktree `json:"worktrees"`
}

type daemonRequest struct {
	Op           string `json:"op"`
	RepoRoot     string `json:"repo_root,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Message      string `json:"message,omitempty"`
}

type daemonResponse struct {
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
	State *daemonState `json:"state,omitempty"`
}

type daemonServer struct {
	cfg Config
	mgr *Manager

	mu        sync.RWMutex
	states    map[string]daemonState
	requested map[string]time.Time
	version   int64
	stopping  bool
	stopCh    chan struct{}
}

func normalizeDaemonConfig(cfg Config) Config {
	if cfg.DaemonRefreshMs < 250 {
		cfg.DaemonRefreshMs = 250
	}
	if cfg.DaemonRefreshMs > 30_000 {
		cfg.DaemonRefreshMs = 30_000
	}
	if cfg.DaemonStaleAfterMs < 250 {
		cfg.DaemonStaleAfterMs = 250
	}
	if cfg.DaemonStaleAfterMs > 120_000 {
		cfg.DaemonStaleAfterMs = 120_000
	}
	if strings.TrimSpace(cfg.DaemonSocketPath) == "" {
		cfg.DaemonSocketPath = "~/.cache/sprout/daemon.sock"
	}
	return cfg
}

func daemonSocketPath(cfg Config) string {
	cfg = normalizeDaemonConfig(cfg)
	raw := strings.TrimSpace(cfg.DaemonSocketPath)
	if raw == "" {
		raw = "~/.cache/sprout/daemon.sock"
	}
	resolve := func(value string) string {
		if value == "~" || strings.HasPrefix(value, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				if value == "~" {
					return home
				}
				return filepath.Join(home, value[2:])
			}
		}
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		if abs, err := filepath.Abs(value); err == nil {
			return filepath.Clean(abs)
		}
		return value
	}
	tryPath := func(value string) (string, bool) {
		value = resolve(value)
		if strings.TrimSpace(value) == "" {
			return "", false
		}
		if err := os.MkdirAll(filepath.Dir(value), 0o755); err != nil {
			return "", false
		}
		return value, true
	}

	if path, ok := tryPath(raw); ok {
		return path
	}
	fallback := filepath.Join(os.TempDir(), "sprout-"+safeName(os.Getenv("USER"))+"-daemon.sock")
	if path, ok := tryPath(fallback); ok {
		return path
	}
	return resolve(raw)
}

func daemonPIDPath(cfg Config) string {
	return daemonSocketPath(cfg) + ".pid"
}

func writeDaemonPID(cfg Config) error {
	pidPath := daemonPIDPath(cfg)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func readDaemonPID(cfg Config) (int, error) {
	data, err := os.ReadFile(daemonPIDPath(cfg))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func daemonProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func daemonStart(cfg Config) error {
	cfg = normalizeDaemonConfig(cfg)
	if err := daemonPing(cfg, daemonDialTimeout); err == nil {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon", "run")
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		defer devNull.Close()
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := daemonPing(cfg, daemonDialTimeout); err == nil {
			return nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	return errors.New("daemon did not become ready")
}

func daemonStop(cfg Config) error {
	cfg = normalizeDaemonConfig(cfg)
	_, err := daemonDoRequest(cfg, daemonRequest{Op: "stop"}, daemonDialTimeout)
	if err != nil {
		if pid, pidErr := readDaemonPID(cfg); pidErr == nil && daemonProcessAlive(pid) {
			proc, findErr := os.FindProcess(pid)
			if findErr == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pingErr := daemonPing(cfg, daemonDialTimeout); pingErr != nil {
			_ = os.Remove(daemonPIDPath(cfg))
			_ = os.Remove(daemonSocketPath(cfg))
			return nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	return errors.New("daemon did not stop in time")
}

func daemonStatus(cfg Config) (string, error) {
	cfg = normalizeDaemonConfig(cfg)
	if err := daemonPing(cfg, daemonDialTimeout); err == nil {
		return "running", nil
	}
	if pid, err := readDaemonPID(cfg); err == nil && daemonProcessAlive(pid) {
		return "starting", nil
	}
	return "stopped", nil
}

func daemonPing(cfg Config, timeout time.Duration) error {
	resp, err := daemonDoRequest(cfg, daemonRequest{Op: "ping"}, timeout)
	if err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			return errors.New("daemon ping failed")
		}
		return errors.New(resp.Error)
	}
	return nil
}

func daemonGetState(cfg Config, repoRoot string, timeout time.Duration) (daemonState, error) {
	resp, err := daemonDoRequest(cfg, daemonRequest{Op: "get_state", RepoRoot: repoRoot}, timeout)
	if err != nil {
		return daemonState{}, err
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) == "" {
			return daemonState{}, errors.New("daemon returned failure")
		}
		return daemonState{}, errors.New(resp.Error)
	}
	if resp.State == nil {
		return daemonState{}, errors.New("daemon returned empty state")
	}
	return *resp.State, nil
}

func daemonDoRequest(cfg Config, req daemonRequest, timeout time.Duration) (daemonResponse, error) {
	sock := daemonSocketPath(cfg)
	if strings.TrimSpace(sock) == "" {
		return daemonResponse{}, errors.New("daemon socket path is empty")
	}
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return daemonResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(daemonReadWriteTimeout))

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return daemonResponse{}, err
	}
	dec := json.NewDecoder(conn)
	var resp daemonResponse
	if err := dec.Decode(&resp); err != nil {
		return daemonResponse{}, err
	}
	return resp, nil
}

func newDaemonServer(cfg Config, mgr *Manager) *daemonServer {
	cfg = normalizeDaemonConfig(cfg)
	return &daemonServer{
		cfg:       cfg,
		mgr:       mgr,
		states:    map[string]daemonState{},
		requested: map[string]time.Time{},
		stopCh:    make(chan struct{}),
	}
}

func runDaemonServer(cfg Config) error {
	cfg = normalizeDaemonConfig(cfg)
	sock := daemonSocketPath(cfg)
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		return err
	}

	if pid, err := readDaemonPID(cfg); err == nil && daemonProcessAlive(pid) {
		if err := daemonPing(cfg, daemonDialTimeout); err == nil {
			return errors.New("daemon already running")
		}
	}

	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(sock)
		_ = os.Remove(daemonPIDPath(cfg))
	}()
	if err := os.Chmod(sock, 0o600); err != nil {
		debugLogf("daemon chmod socket failed: %v", err)
	}
	if err := writeDaemonPID(cfg); err != nil {
		debugLogf("daemon write pid failed: %v", err)
	}

	srv := newDaemonServer(cfg, NewManager(cfg))
	go srv.refreshLoop()

	for {
		_ = ln.(*net.UnixListener).SetDeadline(time.Now().Add(daemonServerIdleTimeout))
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if srv.isStopping() {
					return nil
				}
				continue
			}
			if srv.isStopping() {
				return nil
			}
			return err
		}
		go srv.handleConn(conn)
	}
}

func (s *daemonServer) isStopping() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopping
}

func (s *daemonServer) setStopping() {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		close(s.stopCh)
	}
	s.mu.Unlock()
}

func (s *daemonServer) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(daemonReadWriteTimeout))

	dec := json.NewDecoder(conn)
	var req daemonRequest
	if err := dec.Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(daemonResponse{OK: false, Error: err.Error()})
		return
	}

	var resp daemonResponse
	switch strings.ToLower(strings.TrimSpace(req.Op)) {
	case "ping":
		resp = daemonResponse{OK: true}
	case "stop":
		s.setStopping()
		resp = daemonResponse{OK: true}
	case "get_state":
		repoRoot := strings.TrimSpace(req.RepoRoot)
		if repoRoot == "" {
			resp = daemonResponse{OK: false, Error: "repo_root is required"}
			break
		}
		state, err := s.getState(repoRoot)
		if err != nil {
			resp = daemonResponse{OK: false, Error: err.Error()}
			break
		}
		resp = daemonResponse{OK: true, State: &state}
	default:
		resp = daemonResponse{OK: false, Error: "unsupported op"}
	}

	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		debugLogf("daemon write response failed: %v", err)
	}
}

func (s *daemonServer) getState(repoRoot string) (daemonState, error) {
	repoRoot = absPath(repoRoot)
	s.mu.Lock()
	s.requested[repoRoot] = time.Now()
	cached, ok := s.states[repoRoot]
	s.mu.Unlock()
	if ok {
		cached.Source = "cache"
		return cached, nil
	}
	if err := s.refreshRepo(repoRoot); err != nil {
		return daemonState{}, err
	}
	s.mu.RLock()
	state := s.states[repoRoot]
	s.mu.RUnlock()
	state.Source = "fresh"
	return state, nil
}

func (s *daemonServer) refreshLoop() {
	refreshEvery := time.Duration(s.cfg.DaemonRefreshMs) * time.Millisecond
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			repos := s.requestedRepos()
			for _, repo := range repos {
				_ = s.refreshRepo(repo)
			}
		}
	}
}

func (s *daemonServer) requestedRepos() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.requested))
	for repo := range s.requested {
		out = append(out, repo)
	}
	return out
}

func (s *daemonServer) refreshRepo(repoRoot string) error {
	items, err := s.mgr.ListWorktreesForRepo(repoRoot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.version++
	s.states[repoRoot] = daemonState{
		RepoRoot:        repoRoot,
		FetchedAtUnixMs: time.Now().UnixMilli(),
		StateVersion:    s.version,
		Source:          "fresh",
		Worktrees:       items,
	}
	s.mu.Unlock()
	return nil
}

func listWorktreesFastState(mgr *Manager, repoRoot string) (daemonState, error) {
	repoRoot = absPath(repoRoot)
	cfg := normalizeDaemonConfig(mgr.Cfg)
	if !cfg.DaemonEnabled {
		items, err := mgr.ListWorktreesForRepo(repoRoot)
		if err != nil {
			return daemonState{}, err
		}
		return daemonState{RepoRoot: repoRoot, FetchedAtUnixMs: time.Now().UnixMilli(), Source: "fresh", Worktrees: items}, nil
	}
	stage := startProfileStage("daemon_get_state")
	state, err := daemonGetState(cfg, repoRoot, daemonDialTimeout)
	stage()
	if err == nil {
		staleAfter := time.Duration(cfg.DaemonStaleAfterMs) * time.Millisecond
		if staleAfter > 0 && state.FetchedAtUnixMs > 0 {
			age := time.Since(time.UnixMilli(state.FetchedAtUnixMs))
			if age > staleAfter {
				go func() {
					_, _ = daemonGetState(cfg, repoRoot, 2*time.Second)
				}()
			}
		}
		// Return stale state immediately; daemon reconciles in background.
		return state, nil
	}
	debugLogf("daemon_get_state fallback repo=%q err=%v", repoRoot, err)
	items, listErr := mgr.ListWorktreesForRepo(repoRoot)
	if listErr != nil {
		return daemonState{}, listErr
	}
	return daemonState{RepoRoot: repoRoot, FetchedAtUnixMs: time.Now().UnixMilli(), Source: "fresh", Worktrees: items}, nil
}

func listWorktreesFast(mgr *Manager, repoRoot string) ([]Worktree, error) {
	state, err := listWorktreesFastState(mgr, repoRoot)
	if err != nil {
		return nil, err
	}
	return state.Worktrees, nil
}

func daemonInfoLine(cfg Config) string {
	cfg = normalizeDaemonConfig(cfg)
	status, err := daemonStatus(cfg)
	if err != nil {
		return fmt.Sprintf("warn daemon status unknown (%v)", err)
	}
	switch status {
	case "running":
		return "ok   daemon"
	case "starting":
		return "warn daemon starting"
	default:
		return "warn daemon stopped"
	}
}

func daemonRunForeground(cfg Config) error {
	return runDaemonServer(cfg)
}

func daemonEnsureForUI(cfg Config) {
	cfg = normalizeDaemonConfig(cfg)
	if !cfg.DaemonEnabled {
		return
	}
	if err := daemonPing(cfg, daemonDialTimeout); err == nil {
		return
	}
	if err := daemonStartBackground(cfg); err != nil {
		debugLogf("ui daemon ensure failed: %v", err)
	}
}

func daemonStartBackground(cfg Config) error {
	return daemonStart(cfg)
}

func daemonStopBackground(cfg Config) error {
	return daemonStop(cfg)
}

func daemonStateString(cfg Config) (string, error) {
	return daemonStatus(cfg)
}

func daemonStateAge(cfg Config, repoRoot string) (time.Duration, error) {
	state, err := daemonGetState(cfg, repoRoot, daemonDialTimeout)
	if err != nil {
		return 0, err
	}
	if state.FetchedAtUnixMs == 0 {
		return 0, errors.New("daemon state has no timestamp")
	}
	return time.Since(time.UnixMilli(state.FetchedAtUnixMs)), nil
}

func drainAndClose(rc io.Closer) {
	if rc == nil {
		return
	}
	_ = rc.Close()
}
