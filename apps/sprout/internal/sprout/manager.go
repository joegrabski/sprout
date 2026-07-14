package sprout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

var (
	ErrNotGitRepo = errors.New("run this command inside a git worktree")
	typeRe        = regexp.MustCompile(`^(feat|fix|chore|docs|refactor|test)$`)
	slugBadRe     = regexp.MustCompile(`[^a-z0-9/-]+`)
	slashRe       = regexp.MustCompile(`/+`)
	dashRe        = regexp.MustCompile(`-+`)
	safeNameRe    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

type Worktree struct {
	Path       string
	Branch     string
	Current    bool
	Dirty      bool
	TmuxState  string
	AgentState string
}

type DiffFile struct {
	Path         string
	PreviousPath string
	Status       string
}

type WorktreeDiffSnapshot struct {
	Digest string
	Files  []DiffFile
}

type NewOptions struct {
	Branch     string
	Type       string
	Name       string
	BaseBranch string
	FromBranch string
	Launch     bool
}

type DeleteProgress struct {
	Phase        string
	CurrentPath  string
	DeletedFiles int
	TotalFiles   int
	DeletedBytes int64
	TotalBytes   int64
}

// BranchInfo describes a git branch available for creating a new worktree.
type BranchInfo struct {
	Name   string
	Remote bool // true if only available as a remote-tracking branch
}

// ListBranches returns all local and remote branches not already checked out
// in an existing worktree.
func (m *Manager) ListBranches(repoRoot string) ([]BranchInfo, error) {
	inUse := map[string]bool{}
	if worktrees, err := m.ListWorktrees(); err == nil {
		for _, wt := range worktrees {
			if wt.Branch != "" {
				inUse[wt.Branch] = true
			}
		}
	}

	localOut, _ := runCmdOutput(repoRoot, "git", "branch", "--format=%(refname:short)")
	localSet := map[string]bool{}
	var result []BranchInfo
	for _, name := range strings.Split(strings.TrimSpace(localOut), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || inUse[name] {
			continue
		}
		localSet[name] = true
		result = append(result, BranchInfo{Name: name})
	}

	remoteOut, _ := runCmdOutput(repoRoot, "git", "branch", "-r", "--format=%(refname:short)")
	for _, ref := range strings.Split(strings.TrimSpace(remoteOut), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		name := ref
		if idx := strings.Index(ref, "/"); idx >= 0 {
			name = ref[idx+1:]
		}
		if strings.Contains(name, "HEAD") || localSet[name] || inUse[name] {
			continue
		}
		result = append(result, BranchInfo{Name: name, Remote: true})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

type GoOptions struct {
	Target string
	Launch bool
	Attach bool
}

type LaunchOptions struct {
	Target   string
	NoAttach bool
}

type AgentOptions struct {
	Target string
	Attach bool
}

type RemoveOptions struct {
	Target           string
	Force            bool
	DeleteBranch     bool
	OnDeleteProgress func(DeleteProgress)
	// Async removes the worktree without waiting for its files to be deleted:
	// the directory is renamed aside instantly and the bytes are reaped in a
	// background goroutine. OnDeleteProgress is ignored when Async is set.
	Async bool
}

// sproutTrashPrefix marks a directory that a previous (async) removal renamed
// aside and is (or was) reaping in the background. Any such directory left over
// after a crash is swept on the next run — see SweepDeletedTrash.
const sproutTrashPrefix = ".sprout-trash."

// trashPathFor returns a path under the worktree root (same filesystem as the
// worktrees it holds, so the rename is atomic and instant) to move a worktree to
// before background reaping. Keeping trash at the root keeps SweepDeletedTrash a
// cheap top-level scan.
func trashPathFor(worktreeRoot, worktreePath string) string {
	base := filepath.Base(worktreePath)
	return filepath.Join(worktreeRoot, fmt.Sprintf("%s%s.%d", sproutTrashPrefix, base, time.Now().UnixNano()))
}

// SweepDeletedTrash reaps any leftover async-removal trash directories under the
// repo's worktree root — e.g. from a crash or exit mid-reap. Each is removed in
// its own goroutine; the call returns immediately. Trash directories are also
// hidden from worktree listings, so a slow reap never shows up as a worktree.
func (m *Manager) SweepDeletedTrash(repoRoot string) {
	root := m.WorktreeRootDir(repoRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), sproutTrashPrefix) {
			continue
		}
		path := filepath.Join(root, e.Name())
		go func(p string) {
			if err := os.RemoveAll(p); err != nil {
				debugLogf("sweep_trash failed path=%q: %v", p, err)
			}
		}(path)
	}
}

type Manager struct {
	Cfg Config

	commonDirCache sync.Map // repoRoot -> git common dir (immutable per repo)

	// tunnelFetch resolves tunnel name -> public URL for preview-sync; nil uses
	// fetchNgrokTunnels. Overridden in tests to avoid the network.
	tunnelFetch tunnelFetcher
}

func NewManager(cfg Config) *Manager {
	return &Manager{Cfg: cfg}
}

func (m *Manager) RequireRepo() (string, error) {
	out, err := runCmdOutput("", "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNotGitRepo
	}
	return strings.TrimSpace(out), nil
}

func (m *Manager) RepoName(repoRoot string) string {
	// Try to get the common git dir to find the "real" repo name
	out, err := runCmdOutput(repoRoot, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err == nil {
		commonDir := strings.TrimSpace(out)
		// If it's a worktree, commonDir will be /path/to/mainrepo/.git
		// We want 'mainrepo'
		return filepath.Base(filepath.Dir(commonDir))
	}
	return filepath.Base(repoRoot)
}

func (m *Manager) CurrentBranch(repoRoot string) string {
	out, err := runCmdOutput(repoRoot, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (m *Manager) BranchExists(repoRoot, branch string) bool {
	_, err := runCmdOutput(repoRoot, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func (m *Manager) ResolveBaseBranch(repoRoot, requested string) (string, error) {
	if requested != "" {
		if !m.BranchExists(repoRoot, requested) {
			return "", fmt.Errorf("base branch not found: %s", requested)
		}
		return requested, nil
	}

	if m.BranchExists(repoRoot, m.Cfg.BaseBranch) {
		return m.Cfg.BaseBranch, nil
	}

	current := m.CurrentBranch(repoRoot)
	if current == "" {
		return "", fmt.Errorf("unable to infer base branch (detached HEAD and '%s' missing)", m.Cfg.BaseBranch)
	}
	return current, nil
}

func (m *Manager) Slugify(input string) (string, error) {
	slug := strings.ToLower(input)
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = slugBadRe.ReplaceAllString(slug, "-")
	slug = slashRe.ReplaceAllString(slug, "/")
	slug = dashRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-/")
	if slug == "" {
		return "", errors.New("feature name resolves to empty slug")
	}
	return slug, nil
}

func (m *Manager) MakeBranchName(branchType, name string) (string, error) {
	if !typeRe.MatchString(branchType) {
		return "", fmt.Errorf("invalid type '%s' (expected: feat|fix|chore|docs|refactor|test)", branchType)
	}
	slug, err := m.Slugify(name)
	if err != nil {
		return "", err
	}
	return branchType + "/" + slug, nil
}

func safeName(value string) string {
	s := safeNameRe.ReplaceAllString(value, "-")
	s = dashRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "default"
	}
	return s
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(abs)
}

func (m *Manager) WorktreeRootDir(repoRoot string) string {
	repoName := m.RepoName(repoRoot)
	expanded := strings.ReplaceAll(m.Cfg.WorktreeRootTemplate, "{repo}", repoName)
	if filepath.IsAbs(expanded) {
		return absPath(expanded)
	}
	return absPath(filepath.Join(repoRoot, expanded))
}

func (m *Manager) parseWorktreeList(repoRoot string) ([]Worktree, error) {
	out, err := runCmdBytes(repoRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var res []Worktree
	var curPath string
	var curBranch string

	flush := func() {
		if curPath != "" {
			res = append(res, Worktree{Path: curPath, Branch: curBranch})
		}
		curPath = ""
		curBranch = ""
	}

	for _, raw := range bytes.Split(out, []byte{'\n'}) {
		line := strings.TrimRight(string(raw), "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			curPath = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			curBranch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "branch "):
			curBranch = strings.TrimPrefix(line, "branch ")
		}
	}
	flush()
	return res, nil
}

func (m *Manager) tmuxSessionName(repoRoot string) string {
	repo := safeName(m.RepoName(repoRoot))
	prefix := safeName(m.Cfg.SessionPrefix)
	if prefix == "" {
		return repo
	}
	return fmt.Sprintf("%s-%s", prefix, repo)
}

func (m *Manager) tmuxWorktreeSessionName(repoRoot string, wt *Worktree) string {
	if wt == nil {
		return m.tmuxSessionName(repoRoot)
	}
	return m.tmuxWorktreeSessionNameFrom(repoRoot, wt.Branch, wt.Path)
}

func (m *Manager) tmuxWorktreeSessionNameFrom(repoRoot, branch, worktreePath string) string {
	base := m.tmuxSessionName(repoRoot)
	// Use worktree path as the stable tmux session token. Branch can change
	// inside the same worktree, but path remains consistent.
	token := strings.TrimSpace(worktreePath)
	if token != "" {
		token = filepath.Base(absPath(token))
	} else {
		token = strings.TrimSpace(branch)
	}
	suffix := safeName(token)
	if suffix == "" {
		return base
	}
	name := fmt.Sprintf("%s-%s", base, suffix)
	if len(name) > 100 {
		return name[:100]
	}
	return name
}

func (m *Manager) tmuxWindowName(branch string) string {
	name := safeName(branch)
	if len(name) > 60 {
		return name[:60]
	}
	return name
}

func (m *Manager) tmuxAgentWindowName(branch string) string {
	name := "agent-" + safeName(branch)
	if len(name) > 60 {
		return name[:60]
	}
	return name
}

func (m *Manager) tmuxLazygitWindowName(branch string) string {
	name := "git-" + safeName(branch)
	if len(name) > 60 {
		return name[:60]
	}
	return name
}

func (m *Manager) agentCommand() string {
	cmd := strings.TrimSpace(m.Cfg.AgentCommand)
	if cmd != "" {
		return cmd
	}
	if commandExists("codex") {
		return "codex"
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	return shell
}

func worktreeBranchOrName(wt *Worktree) string {
	branch := wt.Branch
	if branch == "" {
		branch = filepath.Base(wt.Path)
	}
	return branch
}

func commandExists(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if cached, ok := commandExistsCache.Load(name); ok {
		return cached.(bool)
	}
	_, err := exec.LookPath(name)
	ok := err == nil
	commandExistsCache.Store(name, ok)
	return ok
}

var commandExistsCache sync.Map

func (m *Manager) tmuxHasSession(session string) bool {
	_, err := runCmdOutput("", "tmux", "has-session", "-t", session)
	return err == nil
}

func (m *Manager) tmuxWindowExists(session, window string) bool {
	_, err := runCmdOutput("", "tmux", "has-session", "-t", session+":"+window)
	return err == nil
}

// tmuxSessionWindows returns the window names currently in a session.
func (m *Manager) tmuxSessionWindows(session string) ([]string, error) {
	out, err := runCmdOutput("", "tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func defaultShellCommand() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "bash"
	}
	return shell
}

func commandExecutableName(command string) string {
	parts := strings.Fields(strings.TrimSpace(command))
	if len(parts) == 0 {
		return ""
	}
	return filepath.Base(parts[0])
}

func commandShouldRemainOnExit(command string) bool {
	execName := strings.ToLower(strings.TrimSpace(commandExecutableName(command)))
	if execName == "" {
		return false
	}
	switch execName {
	case "bash", "zsh", "fish", "sh", "dash", "ksh", "csh", "tcsh":
		return false
	}
	return true
}

func shellQuoteSingle(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}

// loginInteractiveShellCommand wraps inner so it runs under the user's shell as
// a login + interactive shell. This sources the profile files that set up PATH
// and tool-manager shims (~/.zprofile + ~/.zshrc for zsh, the bash equivalents),
// so preview/worktree panes can find tools like task, ngrok, yarn, and asdf/mise
// shims. For shells whose flag syntax we don't recognise it falls back to the
// portable `sh -lc`, which only guarantees login-profile sourcing.
func loginInteractiveShellCommand(inner string) string {
	shell := defaultShellCommand()
	switch strings.ToLower(filepath.Base(shell)) {
	case "zsh", "bash":
		return shell + " -l -i -c '" + shellQuoteSingle(inner) + "'"
	default:
		return "sh -lc '" + shellQuoteSingle(inner) + "'"
	}
}

// tmuxCommandWithShellFallback runs a command and then returns to an interactive shell.
// This keeps the pane usable after short-lived processes exit. The command runs
// under the user's login+interactive shell so their environment is loaded.
func tmuxCommandWithShellFallback(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return defaultShellCommand()
	}
	if !commandShouldRemainOnExit(cmd) {
		return cmd
	}
	shell := defaultShellCommand()
	inner := cmd + "; exec " + shell + " -i"
	return loginInteractiveShellCommand(inner)
}

func (m *Manager) tmuxSetRemainOnExit(session, window string) error {
	return runCmdQuiet("", "tmux", "set-window-option", "-t", session+":"+window, "remain-on-exit", "on")
}

type tmuxWindowSpec struct {
	Name    string
	Command string
}

func trimTmuxWindowName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "main"
	}
	if len(name) > 60 {
		return name[:60]
	}
	return name
}

func nextTmuxWindowName(base string, seen map[string]struct{}) string {
	name := trimTmuxWindowName(base)
	if _, ok := seen[name]; !ok {
		seen[name] = struct{}{}
		return name
	}
	for i := 2; ; i++ {
		suffix := "-" + strconv.Itoa(i)
		prefix := name
		maxPrefixLen := 60 - len(suffix)
		if maxPrefixLen < 1 {
			maxPrefixLen = 1
		}
		if len(prefix) > maxPrefixLen {
			prefix = prefix[:maxPrefixLen]
		}
		candidate := prefix + suffix
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		return candidate
	}
}

func (m *Manager) tmuxCustomWindowName(command string) string {
	execName := safeName(commandExecutableName(command))
	if execName == "" {
		execName = "tool"
	}
	return trimTmuxWindowName("tool-" + execName)
}

func (m *Manager) tmuxConfiguredWindows(branch string, hasCommand func(string) bool) []tmuxWindowSpec {
	tools := normalizeSessionTools(m.Cfg.SessionTools)
	if len(tools) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	windows := make([]tmuxWindowSpec, 0, len(tools))
	for _, tool := range tools {
		command := ""
		windowBase := ""

		switch strings.ToLower(strings.TrimSpace(tool)) {
		case "agent":
			command = strings.TrimSpace(m.agentCommand())
			windowBase = m.tmuxAgentWindowName(branch)
		case "lazygit":
			if !hasCommand("lazygit") {
				continue
			}
			command = "lazygit -p ."
			windowBase = m.tmuxLazygitWindowName(branch)
		case "nvim", "neovim":
			if !hasCommand("nvim") {
				continue
			}
			command = "nvim ."
			windowBase = m.tmuxWindowName(branch)
		default:
			command = strings.TrimSpace(tool)
			windowBase = m.tmuxCustomWindowName(command)
		}

		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		windows = append(windows, tmuxWindowSpec{
			Name:    nextTmuxWindowName(windowBase, seen),
			Command: command,
		})
	}
	return windows
}

func trimWindowConfigName(win WindowConfig, fallbackIndex int) string {
	name := trimTmuxWindowName(win.Name)
	if name == "" {
		name = fmt.Sprintf("window-%d", fallbackIndex+1)
	}
	return name
}

func (m *Manager) configuredAgentWindowName() string {
	for i, win := range m.Cfg.Windows {
		if strings.EqualFold(strings.TrimSpace(win.Role), "agent") {
			return trimWindowConfigName(win, i)
		}
	}
	return ""
}

func (m *Manager) tmuxEnsureSession(session, repoRoot, initialWindow, initialCommand string) error {
	if m.tmuxHasSession(session) {
		return nil
	}
	window := strings.TrimSpace(initialWindow)
	if window == "" {
		window = "main"
	}
	command := strings.TrimSpace(initialCommand)
	if command == "" {
		command = defaultShellCommand()
	}
	runCommand := tmuxCommandWithShellFallback(command)
	if err := runCmdQuiet("", "tmux", "new-session", "-d", "-s", session, "-n", window, "-c", repoRoot, runCommand); err != nil {
		return err
	}
	if commandShouldRemainOnExit(command) {
		return m.tmuxSetRemainOnExit(session, window)
	}
	return nil
}

func (m *Manager) tmuxEnsureWindow(session, window, worktreePath, command string) error {
	if m.tmuxWindowExists(session, window) {
		return nil
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		cmd = defaultShellCommand()
	}
	runCommand := tmuxCommandWithShellFallback(cmd)
	if err := runCmdQuiet("", "tmux", "new-window", "-d", "-t", session, "-n", window, "-c", worktreePath, runCommand); err != nil {
		return err
	}
	if commandShouldRemainOnExit(cmd) {
		return m.tmuxSetRemainOnExit(session, window)
	}
	return nil
}

func (m *Manager) tmuxFocusWindow(session, window string, attachOutside bool) error {
	if err := runCmdQuiet("", "tmux", "select-window", "-t", session+":"+window); err != nil {
		return err
	}

	if os.Getenv("TMUX") != "" {
		return runCmdQuiet("", "tmux", "switch-client", "-t", session)
	}

	if attachOutside {
		return runCmdInherit("", "tmux", "attach-session", "-t", session)
	}
	return nil
}

func (m *Manager) tmuxFocusSession(session string, attachOutside bool) error {
	if os.Getenv("TMUX") != "" {
		return runCmdQuiet("", "tmux", "switch-client", "-t", session)
	}
	if attachOutside {
		return runCmdInherit("", "tmux", "attach-session", "-t", session)
	}
	return nil
}

// resolvePaneDir resolves a pane dir spec to an absolute path.
// Returns "" when dir is empty (caller should use the worktree path as default).
//   - "~" or "~/..." → expands to home directory
//   - "{worktree}" prefix → replaced with worktreePath
//   - Absolute path → returned as-is
//   - Relative path → resolved against worktreePath
func resolvePaneDir(dir, worktreePath string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if dir == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return dir
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, dir[2:])
		}
		return dir
	}
	if strings.HasPrefix(dir, "{worktree}") {
		rest := strings.TrimPrefix(dir, "{worktree}")
		if rest == "" {
			return worktreePath
		}
		return filepath.Join(worktreePath, rest)
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Clean(filepath.Join(worktreePath, dir))
}

// tmuxSplitFlag returns the tmux split-window flag for a given layout name.
// Horizontal layouts use -h (split left/right); everything else uses -v.
func tmuxSplitFlag(layout string) string {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case "even-horizontal", "main-vertical":
		return "-h"
	default:
		return "-v"
	}
}

// tmuxLaunchWindowedSession creates (or attaches to) a tmux session built from
// a structured []WindowConfig. It is idempotent: if the session already exists
// all ensure calls are no-ops and pane splitting is skipped.
func (m *Manager) tmuxLaunchWindowedSession(session, worktreePath string, windows []WindowConfig) (string, string, error) {
	sessionIsNew := !m.tmuxHasSession(session)

	for i, win := range windows {
		winName := trimTmuxWindowName(win.Name)
		if winName == "" {
			winName = fmt.Sprintf("window-%d", i+1)
		}

		// Resolve pane 0's dir and command.
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

		if i == 0 && sessionIsNew {
			if err := m.tmuxEnsureSession(session, pane0Dir, winName, pane0Cmd); err != nil {
				return "", "", err
			}
		} else {
			if err := m.tmuxEnsureWindow(session, winName, pane0Dir, pane0Cmd); err != nil {
				return "", "", err
			}
		}

		if !sessionIsNew {
			continue // don't re-split panes in an existing session
		}

		if err := m.tmuxBuildWindowPanes(session, winName, win, worktreePath); err != nil {
			return "", "", err
		}
	}

	firstWin := ""
	if len(windows) > 0 {
		firstWin = trimTmuxWindowName(windows[0].Name)
		if firstWin == "" {
			firstWin = "window-1"
		}
	}
	return session, firstWin, nil
}

// tmuxBuildWindowPanes splits panes 1..n into an existing window (pane 0 is
// created together with the window) and applies the configured tmux layout.
func (m *Manager) tmuxBuildWindowPanes(session, winName string, win WindowConfig, worktreePath string) error {
	splitFlag := tmuxSplitFlag(win.Layout)
	for j, pane := range win.Panes {
		if j == 0 {
			continue // pane 0 was created with the window/session
		}
		paneDir := worktreePath
		if d := resolvePaneDir(pane.Dir, worktreePath); d != "" {
			paneDir = d
		}
		args := []string{"split-window", splitFlag, "-t", session + ":" + winName, "-c", paneDir}
		if pane.Run != "" {
			args = append(args, tmuxCommandWithShellFallback(pane.Run))
		}
		if err := runCmdQuiet("", "tmux", args...); err != nil {
			return err
		}
	}

	// Apply the tmux layout. Default to even-horizontal when multiple panes
	// are defined but no explicit layout is set.
	layout := win.Layout
	if layout == "" && len(win.Panes) > 1 {
		layout = "even-horizontal"
	}
	if layout != "" && len(win.Panes) > 1 {
		_ = runCmdQuiet("", "tmux", "select-layout", "-t", session+":"+winName, layout)
	}
	return nil
}

func (m *Manager) tmuxEnsureWorktreeWindow(repoRoot, branch, worktreePath string) (string, string, error) {
	session := m.tmuxWorktreeSessionNameFrom(repoRoot, branch, worktreePath)

	// Priority 1: structured [[windows]] config
	if len(m.Cfg.Windows) > 0 {
		return m.tmuxLaunchWindowedSession(session, worktreePath, m.Cfg.Windows)
	}

	// Priority 2: legacy flat layout_* config
	repoName := m.RepoName(repoRoot)
	if layout, ok := m.Cfg.SessionLayouts[repoName]; ok {
		if len(layout.Windows) > 0 {
			for i, win := range layout.Windows {
				winName := trimTmuxWindowName(win.Name)
				if i == 0 && !m.tmuxHasSession(session) {
					// Use first pane of first window for session creation
					initialCmd := defaultShellCommand()
					if len(win.Panes) > 0 {
						initialCmd = win.Panes[0].Command
					}
					if err := m.tmuxEnsureSession(session, worktreePath, winName, initialCmd); err != nil {
						return "", "", err
					}
				}

				if err := m.tmuxEnsureWindow(session, winName, worktreePath, ""); err != nil {
					return "", "", err
				}

				// Create panes
				for j, pane := range win.Panes {
					if i == 0 && j == 0 && !m.tmuxHasSession(session) {
						continue // already created as initial session pane
					}
					if j == 0 {
						// The window itself is the first pane
						if pane.Command != "" {
							_ = tmuxSendPaneCommand(session+":"+winName+".0", pane.Command)
						}
						continue
					}
					// Split window for subsequent panes
					args := []string{"split-window", "-v", "-t", session + ":" + winName, "-c", worktreePath}
					if pane.Command != "" {
						args = append(args, tmuxCommandWithShellFallback(pane.Command))
					}
					if err := runCmdQuiet("", "tmux", args...); err != nil {
						return "", "", err
					}
				}
				// Equalize panes
				_ = runCmdQuiet("", "tmux", "select-layout", "-t", session+":"+winName, "even-vertical")
			}
			return session, trimTmuxWindowName(layout.Windows[0].Name), nil
		}
	}

	// Default tool-based layout
	windows := m.tmuxConfiguredWindows(branch, commandExists)
	if len(windows) == 0 {
		windows = []tmuxWindowSpec{{
			Name:    m.tmuxWindowName(branch),
			Command: defaultShellCommand(),
		}}
	}

	initial := windows[0]
	if !m.tmuxHasSession(session) {
		if err := m.tmuxEnsureSession(session, worktreePath, initial.Name, initial.Command); err != nil {
			return "", "", err
		}
	}
	for _, window := range windows {
		if err := m.tmuxEnsureWindow(session, window.Name, worktreePath, window.Command); err != nil {
			return "", "", err
		}
	}
	return session, initial.Name, nil
}

func (m *Manager) LaunchOrFocus(repoRoot, branch, worktreePath string, attachOutside bool) error {
	if !commandExists("tmux") {
		return errors.New("tmux is required for launch/go workflows")
	}
	session, window, err := m.tmuxEnsureWorktreeWindow(repoRoot, branch, worktreePath)
	if err != nil {
		return err
	}
	return m.tmuxFocusWindow(session, window, attachOutside)
}

func (m *Manager) ListWorktrees() ([]Worktree, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return nil, err
	}
	return m.ListWorktreesForRepo(repoRoot)
}

