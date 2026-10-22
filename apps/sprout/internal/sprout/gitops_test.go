package sprout

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.co", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.co")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeT(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// statusMap returns path -> XY from porcelain v2, using the production parser.
func statusMap(t *testing.T, m *Manager, dir string) map[string]string {
	t.Helper()
	snap, err := m.WorktreeDiffSnapshotContext(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range snap.Files {
		out[f.Path] = f.Status
	}
	return out
}

// statusRepo builds a repo with one file in each status shape.
func statusRepo(t *testing.T) (string, *Manager) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	for _, f := range []string{"mod.txt", "staged.txt", "both.txt", "del.txt", "stagedel.txt", "old.txt", "keep.txt"} {
		writeT(t, dir, f, "base of "+f+"\nwith some more text so renames are unambiguous\nline three of "+f+"\n")
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-qm", "init")

	writeT(t, dir, "mod.txt", "changed\n")    // .M
	writeT(t, dir, "staged.txt", "changed\n") // M.
	gitT(t, dir, "add", "staged.txt")
	writeT(t, dir, "both.txt", "one\n") // MM
	gitT(t, dir, "add", "both.txt")
	writeT(t, dir, "both.txt", "two\n")
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil { // .D
		t.Fatal(err)
	}
	gitT(t, dir, "rm", "-q", "stagedel.txt") // D.
	gitT(t, dir, "mv", "old.txt", "new.txt") // R.
	writeT(t, dir, "added.txt", "new\n")     // A.
	gitT(t, dir, "add", "added.txt")
	writeT(t, dir, "addedmod.txt", "new\n") // AM
	gitT(t, dir, "add", "addedmod.txt")
	writeT(t, dir, "addedmod.txt", "newer\n")
	writeT(t, dir, "untracked.txt", "u\n")  // ??
	writeT(t, dir, "udir/sub/a.txt", "u\n") // ?? inside an untracked dir
	writeT(t, dir, "udir/b.txt", "u\n")
	return dir, NewManager(DefaultConfig())
}

func filesByPath(t *testing.T, m *Manager, dir string, paths ...string) []DiffFile {
	t.Helper()
	snap, err := m.WorktreeDiffSnapshotContext(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []DiffFile
	for _, p := range paths {
		found := false
		for _, f := range snap.Files {
			if f.Path == p {
				out = append(out, f)
				found = true
			}
		}
		if !found {
			t.Fatalf("%s not in status: %v", p, snap.Files)
		}
	}
	return out
}

func TestParseFileStage(t *testing.T) {
	cases := map[string]fileStage{
		".M": {unstaged: true},
		"M.": {staged: true},
		"MM": {staged: true, unstaged: true},
		"A.": {staged: true, newInIdx: true},
		"AM": {staged: true, unstaged: true, newInIdx: true},
		".D": {unstaged: true},
		"D.": {staged: true},
		"R.": {staged: true},
		"??": {untracked: true, unstaged: true},
		"UU": {staged: true, unstaged: true, conflict: true},
	}
	for status, want := range cases {
		if got := parseFileStage(status); got != want {
			t.Errorf("parseFileStage(%q) = %+v, want %+v", status, got, want)
		}
	}
}

func TestStageAndUnstageEveryShape(t *testing.T) {
	dir, m := statusRepo(t)
	before := statusMap(t, m, dir)
	for path, want := range map[string]string{
		"mod.txt": ".M", "staged.txt": "M.", "both.txt": "MM", "del.txt": ".D", "stagedel.txt": "D.",
		"new.txt": "R.", "added.txt": "A.", "addedmod.txt": "AM", "untracked.txt": "??", "udir/b.txt": "??", "udir/sub/a.txt": "??",
	} {
		if before[path] != want {
			t.Fatalf("setup: %s = %q, want %q (all: %v)", path, before[path], want, before)
		}
	}

	all := filesByPath(t, m, dir, "mod.txt", "both.txt", "del.txt", "addedmod.txt", "untracked.txt", "udir/b.txt", "udir/sub/a.txt")
	if err := m.StageFiles(dir, all); err != nil {
		t.Fatal(err)
	}
	after := statusMap(t, m, dir)
	for path, want := range map[string]string{
		"mod.txt": "M.", "both.txt": "M.", "del.txt": "D.", "addedmod.txt": "A.", "untracked.txt": "A.", "udir/b.txt": "A.", "udir/sub/a.txt": "A.",
	} {
		if after[path] != want {
			t.Errorf("after stage: %s = %q, want %q", path, after[path], want)
		}
	}

	if err := m.UnstageFiles(dir, filesByPath(t, m, dir, "mod.txt", "del.txt", "untracked.txt", "new.txt")); err != nil {
		t.Fatal(err)
	}
	after = statusMap(t, m, dir)
	for path, want := range map[string]string{"mod.txt": ".M", "del.txt": ".D", "untracked.txt": "??"} {
		if after[path] != want {
			t.Errorf("after unstage: %s = %q, want %q", path, after[path], want)
		}
	}
	// Unstaging a rename must release both sides: old.txt becomes a plain
	// deletion and new.txt a plain untracked file.
	if after["old.txt"] != ".D" || after["new.txt"] != "??" {
		t.Errorf("rename not fully unstaged: old.txt=%q new.txt=%q", after["old.txt"], after["new.txt"])
	}
}

func TestDiscardEveryShape(t *testing.T) {
	dir, m := statusRepo(t)
	files := filesByPath(t, m, dir, "mod.txt", "staged.txt", "both.txt", "del.txt", "stagedel.txt", "new.txt", "added.txt", "addedmod.txt", "untracked.txt", "udir/b.txt", "udir/sub/a.txt")
	if err := m.DiscardFiles(dir, files); err != nil {
		t.Fatal(err)
	}
	if left := statusMap(t, m, dir); len(left) != 0 {
		t.Fatalf("discard left changes behind: %v", left)
	}
	for _, name := range []string{"mod.txt", "both.txt", "del.txt", "old.txt"} {
		body := "base of " + name + "\nwith some more text so renames are unambiguous\nline three of " + name + "\n"
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != body {
			t.Errorf("%s = %q, %v; want restored %q", name, got, err, body)
		}
	}
	for _, gone := range []string{"added.txt", "addedmod.txt", "untracked.txt", "new.txt", "udir/b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should be gone after discard (err=%v)", gone, err)
		}
	}
}

func TestUnstageBeforeFirstCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	writeT(t, dir, "a.txt", "a\n")
	m := NewManager(DefaultConfig())
	f := filesByPath(t, m, dir, "a.txt")
	if err := m.StageFiles(dir, f); err != nil {
		t.Fatal(err)
	}
	if err := m.UnstageFiles(dir, filesByPath(t, m, dir, "a.txt")); err != nil {
		t.Fatalf("unstage in a repo with no commits: %v", err)
	}
	if got := statusMap(t, m, dir)["a.txt"]; got != "??" {
		t.Fatalf("a.txt = %q, want ??", got)
	}
}

