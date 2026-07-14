package sprout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetJSONPath(t *testing.T) {
	root := map[string]any{}
	changed, err := setJSONPath(root, "EndpointConfig.DriveApi", "https://drive")
	if err != nil {
		t.Fatalf("nested create: %v", err)
	}
	if !changed {
		t.Fatal("expected create to report a change")
	}
	ec, ok := root["EndpointConfig"].(map[string]any)
	if !ok || ec["DriveApi"] != "https://drive" {
		t.Fatalf("nested value not set: %+v", root)
	}

	// Overwrite an existing leaf.
	changed, err = setJSONPath(root, "EndpointConfig.DriveApi", "https://drive2")
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if !changed {
		t.Fatal("expected overwrite to report a change")
	}
	if ec = root["EndpointConfig"].(map[string]any); ec["DriveApi"] != "https://drive2" {
		t.Fatalf("overwrite failed: %+v", ec)
	}

	// Writing the same value again reports no change.
	changed, err = setJSONPath(root, "EndpointConfig.DriveApi", "https://drive2")
	if err != nil {
		t.Fatalf("noop write: %v", err)
	}
	if changed {
		t.Fatal("expected no change when value is identical")
	}

	// Crossing a non-object value is an error.
	root["Scalar"] = "x"
	if _, err := setJSONPath(root, "Scalar.Nested", "y"); err == nil {
		t.Fatalf("expected error crossing non-object")
	}
}

func TestApplyPreviewSyncWritesAndTemplates(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.local.json")
	orig := `{
  "EndpointConfig": {
    "DriveApi": "https://old",
    "LocationApi": "https://old/add-location-heartbeats"
  }
}
`
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	sync := PreviewSync{
		File: file,
		Sets: []PreviewSyncSet{
			{Path: "EndpointConfig.DriveApi", Tunnel: "drive"},
			{Path: "EndpointConfig.LocationApi", Tunnel: "functions", Template: "{url}/add-location-heartbeats"},
		},
	}
	tunnels := map[string]string{
		"drive":     "https://drive.ngrok.app",
		"functions": "https://functions.ngrok.app",
	}

	changed, warnings, err := applyPreviewSync(file, sync, tunnels)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !changed {
		t.Fatal("expected file to change")
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	var got map[string]map[string]string
	raw, _ := os.ReadFile(file)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["EndpointConfig"]["DriveApi"] != "https://drive.ngrok.app" {
		t.Fatalf("DriveApi not written: %v", got)
	}
	if got["EndpointConfig"]["LocationApi"] != "https://functions.ngrok.app/add-location-heartbeats" {
		t.Fatalf("template not applied: %v", got)
	}

	// Re-applying with the same tunnels is a no-op (no write).
	changed, _, err = applyPreviewSync(file, sync, tunnels)
	if err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if changed {
		t.Fatal("expected no change on idempotent re-apply")
	}
}

func TestApplyPreviewSyncMissingTunnelWarns(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.local.json")
	if err := os.WriteFile(file, []byte(`{"EndpointConfig":{"DriveApi":"https://keep"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sync := PreviewSync{
		File: file,
		Sets: []PreviewSyncSet{{Path: "EndpointConfig.DriveApi", Tunnel: "drive"}},
	}

	changed, warnings, err := applyPreviewSync(file, sync, map[string]string{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if changed {
		t.Fatal("expected no change when tunnel missing")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "drive") {
		t.Fatalf("expected missing-tunnel warning, got: %v", warnings)
	}
}

func TestSyncPreviewConfigsResolvesWorktree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), []byte(`{"EndpointConfig":{"DriveApi":"https://old"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(DefaultConfig())
	m.Cfg.PreviewSyncs = []PreviewSync{{
		File: "{worktree}/config.local.json",
		Sets: []PreviewSyncSet{{Path: "EndpointConfig.DriveApi", Tunnel: "drive"}},
	}}
	m.tunnelFetch = func(apiURL string) (map[string]string, error) {
		return map[string]string{"drive": "https://drive.ngrok.app"}, nil
	}

	warnings, err := m.syncPreviewConfigs("", dir, true)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "config.local.json"))
	if !strings.Contains(string(raw), "https://drive.ngrok.app") {
		t.Fatalf("worktree file not synced: %s", raw)
	}
}
