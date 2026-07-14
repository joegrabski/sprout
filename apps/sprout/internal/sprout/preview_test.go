package sprout

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTmuxPreviewSessionName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionPrefix = "sprout"
	m := NewManager(cfg)

	got := m.tmuxPreviewSessionName("/tmp/work/dotnet")
	if got != "sprout-dotnet-preview" {
		t.Fatalf("unexpected preview session name: %q", got)
	}

	cfg.PreviewSessionSuffix = "stage"
	m = NewManager(cfg)
	if got := m.tmuxPreviewSessionName("/tmp/work/dotnet"); got != "sprout-dotnet-stage" {
		t.Fatalf("unexpected preview session name with custom suffix: %q", got)
	}
}

func TestWindowResolvesIdentically(t *testing.T) {
	const oldPath = "/work/wt/old"
	const newPath = "/work/wt/new"

	cases := []struct {
		name string
		win  WindowConfig
		want bool
	}{
		{
			name: "fixed absolute dir is worktree-independent",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "/srv/mobile", Run: "npx expo start"}}},
			want: true,
		},
		{
			name: "home dir is worktree-independent",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "~/shared/api", Run: "dotnet watch"}}},
			want: true,
		},
		{
			name: "worktree placeholder is worktree-bound",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "{worktree}/app", Run: "npm run dev"}}},
			want: false,
		},
		{
			name: "relative dir resolves against worktree",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "src/api", Run: "go run ."}}},
			want: false,
		},
		{
			name: "empty dir runs at worktree root",
			win:  WindowConfig{Panes: []PaneConfig{{Run: "make dev"}}},
			want: false,
		},
		{
			name: "no panes is never identical",
			win:  WindowConfig{},
			want: false,
		},
		{
			name: "mixed panes are bound if any differs",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "/srv/mobile"}, {Dir: "{worktree}/app"}}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowResolvesIdentically(tc.win, oldPath, newPath); got != tc.want {
				t.Fatalf("windowResolvesIdentically(%+v) = %v, want %v", tc.win, got, tc.want)
			}
		})
	}

	// Promoting the same worktree (RestartPreview path uses Force, but guard the
	// identity short-circuit anyway) is always identical.
	same := WindowConfig{Panes: []PaneConfig{{Dir: "{worktree}/app"}}}
	if !windowResolvesIdentically(same, oldPath, oldPath) {
		t.Fatalf("expected identical when old and new path match")
	}
}

func TestWindowIsWorktreeIndependent(t *testing.T) {
	cases := []struct {
		name string
		win  WindowConfig
		want bool
	}{
		{
			name: "home dir tunnel is independent",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "~", Run: "ngrok start --all"}}},
			want: true,
		},
		{
			name: "absolute dir is independent",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "/srv/api", Run: "dotnet watch"}}},
			want: true,
		},
		{
			name: "worktree placeholder is bound",
			win:  WindowConfig{Panes: []PaneConfig{{Dir: "{worktree}/app", Run: "yarn start"}}},
			want: false,
		},
		{
			name: "empty dir runs at worktree root",
			win:  WindowConfig{Panes: []PaneConfig{{Run: "make dev"}}},
			want: false,
		},
		{
			name: "no panes is bound",
			win:  WindowConfig{},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowIsWorktreeIndependent(tc.win); got != tc.want {
				t.Fatalf("windowIsWorktreeIndependent(%+v) = %v, want %v", tc.win, got, tc.want)
			}
		})
	}
}

func TestPollTunnelsFailsFastWhenNotJustStarted(t *testing.T) {
	m := NewManager(DefaultConfig())
	required := map[string]bool{"drive": true}
	calls := 0
	fetch := func(apiURL string) (map[string]string, error) {
		calls++
		return map[string]string{}, nil // tunnel never appears
	}

	start := time.Now()
	tunnels, err := m.pollTunnels(fetch, "http://x", required, previewTunnelFastTimeout)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("pollTunnels: %v", err)
	}
	if _, ok := tunnels["drive"]; ok {
		t.Fatalf("did not expect the missing tunnel to resolve")
	}
	// Fast path must not block anywhere near the full poll timeout.
	if elapsed >= previewTunnelPollTimeout {
		t.Fatalf("fast poll took too long: %v", elapsed)
	}
	if calls == 0 {
		t.Fatalf("expected at least one fetch attempt")
	}
}

