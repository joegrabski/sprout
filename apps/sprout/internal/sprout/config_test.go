package sprout

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseTOMLFlat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `base_branch = "main"
worktree_root_template = "../trees/{repo}"
auto_launch = false
auto_start_agent = false
launch_nvim = true
launch_lazygit = false
copy_untracked_exclude = ["build", "dist/**"]
update_check = false
agent_command = "aider --model sonnet"
session_prefix = "spr"`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := DefaultConfig()
	if err := parseTOMLFlat(path, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	expectedTools := []string{"agent", "nvim"}
	expectedExclude := []string{"build", "dist/**"}
	if cfg.BaseBranch != "main" || cfg.WorktreeRootTemplate != "../trees/{repo}" || cfg.AutoLaunch || cfg.AutoStartAgent || cfg.UpdateCheck || !cfg.LaunchNvim || cfg.LaunchLazygit || cfg.AgentCommand != "aider --model sonnet" || cfg.SessionPrefix != "spr" || !reflect.DeepEqual(cfg.SessionTools, expectedTools) || !reflect.DeepEqual(cfg.CopyUntrackedExclude, expectedExclude) {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestParseTOMLFlatSessionTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `session_tools = ["agent", "lazygit", "nvim", "pnpm dev"]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := DefaultConfig()
	if err := parseTOMLFlat(path, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	expectedTools := []string{"agent", "lazygit", "nvim", "pnpm dev"}
	if !reflect.DeepEqual(cfg.SessionTools, expectedTools) {
		t.Fatalf("unexpected session tools: got=%v want=%v", cfg.SessionTools, expectedTools)
	}
}

func TestApplyEnvOverridesSessionTools(t *testing.T) {
	t.Setenv("SPROUT_SESSION_TOOLS", "agent, k9s, nvim")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	expectedTools := []string{"agent", "k9s", "nvim"}
	if !reflect.DeepEqual(cfg.SessionTools, expectedTools) {
		t.Fatalf("unexpected session tools from env: got=%v want=%v", cfg.SessionTools, expectedTools)
	}
}

func TestApplyEnvOverridesCopyUntrackedExclude(t *testing.T) {
	t.Setenv("SPROUT_COPY_UNTRACKED_EXCLUDE", "build, dist/**")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	expected := []string{"build", "dist/**"}
	if !reflect.DeepEqual(cfg.CopyUntrackedExclude, expected) {
		t.Fatalf("unexpected copy_untracked_exclude from env: got=%v want=%v", cfg.CopyUntrackedExclude, expected)
	}
}

func TestApplyEnvOverridesUpdateCheck(t *testing.T) {
	t.Setenv("SPROUT_UPDATE_CHECK", "false")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	if cfg.UpdateCheck {
		t.Fatalf("unexpected update_check from env: got=%v want=false", cfg.UpdateCheck)
	}
}