func (m *Manager) ListWorktreesForRepo(repoRoot string) ([]Worktree, error) {
	items, err := m.parseWorktreeList(repoRoot)
	if err != nil {
		return nil, err
	}
	current := absPath(repoRoot)
	for i := range items {
		items[i].Path = absPath(items[i].Path)
		items[i].Current = items[i].Path == current
	}

	hasTmux := commandExists("tmux")
	tmuxSessions, tmuxWindows := map[string]struct{}{}, map[string]map[string]struct{}{}
	tmuxSnapshotOK := false
	if hasTmux {
		if sessions, windows, snapErr := tmuxSessionSnapshot(); snapErr == nil {
			tmuxSessions = sessions
			tmuxWindows = windows
			tmuxSnapshotOK = true
		}
	}

	if len(items) <= 2 {
		for i := range items {
			items[i].Dirty = m.WorktreeDirty(items[i].Path)
		}
	} else {
		workers := runtime.GOMAXPROCS(0)
		if workers < 2 {
			workers = 2
		}
		if workers > len(items) {
			workers = len(items)
		}
		if workers > 4 {
			workers = 4
		}
		if workers < 1 {
			workers = 1
		}
		jobs := make(chan int, len(items))
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					items[idx].Dirty = m.WorktreeDirty(items[idx].Path)
				}
			}()
		}
		for i := range items {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}

	for i := range items {
		items[i].TmuxState = "n/a"
		items[i].AgentState = "n/a"
		if !hasTmux {
			continue
		}

		items[i].TmuxState = "no"
		items[i].AgentState = "no"
		session := m.tmuxWorktreeSessionName(repoRoot, &items[i])
		if _, ok := tmuxSessions[session]; ok {
			items[i].TmuxState = "yes"
			agentWindow := m.configuredAgentWindowName()
			if agentWindow == "" {
				agentWindow = m.tmuxAgentWindowName(worktreeBranchOrName(&items[i]))
			}
			if windows, ok := tmuxWindows[session]; ok {
				if _, hasAgentWindow := windows[agentWindow]; hasAgentWindow {
					items[i].AgentState = "yes"
				}
				if items[i].AgentState != "yes" {
					if _, ok := m.findAgentWindowInSession(session); ok {
						items[i].AgentState = "yes"
					}
				}
				for window := range windows {
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(window)), "agent") {
						items[i].AgentState = "yes"
						break
					}
				}
			}
			if !tmuxSnapshotOK && items[i].AgentState != "yes" {
				if m.tmuxWindowExists(session, agentWindow) {
					items[i].AgentState = "yes"
				} else if _, ok := m.findAgentPaneInSession(session); ok {
					items[i].AgentState = "yes"
				}
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Current {
			return true
		}
		if items[j].Current {
			return false
		}
		return items[i].Path < items[j].Path
	})

	return items, nil
}

