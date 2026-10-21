package sprout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type paneID int

const (
	paneStatus paneID = iota
	paneWorktrees
	paneFiles
	panePatch
	paneCount
)

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return r.w > 0 && r.h > 0 && x >= r.x && y >= r.y && x < r.x+r.w && y < r.y+r.h
}

type uiLayout struct {
	status, worktrees, files, patch rect
}

// model is the Bubble Tea model for the sprout TUI. Everything slow (git,
// tmux, daemon) runs in tea.Cmds and reports back through typed messages.
type model struct {
	mgr        *Manager
	repoName   string
	repoRoot   string
	repoSlug   string
	repoBranch string

	width, height int
	focus         paneID
	modal         modal
	send          func(tea.Msg) // Program.Send; nil in tests

	items    []Worktree
	visible  []int
	selected int
	listTop  int
	rowH     int
	filter   string

	repos     []repoChoice
	repoCache map[string]repoChoice
	lastScan  time.Time

	previewWorktreePath string
	updateLatest        string

	// diff
	diffs         *diffStore
	diffSeq       int64
	diffCancel    context.CancelFunc
	diffPath      string
	diffItems     []DiffFile
	diffSel       int
	diffTree      bool
	diffRows      []diffTreeRow
	diffRowSel    int
	diffTop       int
	diffCollapsed map[string]bool
	diffLoaded    bool
	patchLines    []string
	patchNote     []string
	patchKey      string
	patchTop      int
	patchX        int
	patchW        int

	// agent prompt state
	agentPrompt map[string]agentPromptState
	probe       *agentProbe
	sweeping    bool

	// GitHub
	prs            map[string]PRInfo
	prsBusy        bool
	prsUnavailable bool

	// pane 4 tabs
	tab             detailTab
	agentScreen     []string
	agentScreenPath string
	agentScreenErr  error
	screenBusy      bool

	// footer
	msgLevel string
	msgText  string
	busy     int
	spin     spinner.Model
	ticks    int
}

func newModel(mgr *Manager, repoRoot string) *model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = styleUIAccent
	return &model{
		mgr:           mgr,
		repoName:      mgr.RepoName(repoRoot),
		repoRoot:      repoRoot,
		focus:         paneWorktrees,
		tab:           detailTabDiff,
		rowH:          1,
		repoCache:     map[string]repoChoice{},
		diffs:         newDiffStore(),
		diffTree:      true,
		diffCollapsed: map[string]bool{},
		agentPrompt:   map[string]agentPromptState{},
		probe:         newAgentProbe(),
		spin:          sp,
	}
}

// RunUI starts the TUI and returns the process exit code.
func RunUI(mgr *Manager) int {
	repoStage := startProfileStage("ui_require_repo")
	repoRoot, err := mgr.RequireRepo()
	repoStage()
	if err != nil {
		fmt.Println("error: run this command inside a git worktree")
		return 1
	}

	daemonStage := startProfileStage("ui_daemon_ensure")
	daemonEnsureForUI(mgr.Cfg)
	daemonStage()

	// Reap any leftover trash from a prior async removal that didn't finish.
	go mgr.SweepDeletedTrash(repoRoot)

	m := newModel(mgr, repoRoot)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.send = p.Send
	if _, err := p.Run(); err != nil {
		fmt.Printf("error: ui failed: %v\n", err)
		return 1
	}
	return 0
}

func (u *model) Init() tea.Cmd {
	return tea.Batch(
		u.refreshCmd(refreshReq{}),
		u.prsCmd(),
		tickCmd(),
		func() tea.Msg {
			time.Sleep(250 * time.Millisecond)
			if latest, ok := checkForUpdate(Version, u.mgr.Cfg); ok {
				return updateAvailableMsg{latest: latest}
			}
			return nil
		},
	)
}

// ---- messages / footer ----------------------------------------------------

func (u *model) setMsg(level, format string, args ...any) {
	u.msgLevel = level
	u.msgText = fmt.Sprintf(format, args...)
}
func (u *model) setStatus(format string, args ...any) { u.setMsg(levelStatus, format, args...) }
func (u *model) setInfo(format string, args ...any)   { u.setMsg(levelInfo, format, args...) }
func (u *model) setWarn(format string, args ...any)   { u.setMsg(levelWarn, format, args...) }
func (u *model) setError(format string, args ...any)  { u.setMsg(levelError, format, args...) }

// startOp marks a background operation as running and shows a spinner.
func (u *model) startOp(format string, args ...any) tea.Cmd {
	u.setStatus(format, args...)
	u.busy++
	if u.busy == 1 {
		return u.spin.Tick
	}
	return nil
}

// ---- update ---------------------------------------------------------------

func (u *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		u.width, u.height = msg.Width, msg.Height
		if w := u.patchInner().w; w != u.patchW {
			u.patchW = w
			return u, u.requestDiff(true)
		}
		return u, nil

	case tea.KeyMsg:
		// Keys that arrive in one read (key repeat, fast typing) come through
		// as a single multi-rune message. Replay them one by one unless a text
		// field has focus and wants the whole run.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && !u.textInputFocused() {
			cmds := make([]tea.Cmd, 0, len(msg.Runes))
			for _, r := range msg.Runes {
				_, cmd := u.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
			}
			return u, tea.Batch(cmds...)
		}
		if u.modal != nil {
			return u, u.modal.update(u, msg)
		}
		return u, u.handleKey(msg)

	case tea.MouseMsg:
		if u.modal != nil {
			return u, nil
		}
		return u, u.handleMouse(msg)

	case refreshedMsg:
		return u, u.applyRefresh(msg)

	case diffLoadedMsg:
		u.applyDiff(msg)
		return u, nil

	case agentStatesMsg:
		u.sweeping = false
		if msg.repoRoot == u.repoRoot {
			for path, state := range msg.states {
				u.setAgentState(path, state)
			}
		}
		return u, nil

	case agentScreenMsg:
		u.screenBusy = false
		if msg.repoRoot != u.repoRoot {
			return u, nil
		}
		if it := u.selectedItem(); it != nil && it.Path == msg.path {
			u.agentScreenPath = msg.path
			u.agentScreen, u.agentScreenErr = msg.lines, msg.err
		}
		return u, nil

	case tickMsg:
		return u, u.onTick()

	case workItemsMsg:
		if cm, ok := u.modal.(*createModal); ok && cm == msg.modal {
			cm.setWorkItems(msg.items, msg.err)
		}
		return u, nil

	case prsMsg:
		u.prsBusy = false
		if msg.repoRoot != u.repoRoot {
			return u, nil
		}
		switch {
		case errors.Is(msg.err, errGHUnavailable):
			u.prsUnavailable = true
			u.prs = nil
		case msg.err != nil:
			debugLogf("ui_prs failed: %v", msg.err) // keep the last good data
		default:
			u.prsUnavailable = false
			u.prs = msg.prs
		}
		return u, nil

	case noticeMsg:
		u.setMsg(msg.level, "%s", msg.text)
		return u, nil

	case opDoneMsg:
		if u.busy > 0 {
			u.busy--
		}
		u.setMsg(msg.level, "%s", msg.text)
		if msg.refresh {
			u.diffs.clear()
			return u, u.refreshCmd(refreshReq{fresh: msg.fresh, selectPath: msg.selectPath})
		}
		return u, nil

	case execDoneMsg:
		if msg.err != nil {
			u.setError("%s failed: %v", msg.what, msg.err)
			return u, u.refreshCmd(refreshReq{fresh: true})
		}
		if msg.what == "commit" {
			u.setInfo("committed in %s", msg.path)
		} else {
			u.setInfo("%s: %s", msg.what, msg.path)
		}
		u.diffs.clear()
		return u, u.refreshCmd(refreshReq{fresh: msg.what != "attach"})

	case commitTTYMsg:
		return u, u.commitWithTerminal(msg)

	case updateAvailableMsg:
		u.updateLatest = msg.latest
		u.setWarn("update available: %s (current %s)", msg.latest, Version)
		return u, nil

	case spinner.TickMsg:
		if u.busy <= 0 {
			return u, nil
		}
		var cmd tea.Cmd
		u.spin, cmd = u.spin.Update(msg)
		return u, cmd
	}
	return u, nil
}

