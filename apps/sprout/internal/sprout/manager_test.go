package sprout

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	if !strings.Contains(got, "my-feature") {
		t.Fatalf("expected worktree path token in session, got %q", got)
	}
}

func TestTmuxWorktreeSessionNameStableAcrossBranchSwitch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionPrefix = "sprout"
	m := NewManager(cfg)

	path := "/tmp/work/dotnet/.worktrees/feat/my-feature"
	before := m.tmuxWorktreeSessionNameFrom("/tmp/work/dotnet", "feat/my feature", path)
	after := m.tmuxWorktreeSessionNameFrom("/tmp/work/dotnet", "main", path)
	if before != after {
		t.Fatalf("expected session name to be stable for worktree path, got before=%q after=%q", before, after)
	}
}

func TestConfiguredAgentWindowName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Windows = []WindowConfig{
		{Name: "editor"},
		{Name: "assistant", Role: "agent"},
	}
	m := NewManager(cfg)
	if got := m.configuredAgentWindowName(); got != "assistant" {
		t.Fatalf("expected configured agent window name, got %q", got)
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

func TestTmuxCommandWithShellFallback(t *testing.T) {
	origShell := os.Getenv("SHELL")
	t.Cleanup(func() {
		_ = os.Setenv("SHELL", origShell)
	})
	_ = os.Setenv("SHELL", "/bin/zsh")

	shellCmd := tmuxCommandWithShellFallback("zsh")
	if shellCmd != "zsh" {
		t.Fatalf("expected shell command passthrough, got %q", shellCmd)
	}

	// Tool commands run under the user's login+interactive shell so their
	// profile (PATH, tool-manager shims) is sourced.
	toolCmd := tmuxCommandWithShellFallback("lazygit -p .")
	if !strings.Contains(toolCmd, "/bin/zsh -l -i -c") {
		t.Fatalf("expected login+interactive zsh wrapper, got %q", toolCmd)
	}
	if !strings.Contains(toolCmd, "lazygit -p .; exec /bin/zsh -i") {
		t.Fatalf("expected command line fallback to shell, got %q", toolCmd)
	}

	// An unrecognised shell falls back to the portable sh -lc wrapper.
	_ = os.Setenv("SHELL", "/usr/bin/fish")
	fishCmd := tmuxCommandWithShellFallback("lazygit -p .")
	if !strings.Contains(fishCmd, "sh -lc") {
		t.Fatalf("expected sh -lc fallback for unknown shell, got %q", fishCmd)
	}
}

func TestMatchesAgentCommandWrappedShell(t *testing.T) {
	candidates := map[string]struct{}{
		"codex": {},
	}
	pane := tmuxPaneInfo{
		CurrentCommand: "zsh",
		StartCommand:   "sh -lc 'codex --full-auto; exec /bin/zsh -i'",
	}
	if !matchesAgentCommand(pane, candidates) {
		t.Fatalf("expected wrapped shell start command to match agent candidate")
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

func TestParsePorcelainV2DiffFiles(t *testing.T) {
	raw := []byte("1 MM N... 100644 100644 100644 abc def file1.txt\x00" +
		"2 R. N... 100644 100644 100644 abc def R100 renamed.txt\x00old.txt\x00" +
		"? untracked.txt\x00" +
		"u UU N... 100644 100644 100644 100644 abc def ghi conflicted.txt\x00")

	files := parsePorcelainV2DiffFiles(raw)
	if len(files) != 4 {
		t.Fatalf("expected 4 files, got %d: %+v", len(files), files)
	}
	if files[0].Path != "file1.txt" || files[0].Status != "MM" {
		t.Fatalf("unexpected tracked file parse: %+v", files[0])
	}
	if files[1].Path != "renamed.txt" || files[1].PreviousPath != "old.txt" || files[1].Status != "R." {
		t.Fatalf("unexpected rename parse: %+v", files[1])
	}
	if files[2].Path != "untracked.txt" || files[2].Status != "??" {
		t.Fatalf("unexpected untracked parse: %+v", files[2])
	}
	if files[3].Path != "conflicted.txt" || files[3].Status != "UU" {
		t.Fatalf("unexpected unmerged parse: %+v", files[3])
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

func runGitBench(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, string(out))
	}
	return string(out)
}

func setupBenchRepoWithWorktrees(t testing.TB, count int) (string, []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for benchmark")
	}

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo failed: %v", err)
	}

	runGitBench(t, repo, "init")
	runGitBench(t, repo, "config", "user.email", "bench@example.com")
	runGitBench(t, repo, "config", "user.name", "Bench")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	runGitBench(t, repo, "add", "README.md")
	runGitBench(t, repo, "commit", "-m", "init")

	base := strings.TrimSpace(runGitBench(t, repo, "branch", "--show-current"))
	if base == "" {
		base = "main"
	}

	paths := []string{repo}
	for i := 0; i < count; i++ {
		branch := "feat/bench-" + strconv.Itoa(i)
		wtPath := filepath.Join(root, "wt-"+strconv.Itoa(i))
		runGitBench(t, repo, "worktree", "add", "-b", branch, wtPath, base)
		paths = append(paths, wtPath)
		if i%2 == 0 {
			_ = os.WriteFile(filepath.Join(wtPath, fmt.Sprintf("dirty-%d.tmp", i)), []byte("dirty\n"), 0o644)
		}
	}

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

	return repo, paths
}

func BenchmarkManagerListWorktrees(b *testing.B) {
	_, _ = setupBenchRepoWithWorktrees(b, 8)
	m := NewManager(DefaultConfig())
	commandExistsCache = sync.Map{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := m.ListWorktrees()
		if err != nil {
			b.Fatalf("ListWorktrees failed: %v", err)
		}
		if len(items) < 2 {
			b.Fatalf("expected multiple worktrees, got %d", len(items))
		}
	}
}

func BenchmarkWorktreeDirty(b *testing.B) {
	_, paths := setupBenchRepoWithWorktrees(b, 6)
	m := NewManager(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.WorktreeDirty(paths[i%len(paths)])
	}
}

func BenchmarkRefreshRepoChoicesForce(b *testing.B) {
	if _, err := exec.LookPath("git"); err != nil {
		b.Skip("git is required for benchmark")
	}
	root := b.TempDir()
	repoCount := 8
	var firstRepo string
	for i := 0; i < repoCount; i++ {
		repo := filepath.Join(root, fmt.Sprintf("repo-%d", i))
		if err := os.MkdirAll(repo, 0o755); err != nil {
			b.Fatalf("mkdir repo failed: %v", err)
		}
		runGitBench(b, repo, "init")
		runGitBench(b, repo, "config", "user.email", "bench@example.com")
		runGitBench(b, repo, "config", "user.name", "Bench")
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
			b.Fatalf("write file failed: %v", err)
		}
		runGitBench(b, repo, "add", "README.md")
		runGitBench(b, repo, "commit", "-m", "init")
		if i == 0 {
			firstRepo = repo
		}
	}

	u := &tuiState{repoRoot: firstRepo}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.lastRepoScan = time.Time{}
		u.refreshRepoChoices(true)
		if len(u.repos) == 0 {
			b.Fatal("expected repos")
		}
	}
}