func tmuxSessionSnapshot() (map[string]struct{}, map[string]map[string]struct{}, error) {
	out, err := runCmdBytes("", "tmux", "list-windows", "-a", "-F", "#{session_name}\t#{window_name}")
	if err != nil {
		return nil, nil, err
	}
	sessions := map[string]struct{}{}
	windows := map[string]map[string]struct{}{}
	for _, raw := range bytes.Split(out, []byte{'\n'}) {
		line := string(raw)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		session := strings.TrimSpace(parts[0])
		window := strings.TrimSpace(parts[1])
		if session == "" {
			continue
		}
		sessions[session] = struct{}{}
		if windows[session] == nil {
			windows[session] = map[string]struct{}{}
		}
		if window != "" {
			windows[session][window] = struct{}{}
		}
	}
	return sessions, windows, nil
}

func (m *Manager) paneLogDir(repoRoot string) string {
	return filepath.Join(os.TempDir(), "sprout-panes", safeName(m.tmuxSessionName(repoRoot)))
}

func (m *Manager) paneStreamLogPath(repoRoot, paneTarget string) string {
	paneID := safeName(strings.TrimPrefix(strings.TrimSpace(paneTarget), "%"))
	if paneID == "" {
		paneID = "pane"
	}
	return filepath.Join(m.paneLogDir(repoRoot), fmt.Sprintf("pane-%s.log", paneID))
}

func (m *Manager) ensurePaneStream(repoRoot, paneTarget string, seedLines int, reset bool) (string, error) {
	logPath := m.paneStreamLogPath(repoRoot, paneTarget)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", err
	}
	if reset {
		snapshot, err := tmuxCapturePane(paneTarget, seedLines, false)
		if err != nil {
			snapshot = "agent pane unavailable"
		}
		if !strings.HasSuffix(snapshot, "\n") {
			snapshot += "\n"
		}
		if err := os.WriteFile(logPath, []byte(snapshot), 0o644); err != nil {
			return "", err
		}
	} else {
		if _, err := os.Stat(logPath); errors.Is(err, os.ErrNotExist) {
			snapshot, snapErr := tmuxCapturePane(paneTarget, seedLines, false)
			if snapErr != nil {
				snapshot = "agent pane unavailable"
			}
			if !strings.HasSuffix(snapshot, "\n") {
				snapshot += "\n"
			}
			if err := os.WriteFile(logPath, []byte(snapshot), 0o644); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
	}
	_ = runCmdQuiet("", "tmux", "pipe-pane", "-t", paneTarget)
	pipeCommand := "cat >> '" + shellQuoteSingle(logPath) + "'"
	if err := runCmdQuiet("", "tmux", "pipe-pane", "-t", paneTarget, pipeCommand); err != nil {
		return "", err
	}
	return logPath, nil
}

