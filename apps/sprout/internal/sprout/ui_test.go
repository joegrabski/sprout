package sprout

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func testModel(t *testing.T, w, h int) *model {
	t.Helper()
	u := newModel(NewManager(DefaultConfig()), t.TempDir())
	u.width, u.height = w, h
	return u
}

func TestViewFillsTerminalExactly(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 40}, {200, 50}, {45, 12}} {
		u := testModel(t, size[0], size[1])
		u.items = []Worktree{
			{Path: "/tmp/repo", Branch: "main", Current: true, TmuxState: "yes"},
			{Path: "/tmp/repo/.worktrees/feat-é", Branch: "feat/ünïcode-branch-with-a-very-long-name", Dirty: true, AgentState: "yes"},
		}
		u.applyFilter()
		for _, focus := range []paneID{paneStatus, paneWorktrees, paneFiles, panePatch} {
			u.focus = focus
			lines := strings.Split(u.View(), "\n")
			if len(lines) != size[1] {
				t.Fatalf("%dx%d focus %d: view has %d lines, want %d", size[0], size[1], focus, len(lines), size[1])
			}
			for i, line := range lines {
				if w := cell(line); w > size[0] {
					t.Fatalf("%dx%d focus %d: line %d is %d cells wide: %q", size[0], size[1], focus, i, w, line)
				}
			}
		}
	}
}

func TestWorktreeRowShowsAgentStatus(t *testing.T) {
	u := testModel(t, 120, 40)
	item := Worktree{Path: "/tmp/repo/.worktrees/feat-x", Branch: "feat/x", AgentState: "yes", TmuxState: "yes", Dirty: true}
	u.items = []Worktree{item}
	u.agentPrompt[item.Path] = agentPromptNeedsInput
	u.applyFilter()

	rows := u.worktreeRow(item, true, true, 60, true)
	if len(rows) != 2 {
		t.Fatalf("two-line row = %d lines, want 2", len(rows))
	}
	plain := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"feat/x", "dirty", "tmux", "needs input", "/tmp/repo/.worktrees/feat-x"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("row missing %q in %q", want, plain)
		}
	}
	for i, r := range rows {
		if w := cell(r); w != 60 {
			t.Fatalf("row line %d is %d cells, want 60", i, w)
		}
	}
	if got := ansi.Strip(strings.Join(u.worktreeRow(item, false, false, 20, false), "")); strings.Contains(got, "needs input") {
		t.Fatalf("narrow row should drop low priority chips, got %q", got)
	}
}

func TestFocusedPaneUsesAccentBorderAndNumber(t *testing.T) {
	u := testModel(t, 120, 40)
	u.focus = paneFiles
	l := u.layout()
	files := ansi.Strip(u.filesView(l.files))
	if !strings.Contains(files, "3 Files") {
		t.Fatalf("files pane missing numbered title: %q", files)
	}
	focused := box("3", "Files", "", nil, 20, 4, true)
	unfocused := box("3", "Files", "", nil, 20, 4, false)
	if focused == unfocused && lipgloss.ColorProfile() != termenv.Ascii {
		t.Fatalf("focused and unfocused boxes should differ")
	}
}

func TestFocusKeys(t *testing.T) {
	u := testModel(t, 120, 40)
	u.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if u.focus != panePatch {
		t.Fatalf("focus = %d after '4', want patch", u.focus)
	}
	u.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if u.focus != paneStatus {
		t.Fatalf("focus = %d after tab, want status", u.focus)
	}
	u.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if u.focus != panePatch {
		t.Fatalf("focus = %d after shift+tab, want patch", u.focus)
	}
}

func TestModalOverlayKeepsFrameSize(t *testing.T) {
	u := testModel(t, 100, 30)
	u.items = []Worktree{{Path: "/tmp/repo", Branch: "main"}}
	u.applyFilter()
	u.openDeleteModal()
	if u.modal == nil {
		t.Fatal("delete modal did not open")
	}
	lines := strings.Split(u.View(), "\n")
	if len(lines) != 30 {
		t.Fatalf("view has %d lines, want 30", len(lines))
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "Remove worktree") {
		t.Fatal("modal content missing from view")
	}
	for i, l := range lines {
		if w := cell(l); w != 100 {
			t.Fatalf("line %d is %d cells, want 100", i, w)
		}
	}
	u.handleKeyThroughModal(tea.KeyMsg{Type: tea.KeyEscape})
	if u.modal != nil {
		t.Fatal("esc should close the modal")
	}
}