// textInputFocused reports whether an open dialog is collecting text.
func (u *model) textInputFocused() bool {
	tm, ok := u.modal.(interface{ takesText() bool })
	return ok && tm.takesText()
}

func (u *model) onTick() tea.Cmd {
	u.ticks++
	cmds := []tea.Cmd{tickCmd()}
	if u.modal == nil {
		if u.ticks%2 == 0 {
			cmds = append(cmds, u.sweepCmd())
		}
		cmds = append(cmds, u.screenCmd())
		// Keep the diff and worktree list fresh without hammering git.
		if u.ticks%8 == 0 {
			cmds = append(cmds, u.requestDiff(true))
		}
		if u.ticks%25 == 0 {
			cmds = append(cmds, u.refreshCmd(refreshReq{}))
		}
		if u.ticks%225 == 0 {
			cmds = append(cmds, u.prsCmd())
		}
	}
	return tea.Batch(cmds...)
}

// ---- refresh --------------------------------------------------------------

func (u *model) refreshCmd(req refreshReq) tea.Cmd {
	req.mgr = u.mgr
	req.repoRoot = u.repoRoot
	req.skipRepos = len(u.repos) > 0 && time.Since(u.lastScan) < 5*time.Second
	req.repoCache = make(map[string]repoChoice, len(u.repoCache))
	for k, v := range u.repoCache {
		req.repoCache[k] = v
	}
	return req.run
}

func (u *model) applyRefresh(msg refreshedMsg) tea.Cmd {
	if msg.err != nil {
		u.setError("refresh failed: %v", msg.err)
		return nil
	}
	prevPath := ""
	if it := u.selectedItem(); it != nil {
		prevPath = it.Path
	}

	u.items = msg.items
	u.previewWorktreePath = msg.previewDir
	u.repoBranch = msg.repoBranch
	if msg.repos != nil {
		u.repos, u.repoSlug, u.repoCache = msg.repos, msg.repoSlug, msg.repoCache
		u.lastScan = time.Now()
	}

	alive := map[string]bool{}
	for _, it := range msg.items {
		alive[it.Path] = true
		if it.AgentState != "yes" {
			delete(u.agentPrompt, it.Path)
			u.probe.forget(it.Path)
		}
	}
	for path := range u.agentPrompt {
		if !alive[path] {
			delete(u.agentPrompt, path)
		}
	}

	u.applyFilter()
	want := msg.selectPath
	if want == "" {
		want = prevPath
	}
	u.selectPathIfVisible(want)

	if msg.notice != "" {
		u.setMsg(msg.level, "%s", msg.notice)
	}

	return tea.Batch(u.requestDiff(true), u.sweepCmd(), u.screenCmd())
}

func (u *model) applyFilter() {
	u.visible = u.visible[:0]
	q := strings.ToLower(strings.TrimSpace(u.filter))
	for i, item := range u.items {
		if q == "" || strings.Contains(strings.ToLower(item.Branch+" "+item.Path), q) {
			u.visible = append(u.visible, i)
		}
	}
	u.selected = max(0, min(u.selected, len(u.visible)-1))
}

func (u *model) selectPathIfVisible(path string) {
	if path == "" {
		return
	}
	for pos, idx := range u.visible {
		if u.items[idx].Path == path {
			u.selected = pos
			return
		}
	}
}

func (u *model) selectedItem() *Worktree {
	if len(u.visible) == 0 || u.selected < 0 || u.selected >= len(u.visible) {
		return nil
	}
	item := u.items[u.visible[u.selected]]
	return &item
}

func (u *model) setAgentState(path string, state agentPromptState) {
	for _, it := range u.items {
		if it.Path == path && it.AgentState == "yes" {
			u.agentPrompt[path] = state
			return
		}
	}
}

// prsCmd refreshes pull request and CI info, one request at a time.
func (u *model) prsCmd() tea.Cmd {
	if u.prsBusy {
		return nil
	}
	u.prsBusy = true
	root := u.repoRoot
	return func() tea.Msg {
		prs, err := fetchPRs(context.Background(), root)
		return prsMsg{repoRoot: root, prs: prs, err: err}
	}
}

// sweepCmd re-reads every running agent's prompt state, one sweep at a time.
func (u *model) sweepCmd() tea.Cmd {
	if u.sweeping {
		return nil
	}
	running := false
	for _, it := range u.items {
		if it.AgentState == "yes" {
			running = true
			break
		}
	}
	if !running {
		return nil
	}
	u.sweeping = true
	mgr, root, probe := u.mgr, u.repoRoot, u.probe
	items := append([]Worktree(nil), u.items...)
	return func() tea.Msg {
		return agentStatesMsg{repoRoot: root, states: probe.sweep(mgr, root, items)}
	}
}

// screenCmd captures the selected agent's screen while the Agent tab is open.
func (u *model) screenCmd() tea.Cmd {
	item := u.selectedItem()
	if u.tab != detailTabAgent || item == nil || item.AgentState != "yes" || u.screenBusy {
		return nil
	}
	u.screenBusy = true
	mgr, root, probe, it, rows := u.mgr, u.repoRoot, u.probe, *item, max(10, u.patchInner().h)
	return func() tea.Msg {
		lines, err := probe.screen(mgr, root, it, rows)
		return agentScreenMsg{repoRoot: root, path: it.Path, lines: lines, err: err}
	}
}

func (u *model) agentLabel(item Worktree) string {
	if item.AgentState != "yes" {
		return item.AgentState
	}
	switch u.agentPrompt[item.Path] {
	case agentPromptNeedsInput:
		return "needs input"
	case agentPromptReady:
		return "ready"
	case agentPromptBusy:
		return "busy"
	}
	return "yes"
}

// ---- diff state -----------------------------------------------------------

func (u *model) patchInner() rect {
	l := u.layout()
	return rect{l.patch.x + 1, l.patch.y + 1, max(0, l.patch.w-2), max(0, l.patch.h-2)}
}