func (m *Manager) FindWorktree(target string) (*Worktree, error) {
	items, err := m.ListWorktrees()
	if err != nil {
		return nil, err
	}

	targetAbs := ""
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		targetAbs = absPath(target)
	}

	for i := range items {
		if target == items[i].Branch || target == items[i].Path || targetAbs == items[i].Path || target == filepath.Base(items[i].Path) {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("worktree not found for target: %s", target)
}

func (m *Manager) findWorktreeLite(repoRoot, target string) (*Worktree, error) {
	items, err := m.parseWorktreeList(repoRoot)
	if err != nil {
		return nil, err
	}

	targetAbs := ""
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		targetAbs = absPath(target)
	}

	for i := range items {
		items[i].Path = absPath(items[i].Path)
		if target == items[i].Branch || target == items[i].Path || targetAbs == items[i].Path || target == filepath.Base(items[i].Path) {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("worktree not found for target: %s", target)
}

func (m *Manager) BranchCheckedOutAnywhere(branch string) bool {
	items, err := m.ListWorktrees()
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.Branch == branch {
			return true
		}
	}
	return false
}

func (m *Manager) WorktreeDirty(path string) bool {
	out, err := runCmdBytes(path, "git", "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return false
	}
	return len(out) != 0
}

func (m *Manager) WorktreeDiff(path string, width int) (string, error) {
	_ = width
	status, err := runCmdOutput(path, "git", "--no-pager", "status", "--short")
	if err != nil {
		return "", err
	}
	staged, err := runCmdOutput(path, "git", "--no-pager", "diff", "--cached", "--color=always", "--no-ext-diff")
	if err != nil {
		return "", err
	}
	unstaged, err := runCmdOutput(path, "git", "--no-pager", "diff", "--color=always", "--no-ext-diff")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if strings.TrimSpace(status) != "" {
		b.WriteString("\x1b[36m# Status\x1b[0m\n")
		b.WriteString(status)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(staged) != "" {
		b.WriteString("\x1b[36m# Staged\x1b[0m\n")
		b.WriteString(staged)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(unstaged) != "" {
		b.WriteString("\x1b[36m# Unstaged\x1b[0m\n")
		b.WriteString(unstaged)
	}
	return strings.TrimSpace(b.String()), nil
}

func (m *Manager) WorktreeDiffFiles(path string) ([]DiffFile, error) {
	snapshot, err := m.WorktreeDiffSnapshotContext(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return snapshot.Files, nil
}

func (m *Manager) WorktreeDiffForFile(path string, file DiffFile, width int) (string, error) {
	return m.WorktreeDiffForFileContext(context.Background(), path, file, width)
}

func (m *Manager) WorktreeDiffSnapshotContext(ctx context.Context, path string) (WorktreeDiffSnapshot, error) {
	out, err := runCmdBytesContext(path, ctx, "git", "--no-pager", "status", "--porcelain=v2", "--untracked-files=all", "-z")
	if err != nil {
		return WorktreeDiffSnapshot{}, err
	}
	return WorktreeDiffSnapshot{
		Digest: string(out),
		Files:  parsePorcelainV2DiffFiles(out),
	}, nil
}

func (m *Manager) WorktreeDiffForFileContext(ctx context.Context, path string, file DiffFile, width int) (string, error) {
	_ = width
	statusRaw := file.Status
	stageState, workState := parsePorcelainStatus(statusRaw)
	statusLabel := strings.TrimSpace(statusRaw)

	staged := ""
	unstaged := ""

	needsStaged := stageState != ' ' && stageState != '?'
	needsUnstaged := workState != ' ' && workState != '?'

	isUntracked := stageState == '?' && workState == '?'
	if isUntracked {
		out, err := runCmdOutputContextAllowExitCodes(path, ctx, []int{1}, "git", "--no-pager", "diff", "--no-index", "--color=always", "--no-ext-diff", "--", "/dev/null", file.Path)
		if err != nil {
			return "", err
		}
		unstaged = out
	} else {
		var stagedErr error
		var unstagedErr error
		var wg sync.WaitGroup
		if needsStaged {
			wg.Add(1)
			go func() {
				defer wg.Done()
				staged, stagedErr = runCmdOutputContext(path, ctx, "git", "--no-pager", "diff", "--cached", "--color=always", "--no-ext-diff", "--", file.Path)
			}()
		}
		if needsUnstaged {
			wg.Add(1)
			go func() {
				defer wg.Done()
				unstaged, unstagedErr = runCmdOutputContext(path, ctx, "git", "--no-pager", "diff", "--color=always", "--no-ext-diff", "--", file.Path)
			}()
		}
		wg.Wait()
		if stagedErr != nil {
			return "", stagedErr
		}
		if unstagedErr != nil {
			return "", unstagedErr
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\x1b[36m# %s\x1b[0m", file.Path))
	if statusLabel != "" {
		b.WriteString(fmt.Sprintf(" \x1b[36m(%s)\x1b[0m", statusLabel))
	}
	if file.PreviousPath != "" {
		b.WriteString(fmt.Sprintf(" \x1b[36m(from %s)\x1b[0m", file.PreviousPath))
	}
	b.WriteString("\n\n")

	if strings.TrimSpace(staged) != "" {
		b.WriteString("\x1b[36m# Staged\x1b[0m\n")
		b.WriteString(staged)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(unstaged) != "" {
		b.WriteString("\x1b[36m# Unstaged\x1b[0m\n")
		b.WriteString(unstaged)
	}
	if strings.TrimSpace(staged) == "" && strings.TrimSpace(unstaged) == "" {
		if stageState == '?' && workState == '?' {
			b.WriteString("(untracked file: stage it to view a patch)")
		} else {
			b.WriteString("(no textual diff available for this file)")
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func parsePorcelainV2DiffFiles(out []byte) []DiffFile {
	parts := bytes.Split(out, []byte{0})
	files := make([]DiffFile, 0, len(parts))
	seen := map[string]struct{}{}
	for i := 0; i < len(parts); i++ {
		part := bytes.TrimSpace(parts[i])
		if len(part) == 0 {
			continue
		}
		switch part[0] {
		case '1':
			fields := strings.Fields(string(part))
			if len(fields) < 9 {
				continue
			}
			file := fields[8]
			if _, ok := seen[file]; ok {
				continue
			}
			seen[file] = struct{}{}
			files = append(files, DiffFile{Path: file, Status: fields[1]})
		case '2':
			fields := strings.Fields(string(part))
			if len(fields) < 10 || i+1 >= len(parts) {
				continue
			}
			file := fields[9]
			previous := strings.TrimSpace(string(parts[i+1]))
			i++
			if _, ok := seen[file]; ok {
				continue
			}
			seen[file] = struct{}{}
			files = append(files, DiffFile{Path: file, PreviousPath: previous, Status: fields[1]})
		case 'u':
			fields := strings.Fields(string(part))
			if len(fields) < 11 {
				continue
			}
			file := fields[10]
			if _, ok := seen[file]; ok {
				continue
			}
			seen[file] = struct{}{}
			files = append(files, DiffFile{Path: file, Status: fields[1]})
		case '?':
			file := strings.TrimSpace(strings.TrimPrefix(string(part), "?"))
			if file == "" {
				continue
			}
			if _, ok := seen[file]; ok {
				continue
			}
			seen[file] = struct{}{}
			files = append(files, DiffFile{Path: file, Status: "??"})
		}
	}
	return files
}

func parsePorcelainStatus(status string) (rune, rune) {
	runes := []rune(status)
	stageState := ' '
	workState := ' '
	if len(runes) > 0 {
		stageState = runes[0]
	}
	if len(runes) > 1 {
		workState = runes[1]
	}
	return stageState, workState
}

func (m *Manager) CreateWorktreeWithBranch(repoRoot, branch, worktreePath, baseBranch string) error {
	if m.BranchExists(repoRoot, branch) {
		return fmt.Errorf("branch already exists: %s", branch)
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("target path already exists: %s", worktreePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	return m.runGitWorktreeAdd(repoRoot, "-b", branch, worktreePath, baseBranch)
}

func (m *Manager) CreateWorktreeFromExisting(repoRoot, branch, worktreePath string) error {
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("target path already exists: %s", worktreePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}
	// Try local branch first, then remote
	if m.BranchExists(repoRoot, branch) {
		return m.runGitWorktreeAdd(repoRoot, worktreePath, branch)
	}
	// If it doesn't exist locally, git might still find it in remotes if --guess-remote is on (default)
	return m.runGitWorktreeAdd(repoRoot, worktreePath, branch)
}

func (m *Manager) findExistingWorktreePath(repoRoot, branch, desiredPath string) (string, bool, error) {
	items, err := m.parseWorktreeList(repoRoot)
	if err != nil {
		return "", false, err
	}
	desiredAbs := absPath(desiredPath)
	branch = strings.TrimSpace(branch)
	branchMatch := ""
	for _, wt := range items {
		path := absPath(wt.Path)
		if desiredAbs != "" && path == desiredAbs {
			return path, true, nil
		}
		if branch != "" && strings.TrimSpace(wt.Branch) == branch {
			branchMatch = path
		}
	}
	if branchMatch != "" {
		return branchMatch, true, nil
	}
	return "", false, nil
}

func (m *Manager) NewWorktree(opts NewOptions) (string, string, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		debugLogf("new_worktree require_repo failed: %v", err)
		return "", "", err
	}

	branch := strings.TrimSpace(opts.Branch)
	isExisting := opts.FromBranch != ""
	if isExisting {
		branch = opts.FromBranch
	}

	if branch == "" {
		branch, err = m.MakeBranchName(opts.Type, opts.Name)
		if err != nil {
			debugLogf("new_worktree make_branch failed type=%q name=%q: %v", opts.Type, opts.Name, err)
			return "", "", err
		}
	}
	debugLogf("new_worktree start repo=%q branch=%q launch=%t existing=%t", repoRoot, branch, opts.Launch, isExisting)

	worktreeRoot := m.WorktreeRootDir(repoRoot)
	worktreePath := absPath(filepath.Join(worktreeRoot, branch))
	if existingPath, exists, findErr := m.findExistingWorktreePath(repoRoot, branch, worktreePath); findErr == nil && exists {
		debugLogf("new_worktree existing_worktree_detected branch=%q requested_path=%q existing_path=%q", branch, worktreePath, existingPath)
		return branch, existingPath, nil
	}

	if isExisting {
		if err := m.CreateWorktreeFromExisting(repoRoot, branch, worktreePath); err != nil {
			if existingPath, exists, findErr := m.findExistingWorktreePath(repoRoot, branch, worktreePath); findErr == nil && exists {
				debugLogf("new_worktree existing_worktree_after_create_error branch=%q requested_path=%q existing_path=%q err=%v", branch, worktreePath, existingPath, err)
				return branch, existingPath, nil
			}
			debugLogf("new_worktree create_worktree_from_existing failed branch=%q path=%q: %v", branch, worktreePath, err)
			return "", "", err
		}
	} else {
		base, err := m.ResolveBaseBranch(repoRoot, opts.BaseBranch)
		if err != nil {
			debugLogf("new_worktree resolve_base failed branch=%q requested_base=%q: %v", branch, opts.BaseBranch, err)
			return "", "", err
		}

		if err := m.CreateWorktreeWithBranch(repoRoot, branch, worktreePath, base); err != nil {
			if existingPath, exists, findErr := m.findExistingWorktreePath(repoRoot, branch, worktreePath); findErr == nil && exists {
				debugLogf("new_worktree existing_worktree_after_create_error branch=%q requested_path=%q existing_path=%q err=%v", branch, worktreePath, existingPath, err)
				return branch, existingPath, nil
			}
			debugLogf("new_worktree create_worktree failed branch=%q path=%q base=%q: %v", branch, worktreePath, base, err)
			return "", "", err
		}
	}

	debugLogf("new_worktree created branch=%q path=%q", branch, worktreePath)

	if opts.Launch {
		if err := m.LaunchOrFocus(repoRoot, branch, worktreePath, true); err != nil {
			debugLogf("new_worktree launch_failed path=%q: %v", worktreePath, err)
			return "", "", err
		}
	}
	debugLogf("new_worktree success branch=%q path=%q", branch, worktreePath)

	return branch, worktreePath, nil
}

func (m *Manager) Path(target string) (string, error) {
	wt, err := m.FindWorktree(target)
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) Go(opts GoOptions) (string, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return "", err
	}
	wt, err := m.FindWorktree(opts.Target)
	if err != nil {
		return "", err
	}

	branch := wt.Branch
	if branch == "" {
		branch = filepath.Base(wt.Path)
	}

	if opts.Launch && commandExists("tmux") {
		attachOutside := false
		if os.Getenv("TMUX") == "" {
			attachOutside = opts.Attach
		}
		session := m.tmuxWorktreeSessionNameFrom(repoRoot, branch, wt.Path)
		if m.tmuxHasSession(session) {
			if err := m.tmuxFocusSession(session, attachOutside); err != nil {
				return "", err
			}
		} else {
			if err := m.LaunchOrFocus(repoRoot, branch, wt.Path, attachOutside); err != nil {
				return "", err
			}
		}
	}

	return wt.Path, nil
}

func (m *Manager) Launch(opts LaunchOptions) (string, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		debugLogf("launch require_repo failed target=%q: %v", opts.Target, err)
		return "", err
	}
	wt, err := m.FindWorktree(opts.Target)
	if err != nil {
		debugLogf("launch find_worktree failed target=%q: %v", opts.Target, err)
		return "", err
	}

	attach := !opts.NoAttach
	if os.Getenv("TMUX") != "" {
		attach = false
	}
	branch := worktreeBranchOrName(wt)
	debugLogf("launch start target=%q path=%q branch=%q no_attach=%t", opts.Target, wt.Path, branch, opts.NoAttach)

	session, window, err := m.tmuxEnsureWorktreeWindow(repoRoot, branch, wt.Path)
	if err != nil {
		debugLogf("launch ensure_window failed path=%q branch=%q: %v", wt.Path, branch, err)
		return "", err
	}
	if attach {
		if err := m.tmuxFocusWindow(session, window, true); err != nil {
			debugLogf("launch focus failed session=%q window=%q: %v", session, window, err)
			return "", err
		}
	}
	debugLogf("launch success path=%q session=%q window=%q attach=%t", wt.Path, session, window, attach)
	return wt.Path, nil
}

func (m *Manager) Detach(target string) (string, bool, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return "", false, err
	}
	wt, err := m.FindWorktree(target)
	if err != nil {
		return "", false, err
	}
	if !commandExists("tmux") {
		return "", false, errors.New("tmux is required for detach workflows")
	}

	session := m.tmuxWorktreeSessionName(repoRoot, wt)
	if !m.tmuxHasSession(session) {
		return wt.Path, false, nil
	}
	if err := runCmdQuiet("", "tmux", "kill-session", "-t", session); err != nil {
		return "", false, err
	}
	return wt.Path, true, nil
}

func (m *Manager) StartAgent(opts AgentOptions) (string, bool, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		debugLogf("start_agent require_repo failed target=%q: %v", opts.Target, err)
		return "", false, err
	}
	wt, err := m.FindWorktree(opts.Target)
	if err != nil {
		debugLogf("start_agent find_worktree failed target=%q: %v", opts.Target, err)
		return "", false, err
	}
	if !commandExists("tmux") {
		debugLogf("start_agent tmux_missing target=%q", opts.Target)
		return "", false, errors.New("tmux is required for agent workflows")
	}

	branch := worktreeBranchOrName(wt)
	session := m.tmuxWorktreeSessionNameFrom(repoRoot, branch, wt.Path)
	agentWindow := m.configuredAgentWindowName()
	if agentWindow == "" {
		agentWindow = m.tmuxAgentWindowName(branch)
	}
	alreadyRunning := m.tmuxHasSession(session) && m.tmuxWindowExists(session, agentWindow)

	_, _, err = m.tmuxEnsureWorktreeWindow(repoRoot, branch, wt.Path)
	if err != nil {
		debugLogf("start_agent ensure_worktree_window failed path=%q branch=%q: %v", wt.Path, branch, err)
		return "", false, err
	}
	if m.configuredAgentWindowName() != "" {
		debugLogf("start_agent using configured agent window path=%q session=%q window=%q attach=%t already_running=%t", wt.Path, session, agentWindow, opts.Attach, alreadyRunning)
		if opts.Attach {
			attachOutside := os.Getenv("TMUX") == ""
			if err := m.tmuxFocusWindow(session, agentWindow, attachOutside); err != nil {
				debugLogf("start_agent focus configured window failed session=%q window=%q: %v", session, agentWindow, err)
				return "", alreadyRunning, err
			}
		}
		return wt.Path, alreadyRunning, nil
	}
	if err := m.tmuxEnsureWindow(session, agentWindow, wt.Path, m.agentCommand()); err != nil {
		debugLogf("start_agent ensure_agent_window failed path=%q branch=%q window=%q: %v", wt.Path, branch, agentWindow, err)
		return "", alreadyRunning, err
	}
	debugLogf("start_agent start path=%q session=%q window=%q attach=%t already_running=%t", wt.Path, session, agentWindow, opts.Attach, alreadyRunning)

	if opts.Attach {
		attachOutside := os.Getenv("TMUX") == ""
		if err := m.tmuxFocusWindow(session, agentWindow, attachOutside); err != nil {
			debugLogf("start_agent focus failed session=%q window=%q: %v", session, agentWindow, err)
			return "", alreadyRunning, err
		}
	}

	debugLogf("start_agent success path=%q session=%q window=%q already_running=%t", wt.Path, session, agentWindow, alreadyRunning)
	return wt.Path, alreadyRunning, nil
}

func (m *Manager) AttachAgent(target string) (string, error) {
	path, _, err := m.StartAgent(AgentOptions{Target: target, Attach: true})
	return path, err
}

func (m *Manager) StopAgent(target string) (string, bool, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return "", false, err
	}
	wt, err := m.FindWorktree(target)
	if err != nil {
		return "", false, err
	}
	if !commandExists("tmux") {
		return "", false, errors.New("tmux is required for agent workflows")
	}

	session := m.tmuxWorktreeSessionName(repoRoot, wt)
	agentWindow := m.configuredAgentWindowName()
	if agentWindow == "" {
		agentWindow = m.tmuxAgentWindowName(worktreeBranchOrName(wt))
	}
	if !m.tmuxHasSession(session) || !m.tmuxWindowExists(session, agentWindow) {
		return wt.Path, false, nil
	}
	if err := runCmdQuiet("", "tmux", "kill-window", "-t", session+":"+agentWindow); err != nil {
		return "", false, err
	}
	return wt.Path, true, nil
}

func (m *Manager) resolveWorktreeForTmux(target string) (string, *Worktree, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return "", nil, err
	}
	wt, err := m.findWorktreeLite(repoRoot, target)
	if err != nil {
		return "", nil, err
	}
	return repoRoot, wt, nil
}

func (m *Manager) agentPaneTarget(repoRoot string, wt *Worktree) string {
	session := m.tmuxWorktreeSessionName(repoRoot, wt)
	window := m.configuredAgentWindowName()
	if window == "" {
		window = m.tmuxAgentWindowName(worktreeBranchOrName(wt))
	}
	if m.tmuxHasSession(session) {
		if m.tmuxWindowExists(session, window) {
			if target, ok := m.findAgentPaneInWindow(session, window); ok {
				return target
			}
		}
		if target, ok := m.findAgentPaneInSession(session); ok {
			return target
		}
	}
	return session + ":" + window + ".0"
}

func (m *Manager) editorPaneTarget(repoRoot string, wt *Worktree) string {
	session := m.tmuxWorktreeSessionName(repoRoot, wt)
	window := m.tmuxWindowName(worktreeBranchOrName(wt))
	return session + ":" + window + ".0"
}

func (m *Manager) lazygitPaneTarget(repoRoot string, wt *Worktree) (string, error) {
	session := m.tmuxWorktreeSessionName(repoRoot, wt)
	window := m.tmuxLazygitWindowName(worktreeBranchOrName(wt))
	if !m.tmuxHasSession(session) || !m.tmuxWindowExists(session, window) {
		return "", errors.New("lazygit pane is not available in this tmux window")
	}
	return session + ":" + window + ".0", nil
}

func (m *Manager) agentOutputForWorktree(repoRoot string, wt *Worktree, lines int) (string, error) {
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for agent workflows")
	}
	return tmuxCapturePane(m.agentPaneTarget(repoRoot, wt), lines, false)
}

func (m *Manager) lazygitOutputForWorktree(repoRoot string, wt *Worktree, lines int) (string, error) {
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for lazygit output")
	}
	targetPane, err := m.lazygitPaneTarget(repoRoot, wt)
	if err != nil {
		return "", err
	}
	return tmuxCapturePane(targetPane, lines, true)
}

func (m *Manager) editorOutputForWorktree(repoRoot string, wt *Worktree, lines int) (string, error) {
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for editor output")
	}
	return tmuxCapturePane(m.editorPaneTarget(repoRoot, wt), lines, true)
}

func (m *Manager) sendAgentKeysForWorktree(repoRoot string, wt *Worktree, keys ...string) error {
	if !commandExists("tmux") {
		return errors.New("tmux is required for agent workflows")
	}
	return tmuxSendPaneKeys(m.agentPaneTarget(repoRoot, wt), keys...)
}

func (m *Manager) sendLazygitKeysForWorktree(repoRoot string, wt *Worktree, keys ...string) error {
	if !commandExists("tmux") {
		return errors.New("tmux is required for lazygit workflows")
	}
	targetPane, err := m.lazygitPaneTarget(repoRoot, wt)
	if err != nil {
		return err
	}
	return tmuxSendPaneKeys(targetPane, keys...)
}

func (m *Manager) sendEditorKeysForWorktree(repoRoot string, wt *Worktree, keys ...string) error {
	if !commandExists("tmux") {
		return errors.New("tmux is required for editor workflows")
	}
	return tmuxSendPaneKeys(m.editorPaneTarget(repoRoot, wt), keys...)
}

func (m *Manager) agentPaneActivity(repoRoot string, wt *Worktree) (int64, error) {
	if !commandExists("tmux") {
		return 0, errors.New("tmux is required for agent workflows")
	}
	return tmuxPaneActivity(m.agentPaneTarget(repoRoot, wt))
}

func (m *Manager) AgentOutput(target string, lines int) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	return m.agentOutputForWorktree(repoRoot, wt, lines)
}

func (m *Manager) SendAgentCommand(target, command string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	if err := tmuxSendPaneCommand(m.agentPaneTarget(repoRoot, wt), command); err != nil {
		return "", err
	}
	return wt.Path, nil
}

type tmuxPaneInfo struct {
	WindowName     string
	PaneIndex      string
	PaneID         string
	Active         bool
	CurrentCommand string
	StartCommand   string
}

func (m *Manager) agentExecCandidates() map[string]struct{} {
	candidates := map[string]struct{}{}
	add := func(cmd string) {
		name := strings.ToLower(strings.TrimSpace(commandExecutableName(cmd)))
		if name == "" {
			return
		}
		candidates[name] = struct{}{}
	}

	if strings.TrimSpace(m.Cfg.AgentCommand) != "" {
		add(m.Cfg.AgentCommand)
	} else if m.Cfg.DefaultAgentType != "" {
		if cmd, ok := m.Cfg.AgentCommands[m.Cfg.DefaultAgentType]; ok {
			add(cmd)
		}
	}
	if len(candidates) == 0 {
		add("codex")
	}
	for _, cmd := range m.Cfg.AgentCommands {
		add(cmd)
	}
	return candidates
}

func listSessionPanes(session string) ([]tmuxPaneInfo, error) {
	out, err := runCmdOutput("", "tmux", "list-panes", "-t", session, "-F", "#{window_name}\t#{pane_index}\t#{pane_id}\t#{pane_active}\t#{pane_current_command}\t#{pane_start_command}")
	if err != nil {
		return nil, err
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	panes := make([]tmuxPaneInfo, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 6 {
			continue
		}
		panes = append(panes, tmuxPaneInfo{
			WindowName:     parts[0],
			PaneIndex:      parts[1],
			PaneID:         parts[2],
			Active:         parts[3] == "1",
			CurrentCommand: parts[4],
			StartCommand:   parts[5],
		})
	}
	return panes, nil
}

func matchesAgentCommand(pane tmuxPaneInfo, candidates map[string]struct{}) bool {
	current := strings.ToLower(strings.TrimSpace(commandExecutableName(pane.CurrentCommand)))
	if current != "" {
		if _, ok := candidates[current]; ok {
			return true
		}
	}
	start := strings.ToLower(strings.TrimSpace(commandExecutableName(pane.StartCommand)))
	if start != "" {
		if _, ok := candidates[start]; ok {
			return true
		}
	}
	rawCurrent := strings.ToLower(strings.TrimSpace(pane.CurrentCommand))
	rawStart := strings.ToLower(strings.TrimSpace(pane.StartCommand))
	for candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.Contains(rawCurrent, candidate) || strings.Contains(rawStart, candidate) {
			return true
		}
	}
	return false
}

func (m *Manager) findAgentPaneInWindow(session, window string) (string, bool) {
	panes, err := listSessionPanes(session)
	if err != nil {
		return "", false
	}
	candidates := m.agentExecCandidates()
	var fallback *tmuxPaneInfo
	var match *tmuxPaneInfo
	for i := range panes {
		pane := &panes[i]
		if pane.WindowName != window {
			continue
		}
		if pane.Active {
			fallback = pane
		}
		if matchesAgentCommand(*pane, candidates) {
			if pane.Active {
				return pane.PaneID, true
			}
			if match == nil {
				match = pane
			}
		}
	}
	if match != nil {
		return match.PaneID, true
	}
	if fallback != nil {
		return fallback.PaneID, true
	}
	return "", false
}

func (m *Manager) findAgentPaneInSession(session string) (string, bool) {
	if window := m.configuredAgentWindowName(); window != "" {
		if paneID, ok := m.findAgentPaneInWindow(session, window); ok {
			return paneID, true
		}
	}
	panes, err := listSessionPanes(session)
	if err != nil {
		return "", false
	}
	candidates := m.agentExecCandidates()
	var fallback *tmuxPaneInfo
	var match *tmuxPaneInfo
	var active *tmuxPaneInfo
	for i := range panes {
		pane := &panes[i]
		if pane.Active && active == nil {
			active = pane
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(pane.WindowName)), "agent") {
			if fallback == nil || pane.Active {
				fallback = pane
			}
		}
		if matchesAgentCommand(*pane, candidates) {
			if pane.Active {
				return pane.PaneID, true
			}
			if match == nil {
				match = pane
			}
		}
	}
	if match != nil {
		return match.PaneID, true
	}
	if fallback != nil {
		return fallback.PaneID, true
	}
	if active != nil {
		return active.PaneID, true
	}
	return "", false
}

func (m *Manager) findAgentWindowInSession(session string) (string, bool) {
	if window := m.configuredAgentWindowName(); window != "" {
		if paneID, ok := m.findAgentPaneInWindow(session, window); ok && strings.TrimSpace(paneID) != "" {
			return window, true
		}
	}
	panes, err := listSessionPanes(session)
	if err != nil {
		return "", false
	}
	candidates := m.agentExecCandidates()
	var fallback *tmuxPaneInfo
	var match *tmuxPaneInfo
	var active *tmuxPaneInfo
	for i := range panes {
		pane := &panes[i]
		if pane.Active && active == nil {
			active = pane
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(pane.WindowName)), "agent") {
			if fallback == nil || pane.Active {
				fallback = pane
			}
		}
		if matchesAgentCommand(*pane, candidates) {
			if pane.Active {
				return pane.WindowName, true
			}
			if match == nil {
				match = pane
			}
		}
	}
	if match != nil {
		return match.WindowName, true
	}
	if fallback != nil {
		return fallback.WindowName, true
	}
	if active != nil {
		return active.WindowName, true
	}
	return "", false
}

