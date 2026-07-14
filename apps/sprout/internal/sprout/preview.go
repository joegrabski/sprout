package sprout

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// previewServiceProbeTimeout bounds the liveness probe of a preview window's
// advertised URL. Kept short so a promote isn't noticeably slowed when the
// service is down.
const previewServiceProbeTimeout = 900 * time.Millisecond

// PreviewState records which worktree is currently designated as the preview.
// It is persisted in the git common dir so every worktree of the repo shares it.
type PreviewState struct {
	Branch           string `json:"branch"`
	Path             string `json:"path"`
	PromotedAtUnixMs int64  `json:"promotedAtUnixMs"`

	// SyncWarnings holds non-fatal messages from the preview-sync pass (e.g. a
	// tunnel that wasn't up yet). Not persisted — populated on the value
	// returned from PromotePreview/SyncPreview so callers can surface them.
	SyncWarnings []string `json:"-"`
}

// PreviewService describes one configured preview service and whether its tmux
// window is currently running.
type PreviewService struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	Running bool   `json:"running"`
}

// PreviewOptions controls a PromotePreview call.
type PreviewOptions struct {
	Target string
	Attach bool
	// Force tears the preview session down and rebuilds every service from
	// scratch instead of reconciling it in place. Used by RestartPreview.
	Force bool
}

// ensureRepoRoot returns repoRoot when set, otherwise resolves the current repo.
func (m *Manager) ensureRepoRoot(repoRoot string) (string, error) {
	if strings.TrimSpace(repoRoot) != "" {
		return repoRoot, nil
	}
	return m.RequireRepo()
}

// gitCommonDir returns the absolute path of the repo's common git dir. For a
// worktree this resolves to the main repo's .git directory, so the path is the
// same regardless of which worktree the command is run from. The result is
// memoized per repoRoot since it never changes for a given repo (the TUI hot
// path resolves it on every refresh).
func (m *Manager) gitCommonDir(repoRoot string) (string, error) {
	if cached, ok := m.commonDirCache.Load(repoRoot); ok {
		return cached.(string), nil
	}
	out, err := runCmdOutput(repoRoot, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", errors.New("unable to resolve git common dir")
	}
	m.commonDirCache.Store(repoRoot, dir)
	return dir, nil
}

func (m *Manager) previewStatePath(repoRoot string) (string, error) {
	commonDir, err := m.gitCommonDir(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "sprout", "preview.json"), nil
}

func (m *Manager) readPreviewState(repoRoot string) (*PreviewState, error) {
	path, err := m.previewStatePath(repoRoot)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st PreviewState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("invalid preview state %s: %w", path, err)
	}
	return &st, nil
}