// requestDiff loads the file list and patch for the selected worktree/file.
// silent keeps whatever is on screen until the new data arrives.
func (u *model) requestDiff(silent bool) tea.Cmd {
	u.diffSeq++
	seq := u.diffSeq
	if u.diffCancel != nil {
		u.diffCancel()
		u.diffCancel = nil
	}
	item := u.selectedItem()
	if item == nil {
		u.diffPath = ""
		u.diffItems, u.diffRows, u.diffSel, u.diffRowSel = nil, nil, 0, 0
		u.diffLoaded = true
		u.patchLines = nil
		u.patchNote = []string{styleUIDim.Render("select a worktree to see its changes")}
		return nil
	}
	if item.Path != u.diffPath {
		u.diffSel, u.diffRowSel, u.diffTop = 0, 0, 0
		if !silent || !u.diffs.has(item.Path) {
			u.diffLoaded = false
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.diffCancel = cancel
	mgr, store, path, sel, width := u.mgr, u.diffs, item.Path, u.diffSel, u.patchInner().w
	return func() tea.Msg { return loadDiff(ctx, seq, mgr, store, path, sel, width) }
}

func (u *model) applyDiff(msg diffLoadedMsg) {
	if msg.seq != u.diffSeq {
		return
	}
	u.diffLoaded = true
	if msg.err != nil {
		u.diffPath = msg.path
		u.diffItems, u.diffRows, u.diffSel, u.diffRowSel = nil, nil, 0, 0
		u.patchLines = nil
		u.patchNote = []string{
			styleUIDim.Render("unable to read git diff"),
			"",
			ellipsize(strings.TrimSpace(msg.err.Error()), 80),
		}
		return
	}

	prevKey := ""
	if u.diffRowSel >= 0 && u.diffRowSel < len(u.diffRows) {
		prevKey = u.diffRows[u.diffRowSel].key
	}
	switched := msg.path != u.diffPath
	u.diffPath = msg.path
	u.diffItems = msg.files
	if len(msg.files) == 0 {
		u.diffSel, u.diffRowSel, u.diffRows = 0, 0, nil
		u.patchLines = nil
		u.patchKey = ""
		u.patchNote = []string{
			styleUIAccent.Render("✓"),
			"",
			styleUIDim.Render("working tree clean"),
		}
		return
	}
	u.diffSel = max(0, min(msg.selected, len(msg.files)-1))
	rows, selRow := buildDiffTreeRows(u.diffItems, u.diffSel, u.diffCollapsed, u.diffTree)
	u.diffRows = rows
	u.diffRowSel = selRow
	if !switched && prevKey != "" {
		for i, r := range rows {
			if r.key == prevKey {
				u.diffRowSel = i
				break
			}
		}
	}
	if row := u.diffRows[u.diffRowSel]; row.fileIndex < 0 {
		u.showDirectoryNote(row)
		return
	}
	u.setPatch(msg.text, msg.path+"\x00"+u.diffItems[u.diffSel].Path)
}

func (u *model) setPatch(text, key string) {
	text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", "    ")
	text = strings.ReplaceAll(text, "\r", "")
	u.patchLines = strings.Split(text, "\n")
	u.patchNote = nil
	if key != u.patchKey {
		u.patchTop, u.patchX = 0, 0
	}
	u.patchKey = key
	u.clampPatch()
}

func (u *model) showDirectoryNote(row diffTreeRow) {
	prefix := strings.TrimSuffix(row.key, "/")
	if prefix != "" {
		prefix += "/"
	}
	count := 0
	for _, f := range u.diffItems {
		if strings.HasPrefix(f.Path, prefix) {
			count++
		}
	}
	state := "expanded"
	if u.diffCollapsed[row.key] {
		state = "collapsed"
	}
	u.patchLines = nil
	u.patchKey = "dir:" + row.key
	u.patchNote = []string{
		styleUIBold.Render(row.label),
		styleUIDim.Render(state),
		"",
		fmt.Sprintf("%d changed %s", count, plural(count, "file", "files")),
		"",
		keyHint("enter", "toggle"),
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (u *model) patchViewHeight() int { return max(1, u.patchInner().h) }

func (u *model) clampPatch() {
	u.patchTop = max(0, min(u.patchTop, len(u.patchLines)-u.patchViewHeight()))
	u.patchX = max(0, u.patchX)
}

func (u *model) scrollPatch(delta int) {
	u.patchTop += delta
	u.clampPatch()
}

// ---- diff file list navigation -------------------------------------------

func (u *model) moveDiffRow(delta int) tea.Cmd {
	if len(u.diffRows) == 0 {
		return nil
	}
	return u.selectDiffRow(u.diffRowSel + delta)
}

func (u *model) selectDiffRow(idx int) tea.Cmd {
	if len(u.diffRows) == 0 {
		return nil
	}
	idx = max(0, min(idx, len(u.diffRows)-1))
	if idx == u.diffRowSel {
		return nil
	}
	u.diffRowSel = idx
	row := u.diffRows[idx]
	if row.fileIndex >= 0 {
		u.diffSel = row.fileIndex
		return u.requestDiff(true)
	}
	// Directory rows have nothing to fetch; drop any in-flight patch.
	u.diffSeq++
	if u.diffCancel != nil {
		u.diffCancel()
		u.diffCancel = nil
	}
	u.showDirectoryNote(row)
	return nil
}

func (u *model) toggleDiffDirectory() {
	if u.diffRowSel < 0 || u.diffRowSel >= len(u.diffRows) {
		return
	}
	row := u.diffRows[u.diffRowSel]
	if !row.isDir || strings.TrimSpace(row.key) == "" {
		return
	}
	u.diffCollapsed[row.key] = !u.diffCollapsed[row.key]
	u.rebuildDiffRows(row.key)
	if u.diffRowSel < len(u.diffRows) && u.diffRows[u.diffRowSel].isDir {
		u.showDirectoryNote(u.diffRows[u.diffRowSel])
	}
}

func (u *model) collapseAllDiffDirectories(collapsed bool) {
	prev := ""
	if u.diffRowSel < len(u.diffRows) {
		prev = u.diffRows[u.diffRowSel].key
	}
	all, _ := buildDiffTreeRows(u.diffItems, u.diffSel, map[string]bool{}, true)
	u.diffCollapsed = map[string]bool{}
	if collapsed {
		for _, r := range all {
			if r.isDir && r.key != "" {
				u.diffCollapsed[r.key] = true
			}
		}
	}
	u.rebuildDiffRows(prev)
	if u.diffRowSel < len(u.diffRows) && u.diffRows[u.diffRowSel].isDir {
		u.showDirectoryNote(u.diffRows[u.diffRowSel])
	}
}

func (u *model) toggleDiffTree() tea.Cmd {
	u.diffTree = !u.diffTree
	u.rebuildDiffRows("")
	if u.diffRowSel < len(u.diffRows) {
		if row := u.diffRows[u.diffRowSel]; row.fileIndex >= 0 {
			u.diffSel = row.fileIndex
			return u.requestDiff(true)
		}
	}
	return nil
}

// rebuildDiffRows recomputes the visible rows and re-selects keepKey (or the
// selected file when keepKey is empty/gone).
func (u *model) rebuildDiffRows(keepKey string) {
	rows, selRow := buildDiffTreeRows(u.diffItems, u.diffSel, u.diffCollapsed, u.diffTree)
	u.diffRows = rows
	u.diffRowSel = selRow
	if keepKey != "" {
		for i, r := range rows {
			if r.key == keepKey {
				u.diffRowSel = i
				break
			}
		}
	}
	u.diffRowSel = max(0, min(u.diffRowSel, len(rows)-1))
}

// ---- staging, discarding, committing -----------------------------------------

// selectedFiles returns the files under the selected row (a file, or every
// file below a directory) and the worktree they belong to.
func (u *model) selectedFiles() ([]DiffFile, string) {
	item := u.selectedItem()
	if item == nil || u.diffPath != item.Path || u.diffRowSel < 0 || u.diffRowSel >= len(u.diffRows) {
		return nil, ""
	}
	row := u.diffRows[u.diffRowSel]
	if row.fileIndex >= 0 {
		return []DiffFile{u.diffItems[row.fileIndex]}, item.Path
	}
	prefix := strings.TrimSuffix(row.key, "/") + "/"
	var files []DiffFile
	for _, f := range u.diffItems {
		if strings.HasPrefix(f.Path, prefix) {
			files = append(files, f)
		}
	}
	return files, item.Path
}

// needsStaging reports whether any file still has unstaged or untracked changes.
func needsStaging(files []DiffFile) bool {
	for _, f := range files {
		if st := parseFileStage(f.Status); st.unstaged {
			return true
		}
	}
	return false
}

func (u *model) toggleStageSelected() tea.Cmd {
	files, dir := u.selectedFiles()
	if len(files) == 0 {
		return nil
	}
	return u.stageFiles(dir, files, needsStaging(files))
}

func (u *model) toggleStageAll() tea.Cmd {
	item := u.selectedItem()
	if item == nil || u.diffPath != item.Path || len(u.diffItems) == 0 {
		return nil
	}
	return u.stageFiles(item.Path, u.diffItems, needsStaging(u.diffItems))
}

func (u *model) stageFiles(dir string, files []DiffFile, stage bool) tea.Cmd {
	mgr := u.mgr
	files = append([]DiffFile(nil), files...)
	verb, done := "unstaging", "unstaged"
	if stage {
		verb, done = "staging", "staged"
	}
	label := fmt.Sprintf("%d %s", len(files), plural(len(files), "file", "files"))
	if len(files) == 1 {
		label = filepath.Base(files[0].Path)
	}
	spin := u.startOp("%s %s…", verb, label)
	run := func() tea.Msg {
		var err error
		if stage {
			err = mgr.StageFiles(dir, files)
		} else {
			err = mgr.UnstageFiles(dir, files)
		}
		if err != nil {
			return opDoneMsg{level: levelError, refresh: true, text: err.Error()}
		}
		return opDoneMsg{level: levelInfo, refresh: true, text: fmt.Sprintf("%s %s", done, label)}
	}
	return tea.Batch(spin, run)
}

func (u *model) discardFiles(dir string, files []DiffFile) tea.Cmd {
	mgr := u.mgr
	files = append([]DiffFile(nil), files...)
	label := fmt.Sprintf("%d %s", len(files), plural(len(files), "file", "files"))
	if len(files) == 1 {
		label = filepath.Base(files[0].Path)
	}
	spin := u.startOp("discarding %s…", label)
	run := func() tea.Msg {
		if err := mgr.DiscardFiles(dir, files); err != nil {
			return opDoneMsg{level: levelError, refresh: true, text: err.Error()}
		}
		return opDoneMsg{level: levelWarn, refresh: true, text: fmt.Sprintf("discarded %s", label)}
	}
	return tea.Batch(spin, run)
}

// commitTTYMsg asks the UI to hand the terminal over for a commit that may
// prompt (GPG signing).
type commitTTYMsg struct{ dir, message string }

func (u *model) commitCmd(dir, message string) tea.Cmd {
	mgr := u.mgr
	spin := u.startOp("committing…")
	run := func() tea.Msg {
		if commitNeedsTerminal(dir) {
			return commitTTYMsg{dir: dir, message: message}
		}
		summary, err := mgr.CommitStaged(dir, message)
		if err != nil {
			return opDoneMsg{level: levelError, refresh: true, text: "commit failed: " + err.Error()}
		}
		return opDoneMsg{level: levelInfo, refresh: true, text: "committed: " + summary}
	}
	return tea.Batch(spin, run)
}

func (u *model) commitWithTerminal(msg commitTTYMsg) tea.Cmd {
	if u.busy > 0 {
		u.busy--
	}
	run := &funcExec{fn: func() error {
		cmd := exec.Command("git", "commit", "-m", msg.message)
		cmd.Dir = msg.dir
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}}
	return tea.Exec(run, func(err error) tea.Msg {
		return execDoneMsg{err: err, path: filepath.Base(msg.dir), what: "commit"}
	})
}

// ---- worktree list navigation --------------------------------------------

func (u *model) moveSelection(delta int) tea.Cmd { return u.selectIndex(u.selected + delta) }

func (u *model) selectIndex(idx int) tea.Cmd {
	if len(u.visible) == 0 {
		return nil
	}
	idx = max(0, min(idx, len(u.visible)-1))
	if idx == u.selected {
		return nil
	}
	u.selected = idx
	u.agentScreen, u.agentScreenErr = nil, nil
	return tea.Batch(u.requestDiff(true), u.screenCmd())
}

// ---- layout ---------------------------------------------------------------

const minWideWidth = 100

func (u *model) layout() uiLayout {
	w, h := u.width, u.height
	bodyH := h - 1
	var l uiLayout
	stackH := func(x, w int) (rect, rect, rect) {
		rest := bodyH - 3
		wt := rest * 45 / 100
		if rest < 12 {
			wt = rest / 2
		}
		return rect{x, 0, w, 3}, rect{x, 3, w, wt}, rect{x, 3 + wt, w, rest - wt}
	}
	if w >= minWideWidth {
		leftW := max(38, min(w*2/5, 64))
		l.status, l.worktrees, l.files = stackH(0, leftW)
		l.patch = rect{leftW, 0, w - leftW, bodyH}
		return l
	}
	if u.focus == panePatch {
		l.patch = rect{0, 0, w, bodyH}
		return l
	}
	l.status, l.worktrees, l.files = stackH(0, w)
	return l
}

func (u *model) paneAt(x, y int) (paneID, bool) {
	l := u.layout()
	switch {
	case l.status.contains(x, y):
		return paneStatus, true
	case l.worktrees.contains(x, y):
		return paneWorktrees, true
	case l.files.contains(x, y):
		return paneFiles, true
	case l.patch.contains(x, y):
		return panePatch, true
	}
	return 0, false
}

func (u *model) setFocus(p paneID) tea.Cmd {
	u.focus = p
	if u.width < minWideWidth {
		// The narrow layout swaps the patch in for the stack; re-measure it.
		if w := u.patchInner().w; w != u.patchW {
			u.patchW = w
			return u.requestDiff(true)
		}
	}
	return nil
}

func (u *model) cycleFocus(delta int) tea.Cmd {
	next := (int(u.focus) + delta + int(paneCount)) % int(paneCount)
	if u.width < minWideWidth && next == int(panePatch) && delta < 0 {
		next = int(paneFiles)
	}
	return u.setFocus(paneID(next))
}

// ---- keys -----------------------------------------------------------------

func (u *model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	case "tab":
		return u.cycleFocus(1)
	case "shift+tab":
		return u.cycleFocus(-1)
	case "1":
		return u.setFocus(paneStatus)
	case "2":
		return u.setFocus(paneWorktrees)
	case "3":
		return u.setFocus(paneFiles)
	case "4":
		return u.setFocus(panePatch)
	case "?":
		u.modal = newHelpModal(u)
		return nil
	case "[", "]":
		return u.toggleTab()
	case "R":
		u.diffs.clear()
		return tea.Batch(u.refreshCmd(refreshReq{fresh: true, level: levelInfo, notice: "refreshed"}), u.prsCmd())
	case "n":
		return u.openCreateModal()
	case "/":
		u.modal = newFilterModal(u)
		return nil
	}

	switch u.focus {
	case paneStatus:
		if key == "enter" {
			u.openRepoModal()
		}
	case paneWorktrees:
		return u.worktreesKey(key)
	case paneFiles:
		return u.filesKey(key)
	case panePatch:
		return u.patchPaneKey(key)
	}
	return nil
}

func (u *model) worktreesKey(key string) tea.Cmd {
	switch key {
	case "j", "down":
		return u.moveSelection(1)
	case "k", "up":
		return u.moveSelection(-1)
	case "ctrl+d", "pgdown":
		return u.moveSelection(max(1, u.worktreeCapacity()-1))
	case "ctrl+u", "pgup":
		return u.moveSelection(-max(1, u.worktreeCapacity()-1))
	case "home":
		return u.selectIndex(0)
	case "end":
		return u.selectIndex(len(u.visible) - 1)
	case "enter", "g":
		return u.goCurrent()
	case "o":
		return u.openPR()
	case "p":
		return u.promoteSelectedPreview()
	case "x":
		u.openDeleteModal()
	case "d":
		u.openDetachModal()
	case "esc":
		if u.filter != "" {
			u.filter = ""
			u.applyFilter()
			u.setInfo("filter cleared")
			return u.requestDiff(true)
		}
	}
	return nil
}

func (u *model) filesKey(key string) tea.Cmd {
	switch key {
	case "j", "down":
		return u.moveDiffRow(1)
	case "k", "up":
		return u.moveDiffRow(-1)
	case "g", "home":
		return u.selectDiffRow(0)
	case "G", "end":
		return u.selectDiffRow(len(u.diffRows) - 1)
	case "J", "pgdown", "ctrl+d":
		u.scrollPatch(max(1, u.patchViewHeight()/2))
	case "K", "pgup", "ctrl+u":
		u.scrollPatch(-max(1, u.patchViewHeight()/2))
	case "enter":
		if u.diffRowSel < len(u.diffRows) {
			if u.diffRows[u.diffRowSel].isDir {
				u.toggleDiffDirectory()
			} else {
				return u.setFocus(panePatch)
			}
		}
	case " ":
		return u.toggleStageSelected()
	case "a":
		return u.toggleStageAll()
	case "c":
		return u.openCommitModal()
	case "d":
		u.openDiscardModal()
	case "`":
		return u.toggleDiffTree()
	case "-":
		if u.diffTree {
			u.collapseAllDiffDirectories(true)
		}
	case "=":
		if u.diffTree {
			u.collapseAllDiffDirectories(false)
		}
	case "esc":
		return u.setFocus(paneWorktrees)
	}
	return nil
}

func (u *model) toggleTab() tea.Cmd {
	if u.tab == detailTabDiff {
		u.tab = detailTabAgent
	} else {
		u.tab = detailTabDiff
	}
	u.agentScreen, u.agentScreenErr = nil, nil
	return u.screenCmd()
}

func (u *model) patchPaneKey(key string) tea.Cmd {
	if u.tab == detailTabAgent {
		switch key {
		case "enter", "g":
			return u.goCurrent()
		case "esc":
			return u.setFocus(paneFiles)
		}
		return nil
	}
	half := max(1, u.patchViewHeight()/2)
	switch key {
	case "j", "down":
		u.scrollPatch(1)
	case "k", "up":
		u.scrollPatch(-1)
	case "ctrl+d", "pgdown", " ":
		u.scrollPatch(half)
	case "ctrl+u", "pgup", "b":
		u.scrollPatch(-half)
	case "g", "home":
		u.patchTop = 0
	case "G", "end":
		u.patchTop = len(u.patchLines)
		u.clampPatch()
	case "h", "left":
		u.patchX = max(0, u.patchX-8)
	case "l", "right":
		u.patchX += 8
	case "esc":
		return u.setFocus(paneFiles)
	}
	return nil
}

func (u *model) worktreeCapacity() int {
	l := u.layout()
	return max(1, (l.worktrees.h-2)/max(1, u.rowH))
}

// ---- mouse ----------------------------------------------------------------

func (u *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	pane, ok := u.paneAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	l := u.layout()
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		switch pane {
		case paneWorktrees:
			return u.moveSelection(dir)
		case paneFiles:
			return u.moveDiffRow(dir)
		case panePatch:
			u.scrollPatch(dir * 3)
		}
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		cmd := u.setFocus(pane)
		switch pane {
		case paneWorktrees:
			if row := (msg.Y - l.worktrees.y - 1); row >= 0 {
				return tea.Batch(cmd, u.selectIndex(u.listTop+row/max(1, u.rowH)))
			}
		case paneFiles:
			if row := (msg.Y - l.files.y - 1); row >= 0 {
				return tea.Batch(cmd, u.selectDiffRow(u.diffTop+row))
			}
		}
		return cmd
	}
	return nil
}

// ---- actions --------------------------------------------------------------

func (u *model) goCurrent() tea.Cmd {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return nil
	}
	mgr, target := u.mgr, item.Path
	var path string
	run := &funcExec{fn: func() error {
		var err error
		path, err = mgr.Go(GoOptions{Target: target, Launch: true, Attach: true})
		return err
	}}
	return tea.Exec(run, func(err error) tea.Msg {
		return execDoneMsg{err: err, path: path, what: "attach"}
	})
}

func (u *model) promoteSelectedPreview() tea.Cmd {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return nil
	}
	if len(u.mgr.Cfg.PreviewWindows) == 0 {
		u.setWarn("no preview services configured; add [[preview_windows]] to .sprout.toml")
		return nil
	}
	branch := branchLabel(*item)
	target := item.Path
	mgr := u.mgr
	spin := u.startOp("promoting preview to %s…", branch)
	run := func() tea.Msg {
		st, err := mgr.PromotePreview(PreviewOptions{Target: target})
		switch {
		case err != nil:
			return opDoneMsg{level: levelError, text: fmt.Sprintf("preview failed: %v", err)}
		case len(st.SyncWarnings) > 0:
			return opDoneMsg{level: levelWarn, refresh: true, text: fmt.Sprintf("preview running from %s (%s)", branch, strings.Join(st.SyncWarnings, "; "))}
		}
		return opDoneMsg{level: levelInfo, refresh: true, text: fmt.Sprintf("preview now running from %s", branch)}
	}
	return tea.Batch(spin, run)
}

