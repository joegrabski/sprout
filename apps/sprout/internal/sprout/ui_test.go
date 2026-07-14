package sprout

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTerminalNeedsReseed(t *testing.T) {
	size := paneSize{w: 80, h: 24}
	state := agentTerminalViewState{
		paneTarget:   "%1",
		streamOffset: 42,
		size:         size,
	}

	tests := []struct {
		name        string
		state       agentTerminalViewState
		paneTarget  string
		size        paneSize
		streamReset bool
		want        bool
	}{
		{name: "stable state does not reseed", state: state, paneTarget: "%1", size: size, want: false},
		{name: "pane target changed", state: state, paneTarget: "%2", size: size, want: true},
		{name: "terminal size changed", state: state, paneTarget: "%1", size: paneSize{w: 100, h: 24}, want: true},
		{name: "stream reset requests reseed", state: state, paneTarget: "%1", size: size, streamReset: true, want: true},
		{name: "explicit reset requests reseed", state: agentTerminalViewState{paneTarget: "%1", size: size, needsReset: true}, paneTarget: "%1", size: size, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := agentTerminalNeedsReseed(tc.state, tc.paneTarget, tc.size, tc.streamReset)
			if got != tc.want {
				t.Fatalf("agentTerminalNeedsReseed() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestReadFileChunkRollbackOnTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pane.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	chunk, err := readFileChunk(path, 0)
	if err != nil {
		t.Fatalf("readFileChunk() error = %v", err)
	}
	if string(chunk.data) != "hello" {
		t.Fatalf("readFileChunk() data = %q, want %q", string(chunk.data), "hello")
	}
	if chunk.nextOffset != 5 {
		t.Fatalf("readFileChunk() nextOffset = %d, want 5", chunk.nextOffset)
	}
	if chunk.reset {
		t.Fatalf("readFileChunk() reset = true, want false")
	}

	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile() truncate error = %v", err)
	}

	chunk, err = readFileChunk(path, 5)
	if err != nil {
		t.Fatalf("readFileChunk() after truncate error = %v", err)
	}
	if string(chunk.data) != "hi" {
		t.Fatalf("readFileChunk() after truncate data = %q, want %q", string(chunk.data), "hi")
	}
	if chunk.nextOffset != 2 {
		t.Fatalf("readFileChunk() after truncate nextOffset = %d, want 2", chunk.nextOffset)
	}
	if !chunk.reset {
		t.Fatalf("readFileChunk() after truncate reset = false, want true")
	}
}

func TestSyncAgentTerminalMirrorFallsBackToSnapshotWhenStreamMissing(t *testing.T) {
	state := agentTerminalViewState{
		paneTarget:   "%1",
		streamOffset: 12,
		size:         paneSize{w: 80, h: 24},
	}

	result, err := syncAgentTerminalMirror(state, "%1", state.size, 40, agentTerminalSyncDeps{
		ensureLog: func(reset bool) (string, error) {
			return "/tmp/missing.log", nil
		},
		readChunk: func(path string, offset int64) (fileChunkResult, error) {
			return fileChunkResult{nextOffset: offset}, os.ErrNotExist
		},
		capture: func(lines int) (string, error) {
			return "snapshot output", nil
		},
	})
	if err != nil {
		t.Fatalf("syncAgentTerminalMirror() error = %v", err)
	}
	if !result.reset {
		t.Fatalf("syncAgentTerminalMirror() reset = false, want true")
	}
	if string(result.data) != "snapshot output" {
		t.Fatalf("syncAgentTerminalMirror() data = %q, want snapshot", string(result.data))
	}
	if !result.nextState.needsReset {
		t.Fatalf("syncAgentTerminalMirror() nextState.needsReset = false, want true")
	}
	if result.nextState.paneTarget != "%1" {
		t.Fatalf("syncAgentTerminalMirror() nextState.paneTarget = %q, want %%1", result.nextState.paneTarget)
	}
}

func TestSyncAgentTerminalMirrorPrefersStreamBootstrapWhenControlBytesExist(t *testing.T) {
	stream := []byte("\x1b[31mhello\x1b[0m\r\n")
	result, err := syncAgentTerminalMirror(agentTerminalViewState{}, "%1", paneSize{w: 80, h: 24}, 40, agentTerminalSyncDeps{
		ensureLog: func(reset bool) (string, error) {
			return "/tmp/pane.log", nil
		},
		readChunk: func(path string, offset int64) (fileChunkResult, error) {
			return fileChunkResult{data: stream, nextOffset: int64(len(stream))}, nil
		},
		capture: func(lines int) (string, error) {
			return "snapshot output", nil
		},
	})
	if err != nil {
		t.Fatalf("syncAgentTerminalMirror() error = %v", err)
	}
	if !result.reset {
		t.Fatalf("syncAgentTerminalMirror() reset = false, want true")
	}
	if string(result.data) != string(stream) {
		t.Fatalf("syncAgentTerminalMirror() data = %q, want stream bootstrap", string(result.data))
	}
	if result.nextState.streamOffset != int64(len(stream)) {
		t.Fatalf("syncAgentTerminalMirror() nextState.streamOffset = %d, want %d", result.nextState.streamOffset, len(stream))
	}
}

func TestSyncAgentTerminalMirrorReturnsStreamErrorsWhenSnapshotFails(t *testing.T) {
	wantErr := errors.New("stream read failed")
	_, err := syncAgentTerminalMirror(agentTerminalViewState{}, "%1", paneSize{w: 80, h: 24}, 40, agentTerminalSyncDeps{
		ensureLog: func(reset bool) (string, error) {
			return "/tmp/pane.log", nil
		},
		readChunk: func(path string, offset int64) (fileChunkResult, error) {
			return fileChunkResult{}, wantErr
		},
		capture: func(lines int) (string, error) {
			return "", errors.New("capture failed")
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("syncAgentTerminalMirror() error = %v, want %v", err, wantErr)
	}
}

func TestUpdatePaneFocusStylesTreatsAgentTerminalAsDetailFocus(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(DefaultConfig())
	u := newTUI(mgr, dir)
	u.app.SetFocus(u.agentTerm)
	u.updatePaneFocusStyles()

	if got := u.detailPane.GetTitle(); got != "> [2]-Details" {
		t.Fatalf("detailPane title = %q, want focused details title", got)
	}
	if got := u.detailPane.GetBorderColor(); got != paneFocusColor() {
		t.Fatalf("detailPane border color = %v, want %v", got, paneFocusColor())
	}
}

func TestRenderAgentStatusSummary(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(DefaultConfig())
	u := newTUI(mgr, dir)
	item := &Worktree{Path: "/tmp/repo/.worktrees/feat-x", Branch: "feat/x", AgentState: "yes"}
	u.agentPrompt[item.Path] = agentPromptReady

	summary := u.renderAgentStatusSummary(item, "%12", 1, nil)
	for _, want := range []string{
		"Status: ready",
		"Branch: feat/x",
		"Pane: %12",
		"Actions:",
		"enter or g: attach to the worktree tmux session",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("renderAgentStatusSummary() missing %q in %q", want, summary)
		}
	}
}

func TestRenderTableIncludesAgentStatus(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(DefaultConfig())
	u := newTUI(mgr, dir)
	u.items = []Worktree{{Path: "/tmp/repo/.worktrees/feat-x", Branch: "feat/x", AgentState: "yes", TmuxState: "yes"}}
	u.agentPrompt["/tmp/repo/.worktrees/feat-x"] = agentPromptNeedsInput
	u.applyFilter()
	u.renderTable()

	for col, want := range []string{"CUR", "BRANCH", "STATUS", "TMUX", "AGENT", "PATH"} {
		if got := u.table.GetCell(0, col).Text; got != want {
			t.Fatalf("header col %d = %q, want %q", col, got, want)
		}
	}
	if got := u.table.GetCell(1, 4).Text; got != "needs input" {
		t.Fatalf("agent cell = %q, want needs input", got)
	}
}

func TestAgentPromptStateForOutputNeedsInput(t *testing.T) {
	output := "I need your input before I continue.\nCan you confirm which path to take?"
	if got := agentPromptStateForOutput(output); got != agentPromptNeedsInput {
		t.Fatalf("agentPromptStateForOutput() = %v, want needs input", got)
	}
}

func TestAgentPromptStateForOutputReady(t *testing.T) {
	output := "Ready for your next instruction"
	if got := agentPromptStateForOutput(output); got != agentPromptReady {
		t.Fatalf("agentPromptStateForOutput() = %v, want ready", got)
	}
}