func (m *Manager) writePreviewState(repoRoot string, st PreviewState) error {
	path, err := m.previewStatePath(repoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (m *Manager) clearPreviewState(repoRoot string) error {
	path, err := m.previewStatePath(repoRoot)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// tmuxPreviewSessionName returns the dedicated tmux session name that hosts the
// preview services, e.g. "sprout-<repo>-preview".
func (m *Manager) tmuxPreviewSessionName(repoRoot string) string {
	base := m.tmuxSessionName(repoRoot)
	suffix := safeName(strings.TrimSpace(m.Cfg.PreviewSessionSuffix))
	if suffix == "" {
		suffix = "preview"
	}
	name := fmt.Sprintf("%s-%s", base, suffix)
	if len(name) > 100 {
		return name[:100]
	}
	return name
}

// previewWindows returns a copy of the configured preview windows with the
// optional PreviewCommandPrefix applied to each non-empty pane command.
func (m *Manager) previewWindows() []WindowConfig {
	prefix := strings.TrimSpace(m.Cfg.PreviewCommandPrefix)
	if prefix == "" {
		return m.Cfg.PreviewWindows
	}
	out := make([]WindowConfig, 0, len(m.Cfg.PreviewWindows))
	for _, win := range m.Cfg.PreviewWindows {
		w := win
		w.Panes = make([]PaneConfig, len(win.Panes))
		for i, p := range win.Panes {
			np := p
			if run := strings.TrimSpace(p.Run); run != "" {
				np.Run = prefix + " " + run
			}
			w.Panes[i] = np
		}
		out = append(out, w)
	}
	return out
}

// PromotePreview makes the target worktree the preview, pointing the configured
// services at the target worktree path and recording it in the state file.
//
// When a preview session is already running it is reconciled in place rather
// than torn down: services whose resolved command and working directory are the
// same for the old and new worktree (e.g. a mobile dev server rooted at a fixed
// or shared path) keep running untouched, while services bound to the worktree
// path are recreated so they pick up the new code. Pass Force to rebuild every
// service from scratch instead.
func (m *Manager) PromotePreview(opts PreviewOptions) (PreviewState, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return PreviewState{}, err
	}
	if !commandExists("tmux") {
		return PreviewState{}, errors.New("tmux is required for preview workflows")
	}
	if len(m.Cfg.PreviewWindows) == 0 {
		return PreviewState{}, errors.New("no preview services configured; add [[preview_windows]] to .sprout.toml")
	}
	wt, err := m.FindWorktree(opts.Target)
	if err != nil {
		return PreviewState{}, err
	}
	branch := worktreeBranchOrName(wt)

	session := m.tmuxPreviewSessionName(repoRoot)
	windows := m.previewWindows()

	// The old preview path tells us which services are worktree-bound and must
	// be recreated; a nil/empty prior path forces a clean (re)launch.
	prev, err := m.readPreviewState(repoRoot)
	if err != nil {
		return PreviewState{}, err
	}
	oldPath := ""
	if prev != nil {
		oldPath = prev.Path
	}

	sessionAlive := m.tmuxHasSession(session)

	// A window is preserved (left running) when it is already alive and does not
	// need to change for the new worktree. On a plain switch that means any window
	// whose panes resolve to the same directories for the old and new worktree
	// (e.g. a tunnel agent pinned to `~`). On a forced restart we still keep
	// worktree-independent services alive — restarting them would needlessly bounce
	// long-lived tunnels and can trip agents (like ngrok) that refuse a second
	// concurrent session — but rebuild everything worktree-bound.
	var preserve func(WindowConfig) bool
	if opts.Force {
		preserve = windowIsWorktreeIndependent
	} else {
		preserve = func(win WindowConfig) bool {
			return windowResolvesIdentically(win, oldPath, wt.Path)
		}
	}

	var window string
	if len(windows) > 0 {
		window = trimWindowConfigName(windows[0], 0)
	}

	// tunnelsJustStarted drives how long the preview-sync pass waits for tunnels:
	// a freshly (re)started tunnel agent needs time to come up, but one left
	// running is already resolvable, so we can fail fast instead of blocking.
	tunnelsJustStarted := false
	if sessionAlive {
		// Reconcile in place, even on a forced restart or a first promote onto an
		// already-running session, so worktree-independent services never overlap
		// a still-alive instance of themselves.
		started, err := m.reconcilePreviewSession(session, wt.Path, windows, preserve)
		if err != nil {
			return PreviewState{}, err
		}
		tunnelsJustStarted = started
	} else {
		// Fresh session: launch every window, except a worktree-independent
		// service that's already running (e.g. an ngrok agent up from another
		// session) — reuse it instead of starting a second one.
		launchWindows := make([]WindowConfig, 0, len(windows))
		for _, win := range windows {
			if m.shouldReuseRunningService(win) {
				continue
			}
			launchWindows = append(launchWindows, win)
		}
		if len(launchWindows) == 0 {
			launchWindows = windows
		}
		_, window, err = m.tmuxLaunchWindowedSession(session, wt.Path, launchWindows)
		if err != nil {
			return PreviewState{}, err
		}
		tunnelsJustStarted = true // fresh session: every service, tunnels included, just started
	}

	st := PreviewState{
		Branch:           branch,
		Path:             wt.Path,
		PromotedAtUnixMs: time.Now().UnixMilli(),
	}
	if err := m.writePreviewState(repoRoot, st); err != nil {
		return PreviewState{}, err
	}

	if len(m.Cfg.PreviewSyncs) > 0 {
		warnings, err := m.syncPreviewConfigs(session, wt.Path, tunnelsJustStarted)
		if err != nil {
			st.SyncWarnings = append(st.SyncWarnings, err.Error())
		}
		st.SyncWarnings = append(st.SyncWarnings, warnings...)
	}

	if opts.Attach || m.Cfg.PreviewAutoAttach {
		attachOutside := os.Getenv("TMUX") == ""
		if err := m.tmuxFocusWindow(session, window, attachOutside); err != nil {
			return st, err
		}
	}
	return st, nil
}

// reconcilePreviewSession brings a live preview session in line with the desired
// windows for newPath. A window is left running only when it is currently alive
// and preserve reports it need not change; otherwise it is (re)built pointed at
// newPath. A window that still "exists" but whose pane has died (preview windows
// run with remain-on-exit) is treated as not alive and rebuilt, so a crashed
// service recovers on the next promote. Windows no longer configured are pruned.
//
// It reports whether any worktree-independent window (e.g. a tunnel agent) was
// (re)built, so the caller knows whether the tunnels need time to come up.
func (m *Manager) reconcilePreviewSession(session, newPath string, windows []WindowConfig, preserve func(WindowConfig) bool) (bool, error) {
	startedIndependent := false
	desired := make(map[string]bool, len(windows))
	for i, win := range windows {
		winName := trimWindowConfigName(win, i)
		desired[winName] = true

		if m.shouldReuseRunningService(win) {
			continue // service already running (e.g. ngrok); don't start a second
		}
		if m.previewWindowAlive(session, winName) && preserve(win) {
			continue // unchanged service: leave it running
		}
		if m.tmuxWindowExists(session, winName) {
			if err := runCmdQuiet("", "tmux", "kill-window", "-t", session+":"+winName); err != nil {
				return startedIndependent, err
			}
		}
		if err := m.buildPreviewWindow(session, winName, win, newPath); err != nil {
			return startedIndependent, err
		}
		if windowIsWorktreeIndependent(win) {
			startedIndependent = true
		}
	}

	// Prune windows that are no longer configured.
	existing, err := m.tmuxSessionWindows(session)
	if err != nil {
		return startedIndependent, err
	}
	for _, name := range existing {
		if !desired[name] {
			_ = runCmdQuiet("", "tmux", "kill-window", "-t", session+":"+name)
		}
	}
	return startedIndependent, nil
}

// previewWindowAlive reports whether a preview window exists and has at least one
// live pane. Preview windows run with remain-on-exit, so a service that crashed
// (e.g. ngrok refusing a second concurrent session) leaves a window that still
// "exists" but whose pane is dead; such a window must be rebuilt rather than
// treated as running.
func (m *Manager) previewWindowAlive(session, window string) bool {
	out, err := runCmdOutput("", "tmux", "list-panes", "-t", session+":"+window, "-F", "#{pane_dead}")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "0" {
			return true // at least one pane still running
		}
	}
	return false
}

// buildPreviewWindow creates a single preview window (and its panes) inside an
// already-running session, pointed at worktreePath.
func (m *Manager) buildPreviewWindow(session, winName string, win WindowConfig, worktreePath string) error {
	pane0Dir := worktreePath
	pane0Cmd := defaultShellCommand()
	if len(win.Panes) > 0 {
		if d := resolvePaneDir(win.Panes[0].Dir, worktreePath); d != "" {
			pane0Dir = d
		}
		if c := strings.TrimSpace(win.Panes[0].Run); c != "" {
			pane0Cmd = c
		}
	}
	if err := m.tmuxEnsureWindow(session, winName, pane0Dir, pane0Cmd); err != nil {
		return err
	}
	return m.tmuxBuildWindowPanes(session, winName, win, worktreePath)
}

// windowIsWorktreeIndependent reports whether none of a window's panes depend on
// the worktree path — i.e. it resolves to the same working directories for any two
// distinct worktrees (e.g. a tunnel agent pinned to `~` or an absolute path). Such
// a service is started once and kept alive across preview switches rather than
// bounced, which also avoids agents (like ngrok) that refuse a second concurrent
// session. A window with no panes runs a shell at the worktree root and is
// therefore worktree-bound.
func windowIsWorktreeIndependent(win WindowConfig) bool {
	return windowResolvesIdentically(win, "/sprout/__wt_a__", "/sprout/__wt_b__")
}

// shouldReuseRunningService reports whether a preview window should be left
// untouched because an instance of its service is already running. It applies
// only to worktree-independent services (e.g. an ngrok tunnel agent pinned to
// `~`): those are the same for every worktree, so a live one is reused rather
// than relaunched — which also avoids starting a second agent for tools like
// ngrok that refuse a concurrent session (ERR_NGROK_108). Worktree-bound
// services are always rebuilt so they point at the new worktree, even if the old
// one is still serving. A window with no `url` can't be probed and is not reused.
func (m *Manager) shouldReuseRunningService(win WindowConfig) bool {
	return windowIsWorktreeIndependent(win) && previewServiceLive(win.URL)
}

// previewServiceLive reports whether url is being served — any completed HTTP
// response (even an error status) means something is listening and speaking
// HTTP. TLS verification is skipped so a self-signed dev endpoint still probes
// as live, and redirects are not followed (the first response is enough).
func previewServiceLive(url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}
	client := &http.Client{
		Timeout: previewServiceProbeTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// windowResolvesIdentically reports whether a window's panes resolve to the same
// working directories for two worktree paths. Pane commands carry no {worktree}
// substitution, so only the directories can differ between worktrees. A window
// with no panes runs a shell at the worktree root and is therefore never
// considered identical across a switch.
func windowResolvesIdentically(win WindowConfig, oldPath, newPath string) bool {
	if oldPath == newPath {
		return true
	}
	if len(win.Panes) == 0 {
		return false
	}
	for _, pane := range win.Panes {
		oldDir := resolvePaneDir(pane.Dir, oldPath)
		if oldDir == "" {
			oldDir = oldPath
		}
		newDir := resolvePaneDir(pane.Dir, newPath)
		if newDir == "" {
			newDir = newPath
		}
		if oldDir != newDir {
			return false
		}
	}
	return true
}

// PreviewStatus returns the recorded preview state (nil if none) along with each
// configured service and whether its window is currently running.
func (m *Manager) PreviewStatus(repoRoot string) (*PreviewState, []PreviewService, error) {
	repoRoot, err := m.ensureRepoRoot(repoRoot)
	if err != nil {
		return nil, nil, err
	}
	st, err := m.readPreviewState(repoRoot)
	if err != nil {
		return nil, nil, err
	}

	session := m.tmuxPreviewSessionName(repoRoot)
	sessionAlive := commandExists("tmux") && m.tmuxHasSession(session)

	services := make([]PreviewService, 0, len(m.Cfg.PreviewWindows))
	for i, win := range m.Cfg.PreviewWindows {
		name := trimWindowConfigName(win, i)
		services = append(services, PreviewService{
			Name:    name,
			URL:     strings.TrimSpace(win.URL),
			Running: sessionAlive && m.tmuxWindowExists(session, name),
		})
	}
	return st, services, nil
}

// StopPreview kills the preview session and clears the recorded state.
func (m *Manager) StopPreview(repoRoot string) (bool, error) {
	repoRoot, err := m.ensureRepoRoot(repoRoot)
	if err != nil {
		return false, err
	}
	stopped := false
	session := m.tmuxPreviewSessionName(repoRoot)
	if commandExists("tmux") && m.tmuxHasSession(session) {
		if err := runCmdQuiet("", "tmux", "kill-session", "-t", session); err != nil {
			return false, err
		}
		stopped = true
	}
	if err := m.clearPreviewState(repoRoot); err != nil {
		return stopped, err
	}
	return stopped, nil
}

// RestartPreview re-promotes the worktree currently recorded as the preview,
// restarting its services from the same path.
func (m *Manager) RestartPreview(opts PreviewOptions) (PreviewState, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return PreviewState{}, err
	}
	st, err := m.readPreviewState(repoRoot)
	if err != nil {
		return PreviewState{}, err
	}
	if st == nil {
		return PreviewState{}, errors.New("no preview worktree set; run 'sprout preview <target>' first")
	}
	target := st.Path
	if target == "" {
		target = st.Branch
	}
	return m.PromotePreview(PreviewOptions{Target: target, Attach: opts.Attach, Force: true})
}

// SyncPreview re-runs the preview-sync pass against the worktree currently
// recorded as the preview, rewriting configured files with the live tunnel URLs.
// Useful after tunnels are (re)started without re-promoting.
func (m *Manager) SyncPreview(repoRoot string) (PreviewState, error) {
	repoRoot, err := m.ensureRepoRoot(repoRoot)
	if err != nil {
		return PreviewState{}, err
	}
	if len(m.Cfg.PreviewSyncs) == 0 {
		return PreviewState{}, errors.New("no preview_sync entries configured in .sprout.toml")
	}
	st, err := m.readPreviewState(repoRoot)
	if err != nil {
		return PreviewState{}, err
	}
	if st == nil || strings.TrimSpace(st.Path) == "" {
		return PreviewState{}, errors.New("no preview worktree set; run 'sprout preview <target>' first")
	}
	session := m.tmuxPreviewSessionName(repoRoot)
	// Manual sync is used right after (re)starting tunnels, so wait the full
	// timeout for them to come up rather than failing fast.
	warnings, err := m.syncPreviewConfigs(session, st.Path, true)
	if err != nil {
		return *st, err
	}
	st.SyncWarnings = warnings
	return *st, nil
}

// PreviewServiceOutput captures the recent output of a preview service window.
func (m *Manager) PreviewServiceOutput(repoRoot, service string, lines int) (string, error) {
	repoRoot, err := m.ensureRepoRoot(repoRoot)
	if err != nil {
		return "", err
	}
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for preview workflows")
	}
	session := m.tmuxPreviewSessionName(repoRoot)
	if !m.tmuxHasSession(session) {
		return "", errors.New("preview session is not running")
	}

	window := ""
	for i, win := range m.Cfg.PreviewWindows {
		name := trimWindowConfigName(win, i)
		if name == service || strings.EqualFold(strings.TrimSpace(win.Name), service) {
			window = name
			break
		}
	}
	if window == "" {
		return "", fmt.Errorf("unknown preview service: %s", service)
	}
	if !m.tmuxWindowExists(session, window) {
		return "", fmt.Errorf("preview service not running: %s", service)
	}

	if lines <= 0 {
		lines = 200
	}
	return tmuxCapturePane(session+":"+window+".0", lines, false)
}