// openPR opens the selected worktree's pull request in the browser.
func (u *model) openPR() tea.Cmd {
	item := u.selectedItem()
	if item == nil {
		return nil
	}
	pr, ok := u.prs[item.Branch]
	if !ok || pr.URL == "" {
		if u.prsUnavailable {
			u.setWarn("pull requests unavailable (is gh installed and signed in?)")
		} else {
			u.setWarn("no pull request for %s", branchLabel(*item))
		}
		return nil
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	u.setInfo("opening #%d", pr.Number)
	url := pr.URL
	return func() tea.Msg {
		_ = exec.Command(opener, url).Start()
		return nil
	}
}

func branchLabel(item Worktree) string {
	if b := strings.TrimSpace(item.Branch); b != "" {
		return b
	}
	return filepath.Base(item.Path)
}

func (u *model) switchRepo(repo repoChoice) tea.Cmd {
	if repo.Root == "" || repo.Root == u.repoRoot {
		return nil
	}
	if err := os.Chdir(repo.Root); err != nil {
		u.setError("switch failed: %v", err)
		return nil
	}
	u.repoRoot = repo.Root
	u.repoName = repo.Name
	u.repoSlug = repo.GitHubRepo
	u.filter = ""
	u.selected = 0
	u.listTop = 0
	u.items, u.visible = nil, nil
	u.diffPath = ""
	u.diffs.clear()
	u.agentPrompt = map[string]agentPromptState{}
	u.probe = newAgentProbe()
	u.sweeping, u.screenBusy = false, false
	u.prs, u.prsBusy, u.prsUnavailable = nil, false, false
	u.lastScan = time.Time{}
	return tea.Batch(
		u.refreshCmd(refreshReq{fresh: true, level: levelInfo, notice: "switched repo: " + repoChoiceLabel(repo)}),
		u.prsCmd(),
	)
}

// createCmd creates (and bootstraps) a worktree. prep, when set, runs first;
// the dialog uses it to fetch a pull request's branch.
func (u *model) createCmd(branch string, fromExisting bool, prep func() error) tea.Cmd {
	mgr, send := u.mgr, u.send
	progress := func(msg string) {
		if send != nil {
			send(noticeMsg{level: levelStatus, text: msg})
		}
	}
	spin := u.startOp("creating worktree %s…", branch)
	run := func() tea.Msg {
		opts := NewOptions{Branch: branch, Launch: false}
		if fromExisting {
			opts = NewOptions{FromBranch: branch, Launch: false}
		}
		var warnings []string

		if prep != nil {
			progress(fmt.Sprintf("fetching %s…", branch))
			if err := prep(); err != nil {
				return opDoneMsg{level: levelError, text: fmt.Sprintf("create failed (%s): %v", branch, err)}
			}
		}
		debugLogf("ui_create start branch=%q existing=%t auto_launch=%t auto_start_agent=%t", branch, fromExisting, mgr.Cfg.AutoLaunch, mgr.Cfg.AutoStartAgent)
		_, path, err := mgr.NewWorktree(opts)
		if err != nil {
			debugLogf("ui_create new_worktree failed branch=%q: %v", branch, err)
			return opDoneMsg{level: levelError, text: fmt.Sprintf("create failed (%s): %v", branch, err)}
		}

		if mgr.HasBootstrap() {
			progress(fmt.Sprintf("bootstrapping %s…", branch))
			last := time.Time{}
			out := newLineWriter(func(line string) {
				line = strings.TrimSpace(line)
				if line == "" || (!last.IsZero() && time.Since(last) < 120*time.Millisecond) {
					return
				}
				last = time.Now()
				progress(fmt.Sprintf("bootstrapping %s: %s", branch, ellipsize(line, 60)))
			})
			if _, err := mgr.Bootstrap(BootstrapOptions{
				Target: path,
				OnStep: func(dir, run string) {
					progress(fmt.Sprintf("bootstrapping %s: %s", branch, ellipsize(run, 60)))
				},
				Out: out,
			}); err != nil {
				debugLogf("ui_create bootstrap failed path=%q: %v", path, err)
				warnings = append(warnings, fmt.Sprintf("bootstrap failed: %v", err))
			}
		}
		if mgr.Cfg.AutoLaunch {
			progress(fmt.Sprintf("launching tmux tools for %s…", branch))
			if _, err := mgr.Launch(LaunchOptions{Target: path, NoAttach: true}); err != nil {
				debugLogf("ui_create auto_launch failed path=%q: %v", path, err)
				warnings = append(warnings, fmt.Sprintf("launch failed: %v", err))
			}
		}
		if mgr.Cfg.AutoStartAgent {
			progress(fmt.Sprintf("starting agent for %s…", branch))
			if _, _, err := mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
				debugLogf("ui_create auto_agent failed path=%q: %v", path, err)
				warnings = append(warnings, fmt.Sprintf("agent start failed: %v", err))
			}
		}
		if len(warnings) > 0 {
			return opDoneMsg{level: levelWarn, refresh: true, fresh: true, selectPath: path, text: fmt.Sprintf("created %s (warnings: %s)", branch, strings.Join(warnings, " | "))}
		}
		debugLogf("ui_create success path=%q", path)
		return opDoneMsg{level: levelInfo, refresh: true, fresh: true, selectPath: path, text: fmt.Sprintf("created %s", branch)}
	}
	return tea.Batch(spin, run)
}

