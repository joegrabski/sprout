package sprout

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BootstrapOptions controls a Bootstrap call.
type BootstrapOptions struct {
	Target string                // worktree to bootstrap (branch/path/name); empty = current repo
	OnStep func(dir, run string) // optional callback before each command step runs
	Out    io.Writer             // where command output goes; nil = inherit os.Stdout/Stderr
}

// BootstrapResult summarizes what bootstrapping did.
type BootstrapResult struct {
	WorktreePath string
	MainWorktree string
	LinksCreated []string // symlinks created
	LinksSkipped []string // target already existed
	LinksMissing []string // source not present in the main worktree
	StepsRun     int
}

// HasBootstrap reports whether any bootstrap behavior is configured.
func (m *Manager) HasBootstrap() bool {
	return len(m.Cfg.BootstrapLinks) > 0 || len(m.Cfg.BootstrapSteps) > 0
}

// mainWorktreePath returns the repo's primary (non-linked) worktree path,
// derived from the git common dir (…/main/.git → …/main).
func (m *Manager) mainWorktreePath(repoRoot string) (string, error) {
	commonDir, err := m.gitCommonDir(repoRoot)
	if err != nil {
		return "", err
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	return commonDir, nil // bare repo: common dir is the repo itself
}

// Bootstrap prepares a worktree for use: it symlinks the configured gitignored
// local files from the main worktree and runs the configured setup commands.
func (m *Manager) Bootstrap(opts BootstrapOptions) (BootstrapResult, error) {
	repoRoot, err := m.RequireRepo()
	if err != nil {
		return BootstrapResult{}, err
	}
	worktreePath := repoRoot
	if target := strings.TrimSpace(opts.Target); target != "" {
		wt, err := m.FindWorktree(target)
		if err != nil {
			return BootstrapResult{}, err
		}
		worktreePath = wt.Path
	}
	return m.bootstrapWorktree(repoRoot, worktreePath, opts.OnStep, opts.Out)
}

// bootstrapWorktree is the core routine shared by Bootstrap and NewWorktree.
func (m *Manager) bootstrapWorktree(repoRoot, worktreePath string, onStep func(dir, run string), out io.Writer) (BootstrapResult, error) {
	worktreePath = absPath(worktreePath)
	mainWorktree, err := m.mainWorktreePath(repoRoot)
	if err != nil {
		return BootstrapResult{}, err
	}
	res := BootstrapResult{WorktreePath: worktreePath, MainWorktree: mainWorktree}

	sameWorktree := absPath(mainWorktree) == worktreePath

	// 1. Symlink gitignored local files/dirs from the main worktree.
	for _, rel := range m.Cfg.BootstrapLinks {
		rel = strings.TrimSpace(rel)
		if rel == "" || sameWorktree {
			continue // nothing to link into the main worktree itself
		}
		src := filepath.Join(mainWorktree, rel)
		dst := filepath.Join(worktreePath, rel)
		if _, err := os.Lstat(src); err != nil {
			res.LinksMissing = append(res.LinksMissing, rel)
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			res.LinksSkipped = append(res.LinksSkipped, rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return res, fmt.Errorf("bootstrap link %s: %w", rel, err)
		}
		if err := os.Symlink(src, dst); err != nil {
			return res, fmt.Errorf("bootstrap link %s: %w", rel, err)
		}
		res.LinksCreated = append(res.LinksCreated, rel)
	}

	// 2. Run the configured setup commands.
	for _, step := range m.Cfg.BootstrapSteps {
		run := strings.TrimSpace(step.Run)
		if run == "" {
			continue
		}
		dir := resolvePaneDir(step.Dir, worktreePath)
		if dir == "" {
			dir = worktreePath
		}
		if onStep != nil {
			onStep(dir, run)
		}
		if err := runBootstrapCommand(dir, run, out); err != nil {
			return res, fmt.Errorf("bootstrap step %q failed: %w", run, err)
		}
		res.StepsRun++
	}

	return res, nil
}

// lineWriter is an io.Writer that invokes fn for each newline-terminated line,
// letting callers stream command output into a UI label.
type lineWriter struct {
	fn  func(string)
	buf []byte
}

func newLineWriter(fn func(string)) *lineWriter { return &lineWriter{fn: fn} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if w.fn != nil {
			w.fn(line)
		}
	}
	return len(p), nil
}

// runBootstrapCommand runs a single setup command. With out == nil it inherits
// the process stdio (CLI); otherwise it streams combined output to out (TUI).
func runBootstrapCommand(dir, run string, out io.Writer) error {
	if out == nil {
		return runCmdInherit(dir, "sh", "-lc", run)
	}
	cmd := exec.Command("sh", "-lc", run)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