func TestPollTunnelsReturnsImmediatelyWhenPresent(t *testing.T) {
	m := NewManager(DefaultConfig())
	required := map[string]bool{"drive": true}
	fetch := func(apiURL string) (map[string]string, error) {
		return map[string]string{"drive": "https://drive.ngrok.app"}, nil
	}
	start := time.Now()
	tunnels, err := m.pollTunnels(fetch, "http://x", required, previewTunnelPollTimeout)
	if err != nil {
		t.Fatalf("pollTunnels: %v", err)
	}
	if tunnels["drive"] != "https://drive.ngrok.app" {
		t.Fatalf("unexpected tunnels: %+v", tunnels)
	}
	// Even with the full timeout, a resolved tunnel returns on the first fetch.
	if elapsed := time.Since(start); elapsed >= previewTunnelPollEvery {
		t.Fatalf("resolved poll should return immediately, took %v", elapsed)
	}
}

func TestPreviewStateRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	run("init")
	run("config", "user.email", "sprout-test@example.com")
	run("config", "user.name", "Sprout Test")

	m := NewManager(DefaultConfig())

	// No state file yet → nil, no error.
	st, err := m.readPreviewState(repo)
	if err != nil {
		t.Fatalf("read empty preview state: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil preview state, got %+v", st)
	}

	want := PreviewState{Branch: "feature/x", Path: "/tmp/wt/x", PromotedAtUnixMs: 1700000000000}
	if err := m.writePreviewState(repo, want); err != nil {
		t.Fatalf("write preview state: %v", err)
	}

	// Verify it lands in the git common dir (shared across worktrees).
	statePath, err := m.previewStatePath(repo)
	if err != nil {
		t.Fatalf("preview state path: %v", err)
	}
	if !strings.Contains(statePath, filepath.Join(".git", "sprout", "preview.json")) {
		t.Fatalf("unexpected preview state path: %q", statePath)
	}

	got, err := m.readPreviewState(repo)
	if err != nil {
		t.Fatalf("read preview state: %v", err)
	}
	if got == nil || got.Branch != want.Branch || got.Path != want.Path || got.PromotedAtUnixMs != want.PromotedAtUnixMs {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}

	if err := m.clearPreviewState(repo); err != nil {
		t.Fatalf("clear preview state: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected state file removed, stat err: %v", err)
	}
	// Clearing again is a no-op.
	if err := m.clearPreviewState(repo); err != nil {
		t.Fatalf("clear preview state twice: %v", err)
	}
}

func TestShouldReuseRunningService(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	// A closed endpoint: bind a listener then close it so nothing answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadURL := "http://" + ln.Addr().String()
	_ = ln.Close()

	independent := []PaneConfig{{Dir: "~", Run: "ngrok start --all"}}
	bound := []PaneConfig{{Dir: "{worktree}", Run: "task api:dev"}}

	m := NewManager(DefaultConfig())
	cases := []struct {
		name string
		win  WindowConfig
		want bool
	}{
		{"independent + live url → reuse", WindowConfig{Panes: independent, URL: live.URL}, true},
		{"independent + dead url → rebuild", WindowConfig{Panes: independent, URL: deadURL}, false},
		{"independent + no url → rebuild", WindowConfig{Panes: independent}, false},
		{"worktree-bound + live url → rebuild", WindowConfig{Panes: bound, URL: live.URL}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.shouldReuseRunningService(tc.win); got != tc.want {
				t.Errorf("shouldReuseRunningService = %v, want %v", got, tc.want)
			}
		})
	}
}