func (u *model) removeCmd(item Worktree) tea.Cmd {
	mgr := u.mgr
	branch := branchLabel(item)
	spin := u.startOp("removing %s…", branch)
	run := func() tea.Msg {
		// Async removal renames the worktree aside instantly and reaps its
		// files in the background, so the list refreshes immediately instead
		// of blocking on a large tree (node_modules, build output).
		_, warnings, err := mgr.Remove(RemoveOptions{
			Target:       item.Path,
			Force:        item.Dirty,
			DeleteBranch: false,
			Async:        true,
		})
		switch {
		case err != nil:
			return opDoneMsg{level: levelError, text: fmt.Sprintf("remove failed: %v", err)}
		case len(warnings) > 0:
			return opDoneMsg{level: levelWarn, refresh: true, fresh: true, text: fmt.Sprintf("removed with warning: %s", warnings[0])}
		}
		return opDoneMsg{level: levelInfo, refresh: true, fresh: true, text: fmt.Sprintf("removed %s", branch)}
	}
	return tea.Batch(spin, run)
}

func (u *model) detachCmd(item Worktree) tea.Cmd {
	mgr := u.mgr
	spin := u.startOp("detaching %s…", branchLabel(item))
	run := func() tea.Msg {
		path, detached, err := mgr.Detach(item.Path)
		switch {
		case err != nil:
			return opDoneMsg{level: levelError, text: fmt.Sprintf("detach failed: %v", err)}
		case !detached:
			return opDoneMsg{level: levelInfo, refresh: true, text: fmt.Sprintf("session was not running: %s", path)}
		}
		return opDoneMsg{level: levelInfo, refresh: true, text: fmt.Sprintf("detached %s", path)}
	}
	return tea.Batch(spin, run)
}

