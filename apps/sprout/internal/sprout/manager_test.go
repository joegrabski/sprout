package sprout

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	m := NewManager(DefaultConfig())
	got, err := m.Slugify("Checkout Redesign_v2")
	if err != nil {
		t.Fatalf("Slugify returned error: %v", err)
	}
	if got != "checkout-redesign-v2" {
		t.Fatalf("unexpected slug: %q", got)
	}
}

func TestMakeBranchName(t *testing.T) {
	m := NewManager(DefaultConfig())
	got, err := m.MakeBranchName("feat", "my feature")
	if err != nil {
		t.Fatalf("MakeBranchName returned error: %v", err)
	}
	if got != "feat/my-feature" {
		t.Fatalf("unexpected branch name: %q", got)
	}

	if _, err := m.MakeBranchName("unknown", "x"); err == nil {
		t.Fatalf("expected invalid type error")
	}
}

func TestTmuxAgentWindowName(t *testing.T) {
	m := NewManager(DefaultConfig())
	got := m.tmuxAgentWindowName("feat/some very long branch name with spaces and symbols !@# and extra suffix")
	if !strings.HasPrefix(got, "agent-") {
		t.Fatalf("expected agent- prefix, got %q", got)
	}
	if len(got) > 60 {
		t.Fatalf("expected max length <=60, got %d", len(got))
	}
}

func TestTmuxSessionName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionPrefix = "sprout"
	m := NewManager(cfg)

	got := m.tmuxSessionName("/tmp/work/dotnet")
	if strings.Contains(got, ":") {
		t.Fatalf("session name must not contain ':', got %q", got)
	}
	if got != "sprout-dotnet" {
		t.Fatalf("unexpected session name: %q", got)
	}
}

func TestTmuxWorktreeSessionName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionPrefix = "sprout"
	m := NewManager(cfg)

	wt := &Worktree{Branch: "feat/my feature", Path: "/tmp/work/dotnet/.worktrees/feat/my-feature"}
	got := m.tmuxWorktreeSessionName("/tmp/work/dotnet", wt)
	if strings.Contains(got, ":") {
		t.Fatalf("session name must not contain ':', got %q", got)
	}
	if !strings.HasPrefix(got, "sprout-dotnet-") {
		t.Fatalf("expected repo-prefixed worktree session, got %q", got)
	}
	if !strings.Contains(got, "feat-my-feature") {
		t.Fatalf("expected branch token in session, got %q", got)
	}
}

func TestTmuxConfiguredWindows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentCommand = "codex --full-auto"
	cfg.SessionTools = []string{"agent", "lazygit", "nvim", "pnpm dev"}
	m := NewManager(cfg)

	windows := m.tmuxConfiguredWindows("feat/my feature", func(name string) bool {
		return name == "nvim"
	})

	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d (%+v)", len(windows), windows)
	}
	if !strings.HasPrefix(windows[0].Name, "agent-") || windows[0].Command != "codex --full-auto" {
		t.Fatalf("unexpected agent window: %+v", windows[0])
	}
	if windows[1].Command != "nvim ." {
		t.Fatalf("unexpected nvim window: %+v", windows[1])
	}
	if windows[2].Name != "tool-pnpm" || windows[2].Command != "pnpm dev" {
		t.Fatalf("unexpected custom window: %+v", windows[2])
	}
}

func TestTmuxConfiguredWindowsUniqueNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionTools = []string{"npm run dev", "npm test"}
	m := NewManager(cfg)

	windows := m.tmuxConfiguredWindows("feat/my feature", func(name string) bool {
		return true
	})

	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d (%+v)", len(windows), windows)
	}
	if windows[0].Name != "tool-npm" {
		t.Fatalf("unexpected first window name: %q", windows[0].Name)
	}
	if windows[1].Name != "tool-npm-2" {
		t.Fatalf("unexpected second window name: %q", windows[1].Name)
	}
}

func TestResolvePaneDir(t *testing.T) {
	worktree := "/tmp/repo.worktrees/feat/x"

	got := resolvePaneDir("src/apps/web", worktree)
	want := "/tmp/repo.worktrees/feat/x/src/apps/web"
	if got != want {
		t.Fatalf("resolvePaneDir relative = %q, want %q", got, want)
	}

	got = resolvePaneDir("{worktree}/src/apis", worktree)
	want = "/tmp/repo.worktrees/feat/x/src/apis"
	if got != want {
		t.Fatalf("resolvePaneDir {worktree} = %q, want %q", got, want)
	}

	got = resolvePaneDir("/opt/tools", worktree)
	want = "/opt/tools"
	if got != want {
		t.Fatalf("resolvePaneDir absolute = %q, want %q", got, want)
	}
}

func TestCommandShouldRemainOnExit(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "", want: false},
		{command: "bash", want: false},
		{command: "/bin/zsh -l", want: false},
		{command: "fish", want: false},
		{command: "nvim .", want: true},
		{command: "lazygit -p .", want: true},
		{command: "pnpm dev", want: true},
		{command: "codex --full-auto", want: true},
	}

	for _, tc := range tests {
		got := commandShouldRemainOnExit(tc.command)
		if got != tc.want {
			t.Fatalf("commandShouldRemainOnExit(%q) = %t, want %t", tc.command, got, tc.want)
		}
	}
}

func TestCopyUntrackedExcludeMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CopyUntrackedExclude = []string{"build", "dist/**", "*.log", "tmp/"}
	m := NewManager(cfg)

	tests := []struct {
		rel  string
		want bool
	}{
		{rel: "build", want: true},
		{rel: "build/output/app.dll", want: true},
		{rel: "dist/assets/app.js", want: true},
		{rel: "tmp/cache", want: true},
		{rel: "logs/app.log", want: true},
		{rel: "notes/logs.txt", want: false},
		{rel: "src/build/output", want: false},
		{rel: "builds/app", want: false},
	}

	for _, tc := range tests {
		if got := m.shouldExcludeCopyPath(tc.rel); got != tc.want {
			t.Fatalf("shouldExcludeCopyPath(%q) = %t, want %t", tc.rel, got, tc.want)
		}
	}
}

