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
	"sort"
	"strconv"
	"strings"
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
	Path   string
	Status string
}

type NewOptions struct {
	Branch     string
	Type       string
	Name       string
	BaseBranch string
	FromBranch string
	Launch     bool
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
	Target       string
	Force        bool
	DeleteBranch bool
}

type Manager struct {
	Cfg Config
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
	out, err := runCmdOutput(repoRoot, "git", "worktree", "list", "--porcelain")
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

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
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
	branch := worktreeBranchOrName(wt)
	return m.tmuxWorktreeSessionNameFrom(repoRoot, branch, wt.Path)
}

func (m *Manager) tmuxWorktreeSessionNameFrom(repoRoot, branch, worktreePath string) string {
	base := m.tmuxSessionName(repoRoot)
	token := strings.TrimSpace(branch)
	if token == "" {
		token = filepath.Base(worktreePath)
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
	_, err := exec.LookPath(name)
	return err == nil
}

func (m *Manager) tmuxHasSession(session string) bool {
	_, err := runCmdOutput("", "tmux", "has-session", "-t", session)
	return err == nil
}

func (m *Manager) tmuxWindowExists(session, window string) bool {
	_, err := runCmdOutput("", "tmux", "has-session", "-t", session+":"+window)
	return err == nil
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
	if err := runCmdQuiet("", "tmux", "new-session", "-d", "-s", session, "-n", window, "-c", repoRoot, command); err != nil {
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
	if err := runCmdQuiet("", "tmux", "new-window", "-d", "-t", session, "-n", window, "-c", worktreePath, cmd); err != nil {
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

// resolvePaneDir resolves a pane dir spec to an absolute path.
// Returns "" when dir is empty (caller should use the worktree path as default).
//   - "~" or "~/..." → expands to home directory
//   - "{worktree}" prefix → replaced with worktreePath
//   - Absolute path → returned as-is
//   - Relative path → returned as-is (tmux -c accepts relative paths)
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
	return dir
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
				args = append(args, pane.Run)
			}
			if err := runCmdQuiet("", "tmux", args...); err != nil {
				return "", "", err
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
						args = append(args, pane.Command)
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

	items, err := m.parseWorktreeList(repoRoot)
	if err != nil {
		return nil, err
	}
	current := absPath(repoRoot)

	hasTmux := commandExists("tmux")

	for i := range items {
		items[i].Path = absPath(items[i].Path)
		items[i].Current = items[i].Path == current
		items[i].Dirty = m.WorktreeDirty(items[i].Path)
		items[i].TmuxState = "n/a"
		items[i].AgentState = "n/a"
		if !hasTmux {
			continue
		}

		items[i].TmuxState = "no"
		items[i].AgentState = "no"
		session := m.tmuxWorktreeSessionName(repoRoot, &items[i])
		if m.tmuxHasSession(session) {
			items[i].TmuxState = "yes"
			agentWindow := m.tmuxAgentWindowName(worktreeBranchOrName(&items[i]))
			if m.tmuxWindowExists(session, agentWindow) {
				items[i].AgentState = "yes"
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
	out, err := runCmdOutput(path, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

func (m *Manager) WorktreeDiff(path string, width int) (string, error) {
	status, err := runCmdOutput(path, "git", "--no-pager", "status", "--short")
	if err != nil {
		return "", err
	}
	staged, err := runCmdOutput(path, "git", "--no-pager", "diff", "--cached", "--no-color", "--no-ext-diff")
	if err != nil {
		return "", err
	}
	unstaged, err := runCmdOutput(path, "git", "--no-pager", "diff", "--no-color", "--no-ext-diff")
	if err != nil {
		return "", err
	}

	if commandExists("delta") {
		if rendered, renderErr := renderDiffWithDelta(staged, width); renderErr == nil {
			staged = rendered
		} else {
			debugLogf("diff delta staged failed path=%q: %v", path, renderErr)
		}
		if rendered, renderErr := renderDiffWithDelta(unstaged, width); renderErr == nil {
			unstaged = rendered
		} else {
			debugLogf("diff delta unstaged failed path=%q: %v", path, renderErr)
		}
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
	out, err := runCmdOutput(path, "git", "--no-pager", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	files := make([]DiffFile, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		file := strings.TrimSpace(line[3:])
		if file == "" {
			continue
		}
		if idx := strings.LastIndex(file, " -> "); idx >= 0 {
			file = strings.TrimSpace(file[idx+4:])
		}
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		files = append(files, DiffFile{
			Path:   file,
			Status: status,
		})
	}
	return files, nil
}

func (m *Manager) WorktreeDiffForFile(path string, file DiffFile, width int) (string, error) {
	statusRaw := file.Status
	stageState, workState := parsePorcelainStatus(statusRaw)
	statusLabel := strings.TrimSpace(statusRaw)

	staged := ""
	unstaged := ""
	var err error

	needsStaged := stageState != ' ' && stageState != '?'
	needsUnstaged := workState != ' ' && workState != '?'

	isUntracked := stageState == '?' && workState == '?'
	if isUntracked {
		unstaged, err = runCmdOutputAllowExitCodes(path, []int{1}, "git", "--no-pager", "diff", "--no-index", "--no-color", "--no-ext-diff", "--", "/dev/null", file.Path)
		if err != nil {
			return "", err
		}
	} else {
		if needsStaged {
			staged, err = runCmdOutput(path, "git", "--no-pager", "diff", "--cached", "--no-color", "--no-ext-diff", "--", file.Path)
			if err != nil {
				return "", err
			}
		}
		if needsUnstaged {
			unstaged, err = runCmdOutput(path, "git", "--no-pager", "diff", "--no-color", "--no-ext-diff", "--", file.Path)
			if err != nil {
				return "", err
			}
		}
	}

	if commandExists("delta") {
		if rendered, renderErr := renderDiffWithDelta(staged, width); renderErr == nil {
			staged = rendered
		} else {
			debugLogf("diff delta staged file=%q path=%q failed: %v", file.Path, path, renderErr)
		}
		if rendered, renderErr := renderDiffWithDelta(unstaged, width); renderErr == nil {
			unstaged = rendered
		} else {
			debugLogf("diff delta unstaged file=%q path=%q failed: %v", file.Path, path, renderErr)
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\x1b[36m# %s\x1b[0m", file.Path))
	if statusLabel != "" {
		b.WriteString(fmt.Sprintf(" \x1b[36m(%s)\x1b[0m", statusLabel))
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

func renderDiffWithDelta(diff string, width int) (string, error) {
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}
	if !commandExists("delta") {
		return diff, nil
	}
	args := []string{"--paging=never"}
	if width > 0 {
		args = append(args, "--width", strconv.Itoa(width))
	}
	out, err := runCmdBytesInput("", []byte(diff), "delta", args...)
	if err != nil {
		return "", err
	}
	rendered := strings.ReplaceAll(string(out), "\x1b[0K", "")
	rendered = strings.ReplaceAll(rendered, "\x1b[K", "")
	return strings.TrimRight(rendered, "\n"), nil
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

func (m *Manager) collectCopyCandidates(sourceRoot string) ([]string, error) {
	out, err := runCmdBytes(sourceRoot, "git", "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return nil, err
	}
	records := bytes.Split(out, []byte{0})
	set := map[string]struct{}{}
	for _, rec := range records {
		if len(rec) < 3 {
			continue
		}
		line := string(rec)
		if strings.HasPrefix(line, "? ") || strings.HasPrefix(line, "! ") {
			p := strings.TrimSpace(line[2:])
			p = strings.TrimSuffix(p, "/")
			if p == "" {
				continue
			}
			if p == ".git" || strings.HasPrefix(p, ".git/") {
				continue
			}
			set[p] = struct{}{}
		}
	}
	res := make([]string, 0, len(set))
	for p := range set {
		res = append(res, p)
	}
	sort.Strings(res)
	return res, nil
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, time.Now(), info.ModTime())
}

func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Symlink(target, dst)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return copySymlink(path, target)
		}

		if d.IsDir() {
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			return nil
		}
		return copyFile(path, target, info)
	})
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		return copyTree(src, dst)
	}
	return copyFile(src, dst, info)
}

func (m *Manager) CopyUntrackedAndIgnored(sourceRoot, targetRoot string) error {
	candidates, err := m.collectCopyCandidates(sourceRoot)
	if err != nil {
		return err
	}
	for _, rel := range candidates {
		src := filepath.Join(sourceRoot, rel)
		dst := filepath.Join(targetRoot, rel)
		if _, err := os.Lstat(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := copyPath(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
	}
	return nil
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

	if isExisting {
		if err := m.CreateWorktreeFromExisting(repoRoot, branch, worktreePath); err != nil {
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
			debugLogf("new_worktree create_worktree failed branch=%q path=%q base=%q: %v", branch, worktreePath, base, err)
			return "", "", err
		}
	}

	debugLogf("new_worktree created branch=%q path=%q", branch, worktreePath)
	if err := m.CopyUntrackedAndIgnored(repoRoot, worktreePath); err != nil {
		debugLogf("new_worktree copy_untracked_failed path=%q: %v", worktreePath, err)
		return "", "", err
	}
	debugLogf("new_worktree copied_untracked path=%q", worktreePath)

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
		if err := m.LaunchOrFocus(repoRoot, branch, wt.Path, attachOutside); err != nil {
			return "", err
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
	agentWindow := m.tmuxAgentWindowName(branch)
	alreadyRunning := m.tmuxHasSession(session) && m.tmuxWindowExists(session, agentWindow)

	_, _, err = m.tmuxEnsureWorktreeWindow(repoRoot, branch, wt.Path)
	if err != nil {
		debugLogf("start_agent ensure_worktree_window failed path=%q branch=%q: %v", wt.Path, branch, err)
		return "", false, err
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
	agentWindow := m.tmuxAgentWindowName(worktreeBranchOrName(wt))
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
	window := m.tmuxAgentWindowName(worktreeBranchOrName(wt))
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
	return tmuxCapturePaneWithCursor(m.agentPaneTarget(repoRoot, wt), lines)
}

func (m *Manager) lazygitOutputForWorktree(repoRoot string, wt *Worktree, lines int) (string, error) {
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for lazygit output")
	}
	targetPane, err := m.lazygitPaneTarget(repoRoot, wt)
	if err != nil {
		return "", err
	}
	return tmuxCapturePaneWithCursor(targetPane, lines)
}

func (m *Manager) editorOutputForWorktree(repoRoot string, wt *Worktree, lines int) (string, error) {
	if !commandExists("tmux") {
		return "", errors.New("tmux is required for editor output")
	}
	return tmuxCapturePaneWithCursor(m.editorPaneTarget(repoRoot, wt), lines)
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

func tmuxCapturePaneWithCursor(paneTarget string, lines int) (string, error) {
	cursorFlag := "0"
	cursorX, cursorY := 0, 0
	paneHeight := lines
	if paneHeight <= 0 {
		paneHeight = 120
	}

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
	if cursorFlag != "1" {
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

func runCmdOutputAllowExitCodes(dir string, allowedExitCodes []int, name string, args ...string) (string, error) {
	out, err := runCmdBytesAllowExitCodes(dir, allowedExitCodes, name, args...)
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