// ---- view -----------------------------------------------------------------

func (u *model) View() string {
	if u.width == 0 || u.height == 0 {
		return ""
	}
	if u.width < 40 || u.height < 10 {
		msg := styleUIDim.Render("terminal too small — resize to continue")
		lines := centerBlock([]string{msg}, u.width, u.height)
		for len(lines) < u.height {
			lines = append(lines, "")
		}
		return strings.Join(lines[:u.height], "\n")
	}

	l := u.layout()
	var left []string
	if l.status.w > 0 {
		left = append(left,
			u.statusView(l.status),
			u.worktreesView(l.worktrees),
			u.filesView(l.files),
		)
	}
	main := strings.Join(left, "\n")
	if l.patch.w > 0 {
		patch := u.patchView(l.patch)
		if main == "" {
			main = patch
		} else {
			main = joinHorizontal(main, patch)
		}
	}
	frame := main + "\n" + u.footerView()
	if u.modal != nil {
		frame = u.overlayModal(frame)
	}
	return frame
}

func (u *model) overlayModal(frame string) string {
	mv := u.modal.view(u)
	w := min(mv.width, u.width-4)
	h := len(mv.lines) + 2
	body := box("", mv.title, mv.footer, mv.lines, w, h, true)
	x := (u.width - w) / 2
	y := max(0, (u.height-h)/2-1)
	return overlay(frame, body, x, y)
}

func (u *model) statusView(r rect) string {
	iw := r.w - 2
	branch := u.repoBranch
	if branch == "" {
		branch = "(detached)"
	}
	repo := u.repoName
	if u.repoSlug != "" {
		repo = u.repoSlug
	}
	left := " " + styleUIBold.Render(repo) + styleUIDim.Render("  ›  ") + styleUIAccent.Render(branch)
	right := ""
	switch {
	case u.focus == paneStatus && len(u.repos) > 1:
		right = keyHint("enter", "switch repo") + " "
	case u.updateLatest != "":
		right = fg(uiAmber).Render("↑ "+u.updateLatest) + " "
	}
	line := spread(left, right, iw)
	return box("1", "sprout", "", []string{line}, r.w, r.h, u.focus == paneStatus)
}