func trashCount(t testing.TB, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), sproutTrashPrefix) {
			n++
		}
	}
	return n
}

// TestRemoveAsync verifies that an async removal detaches the worktree instantly
// (dir gone, git entry pruned) and that its files are reaped in the background.
func TestRemoveAsync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved // git records the resolved path (macOS /var -> /private/var)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGitBench(t, repo, "init")
	runGitBench(t, repo, "config", "user.email", "t@example.com")
	runGitBench(t, repo, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGitBench(t, repo, "add", "README.md")
	runGitBench(t, repo, "commit", "-m", "init")
	base := strings.TrimSpace(runGitBench(t, repo, "branch", "--show-current"))
	if base == "" {
		base = "main"
	}

	wts := filepath.Join(root, "wts")
	if err := os.MkdirAll(wts, 0o755); err != nil {
		t.Fatalf("mkdir wts: %v", err)
	}
	wtPath := filepath.Join(wts, "feat-x")
	runGitBench(t, repo, "worktree", "add", "-b", "feat/x", wtPath, base)
	if err := os.WriteFile(filepath.Join(wtPath, "big.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}

	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	cfg := DefaultConfig()
	cfg.WorktreeRootTemplate = wts
	m := NewManager(cfg)

	path, _, err := m.Remove(RemoveOptions{Target: wtPath, Force: true, Async: true})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if path != wtPath {
		t.Errorf("returned path = %q, want %q", path, wtPath)
	}

	// The worktree directory is gone immediately (renamed aside).
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree still present after async remove: err=%v", err)
	}
	// git no longer lists it (pruned).
	if list := runGitBench(t, repo, "worktree", "list", "--porcelain"); strings.Contains(list, wtPath) {
		t.Errorf("worktree not pruned from git:\n%s", list)
	}

	// The background reaper eventually clears the trash under the worktree root.
	deadline := time.Now().Add(5 * time.Second)
	for trashCount(t, wts) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("trash not reaped within timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSweepDeletedTrash verifies leftover trash is reaped while real worktree
// directories are left untouched.
func TestSweepDeletedTrash(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wts := filepath.Join(root, "wts")
	for _, d := range []string{repo, wts} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	trash := filepath.Join(wts, sproutTrashPrefix+"feat-old.123")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trash, "junk.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	keep := filepath.Join(wts, "feat-live")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatalf("mkdir keep: %v", err)
	}

	cfg := DefaultConfig()
	cfg.WorktreeRootTemplate = wts
	m := NewManager(cfg)
	m.SweepDeletedTrash(repo)

	deadline := time.Now().Add(5 * time.Second)
	for trashCount(t, wts) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("trash not swept within timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("live worktree dir was removed by sweep: %v", err)
	}
}