func TestFilterKeepsSelectionByPath(t *testing.T) {
	u := testModel(t, 100, 30)
	u.items = []Worktree{{Path: "/a", Branch: "alpha"}, {Path: "/b", Branch: "beta"}, {Path: "/c", Branch: "gamma"}}
	u.applyFilter()
	u.selected = 2
	u.filter = "ta"
	prev := u.selectedItem().Path
	u.applyFilter()
	u.selectPathIfVisible(prev)
	if got := u.selectedItem(); got != nil {
		// gamma doesn't match "ta"; selection clamps to the last match.
		if got.Branch != "beta" {
			t.Fatalf("selected %q, want beta", got.Branch)
		}
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

// handleKeyThroughModal feeds a key to the open modal the way Update does.
func (u *model) handleKeyThroughModal(msg tea.KeyMsg) {
	u.Update(msg)
}

func withFakeTmux(t *testing.T, capture func(target string) string, activity func(target string) (int64, error)) {
	t.Helper()
	prevCapture, prevActivity := tmuxCapturePaneFn, tmuxPaneActivityFn
	tmuxCapturePaneFn = func(target string, lines int, overlay bool) (string, error) { return capture(target), nil }
	tmuxPaneActivityFn = activity
	t.Cleanup(func() { tmuxCapturePaneFn, tmuxPaneActivityFn = prevCapture, prevActivity })
}

func TestAgentSweepClassifiesEveryRunningAgent(t *testing.T) {
	screens := map[string]string{
		"%ready": "doing things\nReady for your next instruction\n>\n\n\n",
		"%input": "I need your input before I continue.\nCan you confirm which path to take?\n\n",
		"%busy":  strings.Repeat("tool call\n", 20),
	}
	withFakeTmux(t,
		func(target string) string { return screens[target] },
		func(string) (int64, error) { return 0, errors.New("no activity info") },
	)
	probe := newAgentProbe()
	items := []Worktree{
		{Path: "/a", AgentState: "yes"},
		{Path: "/b", AgentState: "yes"},
		{Path: "/c", AgentState: "yes"},
		{Path: "/d", AgentState: "no"},
	}
	probe.targets = map[string]string{"/a": "%ready", "/b": "%input", "/c": "%busy", "/d": "%ready"}

	got := probe.sweep(NewManager(DefaultConfig()), "/repo", items)
	want := map[string]agentPromptState{"/a": agentPromptReady, "/b": agentPromptNeedsInput, "/c": agentPromptBusy}
	if len(got) != len(want) {
		t.Fatalf("sweep() = %v, want %v", got, want)
	}
	for path, state := range want {
		if got[path] != state {
			t.Fatalf("sweep()[%s] = %v, want %v", path, got[path], state)
		}
	}
}

func TestAgentSweepSkipsQuietPanes(t *testing.T) {
	captures := 0
	withFakeTmux(t,
		func(string) string { captures++; return "Ready for your next instruction" },
		func(string) (int64, error) { return 1000, nil }, // long ago, and unchanged
	)
	probe := newAgentProbe()
	probe.targets = map[string]string{"/a": "%1"}
	item := Worktree{Path: "/a", AgentState: "yes"}

	if _, changed := probe.classify(NewManager(DefaultConfig()), "/repo", item); !changed {
		t.Fatal("first look should classify")
	}
	if _, changed := probe.classify(NewManager(DefaultConfig()), "/repo", item); changed {
		t.Fatal("unchanged, quiet pane should be skipped")
	}
	if captures != 1 {
		t.Fatalf("captured %d times, want 1", captures)
	}
}

func TestAgentScreenTrimsBlankTailAndKeepsLastRows(t *testing.T) {
	withFakeTmux(t,
		func(string) string { return "one\ntwo\n\x1b[32mthree\x1b[0m\n\n   \n" },
		func(string) (int64, error) { return 0, nil },
	)
	probe := newAgentProbe()
	probe.targets = map[string]string{"/a": "%1"}
	lines, err := probe.screen(NewManager(DefaultConfig()), "/repo", Worktree{Path: "/a", AgentState: "yes"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "two" || !strings.Contains(lines[1], "three") {
		t.Fatalf("screen() = %q, want last two rows", lines)
	}
}

func TestWorktreeRowShowsPRChips(t *testing.T) {
	u := testModel(t, 120, 40)
	u.prs = map[string]PRInfo{
		"feat/a": {Number: 12, State: "OPEN", Checks: "pass"},
		"feat/b": {Number: 13, State: "OPEN", Checks: "fail"},
		"feat/c": {Number: 14, State: "MERGED"},
		"feat/d": {Number: 15, State: "OPEN", Draft: true},
	}
	cases := map[string]string{"feat/a": "#12 ✓", "feat/b": "#13 ✗", "feat/c": "merged #14", "feat/d": "#15 draft"}
	for branch, want := range cases {
		row := ansi.Strip(u.worktreeRow(Worktree{Path: "/x/" + branch, Branch: branch}, false, false, 70, false)[0])
		if !strings.Contains(row, want) {
			t.Errorf("row for %s = %q, want chip %q", branch, row, want)
		}
	}
	if row := ansi.Strip(u.worktreeRow(Worktree{Path: "/x/none", Branch: "feat/none"}, false, false, 70, false)[0]); strings.Contains(row, "#") {
		t.Errorf("row without a PR should have no chip: %q", row)
	}
}

func TestMultiRuneKeyMessagesAreReplayedOneByOne(t *testing.T) {
	u := testModel(t, 120, 40)
	u.items = []Worktree{{Path: "/a", Branch: "a"}, {Path: "/b", Branch: "b"}, {Path: "/c", Branch: "c"}}
	u.applyFilter()
	u.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jj")})
	if u.selected != 2 {
		t.Fatalf("selected = %d after \"jj\", want 2", u.selected)
	}

	// While typing into a text field the run must stay whole.
	u.modal = newFilterModal(u)
	u.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jj")})
	if got := u.modal.(*filterModal).input.Value(); got != "jj" {
		t.Fatalf("filter text = %q, want jj", got)
	}
}

func TestWorktreeKeysAreScopedToTheWorktreePane(t *testing.T) {
	u := testModel(t, 120, 40)
	item := Worktree{Path: "/tmp/repo/wt", Branch: "feat/x"}
	u.items = []Worktree{item}
	u.applyFilter()
	u.diffPath = item.Path
	u.diffItems = []DiffFile{{Path: "a.txt", Status: ".M"}}
	u.diffRows, _ = buildDiffTreeRows(u.diffItems, 0, nil, false)

	key := func(r rune) { u.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }

	u.focus = paneFiles
	key('x')
	if u.modal != nil {
		t.Fatalf("x in the files pane opened %T", u.modal)
	}
	key('d')
	if cm, ok := u.modal.(*confirmModal); !ok || cm.title != "Discard changes" {
		t.Fatalf("d in the files pane opened %T, want the discard dialog", u.modal)
	}
	u.closeModal()

	u.focus = paneWorktrees
	key('d')
	if cm, ok := u.modal.(*confirmModal); !ok || cm.title != "Detach session" {
		t.Fatalf("d in the worktrees pane opened %T, want the detach dialog", u.modal)
	}
}

func TestCommitDialogNeedsStagedFiles(t *testing.T) {
	u := testModel(t, 120, 40)
	item := Worktree{Path: "/tmp/repo/wt", Branch: "feat/x"}
	u.items = []Worktree{item}
	u.applyFilter()
	u.diffPath = item.Path
	u.diffItems = []DiffFile{{Path: "a.txt", Status: ".M"}}
	u.openCommitModal()
	if u.modal != nil || u.msgLevel != levelWarn {
		t.Fatalf("commit with nothing staged: modal=%T level=%q", u.modal, u.msgLevel)
	}
	u.diffItems = []DiffFile{{Path: "a.txt", Status: "M."}, {Path: "b.txt", Status: ".M"}}
	u.openCommitModal()
	cm, ok := u.modal.(*commitModal)
	if !ok || len(cm.staged) != 1 || cm.staged[0] != "a.txt" {
		t.Fatalf("commit dialog = %#v", u.modal)
	}
}