func (u *model) worktreesView(r rect) string {
	iw, ih := r.w-2, r.h-2
	focused := u.focus == paneWorktrees
	u.rowH = 1
	if ih >= 12 {
		u.rowH = 2
	}
	capacity := max(1, ih/u.rowH)
	u.listTop = windowTop(u.listTop, u.selected, capacity, len(u.visible))

	title := "Worktrees"
	if strings.TrimSpace(u.filter) != "" {
		title += styleUIDim.Render("  ⌕ ") + styleUIAccent.Render(u.filter)
	}
	footer := ""
	var lines []string
	if len(u.visible) == 0 {
		note := []string{styleUIDim.Render("loading…")}
		switch {
		case strings.TrimSpace(u.filter) != "":
			note = []string{styleUIDim.Render("no worktrees match “" + u.filter + "”"), "", keyHint("esc", "clear filter")}
		case u.items != nil:
			note = []string{styleUIDim.Render("no worktrees yet"), "", keyHint("n", "new worktree")}
		}
		lines = centerBlock(note, iw, ih)
	} else {
		footer = fmt.Sprintf("%d of %d", u.selected+1, len(u.visible))
		end := min(len(u.visible), u.listTop+capacity)
		for pos := u.listTop; pos < end; pos++ {
			item := u.items[u.visible[pos]]
			lines = append(lines, u.worktreeRow(item, pos == u.selected, focused, iw, u.rowH == 2)...)
		}
	}
	return box("2", title, footer, lines, r.w, r.h, focused)
}

func (u *model) worktreeRow(it Worktree, selected, focused bool, iw int, twoLine bool) []string {
	hl := selected && focused
	st := func(s lipgloss.Style) lipgloss.Style {
		if hl {
			return s.Background(uiSelBg)
		}
		return s
	}
	seg := func(s lipgloss.Style, text string) string { return st(s).Render(text) }
	plain := lipgloss.NewStyle()

	bar := seg(plain, " ")
	if selected {
		bar = seg(styleUIAccent, "▌")
		if !focused {
			bar = seg(styleUIFaint, "▌")
		}
	}
	cur := seg(plain, " ")
	if it.Current {
		cur = seg(styleUIAccent, "●")
	}
	prev := seg(plain, " ")
	isPreview := u.previewWorktreePath != "" && it.Path == u.previewWorktreePath
	if isPreview {
		prev = seg(fg(uiSuccess).Bold(true), "▶")
	}
	const fixedLeft = 5

	// Chips, in display order. Higher prio survives narrow panes.
	var chips []chip
	if it.TmuxState == "yes" {
		chips = append(chips, chip{"tmux", fg(uiSuccess), 1})
	}
	if it.Dirty {
		chips = append(chips, chip{"dirty", fg(uiAmber), 2})
	} else {
		chips = append(chips, chip{"clean", styleUIDim, 1})
	}
	if pr, ok := u.prs[it.Branch]; ok {
		chips = append(chips, prChip(pr))
	}
	if it.AgentState == "yes" {
		label := u.agentLabel(it)
		switch label {
		case "ready":
			chips = append(chips, chip{"● ready", fg(uiSuccess), 3})
		case "busy":
			chips = append(chips, chip{"● busy", fg(uiAmber), 3})
		case "needs input":
			chips = append(chips, chip{"◆ needs input", fg(uiViolet).Bold(true), 4})
		default:
			chips = append(chips, chip{"● agent", fg(uiBlue), 3})
		}
	}
	chipsW := func() int {
		w := 0
		for i, ch := range chips {
			w += cell(ch.text)
			if i > 0 {
				w += 2
			}
		}
		return w
	}
	const minBranch = 18
	for len(chips) > 0 && fixedLeft+minBranch+1+chipsW()+1 > iw {
		drop := 0
		for i, ch := range chips {
			if ch.prio < chips[drop].prio {
				drop = i
			}
		}
		chips = append(chips[:drop], chips[drop+1:]...)
	}

	name := it.Branch
	if strings.TrimSpace(name) == "" {
		name = "detached"
	}
	trailing := 1
	if len(chips) == 0 {
		trailing = 0
	}
	name = ellipsize(name, max(1, iw-fixedLeft-chipsW()-trailing-1))
	nameStyle := plain
	switch {
	case isPreview:
		nameStyle = fg(uiSuccess).Bold(true)
	case it.Current:
		nameStyle = styleUIAccent.Bold(true)
	case selected:
		nameStyle = styleUIBold
	}

	var right strings.Builder
	for i, ch := range chips {
		if i > 0 {
			right.WriteString(seg(plain, "  "))
		}
		right.WriteString(seg(ch.style, ch.text))
	}
	if len(chips) > 0 {
		right.WriteString(seg(plain, " "))
	}
	head := bar + seg(plain, " ") + cur + prev + seg(plain, " ") + seg(nameStyle, name)
	gap := iw - cell(head) - cell(right.String())
	line1 := head + seg(plain, strings.Repeat(" ", max(0, gap))) + right.String()

	if !twoLine {
		return []string{line1}
	}
	path := tildePath(it.Path)
	avail := iw - 5
	body := bar + seg(plain, "    ") + seg(styleUIDim, ellipsizeMiddle(path, avail))
	line2 := body + seg(plain, strings.Repeat(" ", max(0, iw-cell(body))))
	return []string{line1, line2}
}

type chip struct {
	text  string
	style lipgloss.Style
	prio  int
}