func TestShouldRetryWorktreeAdd(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "git worktree add timed out after 45s", want: true},
		{msg: "fatal: branch is already checked out at '/tmp/wt'", want: true},
		{msg: "fatal: cannot lock ref", want: true},
		{msg: "fatal: invalid reference", want: false},
	}

	for _, tc := range tests {
		got := shouldRetryWorktreeAdd(errors.New(tc.msg))
		if got != tc.want {
			t.Fatalf("shouldRetryWorktreeAdd(%q) = %t, want %t", tc.msg, got, tc.want)
		}
	}
}

func TestShouldRetryWorktreeRemove(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "git worktree remove timed out after 45s", want: true},
		{msg: "fatal: '/tmp/wt' is locked", want: true},
		{msg: "fatal: cannot lock ref", want: true},
		{msg: "fatal: not a working tree", want: false},
	}

	for _, tc := range tests {
		got := shouldRetryWorktreeRemove(errors.New(tc.msg))
		if got != tc.want {
			t.Fatalf("shouldRetryWorktreeRemove(%q) = %t, want %t", tc.msg, got, tc.want)
		}
	}
}

func TestGitWorktreeCommandTimeout(t *testing.T) {
	orig := os.Getenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS")
		} else {
			_ = os.Setenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS", orig)
		}
	})

	_ = os.Setenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS", "2")
	if got := gitWorktreeCommandTimeout(); got != 5*time.Second {
		t.Fatalf("expected min-clamped timeout, got %s", got)
	}

	_ = os.Setenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS", "120")
	if got := gitWorktreeCommandTimeout(); got != 120*time.Second {
		t.Fatalf("expected explicit timeout, got %s", got)
	}

	_ = os.Setenv("SPROUT_GIT_WORKTREE_TIMEOUT_SECONDS", "10000")
	if got := gitWorktreeCommandTimeout(); got != 600*time.Second {
		t.Fatalf("expected max-clamped timeout, got %s", got)
	}
}

func TestEstimateCopyPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/a.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.MkdirAll(root+"/nested", 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(root+"/nested/b.txt", []byte("world!"), 0o644); err != nil {
		t.Fatalf("write nested file failed: %v", err)
	}

	files, bytes, err := estimateCopyPath(root)
	if err != nil {
		t.Fatalf("estimateCopyPath failed: %v", err)
	}
	if files != 2 {
		t.Fatalf("expected 2 files, got %d", files)
	}
	if bytes < 11 {
		t.Fatalf("expected at least 11 bytes, got %d", bytes)
	}
}

func TestNewWorktreeFromExistingReturnsExistingWorktreePath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo failed: %v", err)
	}

	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}

	run(repo, "init")
	run(repo, "config", "user.email", "sprout-test@example.com")
	run(repo, "config", "user.name", "Sprout Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	run(repo, "add", "README.md")
	run(repo, "commit", "-m", "init")
	run(repo, "checkout", "-b", "feature/existing")
	run(repo, "checkout", "-")

	existingPath := filepath.Join(parent, "existing-worktree")
	run(repo, "worktree", "add", existingPath, "feature/existing")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	cfg := DefaultConfig()
	m := NewManager(cfg)
	branch, gotPath, err := m.NewWorktree(NewOptions{FromBranch: "feature/existing", Launch: false})
	if err != nil {
		t.Fatalf("NewWorktree failed: %v", err)
	}
	if branch != "feature/existing" {
		t.Fatalf("unexpected branch: %q", branch)
	}
	resolve := func(p string) string {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return absPath(real)
		}
		return absPath(p)
	}
	if resolve(gotPath) != resolve(existingPath) {
		t.Fatalf("expected existing path %q, got %q", resolve(existingPath), resolve(gotPath))
	}
}

func TestParsePorcelainStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		stage rune
		work  rune
	}{
		{name: "unstaged only", input: " M", stage: ' ', work: 'M'},
		{name: "staged only", input: "M ", stage: 'M', work: ' '},
		{name: "both changed", input: "MM", stage: 'M', work: 'M'},
		{name: "untracked", input: "??", stage: '?', work: '?'},
		{name: "empty", input: "", stage: ' ', work: ' '},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stage, work := parsePorcelainStatus(tc.input)
			if stage != tc.stage || work != tc.work {
				t.Fatalf("parsePorcelainStatus(%q) = (%q,%q), want (%q,%q)", tc.input, stage, work, tc.stage, tc.work)
			}
		})
	}
}

func TestWorktreeDiffForFile_UntrackedShowsPatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	repo := t.TempDir()
	initCmd := exec.Command("git", "init")
	initCmd.Dir = repo
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	if err := os.WriteFile(repo+"/newfile.txt", []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	m := NewManager(DefaultConfig())
	diff, err := m.WorktreeDiffForFile(repo, DiffFile{Path: "newfile.txt", Status: "??"}, 120)
	if err != nil {
		t.Fatalf("WorktreeDiffForFile failed: %v", err)
	}
	if !strings.Contains(diff, "# Unstaged") {
		t.Fatalf("expected unstaged section, got: %q", diff)
	}
	if strings.Contains(diff, "stage it to view a patch") {
		t.Fatalf("expected patch content for untracked file, got fallback message: %q", diff)
	}
	if !strings.Contains(diff, "newfile.txt") {
		t.Fatalf("expected file name in diff, got: %q", diff)
	}
}
