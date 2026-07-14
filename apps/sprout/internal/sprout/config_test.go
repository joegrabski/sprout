package sprout

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if cfg.BaseBranch != "main" || cfg.WorktreeRootTemplate != "../trees/{repo}" || cfg.AutoLaunch || cfg.AutoStartAgent || cfg.UpdateCheck || !cfg.LaunchNvim || cfg.LaunchLazygit || cfg.AgentCommand != "aider --model sonnet" || cfg.SessionPrefix != "spr" || !reflect.DeepEqual(cfg.SessionTools, expectedTools) {
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

func TestApplyEnvOverridesUpdateCheck(t *testing.T) {
	t.Setenv("SPROUT_UPDATE_CHECK", "false")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	if cfg.UpdateCheck {
		t.Fatalf("unexpected update_check from env: got=%v want=false", cfg.UpdateCheck)
	}
}

func TestParseTOMLFlatDaemonSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `daemon_enabled = false
daemon_socket_path = "/tmp/sproutd.sock"
daemon_refresh_ms = 1500
daemon_stale_after_ms = 5000`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := DefaultConfig()
	if err := parseTOMLFlat(path, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.DaemonEnabled {
		t.Fatalf("expected daemon disabled")
	}
	if cfg.DaemonSocketPath != "/tmp/sproutd.sock" {
		t.Fatalf("unexpected daemon_socket_path: %s", cfg.DaemonSocketPath)
	}
	if cfg.DaemonRefreshMs != 1500 {
		t.Fatalf("unexpected daemon_refresh_ms: %d", cfg.DaemonRefreshMs)
	}
	if cfg.DaemonStaleAfterMs != 5000 {
		t.Fatalf("unexpected daemon_stale_after_ms: %d", cfg.DaemonStaleAfterMs)
	}
}

func TestApplyEnvOverridesDaemonSettings(t *testing.T) {
	t.Setenv("SPROUT_DAEMON_ENABLED", "false")
	t.Setenv("SPROUT_DAEMON_SOCKET", "/tmp/s.sock")
	t.Setenv("SPROUT_DAEMON_REFRESH_MS", "1234")
	t.Setenv("SPROUT_DAEMON_STALE_AFTER_MS", "9876")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	if cfg.DaemonEnabled {
		t.Fatalf("expected daemon disabled")
	}
	if cfg.DaemonSocketPath != "/tmp/s.sock" {
		t.Fatalf("unexpected daemon socket path: %s", cfg.DaemonSocketPath)
	}
	if cfg.DaemonRefreshMs != 1234 {
		t.Fatalf("unexpected daemon refresh ms: %d", cfg.DaemonRefreshMs)
	}
	if cfg.DaemonStaleAfterMs != 9876 {
		t.Fatalf("unexpected daemon stale ms: %d", cfg.DaemonStaleAfterMs)
	}
}

func TestParseTOMLStructuredWindows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[[windows]]
name = "child-main"
role = "agent"

  [[windows.panes]]
  run = "nvim ."
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := DefaultConfig()
	if err := parseTOMLStructured(path, &cfg, "", true); err != nil {
		t.Fatalf("parse structured config: %v", err)
	}
	if len(cfg.Windows) != 1 || cfg.Windows[0].Name != "child-main" {
		t.Fatalf("unexpected child windows: %+v", cfg.Windows)
	}
	if cfg.Windows[0].Role != "agent" {
		t.Fatalf("unexpected child window role: %+v", cfg.Windows[0])
	}
}

func TestParsePreviewConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
preview_session_suffix = "stage"
preview_command_prefix = "portless run"
preview_auto_attach = true

[[preview_windows]]
name = "drive-api"
url = "https://drive-api.localhost"

  [[preview_windows.panes]]
  dir = "{worktree}/src/apis/Evvn"
  run = "dotnet watch --project Evvn.Drive.Api"

[[preview_windows]]
name = "admin"
url = "https://admin.localhost"

  [[preview_windows.panes]]
  dir = "{worktree}/src/apps"
  run = "yarn workspace admin-site start"
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

	if cfg.PreviewSessionSuffix != "stage" {
		t.Fatalf("unexpected preview_session_suffix: %q", cfg.PreviewSessionSuffix)
	}
	if cfg.PreviewCommandPrefix != "portless run" {
		t.Fatalf("unexpected preview_command_prefix: %q", cfg.PreviewCommandPrefix)
	}
	if !cfg.PreviewAutoAttach {
		t.Fatalf("expected preview_auto_attach true")
	}
	if len(cfg.PreviewWindows) != 2 {
		t.Fatalf("expected 2 preview windows, got %d", len(cfg.PreviewWindows))
	}
	if cfg.PreviewWindows[0].Name != "drive-api" || cfg.PreviewWindows[0].URL != "https://drive-api.localhost" {
		t.Fatalf("unexpected first preview window: %+v", cfg.PreviewWindows[0])
	}
	if len(cfg.PreviewWindows[0].Panes) != 1 || cfg.PreviewWindows[0].Panes[0].Dir != "{worktree}/src/apis/Evvn" {
		t.Fatalf("unexpected first preview pane: %+v", cfg.PreviewWindows[0].Panes)
	}
}

func TestParsePreviewSyncConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
preview_tunnel_api = "http://127.0.0.1:9999/api/tunnels"

[[preview_sync]]
file = "{worktree}/src/apps/customer-app/src/config/config.local.json"
reload_windows = ["mobile"]

  [[preview_sync.set]]
  path = "EndpointConfig.DriveApi"
  tunnel = "drive"

  [[preview_sync.set]]
  path = "EndpointConfig.LocationApi"
  tunnel = "functions"
  template = "{url}/add-location-heartbeats"
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

	if cfg.PreviewTunnelAPI != "http://127.0.0.1:9999/api/tunnels" {
		t.Fatalf("unexpected preview_tunnel_api: %q", cfg.PreviewTunnelAPI)
	}
	if len(cfg.PreviewSyncs) != 1 {
		t.Fatalf("expected 1 preview_sync, got %d", len(cfg.PreviewSyncs))
	}
	sync := cfg.PreviewSyncs[0]
	if !strings.HasSuffix(sync.File, "config.local.json") {
		t.Fatalf("unexpected sync file: %q", sync.File)
	}
	if len(sync.ReloadWindows) != 1 || sync.ReloadWindows[0] != "mobile" {
		t.Fatalf("unexpected reload_windows: %+v", sync.ReloadWindows)
	}
	if len(sync.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(sync.Sets))
	}
	if sync.Sets[0].Path != "EndpointConfig.DriveApi" || sync.Sets[0].Tunnel != "drive" {
		t.Fatalf("unexpected first set: %+v", sync.Sets[0])
	}
	if sync.Sets[1].Template != "{url}/add-location-heartbeats" {
		t.Fatalf("unexpected template: %q", sync.Sets[1].Template)
	}
}

func TestApplyEnvOverridesPreviewTunnelAPI(t *testing.T) {
	t.Setenv("SPROUT_PREVIEW_TUNNEL_API", "http://127.0.0.1:1234/api/tunnels")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)
	if cfg.PreviewTunnelAPI != "http://127.0.0.1:1234/api/tunnels" {
		t.Fatalf("unexpected preview tunnel api: %q", cfg.PreviewTunnelAPI)
	}
}

func TestApplyEnvOverridesPreview(t *testing.T) {
	t.Setenv("SPROUT_PREVIEW_SESSION_SUFFIX", "qa")
	t.Setenv("SPROUT_PREVIEW_COMMAND_PREFIX", "portless run")
	t.Setenv("SPROUT_PREVIEW_AUTO_ATTACH", "true")
	cfg := DefaultConfig()
	applyEnvOverrides(&cfg)

	if cfg.PreviewSessionSuffix != "qa" {
		t.Fatalf("unexpected preview suffix: %q", cfg.PreviewSessionSuffix)
	}
	if cfg.PreviewCommandPrefix != "portless run" {
		t.Fatalf("unexpected preview prefix: %q", cfg.PreviewCommandPrefix)
	}
	if !cfg.PreviewAutoAttach {
		t.Fatalf("expected preview auto attach true")
	}
}

func TestPreviewWindowsCommandPrefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PreviewCommandPrefix = "portless run"
	cfg.PreviewWindows = []WindowConfig{
		{Name: "api", Panes: []PaneConfig{{Run: "dotnet watch"}}},
		{Name: "shell", Panes: []PaneConfig{{Run: ""}}},
	}
	m := NewManager(cfg)
	windows := m.previewWindows()

	if got := windows[0].Panes[0].Run; got != "portless run dotnet watch" {
		t.Fatalf("expected prefixed command, got %q", got)
	}
	if got := windows[1].Panes[0].Run; got != "" {
		t.Fatalf("expected empty command untouched, got %q", got)
	}
	// Original config must not be mutated.
	if cfg.PreviewWindows[0].Panes[0].Run != "dotnet watch" {
		t.Fatalf("previewWindows mutated original config: %q", cfg.PreviewWindows[0].Panes[0].Run)
	}
}