// prChip renders a pull request as "#12 ✓" (checks pass), "#12 ✗" (fail),
// "#12 …" (running), "#12 draft", or "merged #12" / "closed #12".
func prChip(pr PRInfo) chip {
	num := fmt.Sprintf("#%d", pr.Number)
	switch pr.State {
	case "MERGED":
		return chip{"merged " + num, fg(uiViolet), 3}
	case "CLOSED":
		return chip{"closed " + num, styleUIDim, 2}
	}
	switch {
	case pr.Draft:
		return chip{num + " draft", styleUIDim, 3}
	case pr.Checks == "pass":
		return chip{num + " ✓", fg(uiSuccess), 3}
	case pr.Checks == "fail":
		return chip{num + " ✗", fg(uiRed), 4}
	case pr.Checks == "pending":
		return chip{num + " …", fg(uiAmber), 3}
	}
	return chip{num, fg(uiBlue), 3}
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// statusCells renders a two-character XY status like lazygit: the index side
// in green, the work tree side in amber (red for deletions and untracked).
func (u *model) statusCells(status string, seg func(lipgloss.Style, string) string) string {
	st := parseFileStage(status)
	if st.untracked {
		return seg(fg(uiRed), "??")
	}
	r := []rune(status)
	for len(r) < 2 {
		r = append(r, ' ')
	}
	cellOf := func(c rune, style lipgloss.Style) string {
		if c == '.' || c == ' ' {
			return seg(styleUIFaint, "·")
		}
		return seg(style, string(c))
	}
	work := fg(uiAmber)
	if r[1] == 'D' {
		work = fg(uiRed)
	}
	if st.conflict {
		return seg(fg(uiRed).Bold(true), string(r[:2]))
	}
	return cellOf(r[0], fg(uiSuccess)) + cellOf(r[1], work)
}

func (u *model) filesView(r rect) string {
	iw, ih := r.w-2, r.h-2
	focused := u.focus == paneFiles
	title := "Files"
	if !u.diffTree {
		title += styleUIDim.Render("  flat")
	}
	var lines []string
	footer := ""
	switch {
	case !u.diffLoaded:
		lines = centerBlock([]string{styleUIDim.Render("loading…")}, iw, ih)
	case len(u.diffItems) == 0:
		lines = centerBlock([]string{styleUIDim.Render("no changed files")}, iw, ih)
	default:
		u.diffTop = windowTop(u.diffTop, u.diffRowSel, ih, len(u.diffRows))
		footer = fmt.Sprintf("%d of %d", u.diffSel+1, len(u.diffItems))
		end := min(len(u.diffRows), u.diffTop+ih)
		for i := u.diffTop; i < end; i++ {
			lines = append(lines, u.fileRow(u.diffRows[i], i == u.diffRowSel, focused, iw))
		}
	}
	return box("3", title, footer, lines, r.w, r.h, focused)
}

func (u *model) fileRow(row diffTreeRow, selected, focused bool, iw int) string {
	hl := selected && focused
	st := func(s lipgloss.Style) lipgloss.Style {
		if hl {
			return s.Background(uiSelBg)
		}
		return s
	}
	seg := func(s lipgloss.Style, text string) string { return st(s).Render(text) }
	plain := lipgloss.NewStyle()

	bar := seg(plain, " ")
	if selected {
		bar = seg(styleUIAccent, "▌")
		if !focused {
			bar = seg(styleUIFaint, "▌")
		}
	}
	indent := strings.Repeat("  ", row.depth)
	var body string
	if row.isDir {
		icon := "▾ "
		if u.diffCollapsed[row.key] {
			icon = "▸ "
		}
		label := ellipsizeMiddle(row.label, max(1, iw-3-cell(indent)-2))
		body = seg(plain, indent) + seg(styleUIDim, icon) + seg(styleUIDim, label)
	} else {
		status := strings.TrimSpace(row.status)
		if status == "" {
			status = "??"
		}
		label := ellipsizeMiddle(row.label, max(1, iw-3-cell(indent)-3))
		labelStyle := plain
		if selected {
			labelStyle = styleUIBold
		}
		body = seg(plain, indent) + u.statusCells(status, seg) + seg(plain, " ") + seg(labelStyle, label)
	}
	line := bar + seg(plain, " ") + body
	return line + seg(plain, strings.Repeat(" ", max(0, iw-cell(line))))
}

func (u *model) tabTitle() string {
	active, idle := styleUIBold, styleUIDim
	patch, agent := idle.Render("Patch"), idle.Render("Agent")
	if u.tab == detailTabDiff {
		patch = active.Render("Patch")
	} else {
		agent = active.Render("Agent")
	}
	return patch + styleUIFaint.Render(" │ ") + agent
}

func (u *model) patchView(r rect) string {
	if u.tab == detailTabAgent {
		return u.agentView(r)
	}
	iw, ih := r.w-2, r.h-2
	focused := u.focus == panePatch
	var lines []string
	footer := ""
	switch {
	case len(u.patchLines) > 0:
		u.clampPatch()
		end := min(len(u.patchLines), u.patchTop+ih)
		for _, l := range u.patchLines[u.patchTop:end] {
			if u.patchX > 0 {
				l = ansi.Cut(l, u.patchX, u.patchX+iw)
			}
			lines = append(lines, l+"\x1b[0m")
		}
		if len(u.patchLines) > ih {
			footer = fmt.Sprintf("%d%%", min(100, (end*100)/len(u.patchLines)))
		}
	case !u.diffLoaded:
		lines = centerBlock([]string{styleUIDim.Render("loading…")}, iw, ih)
	case len(u.patchNote) > 0:
		lines = centerBlock(u.patchNote, iw, ih)
	}
	return box("4", u.tabTitle(), footer, lines, r.w, r.h, focused)
}

func (u *model) agentView(r rect) string {
	iw, ih := r.w-2, r.h-2
	focused := u.focus == panePatch
	item := u.selectedItem()
	var lines []string
	footer := ""
	switch {
	case item == nil:
		lines = centerBlock([]string{styleUIDim.Render("select a worktree")}, iw, ih)
	case item.AgentState != "yes":
		lines = centerBlock([]string{
			styleUIDim.Render("no agent running in this worktree"),
			"",
			keyHint("⏎", "attach (starts the tmux session)"),
		}, iw, ih)
	case u.agentScreenErr != nil:
		lines = centerBlock([]string{styleUIDim.Render("agent pane unavailable"), "", styleUIDim.Render(ellipsize(u.agentScreenErr.Error(), iw-4))}, iw, ih)
	case u.agentScreenPath != item.Path:
		lines = centerBlock([]string{styleUIDim.Render("loading…")}, iw, ih)
	default:
		rows := u.agentScreen
		if len(rows) > ih {
			rows = rows[len(rows)-ih:]
		}
		for _, l := range rows {
			lines = append(lines, l+"\x1b[0m")
		}
		label := u.agentLabel(*item)
		if label != "yes" {
			footer = label + " · live"
		} else {
			footer = "live"
		}
	}
	return box("4", u.tabTitle(), footer, lines, r.w, r.h, focused)
}

func (u *model) keyHints() []string {
	var hints []string
	switch u.focus {
	case paneStatus:
		hints = []string{keyHint("⏎", "switch repo")}
	case paneWorktrees:
		hints = []string{
			keyHint("⏎", "attach"), keyHint("p", "preview"), keyHint("n", "new"),
			keyHint("x", "remove"), keyHint("d", "detach"), keyHint("o", "PR"), keyHint("/", "filter"),
		}
	case paneFiles:
		hints = []string{
			keyHint("space", "stage"), keyHint("a", "all"), keyHint("c", "commit"), keyHint("d", "discard"),
			keyHint("⏎", "open"), keyHint("`", "tree/flat"),
		}
	case panePatch:
		if u.tab == detailTabAgent {
			hints = []string{keyHint("⏎", "attach")}
		} else {
			hints = []string{keyHint("j/k", "scroll"), keyHint("^d/^u", "page"), keyHint("g/G", "top/end"), keyHint("h/l", "pan")}
		}
	}
	hints = append(hints, keyHint("[ ]", "patch/agent"))
	return append(hints, keyHint("tab", "pane"), keyHint("?", "help"), keyHint("q", "quit"))
}

func (u *model) footerView() string {
	w := u.width

	right := ""
	if u.msgText != "" {
		var icon string
		switch u.msgLevel {
		case levelStatus:
			icon = u.spin.View()
			if u.busy == 0 {
				icon = styleUIAccent.Render("•")
			}
		case levelWarn:
			icon = fg(uiAmber).Render("!")
		case levelError:
			icon = fg(uiRed).Render("✗")
		default:
			icon = styleUIAccent.Render("✓")
		}
		text := ellipsize(u.msgText, max(10, w/2-6))
		if u.msgLevel == levelError {
			text = fg(uiRed).Render(text)
		}
		right = icon + " " + text + "  "
	}
	right += styleUIFaint.Render("v" + Version)
	right += " "

	hints := u.keyHints()
	avail := w - cell(right) - 2
	left := ""
	for i, h := range hints {
		next := left
		if next != "" {
			next += "  "
		}
		next += h
		// Always keep the trailing "? help" and "q quit".
		reserve := 0
		if i < len(hints)-2 {
			reserve = cell("  "+hints[len(hints)-2]) + cell("  "+hints[len(hints)-1])
		}
		if cell(next)+reserve > avail {
			continue
		}
		left = next
	}
	return spread(" "+left, right, w)
}