func (m *Manager) tmuxPaneByCommand(session, window, paneCommand string) (string, bool, error) {
	out, err := runCmdOutput("", "tmux", "list-panes", "-t", session+":"+window, "-F", "#{pane_index}\t#{pane_current_command}")
	if err != nil {
		return "", false, err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[1]) == paneCommand {
			return strings.TrimSpace(parts[0]), true, nil
		}
	}
	return "", false, nil
}

func (m *Manager) tmuxPaneTarget(session, window string, commands []string, fallbackPane string) (string, error) {
	out, err := runCmdOutput("", "tmux", "list-panes", "-t", session+":"+window, "-F", "#{pane_index}\t#{pane_current_command}")
	if err != nil {
		return "", err
	}

	panes := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		panes[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		for paneIdx, paneCmd := range panes {
			if paneCmd == cmd {
				return session + ":" + window + "." + paneIdx, nil
			}
		}
	}

	if fallbackPane != "" {
		if _, ok := panes[fallbackPane]; ok {
			return session + ":" + window + "." + fallbackPane, nil
		}
	}
	return "", errors.New("matching tmux pane not found")
}

func tmuxSendPaneCommand(paneTarget, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return errors.New("command cannot be empty")
	}
	if err := tmuxSendPaneKeys(paneTarget, "-l", command); err != nil {
		return err
	}
	return tmuxSendPaneKeys(paneTarget, "C-m")
}

