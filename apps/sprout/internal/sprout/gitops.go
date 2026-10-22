package sprout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Staging, discarding and committing for the TUI's Files pane. All of it runs
// plain git in the worktree with no prompts and no terminal.

const gitOpTimeout = 60 * time.Second

// fileStage describes a porcelain v2 XY status code (".M", "A.", "MM", "??").
type fileStage struct {
	staged    bool // something is in the index
	unstaged  bool // the work tree differs from the index
	untracked bool
	newInIdx  bool // added to the index and not in HEAD
	conflict  bool
}

func parseFileStage(status string) fileStage {
	s := strings.TrimSpace(status)
	if s == "??" {
		return fileStage{untracked: true, unstaged: true}
	}
	r := []rune(status)
	if len(r) < 2 {
		return fileStage{}
	}
	x, y := r[0], r[1]
	st := fileStage{
		staged:   x != '.' && x != ' ' && x != '?',
		unstaged: y != '.' && y != ' ' && y != '?',
	}
	st.newInIdx = x == 'A'
	st.conflict = x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D')
	return st
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out.String(), fmt.Errorf("git %s timed out after %s", args[0], gitOpTimeout)
		}
		msg := firstErrLine(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

// firstErrLine picks the most informative line of git output.
func firstErrLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "error:") || strings.HasPrefix(l, "fatal:") {
			return l
		}
	}
	return firstLine(out)
}

// filePaths lists the pathspecs for files. withPrevious also includes the old
// side of renames, which only commands that rewrite the index back to HEAD
// can resolve (git add can't: the old path no longer exists anywhere).
func filePaths(files []DiffFile, withPrevious bool) []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, f := range files {
		add(f.Path)
		if withPrevious {
			add(f.PreviousPath)
		}
	}
	return paths
}

// filterFiles keeps the files for which keep reports true.
func filterFiles(files []DiffFile, keep func(fileStage) bool) []DiffFile {
	var out []DiffFile
	for _, f := range files {
		if keep(parseFileStage(f.Status)) {
			out = append(out, f)
		}
	}
	return out
}

// StageFiles adds the work tree changes of files (including deletions) to the
// index. Files with nothing left to stage are skipped, since git rejects
// pathspecs that no longer exist in either place.
func (m *Manager) StageFiles(dir string, files []DiffFile) error {
	paths := filePaths(filterFiles(files, func(st fileStage) bool { return st.unstaged }), false)
	if len(paths) == 0 {
		return nil
	}
	_, err := gitRun(context.Background(), dir, append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

// UnstageFiles removes files from the index, keeping work tree changes. A
// staged rename is released on both sides.
func (m *Manager) UnstageFiles(dir string, files []DiffFile) error {
	paths := filePaths(filterFiles(files, func(st fileStage) bool { return st.staged }), true)
	if len(paths) == 0 {
		return nil
	}
	if _, err := gitRun(context.Background(), dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		// No commits yet: there is no HEAD to restore the index from.
		_, err := gitRun(context.Background(), dir, append([]string{"rm", "--cached", "-r", "-q", "--"}, paths...)...)
		return err
	}
	_, err := gitRun(context.Background(), dir, append([]string{"restore", "--staged", "--"}, paths...)...)
	return err
}

// DiscardFiles throws away all changes to files: untracked files are deleted,
// files added to the index are removed, everything else is restored from HEAD.
func (m *Manager) DiscardFiles(dir string, files []DiffFile) error {
	ctx := context.Background()
	var restore []DiffFile
	for _, f := range files {
		st := parseFileStage(f.Status)
		switch {
		case st.conflict:
			return fmt.Errorf("%s has merge conflicts; resolve them first", f.Path)
		case st.untracked:
			if _, err := gitRun(ctx, dir, "clean", "-f", "-q", "--", f.Path); err != nil {
				return err
			}
		case st.newInIdx:
			if _, err := gitRun(ctx, dir, "rm", "-f", "-q", "--", f.Path); err != nil {
				return err
			}
			if f.PreviousPath != "" { // an added copy/rename keeps its source
				restore = append(restore, DiffFile{Path: f.PreviousPath})
			}
		default:
			restore = append(restore, f)
		}
	}
	if paths := filePaths(restore, true); len(paths) > 0 {
		if _, err := gitRun(ctx, dir, append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, paths...)...); err != nil {
			return err
		}
	}
	return nil
}

// CommitStaged commits the index with message and returns git's summary line.
func (m *Manager) CommitStaged(dir, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("commit message is empty")
	}
	out, err := gitRun(context.Background(), dir, "commit", "-m", message)
	if err != nil {
		return "", err
	}
	return firstLine(out), nil
}

// commitNeedsTerminal reports whether committing may prompt (GPG signing), in
// which case it must run with the real terminal instead of in the background.
func commitNeedsTerminal(dir string) bool {
	out, err := gitRun(context.Background(), dir, "config", "--type=bool", "--get", "commit.gpgsign")
	return err == nil && strings.TrimSpace(out) == "true"
}
