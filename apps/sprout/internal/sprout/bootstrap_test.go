package sprout

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBootstrapConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
bootstrap_links = ["src/apps/customer-app/src/config/config.local.json", "src/apis/Evvn/Evvn.Drive.Api/appsettings.Local.json"]

[[bootstrap]]
dir = "{worktree}/src/apps"
run = "yarn install --immutable"

[[bootstrap]]
dir = "{worktree}/src/apis/Evvn"
run = "dotnet restore"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := DefaultConfig()
	if err := parseTOMLFlat(path, &cfg); err != nil {
		t.Fatalf("parse flat config: %v", err)
	}
	if err := parseTOMLStructured(path, &cfg, "", true); err != nil {
		t.Fatalf("parse structured config: %v", err)
	}

	if len(cfg.BootstrapLinks) != 2 {
		t.Fatalf("expected 2 bootstrap_links, got %d", len(cfg.BootstrapLinks))
	}
	if len(cfg.BootstrapSteps) != 2 || cfg.BootstrapSteps[0].Run != "yarn install --immutable" {
		t.Fatalf("unexpected bootstrap steps: %+v", cfg.BootstrapSteps)
	}
	if cfg.BootstrapSteps[1].Dir != "{worktree}/src/apis/Evvn" {
		t.Fatalf("unexpected bootstrap step dir: %+v", cfg.BootstrapSteps[1])
	}
	if !NewManager(cfg).HasBootstrap() {
		t.Fatalf("expected HasBootstrap() to be true")
	}
}

func TestBootstrapSymlinks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	run(repo, "init")
	run(repo, "config", "user.email", "t@e.com")
	run(repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	run(repo, "add", "README.md")
	run(repo, "commit", "-m", "init")

	// Create a gitignored local config only in the main checkout.
	cfgDir := filepath.Join(repo, "app", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "local.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	// Add a worktree that lacks the local config.
	wt := filepath.Join(parent, "wt-feature")
	run(repo, "worktree", "add", wt, "-b", "feature")

	cfg := DefaultConfig()
	cfg.BootstrapLinks = []string{"app/config/local.json", "app/config/missing.json"}
	m := NewManager(cfg)

	res, err := m.bootstrapWorktree(repo, wt, nil, nil)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(res.LinksCreated) != 1 || res.LinksCreated[0] != "app/config/local.json" {
		t.Fatalf("expected one link created, got %+v", res.LinksCreated)
	}
	if len(res.LinksMissing) != 1 || res.LinksMissing[0] != "app/config/missing.json" {
		t.Fatalf("expected one missing link, got %+v", res.LinksMissing)
	}

	linked := filepath.Join(wt, "app", "config", "local.json")
	target, err := os.Readlink(linked)
	if err != nil {
		t.Fatalf("expected a symlink at %s: %v", linked, err)
	}
	wantSrc, _ := filepath.EvalSymlinks(filepath.Join(repo, "app", "config", "local.json"))
	gotSrc, _ := filepath.EvalSymlinks(target)
	if gotSrc != wantSrc {
		t.Fatalf("symlink resolves to %q, want %q", gotSrc, wantSrc)
	}

	// Idempotent: a second run skips the existing link.
	res2, err := m.bootstrapWorktree(repo, wt, nil, nil)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if len(res2.LinksCreated) != 0 || len(res2.LinksSkipped) != 1 {
		t.Fatalf("expected skip on second run, got created=%v skipped=%v", res2.LinksCreated, res2.LinksSkipped)
	}
}