func tmuxSendPaneKeys(paneTarget string, keys ...string) error {
	if len(keys) == 0 {
		return errors.New("keys cannot be empty")
	}
	args := append([]string{"send-keys", "-t", paneTarget}, keys...)
	return runCmdQuiet("", "tmux", args...)
}

func tmuxResizePane(paneTarget string, width, height int) error {
	if strings.TrimSpace(paneTarget) == "" {
		return errors.New("pane target cannot be empty")
	}
	if width <= 0 || height <= 0 {
		return errors.New("pane size must be positive")
	}
	return runCmdQuiet("", "tmux", "resize-pane", "-t", paneTarget, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
}

func tmuxCapturePane(paneTarget string, lines int, overlayCursor bool) (string, error) {
	cursorFlag := "0"
	cursorX, cursorY := 0, 0
	paneHeight := lines
	if paneHeight <= 0 {
		paneHeight = 120
	}

	if overlayCursor {
		meta, err := runCmdOutput("", "tmux", "display-message", "-p", "-t", paneTarget, "#{cursor_flag} #{cursor_x} #{cursor_y} #{pane_height}")
		if err == nil {
			parts := strings.Fields(strings.TrimSpace(meta))
			if len(parts) == 4 {
				px, errX := strconv.Atoi(parts[1])
				py, errY := strconv.Atoi(parts[2])
				ph, errH := strconv.Atoi(parts[3])
				if errX == nil && errY == nil && errH == nil && ph > 0 {
					cursorFlag = parts[0]
					cursorX = px
					cursorY = py
					paneHeight = ph
				}
			}
		}
	}

	if lines <= 0 {
		lines = paneHeight
	}
	if lines < paneHeight {
		lines = paneHeight
	}

	out, err := runCmdOutput("", "tmux", "capture-pane", "-p", "-N", "-e", "-t", paneTarget, "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", err
	}
	rows := strings.Split(out, "\n")
	if len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		rows = []string{""}
	}
	text := strings.Join(rows, "\n")
	if !overlayCursor || cursorFlag != "1" {
		return text, nil
	}

	screenStart := len(rows) - paneHeight
	if screenStart < 0 {
		screenStart = 0
	}
	targetRow := screenStart + cursorY
	if targetRow < 0 || targetRow >= len(rows) {
		return text, nil
	}
	if cursorX < 0 {
		cursorX = 0
	}
	rows[targetRow] = overlayCursorInANSILine(rows[targetRow], cursorX)
	return strings.Join(rows, "\n"), nil
}