func TestCommitStaged(t *testing.T) {
	dir, m := statusRepo(t)
	t.Setenv("GIT_AUTHOR_NAME", "T")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@e.co")
	t.Setenv("GIT_COMMITTER_NAME", "T")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@e.co")
	if _, err := m.CommitStaged(dir, "   "); err == nil {
		t.Fatal("empty message should fail")
	}
	summary, err := m.CommitStaged(dir, "add things")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "add things") {
		t.Fatalf("summary = %q", summary)
	}
	if got := strings.TrimSpace(gitT(t, dir, "log", "-1", "--format=%s")); got != "add things" {
		t.Fatalf("last commit = %q", got)
	}
	// A failing hook surfaces its message instead of hanging.
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'nope: lint failed' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeT(t, dir, "mod.txt", "again\n")
	gitT(t, dir, "add", "mod.txt")
	if _, err := m.CommitStaged(dir, "blocked"); err == nil || !strings.Contains(err.Error(), "lint failed") {
		t.Fatalf("hook failure = %v, want message containing 'lint failed'", err)
	}
}

// Staging "everything" must cope with files that are already fully staged and
// with renames whose old path no longer exists anywhere.
func TestStageAndUnstageEverythingAtOnce(t *testing.T) {
	dir, m := statusRepo(t)
	all := func() []DiffFile {
		snap, err := m.WorktreeDiffSnapshotContext(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return snap.Files
	}
	if err := m.StageFiles(dir, all()); err != nil {
		t.Fatalf("stage all: %v", err)
	}
	for path, xy := range statusMap(t, m, dir) {
		if st := parseFileStage(xy); st.unstaged {
			t.Errorf("%s = %q still has unstaged changes after stage all", path, xy)
		}
	}
	// Everything is staged now; staging again is a no-op rather than an error.
	if err := m.StageFiles(dir, all()); err != nil {
		t.Fatalf("stage all twice: %v", err)
	}
	if err := m.UnstageFiles(dir, all()); err != nil {
		t.Fatalf("unstage all: %v", err)
	}
	for path, xy := range statusMap(t, m, dir) {
		if st := parseFileStage(xy); st.staged {
			t.Errorf("%s = %q still staged after unstage all", path, xy)
		}
	}
}

func TestStageRenameWithWorkTreeEdit(t *testing.T) {
	dir, m := statusRepo(t)
	// Append so the file stays similar enough for git to keep calling it a rename.
	body, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	writeT(t, dir, "new.txt", string(body)+"one more line\n") // RM
	if got := statusMap(t, m, dir)["new.txt"]; got != "RM" {
		t.Fatalf("setup: new.txt = %q, want RM", got)
	}
	if err := m.StageFiles(dir, filesByPath(t, m, dir, "new.txt")); err != nil {
		t.Fatalf("stage RM: %v", err)
	}
	if got := statusMap(t, m, dir)["new.txt"]; got != "R." {
		t.Fatalf("new.txt = %q after staging, want R.", got)
	}
}
