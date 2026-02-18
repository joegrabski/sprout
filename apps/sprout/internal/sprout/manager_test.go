package sprout

import (
	"os"
	"os/exec"
	"strings"
	"testing"
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