func tmuxPaneActivity(paneTarget string) (int64, error) {
	if strings.TrimSpace(paneTarget) == "" {
		return 0, errors.New("pane target cannot be empty")
	}
	out, err := runCmdOutput("", "tmux", "display-message", "-p", "-t", paneTarget, "#{pane_activity}")
	if err != nil {
		return 0, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return 0, errors.New("missing tmux pane activity")
	}
	return strconv.ParseInt(out, 10, 64)
}

func readTailLines(path string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size == 0 {
		return "", nil
	}

	const chunkSize int64 = 4096
	var (
		offset = size
		buf    []byte
	)
	for offset > 0 && bytes.Count(buf, []byte{'\n'}) <= lines {
		readSize := chunkSize
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, offset); err != nil && err != io.EOF {
			return "", err
		}
		buf = append(chunk, buf...)
	}

	text := strings.TrimRight(string(buf), "\n")
	if text == "" {
		return "", nil
	}
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n"), nil
}

func overlayCursorInANSILine(line string, cursorCol int) string {
	const cursorGlyph = "█"
	if cursorCol < 0 {
		cursorCol = 0
	}

	var out strings.Builder
	out.Grow(len(line) + 8)

	visCols := 0
	i := 0
	inserted := false
	for i < len(line) {
		if line[i] == '\x1b' {
			next, ok := consumeANSIEscape(line, i)
			if ok {
				out.WriteString(line[i:next])
				i = next
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(line[i:])
		if size <= 0 {
			size = 1
		}
		width := runeCellWidth(r, visCols)

		if !inserted && width > 0 && cursorCol >= visCols && cursorCol < visCols+width {
			out.WriteString(cursorGlyph)
			for pad := 1; pad < width; pad++ {
				out.WriteByte(' ')
			}
			inserted = true
		} else {
			out.WriteString(line[i : i+size])
		}
		visCols += width
		i += size
	}

	if !inserted {
		for visCols < cursorCol {
			out.WriteByte(' ')
			visCols++
		}
		out.WriteString(cursorGlyph)
	}
	return out.String()
}

func runeCellWidth(r rune, currentCol int) int {
	if r == '\t' {
		tab := 8 - (currentCol % 8)
		if tab <= 0 {
			return 8
		}
		return tab
	}
	if r == utf8.RuneError {
		return 1
	}
	if r < 0x20 || r == 0x7f {
		return 1
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 1
	}
	return w
}

func consumeANSIEscape(s string, start int) (int, bool) {
	if start < 0 || start >= len(s) || s[start] != '\x1b' || start+1 >= len(s) {
		return start, false
	}

	switch s[start+1] {
	case '[':
		i := start + 2
		for i < len(s) {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1, true
			}
			i++
		}
		return len(s), true
	case ']':
		i := start + 2
		for i < len(s) {
			if s[i] == '\a' {
				return i + 1, true
			}
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, true
			}
			i++
		}
		return len(s), true
	case 'P', 'X', '^', '_':
		i := start + 2
		for i < len(s) {
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, true
			}
			i++
		}
		return len(s), true
	default:
		i := start + 1
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			return i + 1, true
		}
		return len(s), true
	}
}

func (m *Manager) LazygitOutput(target string, lines int) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	return m.lazygitOutputForWorktree(repoRoot, wt, lines)
}

func (m *Manager) EditorOutput(target string, lines int) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	return m.editorOutputForWorktree(repoRoot, wt, lines)
}

func (m *Manager) SendLazygitCommand(target, command string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	targetPane, err := m.lazygitPaneTarget(repoRoot, wt)
	if err != nil {
		return "", err
	}
	if err := tmuxSendPaneCommand(targetPane, command); err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) SendEditorCommand(target, command string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	if err := tmuxSendPaneCommand(m.editorPaneTarget(repoRoot, wt), command); err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) SendAgentKeys(target string, keys ...string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	if err := m.sendAgentKeysForWorktree(repoRoot, wt, keys...); err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) SendLazygitKeys(target string, keys ...string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	if err := m.sendLazygitKeysForWorktree(repoRoot, wt, keys...); err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) SendEditorKeys(target string, keys ...string) (string, error) {
	repoRoot, wt, err := m.resolveWorktreeForTmux(target)
	if err != nil {
		return "", err
	}
	if err := m.sendEditorKeysForWorktree(repoRoot, wt, keys...); err != nil {
		return "", err
	}
	return wt.Path, nil
}

func (m *Manager) Remove(opts RemoveOptions) (string, []string, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return "", nil, err
	}
	wt, err := m.FindWorktree(opts.Target)
	if err != nil {
		return "", nil, err
	}

	if !opts.Force && m.WorktreeDirty(wt.Path) {
		return "", nil, fmt.Errorf("worktree has uncommitted changes: %s (use --force to override)", wt.Path)
	}

	warnings := []string{}
	session := ""
	if commandExists("tmux") {
		session = m.tmuxWorktreeSessionName(repoRoot, wt)
		if m.tmuxHasSession(session) {
			if err := runCmdQuiet("", "tmux", "kill-session", "-t", session); err != nil {
				warnings = append(warnings, fmt.Sprintf("unable to stop tmux session %s before removal: %v", session, err))
			}
		}
	}

	// Async: rename the worktree aside (instant) and reap its files in the
	// background, so the caller returns immediately instead of waiting on a
	// large tree (node_modules, build output). Falls back to a synchronous
	// removal if the rename can't be done (e.g. a cross-filesystem move).
	removedAsync := false
	if opts.Async {
		trash := trashPathFor(m.WorktreeRootDir(repoRoot), wt.Path)
		if err := os.Rename(wt.Path, trash); err != nil {
			debugLogf("remove_worktree async rename failed, falling back to sync path=%q: %v", wt.Path, err)
		} else {
			if err := runCmdQuiet(repoRoot, "git", "worktree", "prune"); err != nil {
				warnings = append(warnings, fmt.Sprintf("worktree prune failed after removal: %v", err))
			}
			go func() {
				start := time.Now()
				if err := os.RemoveAll(trash); err != nil {
					debugLogf("remove_worktree async reap failed path=%q: %v", trash, err)
				} else {
					debugLogf("remove_worktree async reap done path=%q dur=%s", trash, time.Since(start))
				}
			}()
			removedAsync = true
		}
	}

	// worktree already detached + pruned above when removedAsync; otherwise
	// remove it synchronously here.
	if !removedAsync {
		if opts.OnDeleteProgress != nil {
			if err := m.removeWorktreeWithProgress(repoRoot, wt.Path, opts.OnDeleteProgress); err != nil {
				return "", warnings, err
			}
		} else {
			if err := m.runGitWorktreeRemove(repoRoot, wt.Path, opts.Force); err != nil {
				if shouldRetryWorktreeRemove(err) {
					_ = runCmdQuiet(repoRoot, "git", "worktree", "prune")
					if session != "" && m.tmuxHasSession(session) {
						_ = runCmdQuiet("", "tmux", "kill-session", "-t", session)
					}
					if retryErr := m.runGitWorktreeRemove(repoRoot, wt.Path, opts.Force); retryErr == nil {
						warnings = append(warnings, "worktree removal required a retry after cleanup")
					} else {
						return "", warnings, retryErr
					}
				} else {
					return "", warnings, err
				}
			}
		}
	}

	if !removedAsync && opts.OnDeleteProgress != nil {
		if err := runCmdQuiet(repoRoot, "git", "worktree", "prune"); err != nil {
			warnings = append(warnings, fmt.Sprintf("worktree prune failed after removal: %v", err))
		}
	}

	if opts.DeleteBranch && wt.Branch != "" {
		if m.BranchCheckedOutAnywhere(wt.Branch) {
			warnings = append(warnings, fmt.Sprintf("branch still checked out in another worktree, not deleting: %s", wt.Branch))
		} else {
			branchArgs := []string{"branch"}
			if opts.Force {
				branchArgs = append(branchArgs, "-D")
			} else {
				branchArgs = append(branchArgs, "-d")
			}
			branchArgs = append(branchArgs, wt.Branch)
			if err := runCmdQuiet(repoRoot, "git", branchArgs...); err != nil {
				return "", warnings, err
			}
		}
	}

	return wt.Path, warnings, nil
}

type deleteItem struct {
	Rel   string
	Path  string
	Bytes int64
}

func collectDeletePlan(root string) ([]deleteItem, []string, int, int64, error) {
	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, 0, 0, nil
		}
		return nil, nil, 0, 0, err
	}
	if !info.IsDir() {
		return []deleteItem{{Rel: filepath.Base(root), Path: root, Bytes: info.Size()}}, []string{}, 1, info.Size(), nil
	}

	items := []deleteItem{}
	dirs := []string{}
	totalFiles := 0
	var totalBytes int64

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size := info.Size()
		items = append(items, deleteItem{
			Rel:   filepath.ToSlash(rel),
			Path:  path,
			Bytes: size,
		})
		totalFiles++
		totalBytes += size
		return nil
	})
	if err != nil {
		return nil, nil, 0, 0, err
	}
	return items, dirs, totalFiles, totalBytes, nil
}

func (m *Manager) removeWorktreeWithProgress(repoRoot, worktreePath string, onProgress func(DeleteProgress)) error {
	start := time.Now()
	if onProgress != nil {
		onProgress(DeleteProgress{Phase: "scan"})
	}
	items, dirs, totalFiles, totalBytes, err := collectDeletePlan(worktreePath)
	if err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(DeleteProgress{Phase: "scan", TotalFiles: totalFiles, TotalBytes: totalBytes})
	}

	deletedFiles := 0
	var deletedBytes int64
	lastUpdate := time.Time{}
	for _, item := range items {
		if onProgress != nil {
			now := time.Now()
			if deletedFiles == totalFiles || lastUpdate.IsZero() || now.Sub(lastUpdate) >= 120*time.Millisecond {
				onProgress(DeleteProgress{
					Phase:        "delete",
					CurrentPath:  item.Rel,
					DeletedFiles: deletedFiles,
					TotalFiles:   totalFiles,
					DeletedBytes: deletedBytes,
					TotalBytes:   totalBytes,
				})
				lastUpdate = now
			}
		}
		if err := os.Remove(item.Path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", item.Rel, err)
			}
		} else {
			deletedFiles++
			deletedBytes += item.Bytes
		}
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	_ = os.Remove(worktreePath)

	if onProgress != nil {
		onProgress(DeleteProgress{
			Phase:        "delete",
			DeletedFiles: deletedFiles,
			TotalFiles:   totalFiles,
			DeletedBytes: deletedBytes,
			TotalBytes:   totalBytes,
		})
	}
	debugLogf("remove_worktree delete done path=%q deleted=%d total=%d bytes=%d/%d dur=%s", worktreePath, deletedFiles, totalFiles, deletedBytes, totalBytes, time.Since(start))
	return nil
}

type DoctorReport struct {
	Lines       []string
	ExitCode    int
	MissingReqs []string
}

func (m *Manager) Doctor() DoctorReport {
	report := DoctorReport{Lines: []string{}, ExitCode: 0}

	for _, req := range []string{"git", "tmux"} {
		if commandExists(req) {
			report.Lines = append(report.Lines, fmt.Sprintf("ok   %s", req))
		} else {
			report.Lines = append(report.Lines, fmt.Sprintf("miss %s", req))
			report.MissingReqs = append(report.MissingReqs, req)
			report.ExitCode = 1
		}
	}

	optionals := []string{}
	seenOptionals := map[string]struct{}{}
	addOptional := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := seenOptionals[name]; exists {
			return
		}
		seenOptionals[name] = struct{}{}
		optionals = append(optionals, name)
	}

	for _, tool := range normalizeSessionTools(m.Cfg.SessionTools) {
		switch strings.ToLower(strings.TrimSpace(tool)) {
		case "agent":
			addOptional(commandExecutableName(m.agentCommand()))
		case "nvim", "neovim":
			addOptional("nvim")
		case "lazygit":
			addOptional("lazygit")
		default:
			addOptional(commandExecutableName(tool))
		}
	}
	addOptional(commandExecutableName(m.agentCommand()))

	for _, opt := range optionals {
		if commandExists(opt) {
			report.Lines = append(report.Lines, fmt.Sprintf("ok   %s", opt))
		} else {
			report.Lines = append(report.Lines, fmt.Sprintf("warn %s (optional)", opt))
		}
	}
	report.Lines = append(report.Lines, daemonInfoLine(m.Cfg))

	repoRoot, err := m.RequireRepo()
	if err != nil {
		report.Lines = append(report.Lines, "warn not inside a git repository; skipped worktree checks")
		return report
	}

	items, err := m.parseWorktreeList(repoRoot)
	if err != nil {
		report.Lines = append(report.Lines, fmt.Sprintf("warn unable to parse worktrees: %v", err))
		return report
	}
	bad := false
	for _, wt := range items {
		if st, err := os.Stat(wt.Path); err != nil || !st.IsDir() {
			report.Lines = append(report.Lines, fmt.Sprintf("warn missing worktree path: %s", wt.Path))
			bad = true
			continue
		}
		if wt.Branch != "" && !m.BranchExists(repoRoot, wt.Branch) {
			report.Lines = append(report.Lines, fmt.Sprintf("warn branch missing for worktree %s: %s", wt.Path, wt.Branch))
			bad = true
		}
	}
	if !bad {
		report.Lines = append(report.Lines, "ok   worktree metadata")
	}
	return report
}

func runCmdBytes(dir, name string, args ...string) ([]byte, error) {
	return runCmdBytesWithTimeout(dir, 0, name, args...)
}

func runCmdBytesContext(dir string, ctx context.Context, name string, args ...string) ([]byte, error) {
	return runCmdBytesContextAllowExitCodes(dir, ctx, nil, name, args...)
}

func runCmdBytesContextAllowExitCodes(dir string, ctx context.Context, allowedExitCodes []int, name string, args ...string) ([]byte, error) {
	start := time.Now()
	debugLogf("cmd start dir=%q name=%q args=%q allowed_exit=%v ctx=true", dir, name, strings.Join(args, " "), allowedExitCodes)
	if ctx == nil {
		ctx = context.Background()
	}
	allowed := map[int]struct{}{}
	for _, code := range allowedExitCodes {
		allowed[code] = struct{}{}
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if _, ok := allowed[exitErr.ExitCode()]; ok {
				debugLogf("cmd ok-allowed-exit dur=%s dir=%q name=%q args=%q exit=%d out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), exitErr.ExitCode(), len(out))
				return out, nil
			}
		}
		trimmed := strings.TrimSpace(string(out))
		if len(trimmed) > 600 {
			trimmed = trimmed[:600] + "...(truncated)"
		}
		debugLogf("cmd fail dur=%s dir=%q name=%q args=%q err=%v out=%q", elapsed, dir, name, strings.Join(args, " "), err, trimmed)
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if trimmed != "" {
				return nil, fmt.Errorf("%s %s timed out: %s", name, strings.Join(args, " "), trimmed)
			}
			return nil, fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
		}
		if trimmed != "" {
			return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	debugLogf("cmd ok dur=%s dir=%q name=%q args=%q out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), len(out))
	return out, nil
}

func runCmdBytesWithTimeout(dir string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	start := time.Now()
	timeoutInfo := ""
	if timeout > 0 {
		timeoutInfo = fmt.Sprintf(" timeout=%s", timeout)
	}
	debugLogf("cmd start dir=%q name=%q args=%q%s", dir, name, strings.Join(args, " "), timeoutInfo)
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if len(trimmed) > 600 {
			trimmed = trimmed[:600] + "...(truncated)"
		}
		debugLogf("cmd fail dur=%s dir=%q name=%q args=%q err=%v out=%q", elapsed, dir, name, strings.Join(args, " "), err, trimmed)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if trimmed != "" {
				return nil, fmt.Errorf("%s %s timed out after %s: %s", name, strings.Join(args, " "), timeout, trimmed)
			}
			return nil, fmt.Errorf("%s %s timed out after %s", name, strings.Join(args, " "), timeout)
		}
		if trimmed != "" {
			return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	debugLogf("cmd ok dur=%s dir=%q name=%q args=%q out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), len(out))
	return out, nil
}

func runCmdBytesAllowExitCodes(dir string, allowedExitCodes []int, name string, args ...string) ([]byte, error) {
	allowed := map[int]struct{}{}
	for _, code := range allowedExitCodes {
		allowed[code] = struct{}{}
	}

	start := time.Now()
	debugLogf("cmd start dir=%q name=%q args=%q allowed_exit=%v", dir, name, strings.Join(args, " "), allowedExitCodes)
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if _, ok := allowed[exitErr.ExitCode()]; ok {
				debugLogf("cmd ok-allowed-exit dur=%s dir=%q name=%q args=%q exit=%d out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), exitErr.ExitCode(), len(out))
				return out, nil
			}
		}
		trimmed := strings.TrimSpace(string(out))
		if len(trimmed) > 600 {
			trimmed = trimmed[:600] + "...(truncated)"
		}
		debugLogf("cmd fail dur=%s dir=%q name=%q args=%q err=%v out=%q", elapsed, dir, name, strings.Join(args, " "), err, trimmed)
		if trimmed != "" {
			return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	debugLogf("cmd ok dur=%s dir=%q name=%q args=%q out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), len(out))
	return out, nil
}

func runCmdBytesInput(dir string, stdin []byte, name string, args ...string) ([]byte, error) {
	start := time.Now()
	debugLogf("cmd start dir=%q name=%q args=%q stdin_bytes=%d", dir, name, strings.Join(args, " "), len(stdin))
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if len(trimmed) > 600 {
			trimmed = trimmed[:600] + "...(truncated)"
		}
		debugLogf("cmd fail dur=%s dir=%q name=%q args=%q err=%v out=%q", elapsed, dir, name, strings.Join(args, " "), err, trimmed)
		if trimmed != "" {
			return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	debugLogf("cmd ok dur=%s dir=%q name=%q args=%q out_bytes=%d", elapsed, dir, name, strings.Join(args, " "), len(out))
	return out, nil
}

func runCmdOutput(dir, name string, args ...string) (string, error) {
	out, err := runCmdBytes(dir, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func runCmdOutputContext(dir string, ctx context.Context, name string, args ...string) (string, error) {
	out, err := runCmdBytesContext(dir, ctx, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func runCmdOutputAllowExitCodes(dir string, allowedExitCodes []int, name string, args ...string) (string, error) {
	out, err := runCmdBytesAllowExitCodes(dir, allowedExitCodes, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func runCmdOutputContextAllowExitCodes(dir string, ctx context.Context, allowedExitCodes []int, name string, args ...string) (string, error) {
	out, err := runCmdBytesContextAllowExitCodes(dir, ctx, allowedExitCodes, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func runCmdQuiet(dir, name string, args ...string) error {
	_, err := runCmdBytes(dir, name, args...)
	return err
}

func runCmdQuietTimeout(dir string, timeout time.Duration, name string, args ...string) error {
	_, err := runCmdBytesWithTimeout(dir, timeout, name, args...)
	return err
}

func gitWorktreeCommandTimeout() time.Duration {
	const (
		defaultSeconds = 45
		minSeconds     = 5
		maxSeconds     = 600
	)
	raw := strings.TrimSpace(os.Getenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultSeconds * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSeconds * time.Second
	}
	if seconds < minSeconds {
		seconds = minSeconds
	}
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func shouldRetryWorktreeAdd(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timed out"):
		return true
	case strings.Contains(msg, "already checked out"):
		return true
	case strings.Contains(msg, "already exists"):
		return true
	case strings.Contains(msg, "already registered"):
		return true
	case strings.Contains(msg, "unable to create"):
		return true
	case strings.Contains(msg, "cannot lock"):
		return true
	default:
		return false
	}
}

func shouldRetryWorktreeRemove(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timed out"):
		return true
	case strings.Contains(msg, "is locked"):
		return true
	case strings.Contains(msg, "cannot remove"):
		return true
	case strings.Contains(msg, "cannot lock"):
		return true
	default:
		return false
	}
}

func (m *Manager) runGitWorktreeAdd(repoRoot string, args ...string) error {
	allArgs := append([]string{"worktree", "add"}, args...)
	timeout := gitWorktreeCommandTimeout()
	if err := runCmdQuietTimeout(repoRoot, timeout, "git", allArgs...); err != nil {
		if shouldRetryWorktreeAdd(err) {
			_ = runCmdQuiet(repoRoot, "git", "worktree", "prune")
			if retryErr := runCmdQuietTimeout(repoRoot, timeout, "git", allArgs...); retryErr == nil {
				return nil
			} else {
				return retryErr
			}
		}
		return err
	}
	return nil
}

func (m *Manager) runGitWorktreeRemove(repoRoot, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	timeout := gitWorktreeCommandTimeout()
	return runCmdQuietTimeout(repoRoot, timeout, "git", args...)
}

func runCmdInherit(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
