package sprout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type repoChoice struct {
	Root       string
	Name       string
	GitHubRepo string
	Branch     string
}

type tuiState struct {
	mgr        *Manager
	repoName   string
	repoRoot   string
	repoSlug   string
	repoBranch string

	app         *tview.Application
	pages       *tview.Pages
	table       *counterTable
	statusPane  *tview.TextView
	detailPane  *tview.Flex
	detailPages *tview.Pages
	agentPages  *tview.Pages
	detailTabs  *tview.TextView
	detail      *tview.TextView
	agentTerm   *terminalSurface
	diffFiles   *counterTable
	diffView    *tview.TextView
	footerLeft  *tview.TextView
	footerRight *tview.TextView

	items    []Worktree
	visible  []int
	selected int
	filter   string
	repos    []repoChoice

	previewWorktreePath string // path of the worktree currently designated as the preview

	focusables          []tview.Primitive
	lastDetail          string
	lastDiff            string
	detailTab           detailTab
	diffItems           []DiffFile
	diffSel             int
	diffPath            string
	diffTree            bool
	diffTreeRows        []diffTreeRow
	diffTreeRowSel      int
	diffTreeCollapsed   map[string]bool
	diffCache           map[string]diffFilesCacheEntry
	patchCache          map[string]diffPatchCacheEntry
	agentPrompt         map[string]agentPromptState
	agentPaneTargets    map[string]string
	agentStreamLogs     map[string]string
	agentDetailTextMode bool
	agentTerminalView   agentTerminalViewState
	paneSizes           map[string]paneSize
	paneActivity        map[string]int64
	panePromptActivity  map[string]int64
	pendingPaneActivity map[string]int64
	forceTableSelect    bool
	footerLevel         string
	footerMsg           string
	lastRepoScan        time.Time
	lastRepoScanParent  string
	repoChoiceCache     map[string]repoChoice
	previewMu           sync.Mutex
	previewCancel       context.CancelFunc
	previewSeq          int64
	cacheUse            uint64
}

type paneSize struct {
	w int
	h int
}

type agentTerminalViewState struct {
	paneTarget   string
	streamOffset int64
	size         paneSize
	needsReset   bool
}

type fileChunkResult struct {
	data       []byte
	nextOffset int64
	reset      bool
}

type agentTerminalSyncDeps struct {
	ensureLog func(reset bool) (string, error)
	readChunk func(path string, offset int64) (fileChunkResult, error)
	capture   func(lines int) (string, error)
}

type agentTerminalSyncResult struct {
	data      []byte
	reset     bool
	nextState agentTerminalViewState
	ready     agentPromptState
}

type detailTab int

const (
	detailTabAgent detailTab = iota
	detailTabDiff
)

type agentPromptState int

const (
	agentPromptUnknown agentPromptState = iota
	agentPromptBusy
	agentPromptReady
	agentPromptNeedsInput
)

var agentPromptOnlyRe = regexp.MustCompile(`^(>|>>|>>>|\$|#|:|›|❯|➜)\s*$`)
var agentPromptInputRe = regexp.MustCompile(`^(>|>>|>>>|\$|#|:|›|❯|➜)\s+.*$`)

var (
	tmuxCapturePaneFn  = tmuxCapturePane
	tmuxPaneActivityFn = tmuxPaneActivity
	tmuxResizePaneFn   = tmuxResizePane
	ensurePaneStreamFn = func(m *Manager, repoRoot, paneTarget string, seedLines int, reset bool) (string, error) {
		return m.ensurePaneStream(repoRoot, paneTarget, seedLines, reset)
	}
	readFileChunkFn = readFileChunk
)

type diffFilesCacheEntry struct {
	digest   string
	files    []DiffFile
	lastUsed uint64
}

type diffPatchCacheEntry struct {
	text     string
	lastUsed uint64
}

type diffTreeRow struct {
	label     string
	status    string
	fileIndex int
	isDir     bool
	depth     int
	key       string
}

const (
	detailPollInterval = 150 * time.Millisecond
	detailCaptureLines = 60
)

type counterTable struct {
	*tview.Table
	counter string
}

func newCounterTable() *counterTable {
	return &counterTable{Table: tview.NewTable()}
}

func (c *counterTable) SetCounter(value string) {
	c.counter = value
}

func (c *counterTable) Draw(screen tcell.Screen) {
	c.Table.Draw(screen)
	if c.counter == "" {
		return
	}
	x, y, w, h := c.GetRect()
	if w < 6 || h < 2 {
		return
	}
	label := " " + c.counter + " "
	runes := []rune(label)
	start := x + w - 2 - len(runes)
	if start <= x+1 {
		return
	}
	style := tcell.StyleDefault.Foreground(ansiColor(ansiCyan)).Background(tcell.ColorDefault)
	for i, r := range runes {
		screen.SetContent(start+i, y+h-1, r, nil, style)
	}
}

const (
	ansiRed     = 1
	ansiGreen   = 2
	ansiYellow  = 3
	ansiBlue    = 4
	ansiMagenta = 5
	ansiCyan    = 6
)

func paneBorderColor() tcell.Color {
	return ColorToTcell(ThemeColorPrimary)
}

func paneFocusColor() tcell.Color {
	return ColorToTcell(ThemeColorSecondary)
}

func ansiColor(code int) tcell.Color {
	return tcell.PaletteColor(code)
}

func paletteLevelColor(level string) tcell.Color {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERROR":
		return tcell.ColorRed
	case "WARN":
		return tcell.ColorYellow
	case "INFO":
		return ColorToTcell(ThemeColorSecondary)
	default:
		return ColorToTcell(ThemeColorAccent)
	}
}

func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = paneBorderColor()
	tview.Styles.TitleColor = ColorToTcell(ThemeColorPrimary)
	tview.Styles.GraphicsColor = ColorToTcell(ThemeColorAccent)
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = ColorToTcell(ThemeColorSecondary)
	tview.Styles.TertiaryTextColor = ColorToTcell(ThemeColorMuted)
	tview.Styles.InverseTextColor = tcell.ColorDefault
	tview.Styles.ContrastSecondaryTextColor = tcell.ColorRed

	tview.Borders.HorizontalFocus = tview.Borders.Horizontal
	tview.Borders.VerticalFocus = tview.Borders.Vertical
	tview.Borders.TopLeft = tview.BoxDrawingsLightArcDownAndRight
	tview.Borders.TopRight = tview.BoxDrawingsLightArcDownAndLeft
	tview.Borders.BottomLeft = tview.BoxDrawingsLightArcUpAndRight
	tview.Borders.BottomRight = tview.BoxDrawingsLightArcUpAndLeft
	tview.Borders.TopLeftFocus = tview.Borders.TopLeft
	tview.Borders.TopRightFocus = tview.Borders.TopRight
	tview.Borders.BottomLeftFocus = tview.Borders.BottomLeft
	tview.Borders.BottomRightFocus = tview.Borders.BottomRight
}

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

	u := newTUI(mgr, repoRoot)
	firstPaint := startProfileStage("ui_first_paint")
	if err := u.refresh(); err != nil {
		u.setError("refresh failed: %v", err)
	}
	firstPaint()
	go func() {
		time.Sleep(250 * time.Millisecond)
		u.startUpdateCheck()
	}()
	stopLive := u.startLiveDetailUpdates(detailPollInterval)
	defer stopLive()

	if err := u.app.SetRoot(u.pages, true).Run(); err != nil {
		fmt.Printf("error: ui failed: %v\n", err)
		return 1
	}
	return 0
}

func newTUI(mgr *Manager, repoRoot string) *tuiState {
	applyTheme()

	statusPane := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	statusPane.
		SetTextColor(tcell.ColorDefault).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(true).
		SetBorderColor(paneBorderColor()).
		SetTitle("[1]-Status").
		SetTitleColor(paneBorderColor())

	table := newCounterTable()
	table.SetSelectable(true, false)
	table.SetFixed(1, 0)
	table.SetBorders(false)
	table.SetSeparator(' ')
	table.SetBackgroundColor(tcell.ColorDefault)
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	table.
		SetBorder(true).
		SetBorderColor(paneBorderColor()).
		SetTitle("[3]-Worktrees").
		SetTitleColor(paneBorderColor())

	detailTabs := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	detailTabs.
		SetTextColor(ansiColor(ansiCyan)).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(false)

	detail := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	detail.
		SetTextColor(tcell.ColorDefault).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(false)

	agentTerm := newTerminalSurface()
	agentTerm.SetBorder(false)
	agentTerm.SetBackgroundColor(tcell.ColorDefault)

	diffFiles := newCounterTable()
	diffFiles.SetSelectable(false, false)
	diffFiles.SetFixed(1, 0)
	diffFiles.SetBorders(false)
	diffFiles.SetSeparator(' ')
	diffFiles.SetBackgroundColor(tcell.ColorDefault)
	diffFiles.SetBorder(true)
	diffFiles.SetBorderColor(paneBorderColor())
	diffFiles.SetTitle("Files")
	diffFiles.SetTitleColor(paneBorderColor())

	diffView := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetScrollable(true)
	diffView.
		SetTextColor(tcell.ColorDefault).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(true).
		SetBorderColor(paneBorderColor()).
		SetTitle("Patch").
		SetTitleColor(paneBorderColor())

	diffBody := tview.NewFlex().
		AddItem(diffFiles, 0, 2, false).
		AddItem(diffView, 0, 5, false)

	agentPages := tview.NewPages().
		AddPage("terminal", agentTerm, true, true).
		AddPage("text", detail, true, false)

	detailPages := tview.NewPages().
		AddPage("agent", agentPages, true, true).
		AddPage("diff", diffBody, true, false)

	detailPane := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailTabs, 1, 0, false).
		AddItem(detailPages, 0, 1, false)
	detailPane.
		SetBorder(true).
		SetBorderColor(paneBorderColor()).
		SetTitle("[2]-Details").
		SetTitleColor(paneBorderColor()).
		SetBackgroundColor(tcell.ColorDefault)

	footerLeft := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	footerLeft.
		SetTextColor(ansiColor(ansiCyan)).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(false)

	footerRight := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetTextAlign(tview.AlignRight)
	footerRight.
		SetTextColor(ansiColor(ansiCyan)).
		SetBackgroundColor(tcell.ColorDefault).
		SetBorder(false)

	body := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(detailPane, 0, 3, false).
		AddItem(table, 0, 2, true)

	footer := tview.NewFlex().
		AddItem(footerLeft, 0, 1, false).
		AddItem(footerRight, 14, 0, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(statusPane, 3, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(footer, 1, 0, false)

	pages := tview.NewPages().AddPage("main", root, true, true)

	u := &tuiState{
		mgr:                 mgr,
		repoName:            mgr.RepoName(repoRoot),
		repoRoot:            repoRoot,
		app:                 tview.NewApplication().EnableMouse(true),
		pages:               pages,
		table:               table,
		statusPane:          statusPane,
		detailPane:          detailPane,
		detailPages:         detailPages,
		agentPages:          agentPages,
		detailTabs:          detailTabs,
		detail:              detail,
		agentTerm:           agentTerm,
		diffFiles:           diffFiles,
		diffView:            diffView,
		footerLeft:          footerLeft,
		footerRight:         footerRight,
		detailTab:           detailTabDiff,
		diffSel:             0,
		diffTree:            true,
		diffTreeCollapsed:   map[string]bool{},
		diffCache:           map[string]diffFilesCacheEntry{},
		patchCache:          map[string]diffPatchCacheEntry{},
		agentPrompt:         map[string]agentPromptState{},
		agentPaneTargets:    map[string]string{},
		agentStreamLogs:     map[string]string{},
		agentDetailTextMode: false,
		agentTerminalView:   agentTerminalViewState{needsReset: true},
		paneSizes:           map[string]paneSize{},
		paneActivity:        map[string]int64{},
		panePromptActivity:  map[string]int64{},
		pendingPaneActivity: map[string]int64{},
		repoChoiceCache:     map[string]repoChoice{},
	}
	u.focusables = []tview.Primitive{u.statusPane, u.detailPane, u.table}

	table.SetSelectionChangedFunc(func(row, _ int) {
		if u.app.GetFocus() != u.table && !u.forceTableSelect {
			return
		}
		if row <= 0 {
			u.selected = 0
		} else {
			u.selected = row - 1
		}
		u.renderTableMeta()
		u.renderStatusPane()
		u.renderDetails()
	})
	table.SetSelectedFunc(func(row, _ int) {
		if row > 0 {
			u.goCurrent()
		}
	})
	u.app.SetInputCapture(u.handleKey)
	u.app.SetMouseCapture(u.handleMouse)

	u.footerRight.SetText(fmt.Sprintf("v%s", Version))
	u.refreshRepoChoices(true)
	u.app.SetFocus(u.statusPane)
	u.detailPages.ShowPage("diff")
	u.detailPages.HidePage("agent")
	u.updatePaneFocusStyles()
	u.setInfo("ready")
	u.renderDetails()
	return u
}

func (u *tuiState) handleMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event == nil {
		return event, action
	}
	switch action {
	case tview.MouseLeftDown, tview.MouseLeftClick, tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseScrollLeft, tview.MouseScrollRight:
	default:
		return event, action
	}

	x, y := event.Position()
	target := u.mouseFocusTarget(x, y)
	if target == nil {
		return event, action
	}
	if u.app.GetFocus() != target {
		u.app.SetFocus(target)
		u.updatePaneFocusStyles()
	}
	return event, action
}

func (u *tuiState) mouseFocusTarget(x, y int) tview.Primitive {
	switch {
	case pointInPrimitive(u.statusPane, x, y):
		return u.statusPane
	case pointInPrimitive(u.table, x, y):
		return u.table
	case pointInPrimitive(u.diffFiles, x, y):
		return u.diffFiles
	case pointInPrimitive(u.diffView, x, y):
		return u.diffView
	case pointInPrimitive(u.agentTerm, x, y):
		if u.agentDetailTextMode {
			return u.detail
		}
		return u.agentTerm
	case pointInPrimitive(u.detail, x, y):
		return u.detail
	case pointInPrimitive(u.detailPane, x, y):
		if u.detailTab == detailTabDiff {
			return u.diffFiles
		}
		if u.agentDetailTextMode {
			return u.detail
		}
		return u.agentTerm
	default:
		return nil
	}
}

func pointInPrimitive(p tview.Primitive, x, y int) bool {
	if p == nil {
		return false
	}
	px, py, w, h := p.GetRect()
	return x >= px && y >= py && x < px+w && y < py+h
}

func (u *tuiState) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	mainFocus := u.isMainFocus()
	focus := u.app.GetFocus()
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.agentTerm || focus == u.diffFiles || focus == u.diffView

	if mainFocus && inDetail {
		return u.handleDetailBrowseKey(ev)
	}

	switch ev.Key() {
	case tcell.KeyCtrlC:
		u.app.Stop()
		return nil
	case tcell.KeyEnter:
		if mainFocus {
			if u.app.GetFocus() == u.statusPane {
				u.showRepoSwitchModal()
				return nil
			}
			if u.app.GetFocus() != u.table {
				return nil
			}
			u.goCurrent()
			return nil
		}
	case tcell.KeyTAB:
		if mainFocus {
			u.cycleFocus(1)
			return nil
		}
	case tcell.KeyBacktab:
		if mainFocus {
			u.cycleFocus(-1)
			return nil
		}
	case tcell.KeyDown:
		if mainFocus && u.app.GetFocus() == u.table {
			u.moveSelection(1)
			return nil
		}
	case tcell.KeyUp:
		if mainFocus && u.app.GetFocus() == u.table {
			u.moveSelection(-1)
			return nil
		}
	case tcell.KeyRune:
		if !mainFocus {
			return ev
		}
		switch ev.Rune() {
		case 'q':
			u.app.Stop()
			return nil
		case '[':
			u.cycleDetailTab(-1)
			return nil
		case ']':
			u.cycleDetailTab(1)
			return nil
		case 'j':
			u.moveSelection(1)
			return nil
		case 'k':
			u.moveSelection(-1)
			return nil
		case 'R':
			if err := u.refresh(); err != nil {
				u.setError("refresh failed: %v", err)
			}
			return nil
		case 'p':
			u.promoteSelectedPreview()
			return nil
		case 'n':
			u.showCreateModal()
			return nil
		case 'x':
			u.showDeleteModal()
			return nil
		case 'd':
			u.showDetachModal()
			return nil
		case '/':
			u.showFilterModal()
			return nil
		case '?':
			u.showHelpModal()
			return nil
		}
	}
	return ev
}

func (u *tuiState) handleDetailBrowseKey(ev *tcell.EventKey) *tcell.EventKey {
	if u.detailTab == detailTabDiff {
		return u.handleDiffBrowseKey(ev)
	}
	textMode := u.agentDetailTextMode && u.app.GetFocus() == u.detail

	switch ev.Key() {
	case tcell.KeyCtrlC:
		u.app.Stop()
		return nil
	case tcell.KeyTAB:
		u.cycleFocus(1)
		return nil
	case tcell.KeyBacktab:
		u.cycleFocus(-1)
		return nil
	case tcell.KeyEnter:
		u.goCurrent()
		return nil
	case tcell.KeyUp:
		if textMode {
			u.scrollTextView(u.detail, -1)
		}
		return nil
	case tcell.KeyDown:
		if textMode {
			u.scrollTextView(u.detail, 1)
		}
		return nil
	case tcell.KeyPgUp:
		if textMode {
			u.scrollTextView(u.detail, -10)
		}
		return nil
	case tcell.KeyPgDn:
		if textMode {
			u.scrollTextView(u.detail, 10)
		}
		return nil
	case tcell.KeyHome:
		if textMode {
			u.detail.ScrollToBeginning()
		}
		return nil
	case tcell.KeyEnd:
		if textMode {
			u.detail.ScrollToEnd()
		}
		return nil
	case tcell.KeyLeft:
		u.cycleDetailTab(-1)
		return nil
	case tcell.KeyRight:
		u.cycleDetailTab(1)
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'j':
			if textMode {
				u.scrollTextView(u.detail, 1)
			}
		case 'k':
			if textMode {
				u.scrollTextView(u.detail, -1)
			}
		case 'g':
			u.goCurrent()
		case 'G':
			if textMode {
				u.detail.ScrollToEnd()
			}
		case 'p':
			u.promoteSelectedPreview()
		case 'h', '[':
			u.cycleDetailTab(-1)
		case 'l', ']':
			u.cycleDetailTab(1)
		}
		return nil
	default:
		return nil
	}
}

func (u *tuiState) handleDiffBrowseKey(ev *tcell.EventKey) *tcell.EventKey {
	// Navigation logic
	switch ev.Key() {
	case tcell.KeyCtrlC:
		u.app.Stop()
		return nil
	case tcell.KeyTAB:
		u.cycleFocus(1)
		return nil
	case tcell.KeyBacktab:
		u.cycleFocus(-1)
		return nil
	case tcell.KeyCtrlU:
		u.scrollTextView(u.diffView, -10)
		return nil
	case tcell.KeyCtrlD:
		u.scrollTextView(u.diffView, 10)
		return nil
	case tcell.KeyUp:
		u.moveDiffSelection(-1)
		return nil
	case tcell.KeyDown:
		u.moveDiffSelection(1)
		return nil
	case tcell.KeyPgUp:
		u.scrollTextView(u.diffView, -10)
		return nil
	case tcell.KeyPgDn:
		u.scrollTextView(u.diffView, 10)
		return nil
	case tcell.KeyHome:
		u.selectDiffRow(0)
		return nil
	case tcell.KeyEnd:
		u.selectDiffRow(len(u.diffTreeRows) - 1)
		return nil
	case tcell.KeyEnter:
		u.toggleDiffDirectory()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'j':
			u.moveDiffSelection(1)
		case 'k':
			u.moveDiffSelection(-1)
		case 'J':
			u.scrollTextView(u.diffView, 10)
		case 'K':
			u.scrollTextView(u.diffView, -10)
		case 'g':
			u.selectDiffRow(0)
		case 'G':
			u.selectDiffRow(len(u.diffTreeRows) - 1)
		case '`':
			u.diffTree = !u.diffTree
			u.renderDiffFileList()
			if len(u.diffTreeRows) > 0 && u.diffTreeRowSel >= 0 && u.diffTreeRowSel < len(u.diffTreeRows) {
				row := u.diffTreeRows[u.diffTreeRowSel]
				if row.fileIndex >= 0 {
					u.diffSel = row.fileIndex
					u.renderDetails()
				} else {
					u.renderDirectoryPreview(row)
				}
			}
		case '-':
			if u.diffTree {
				u.collapseAllDiffDirectories(true)
			}
		case '=':
			if u.diffTree {
				u.collapseAllDiffDirectories(false)
			}
		case ' ':
			u.toggleDiffDirectory()
		}
		return nil
	}
	return ev
}

func (u *tuiState) isMainFocus() bool {
	current := u.app.GetFocus()
	for _, p := range u.focusables {
		if current == p {
			return true
		}
	}
	// Also check sub-focusables in diff pane or agent pane
	if current == u.diffFiles || current == u.diffView || current == u.detail || current == u.agentTerm {
		return true
	}
	return false
}

func (u *tuiState) cycleFocus(delta int) {
	if len(u.focusables) == 0 {
		return
	}
	current := u.app.GetFocus()
	idx := 0
	for i, p := range u.focusables {
		if current == p {
			idx = i
			break
		}
	}
	next := (idx + delta) % len(u.focusables)
	if next < 0 {
		next += len(u.focusables)
	}
	u.app.SetFocus(u.focusables[next])
	u.updatePaneFocusStyles()
}

func (u *tuiState) cycleDetailTab(delta int) {
	_ = delta
}

func (u *tuiState) setDetailTab(tab detailTab) {
	if tab != detailTabDiff || u.detailTab == detailTabDiff {
		return
	}
}

func (u *tuiState) updatePaneFocusStyles() {
	focus := u.app.GetFocus()
	stylePane := func(active bool, setTitle func(string), setBorderColor func(tcell.Color), setTitleColor func(tcell.Color), baseTitle string) {
		if active {
			setTitle("> " + baseTitle)
			setBorderColor(paneFocusColor())
			setTitleColor(paneFocusColor())
			return
		}
		setTitle(baseTitle)
		setBorderColor(paneBorderColor())
		setTitleColor(paneBorderColor())
	}

	stylePane(
		focus == u.statusPane,
		func(s string) { u.statusPane.SetTitle(s) },
		func(c tcell.Color) { u.statusPane.SetBorderColor(c) },
		func(c tcell.Color) { u.statusPane.SetTitleColor(c) },
		"[1]-Status",
	)
	stylePane(
		focus == u.table,
		func(s string) { u.table.SetTitle(s) },
		func(c tcell.Color) { u.table.SetBorderColor(c) },
		func(c tcell.Color) { u.table.SetTitleColor(c) },
		u.tablePaneTitle(),
	)
	stylePane(
		focus == u.detailPane || focus == u.detail || focus == u.agentTerm || focus == u.diffFiles || focus == u.diffView,
		func(s string) { u.detailPane.SetTitle(s) },
		func(c tcell.Color) { u.detailPane.SetBorderColor(c) },
		func(c tcell.Color) { u.detailPane.SetTitleColor(c) },
		u.detailPaneTitle(),
	)

	if focus == u.table {
		u.table.SetSelectable(true, false)
		u.table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	} else {
		u.table.SetSelectable(false, false)
		u.table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault))
	}

	u.renderStatusPane()
	u.renderDetailTabs()
	u.redrawFooter()
}

func (u *tuiState) moveSelection(delta int) {
	if len(u.visible) == 0 {
		return
	}
	u.selected += delta
	if u.selected < 0 {
		u.selected = 0
	}
	if u.selected >= len(u.visible) {
		u.selected = len(u.visible) - 1
	}
	u.selectTableRow(u.selected+1, false)
	u.renderTableMeta()
	u.renderDetails()
}

func (u *tuiState) moveDiffSelection(delta int) {
	if len(u.diffTreeRows) == 0 {
		return
	}
	next := u.diffTreeRowSel + delta
	if next < 0 {
		next = 0
	}
	if next >= len(u.diffTreeRows) {
		next = len(u.diffTreeRows) - 1
	}
	u.selectDiffRow(next)
}

func (u *tuiState) selectDiffFile(idx int) {
	if len(u.diffItems) == 0 {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(u.diffItems) {
		idx = len(u.diffItems) - 1
	}
	if idx == u.diffSel {
		return
	}
	u.diffSel = idx
	u.renderDiffFileList()
	u.renderDetails()
}

func (u *tuiState) selectDiffRow(idx int) {
	if len(u.diffTreeRows) == 0 {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(u.diffTreeRows) {
		idx = len(u.diffTreeRows) - 1
	}
	if idx == u.diffTreeRowSel {
		return
	}
	u.diffTreeRowSel = idx
	row := u.diffTreeRows[idx]
	u.renderDiffFileList()
	if row.fileIndex >= 0 && row.fileIndex != u.diffSel {
		u.diffSel = row.fileIndex
		u.renderDetails()
		return
	}
	if row.fileIndex < 0 {
		u.previewMu.Lock()
		u.previewSeq++
		if u.previewCancel != nil {
			u.previewCancel()
			u.previewCancel = nil
		}
		u.previewMu.Unlock()
		u.renderDirectoryPreview(row)
	}
}

func (u *tuiState) toggleDiffDirectory() {
	if len(u.diffTreeRows) == 0 || u.diffTreeRowSel < 0 || u.diffTreeRowSel >= len(u.diffTreeRows) {
		return
	}
	row := u.diffTreeRows[u.diffTreeRowSel]
	if !row.isDir || strings.TrimSpace(row.key) == "" {
		return
	}
	u.diffTreeCollapsed[row.key] = !u.diffTreeCollapsed[row.key]
	u.renderDiffFileList()
	u.renderDirectoryPreview(u.diffTreeRows[u.diffTreeRowSel])
}

func (u *tuiState) collapseAllDiffDirectories(collapsed bool) {
	rows, _ := buildDiffTreeRows(u.diffItems, u.diffSel, map[string]bool{}, true)
	u.diffTreeCollapsed = map[string]bool{}
	if collapsed {
		for _, row := range rows {
			if row.isDir && row.key != "" {
				u.diffTreeCollapsed[row.key] = true
			}
		}
	}
	u.renderDiffFileList()
	if len(u.diffTreeRows) > 0 && u.diffTreeRowSel >= 0 && u.diffTreeRowSel < len(u.diffTreeRows) {
		row := u.diffTreeRows[u.diffTreeRowSel]
		if row.fileIndex < 0 {
			u.renderDirectoryPreview(row)
		}
	}
}

func (u *tuiState) applyFilter() {
	u.visible = u.visible[:0]
	q := strings.ToLower(strings.TrimSpace(u.filter))
	for i, item := range u.items {
		if q == "" {
			u.visible = append(u.visible, i)
			continue
		}
		hay := strings.ToLower(item.Branch + " " + item.Path)
		if strings.Contains(hay, q) {
			u.visible = append(u.visible, i)
		}
	}
	if u.selected >= len(u.visible) {
		u.selected = len(u.visible) - 1
	}
	if u.selected < 0 {
		u.selected = 0
	}
}

func (u *tuiState) refresh() error {
	u.refreshRepoChoices(false)
	state, err := listWorktreesFastState(u.mgr, u.repoRoot)
	if err != nil {
		return err
	}
	items := state.Worktrees
	u.repoBranch = u.mgr.CurrentBranch(u.repoRoot)
	u.items = items
	u.previewWorktreePath = ""
	if st, err := u.mgr.readPreviewState(u.repoRoot); err == nil && st != nil {
		u.previewWorktreePath = st.Path
	}
	alive := map[string]struct{}{}
	for _, it := range items {
		if strings.TrimSpace(it.Path) == "" {
			continue
		}
		alive[it.Path] = struct{}{}
		if it.AgentState != "yes" {
			delete(u.agentPrompt, it.Path)
		}
	}
	for path := range u.agentPrompt {
		if _, ok := alive[path]; !ok {
			delete(u.agentPrompt, path)
		}
	}
	u.previewMu.Lock()
	for path := range u.agentPaneTargets {
		if _, ok := alive[path]; !ok {
			delete(u.agentPaneTargets, path)
		}
	}
	for _, it := range items {
		if it.AgentState != "yes" {
			delete(u.agentPaneTargets, it.Path)
		}
	}
	u.previewMu.Unlock()
	u.applyFilter()
	u.renderTable()
	u.renderTableMeta()
	u.renderDetails()
	u.renderStatusPane()
	return nil
}

func (u *tuiState) startLiveDetailUpdates(interval time.Duration) func() {
	done := make(chan struct{})
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				u.app.QueueUpdateDraw(func() {
					if !u.isMainFocus() {
						return
					}
					item := u.selectedItem()
					if item == nil {
						return
					}
					if u.detailTab == detailTabAgent {
						if u.shouldRefreshAgentDetail(item) {
							u.renderDetails()
						}
						return
					}
					u.captureAgentPromptState(item, 40)
				})
			}
		}
	}()
	return func() {
		close(done)
	}
}

func (u *tuiState) detailPaneTitle() string {
	return "[2]-Details"
}

func (u *tuiState) tablePaneTitle() string {
	return "[3]-Worktrees"
}

func (u *tuiState) startUpdateCheck() {
	go func() {
		if latest, ok := checkForUpdate(Version, u.mgr.Cfg); ok {
			u.app.QueueUpdateDraw(func() {
				u.setWarn("update available: %s (current %s)", latest, Version)
			})
		}
	}()
}

func (u *tuiState) shouldRefreshAgentDetail(item *Worktree) bool {
	if item == nil {
		return false
	}
	if item.AgentState != "yes" {
		return false
	}
	paneTarget := u.resolvedAgentPaneTarget(item)
	if paneTarget == "" {
		return true
	}
	activity, err := tmuxPaneActivityFn(paneTarget)
	if err != nil {
		u.previewMu.Lock()
		u.agentTerminalView.needsReset = true
		u.previewMu.Unlock()
		return true
	}
	u.previewMu.Lock()
	last, ok := u.paneActivity[paneTarget]
	if ok && last == activity {
		u.previewMu.Unlock()
		return false
	}
	u.paneActivity[paneTarget] = activity
	u.pendingPaneActivity[paneTarget] = activity
	u.previewMu.Unlock()
	return true
}

func (u *tuiState) renderDetailTabs() {
	diff := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Reverse(true).Render(" GIT DIFF ")
	u.detailTabs.SetText(tview.TranslateANSI(fmt.Sprintf(" %s", diff)))
}

func (u *tuiState) currentFilterLabel() string {
	if strings.TrimSpace(u.filter) == "" {
		return "(none)"
	}
	return u.filter
}

func (u *tuiState) renderStatusPane() {
	repoBranch := u.repoBranch
	if repoBranch == "" {
		repoBranch = "(detached)"
	}
	selectedBranch := "(none)"
	if item := u.selectedItem(); item != nil {
		selectedBranch = item.Branch
		if strings.TrimSpace(selectedBranch) == "" {
			selectedBranch = "(detached)"
		}
	}
	repo := u.repoName

	// Render using lipgloss then translate to tview tags
	check := lipgloss.NewStyle().Foreground(ColorGreen).Render("✓")
	repoStr := lipgloss.NewStyle().Bold(true).Render(repo)
	arrow := lipgloss.NewStyle().Foreground(ColorBlue).Render("->")
	branchStr := lipgloss.NewStyle().Foreground(ColorGreen).Render(repoBranch)
	selLabel := lipgloss.NewStyle().Foreground(ColorBlue).Render("selected:")
	selBranch := lipgloss.NewStyle().Foreground(ColorGreen).Render(selectedBranch)

	status := fmt.Sprintf(
		"%s %s %s %s  %s %s",
		check, repoStr, arrow, branchStr, selLabel, selBranch,
	)

	if u.app.GetFocus() == u.statusPane {
		status = lipgloss.NewStyle().Reverse(true).Render(
			fmt.Sprintf("✓ %s -> %s   selected: %s   (enter to switch repo)", repo, repoBranch, selectedBranch),
		)
	}

	u.statusPane.SetText(tview.TranslateANSI(status))
}

func (u *tuiState) refreshRepoChoices(force bool) {
	parent := filepath.Dir(u.repoRoot)
	if !force && len(u.repos) > 0 && u.lastRepoScanParent == parent && time.Since(u.lastRepoScan) < 5*time.Second {
		return
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		u.repos = []repoChoice{buildRepoChoice(u.repoRoot)}
		u.repoSlug = u.repos[0].GitHubRepo
		u.lastRepoScan = time.Now()
		u.lastRepoScanParent = parent
		return
	}

	repoRoots := []string{u.repoRoot}

	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		root := filepath.Join(parent, ent.Name())
		if !isGitRepoDir(root) {
			continue
		}
		if root == u.repoRoot {
			continue
		}
		repoRoots = append(repoRoots, root)
	}

	u.repos = u.repos[:0]
	u.repos = make([]repoChoice, len(repoRoots))
	if u.repoChoiceCache == nil {
		u.repoChoiceCache = map[string]repoChoice{}
	}
	missing := make([]int, 0, len(repoRoots))
	for i, root := range repoRoots {
		if cached, ok := u.repoChoiceCache[root]; ok {
			if root == u.repoRoot && strings.TrimSpace(u.repoBranch) != "" {
				cached.Branch = u.repoBranch
			}
			u.repos[i] = cached
			continue
		}
		missing = append(missing, i)
	}
	if len(missing) > 1 {
		var wg sync.WaitGroup
		for _, i := range missing {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				u.repos[idx] = buildRepoChoice(repoRoots[idx])
			}(i)
		}
		wg.Wait()
	} else if len(missing) == 1 {
		idx := missing[0]
		u.repos[idx] = buildRepoChoice(repoRoots[idx])
	}
	for i, root := range repoRoots {
		u.repoChoiceCache[root] = u.repos[i]
	}

	sort.Slice(u.repos, func(i, j int) bool {
		if u.repos[i].Root == u.repoRoot {
			return true
		}
		if u.repos[j].Root == u.repoRoot {
			return false
		}
		li := u.repos[i].GitHubRepo
		if li == "" {
			li = u.repos[i].Name
		}
		lj := u.repos[j].GitHubRepo
		if lj == "" {
			lj = u.repos[j].Name
		}
		return li < lj
	})

	u.repoSlug = ""
	for _, r := range u.repos {
		if r.Root == u.repoRoot {
			u.repoSlug = r.GitHubRepo
			break
		}
	}
	u.lastRepoScan = time.Now()
	u.lastRepoScanParent = parent
}

func buildRepoChoice(root string) repoChoice {
	name := filepath.Base(root)
	repo := githubRepoFromRoot(root)
	return repoChoice{
		Root:       root,
		Name:       name,
		GitHubRepo: repo,
		Branch:     branchFromRoot(root),
	}
}

func repoChoiceLabel(repo repoChoice) string {
	if repo.GitHubRepo != "" {
		return repo.GitHubRepo
	}
	return repo.Name
}

func isGitRepoDir(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

func githubRepoFromRoot(root string) string {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseGitHubRepo(strings.TrimSpace(string(out)))
}

func branchFromRoot(root string) string {
	cmd := exec.Command("git", "-C", root, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return "(unknown)"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "(detached)"
	}
	return branch
}

func parseGitHubRepo(url string) string {
	if url == "" {
		return ""
	}
	trimmed := strings.TrimSuffix(url, ".git")
	if i := strings.Index(trimmed, "github.com:"); i >= 0 {
		repo := trimmed[i+len("github.com:"):]
		return strings.TrimPrefix(repo, "/")
	}
	if i := strings.Index(trimmed, "github.com/"); i >= 0 {
		repo := trimmed[i+len("github.com/"):]
		repo = strings.TrimPrefix(repo, "/")
		if slash := strings.Index(repo, "?"); slash >= 0 {
			repo = repo[:slash]
		}
		return repo
	}
	return ""
}

func (u *tuiState) renderTable() {
	u.table.Clear()

	headers := []string{"CUR", "BRANCH", "STATUS", "TMUX", "AGENT", "PATH"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetAttributes(tcell.AttrBold).
			SetTextColor(ColorToTcell(ThemeColorPrimary)).
			SetExpansion(1).
			SetSelectable(false)
		u.table.SetCell(0, col, cell)
	}

	for row, idx := range u.visible {
		item := u.items[idx]
		cur := ""
		if item.Current {
			cur = "*"
		}
		branch := item.Branch
		if branch == "" {
			branch = "detached"
		}
		isPreview := u.previewWorktreePath != "" && item.Path == u.previewWorktreePath
		branchLabel := truncate(branch, 33)
		if isPreview {
			branchLabel = "▶ " + branchLabel
		}
		status := "clean"
		if item.Dirty {
			status = "dirty"
		}
		agentLabel := u.tableAgentLabel(item)
		values := []string{cur, branchLabel, status, item.TmuxState, agentLabel, truncatePath(item.Path, 120)}
		for col, val := range values {
			cell := tview.NewTableCell(val).SetExpansion(1).SetTextColor(tcell.ColorDefault)
			switch col {
			case 0:
				if val != "" {
					cell.SetTextColor(ColorToTcell(ThemeColorAccent))
				}
			case 2:
				if status == "dirty" {
					cell.SetTextColor(tcell.ColorRed)
				} else {
					cell.SetTextColor(tcell.ColorGreen)
				}
			case 3:
				if val == "yes" {
					cell.SetTextColor(tcell.ColorGreen)
				} else if val == "no" {
					cell.SetTextColor(tcell.ColorRed)
				} else {
					cell.SetTextColor(ColorToTcell(ThemeColorSecondary))
				}
			case 4:
				cell.SetTextColor(tableAgentColor(agentLabel))
			case 5:
				cell.SetTextColor(ColorToTcell(ThemeColorSecondary))
			}
			if item.Current && col == 1 {
				cell.SetTextColor(ColorToTcell(ThemeColorAccent))
				cell.SetAttributes(tcell.AttrBold)
			}
			if isPreview && col == 1 {
				cell.SetTextColor(tcell.ColorGreen)
				cell.SetAttributes(tcell.AttrBold)
			}
			if status == "dirty" && col == 2 {
				cell.SetAttributes(tcell.AttrBold)
			}
			u.table.SetCell(row+1, col, cell)
		}
	}

	if len(u.visible) == 0 {
		u.table.SetCell(1, 0, tview.NewTableCell("(no worktrees match filter)").SetTextColor(ansiColor(ansiMagenta)).SetSelectable(false))
		u.selectTableRow(1, true)
		u.renderTableMeta()
		return
	}
	u.selectTableRow(u.selected+1, true)
	u.renderTableMeta()
}

func (u *tuiState) updateSelectedAgentCell() {
}

func (u *tuiState) renderTableMeta() {
	if len(u.visible) == 0 {
		u.table.SetCounter("0 of 0")
		return
	}
	current := u.selected + 1
	if current < 1 {
		current = 1
	}
	if current > len(u.visible) {
		current = len(u.visible)
	}
	u.table.SetCounter(fmt.Sprintf("%d of %d", current, len(u.visible)))
}

func (u *tuiState) selectedItem() *Worktree {
	if len(u.visible) == 0 || u.selected < 0 || u.selected >= len(u.visible) {
		return nil
	}
	item := u.items[u.visible[u.selected]]
	return &item
}

func (u *tuiState) selectedAgentPromptLabel(item *Worktree) (string, string) {
	if item == nil {
		return "n/a", "cyan"
	}
	if item.AgentState != "yes" {
		return "offline", "red"
	}
	state, ok := u.agentPrompt[item.Path]
	if !ok {
		return "running", "blue"
	}
	switch state {
	case agentPromptNeedsInput:
		return "needs input", "magenta"
	case agentPromptReady:
		return "ready", "green"
	case agentPromptBusy:
		return "busy", "yellow"
	default:
		return "running", "blue"
	}
}

func (u *tuiState) tableAgentLabel(item Worktree) string {
	if item.AgentState != "yes" {
		return item.AgentState
	}
	state, ok := u.agentPrompt[item.Path]
	if !ok {
		return "yes"
	}
	switch state {
	case agentPromptNeedsInput:
		return "needs input"
	case agentPromptReady:
		return "ready"
	case agentPromptBusy:
		return "busy"
	default:
		return "yes"
	}
}

func tableAgentColor(label string) tcell.Color {
	switch label {
	case "ready", "yes":
		return tcell.ColorGreen
	case "needs input":
		return ansiColor(ansiMagenta)
	case "busy", "running":
		return tcell.ColorYellow
	case "no", "offline":
		return tcell.ColorRed
	default:
		return ColorToTcell(ThemeColorSecondary)
	}
}

func (u *tuiState) setAgentPromptState(item *Worktree, next agentPromptState) {
	if item == nil || strings.TrimSpace(item.Path) == "" {
		return
	}
	if item.AgentState != "yes" {
		delete(u.agentPrompt, item.Path)
		return
	}
	prev, hadPrev := u.agentPrompt[item.Path]
	if hadPrev && prev == next {
		return
	}
	u.agentPrompt[item.Path] = next
	u.renderStatusPane()
	u.updateSelectedAgentCell()
}

func (u *tuiState) resolvedAgentPaneTarget(item *Worktree) string {
	if item == nil || strings.TrimSpace(item.Path) == "" {
		return ""
	}
	if item.AgentState != "yes" {
		u.previewMu.Lock()
		delete(u.agentPaneTargets, item.Path)
		u.previewMu.Unlock()
		return ""
	}
	u.previewMu.Lock()
	if target, ok := u.agentPaneTargets[item.Path]; ok && strings.TrimSpace(target) != "" {
		u.previewMu.Unlock()
		return target
	}
	u.previewMu.Unlock()
	target := strings.TrimSpace(u.mgr.agentPaneTarget(u.repoRoot, item))
	if target == "" {
		return ""
	}
	u.previewMu.Lock()
	u.agentPaneTargets[item.Path] = target
	u.previewMu.Unlock()
	return target
}

func (u *tuiState) ensureAgentStreamLog(item *Worktree, paneTarget string, seedLines int, reset bool) (string, error) {
	if item == nil || strings.TrimSpace(item.Path) == "" || strings.TrimSpace(paneTarget) == "" {
		return "", fmt.Errorf("agent stream is not available")
	}
	u.previewMu.Lock()
	if !reset {
		if path, ok := u.agentStreamLogs[paneTarget]; ok && strings.TrimSpace(path) != "" {
			u.previewMu.Unlock()
			return path, nil
		}
	}
	u.previewMu.Unlock()

	logPath, err := ensurePaneStreamFn(u.mgr, u.repoRoot, paneTarget, seedLines, reset)
	if err != nil {
		return "", err
	}
	u.previewMu.Lock()
	u.agentStreamLogs[paneTarget] = logPath
	u.previewMu.Unlock()
	return logPath, nil
}

func (u *tuiState) captureAgentPromptState(item *Worktree, lines int) {
	if item == nil || item.AgentState != "yes" {
		return
	}
	paneTarget := u.resolvedAgentPaneTarget(item)
	if paneTarget == "" {
		return
	}
	activity, err := tmuxPaneActivity(paneTarget)
	if err == nil {
		if last, ok := u.panePromptActivity[paneTarget]; ok && last == activity {
			return
		}
		u.panePromptActivity[paneTarget] = activity
	}
	logPath, err := u.ensureAgentStreamLog(item, paneTarget, lines, false)
	if err != nil {
		return
	}
	out, err := readTailLines(logPath, lines)
	if err != nil {
		return
	}
	u.setAgentPromptState(item, agentPromptStateForOutput(out))
}

func stripANSI(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == 0x1b {
			i++
			if i >= len(input) {
				break
			}
			switch input[i] {
			case '[':
				for i+1 < len(input) {
					i++
					d := input[i]
					if d >= 0x40 && d <= 0x7e {
						break
					}
				}
			case ']':
				for i+1 < len(input) {
					i++
					if input[i] == 0x07 {
						break
					}
					if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
						i++
						break
					}
				}
			default:
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func normalizeTerminalStream(input string) string {
	var (
		out  strings.Builder
		line strings.Builder
	)
	line.Grow(len(input))

	flushLine := func() {
		out.WriteString(line.String())
		line.Reset()
	}

	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '\x1b':
			next, ok := consumeANSIEscape(input, i)
			if !ok {
				continue
			}
			seq := input[i:next]
			// Keep SGR color/style codes, ignore cursor/erase controls that a text view
			// cannot replay correctly from a streamed log.
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				line.WriteString(seq)
			}
			i = next - 1
		case '\r':
			line.Reset()
		case '\n':
			flushLine()
			out.WriteByte('\n')
		case '\b':
			current := []rune(line.String())
			if len(current) > 0 {
				line.Reset()
				line.WriteString(string(current[:len(current)-1]))
			}
		default:
			line.WriteByte(input[i])
		}
	}

	if line.Len() > 0 {
		flushLine()
	}
	return out.String()
}

func terminalStreamNeedsSnapshot(input string) bool {
	if strings.Contains(input, "\x1b[?1049") {
		return true
	}
	if strings.Contains(input, "\x1b[2J") || strings.Contains(input, "\x1b[J") {
		return true
	}
	if strings.Contains(input, "\x1b[H") || strings.Contains(input, "\x1b[f") {
		return true
	}
	if strings.Count(input, "\r") > 8 && strings.Count(input, "\n") <= 2 {
		return true
	}
	return false
}

func agentReadyForInstruction(output string) bool {
	plain := stripANSI(output)
	lines := strings.Split(strings.ReplaceAll(plain, "\r", "\n"), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < 12; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		lower := strings.ToLower(line)
		if strings.Contains(lower, "for shortcuts") ||
			strings.Contains(lower, "context left") {
			return true
		}
		if agentPromptOnlyRe.MatchString(line) {
			return true
		}
		if strings.Contains(line, "█") && agentPromptInputRe.MatchString(line) {
			return true
		}
		if strings.Contains(lower, "awaiting your input") ||
			strings.Contains(lower, "waiting for your input") ||
			strings.Contains(lower, "ready for your next instruction") ||
			strings.Contains(lower, "what would you like to do next") ||
			strings.Contains(lower, "enter your prompt") {
			return true
		}
	}
	return false
}

func agentPromptStateName(state agentPromptState) string {
	switch state {
	case agentPromptReady:
		return "ready"
	case agentPromptBusy:
		return "busy"
	case agentPromptNeedsInput:
		return "needs_input"
	default:
		return "unknown"
	}
}

func agentPromptStateFromName(name string) agentPromptState {
	switch strings.TrimSpace(name) {
	case "ready":
		return agentPromptReady
	case "busy":
		return agentPromptBusy
	case "needs_input":
		return agentPromptNeedsInput
	default:
		return agentPromptUnknown
	}
}

func detectNeedsInput(output string) bool {
	plain := strings.ToLower(stripANSI(output))
	lines := strings.Split(strings.ReplaceAll(plain, "\r", "\n"), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < 16; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		if strings.Contains(line, "awaiting your input") ||
			strings.Contains(line, "waiting for your input") ||
			strings.Contains(line, "need your input") ||
			strings.Contains(line, "need input from you") ||
			strings.Contains(line, "please advise") ||
			strings.Contains(line, "please confirm") ||
			strings.Contains(line, "can you confirm") ||
			strings.Contains(line, "which would you prefer") ||
			strings.Contains(line, "what should i do next") ||
			strings.Contains(line, "what would you like me to do next") {
			return true
		}
	}
	return false
}

func agentPromptStateForOutput(output string) agentPromptState {
	if detectNeedsInput(output) {
		return agentPromptNeedsInput
	}
	if agentReadyForInstruction(output) {
		return agentPromptReady
	}
	return agentPromptBusy
}

func (u *tuiState) renderDetails() {
	item := u.selectedItem()
	tab := u.detailTab
	diffSel := u.diffSel
	lines := u.detailCaptureLineCount()
	diffWidth := u.detailDiffWidth()
	termSize := u.agentTerminalInnerSize()

	u.previewMu.Lock()
	u.previewSeq++
	seq := u.previewSeq
	if u.previewCancel != nil {
		u.previewCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.previewCancel = cancel
	u.previewMu.Unlock()

	switch tab {
	case detailTabDiff:
		if strings.TrimSpace(u.lastDiff) == "" {
			u.setDiffText("Loading diff…", false)
		}
	default:
		if strings.TrimSpace(u.lastDetail) == "" {
			u.setDetailText("Loading details…", false)
		}
	}

	go u.computeDetailPreview(ctx, seq, tab, item, diffSel, lines, diffWidth, termSize)
}

func (u *tuiState) computeDetailPreview(ctx context.Context, seq int64, tab detailTab, item *Worktree, diffSel int, captureLines int, diffWidth int, termSize paneSize) {
	switch tab {
	case detailTabDiff:
		u.computeDiffPreview(ctx, seq, item, diffSel, diffWidth)
	default:
		u.computeAgentPreview(ctx, seq, item, captureLines, termSize)
	}
}

func (u *tuiState) computeAgentPreview(ctx context.Context, seq int64, item *Worktree, captureLines int, termSize paneSize) {
	_ = captureLines
	_ = termSize
	if item == nil {
		u.applyDetailPreview(seq, detailTabAgent, func() {
			u.setDetailText("Select a worktree.", false)
		})
		return
	}

	if item.AgentState != "yes" {
		u.applyDetailPreview(seq, detailTabAgent, func() {
			u.setAgentPromptState(item, agentPromptUnknown)
			u.setDetailText(
				"Agent session is not available for this worktree.\n\n"+
					"Press enter on the worktree list to attach.\n"+
					"A tmux session will open with your configured session tools.",
				false,
			)
		})
		return
	}

	paneTarget := u.resolvedAgentPaneTarget(item)
	if paneTarget == "" {
		u.applyDetailPreview(seq, detailTabAgent, func() {
			u.setAgentPromptState(item, agentPromptUnknown)
			u.setDetailText("Agent session is not available for this worktree.", false)
		})
		return
	}

	activity, activityErr := tmuxPaneActivityFn(paneTarget)
	select {
	case <-ctx.Done():
		return
	default:
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	u.applyDetailPreview(seq, detailTabAgent, func() {
		if paneTarget != "" {
			if activityErr == nil {
				u.paneActivity[paneTarget] = activity
			}
		}
		u.setDetailText(u.renderAgentStatusSummary(item, paneTarget, activity, activityErr), false)
	})
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func (u *tuiState) applyDetailPreview(seq int64, tab detailTab, apply func()) {
	u.app.QueueUpdateDraw(func() {
		u.previewMu.Lock()
		currentSeq := u.previewSeq
		u.previewMu.Unlock()
		if seq != currentSeq || u.detailTab != tab {
			return
		}
		apply()
	})
}

func (u *tuiState) computeDiffPreview(ctx context.Context, seq int64, item *Worktree, diffSel int, diffWidth int) {
	if item == nil {
		u.applyDetailPreview(seq, detailTabDiff, func() {
			u.diffItems = nil
			u.diffSel = 0
			u.diffPath = ""
			u.renderDiffFileList()
			u.setDiffText("Select a worktree to view git diff.", false)
		})
		return
	}

	snapshot, err := u.cachedDiffFilesSnapshot(ctx, item.Path)
	if err != nil {
		if errorsIsCanceled(err) {
			return
		}
		u.applyDetailPreview(seq, detailTabDiff, func() {
			u.diffItems = nil
			u.diffSel = 0
			u.diffPath = item.Path
			u.renderDiffFileList()
			u.setDiffText(fmt.Sprintf("Unable to read git diff.\n\n%s", err), false)
		})
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	files := snapshot.files
	selected := diffSel
	if selected < 0 {
		selected = 0
	}
	if len(files) > 0 && selected >= len(files) {
		selected = len(files) - 1
	}
	var diffText string
	if len(files) > 0 {
		diffText, err = u.cachedFileDiff(ctx, item.Path, snapshot.digest, files[selected], diffWidth)
		if err != nil && !errorsIsCanceled(err) {
			diffText = fmt.Sprintf("Unable to read file diff.\n\n%s", err)
		}
	}
	if errorsIsCanceled(err) {
		return
	}
	u.applyDetailPreview(seq, detailTabDiff, func() {
		u.syncDiffFiles(item.Path, files)
		if selected >= 0 && selected < len(u.diffItems) {
			u.diffSel = selected
		}
		u.renderDiffFileList()
		if len(u.diffItems) == 0 {
			u.setDiffText("(working tree is clean)", false)
			return
		}
		u.setDiffANSI(diffText, false)
	})
}

func (u *tuiState) renderAgentDetail() {
	u.renderDetails()
}

func formatPaneActivity(ts int64) string {
	if ts <= 0 {
		return "unknown"
	}
	last := time.Unix(ts, 0)
	if last.IsZero() {
		return "unknown"
	}
	delta := time.Since(last).Round(time.Second)
	if delta < 0 {
		delta = 0
	}
	return fmt.Sprintf("%s ago", delta)
}

func (u *tuiState) renderAgentStatusSummary(item *Worktree, paneTarget string, activity int64, activityErr error) string {
	if item == nil {
		return "Select a worktree."
	}

	status, _ := u.selectedAgentPromptLabel(item)
	branch := strings.TrimSpace(item.Branch)
	if branch == "" {
		branch = filepath.Base(item.Path)
	}

	lastActivity := "unavailable"
	if activityErr == nil {
		lastActivity = formatPaneActivity(activity)
	}

	lines := []string{
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Branch: %s", branch),
		fmt.Sprintf("Path: %s", item.Path),
	}
	if strings.TrimSpace(paneTarget) != "" {
		lines = append(lines, fmt.Sprintf("Pane: %s", paneTarget))
	}
	lines = append(lines, fmt.Sprintf("Last activity: %s", lastActivity))
	lines = append(lines, "")
	lines = append(lines, "Actions:")
	lines = append(lines, "enter or g: attach to the worktree tmux session")
	lines = append(lines, "p: promote the selected worktree to preview")
	lines = append(lines, "h/l or [ ]: switch tabs")

	return strings.Join(lines, "\n")
}

func (u *tuiState) clearDiffCaches() {
	u.diffCache = map[string]diffFilesCacheEntry{}
	u.patchCache = map[string]diffPatchCacheEntry{}
	u.lastDiff = ""
}

type diffSnapshotResult struct {
	digest string
	files  []DiffFile
}

func (u *tuiState) cachedDiffFilesSnapshot(ctx context.Context, path string) (diffSnapshotResult, error) {
	u.previewMu.Lock()
	if entry, ok := u.diffCache[path]; ok {
		u.cacheUse++
		entry.lastUsed = u.cacheUse
		u.diffCache[path] = entry
		u.previewMu.Unlock()
		return diffSnapshotResult{digest: entry.digest, files: entry.files}, nil
	}
	u.previewMu.Unlock()
	snapshot, err := u.mgr.WorktreeDiffSnapshotContext(ctx, path)
	if err != nil {
		return diffSnapshotResult{}, err
	}
	u.previewMu.Lock()
	u.cacheUse++
	u.diffCache[path] = diffFilesCacheEntry{digest: snapshot.Digest, files: snapshot.Files, lastUsed: u.cacheUse}
	u.evictDiffFileCacheLocked(128)
	u.previewMu.Unlock()
	return diffSnapshotResult{digest: snapshot.Digest, files: snapshot.Files}, nil
}

func diffPatchCacheKey(path, digest string, file DiffFile, width int) string {
	return strings.Join([]string{
		path,
		digest,
		file.Path,
		file.PreviousPath,
		file.Status,
		strconv.Itoa(width),
	}, "\x00")
}

func (u *tuiState) cachedFileDiff(ctx context.Context, path, digest string, file DiffFile, width int) (string, error) {
	key := diffPatchCacheKey(path, digest, file, width)
	u.previewMu.Lock()
	if entry, ok := u.patchCache[key]; ok {
		u.cacheUse++
		entry.lastUsed = u.cacheUse
		u.patchCache[key] = entry
		u.previewMu.Unlock()
		return entry.text, nil
	}
	u.previewMu.Unlock()
	diff, err := u.mgr.WorktreeDiffForFileContext(ctx, path, file, width)
	if err != nil {
		return "", err
	}
	u.previewMu.Lock()
	u.cacheUse++
	u.patchCache[key] = diffPatchCacheEntry{text: diff, lastUsed: u.cacheUse}
	u.evictDiffPatchCacheLocked(512)
	u.previewMu.Unlock()
	return diff, nil
}

func (u *tuiState) evictDiffFileCacheLocked(limit int) {
	if len(u.diffCache) <= limit {
		return
	}
	var oldestKey string
	var oldestUse uint64
	first := true
	for key, entry := range u.diffCache {
		if first || entry.lastUsed < oldestUse {
			first = false
			oldestKey = key
			oldestUse = entry.lastUsed
		}
	}
	delete(u.diffCache, oldestKey)
}

func (u *tuiState) evictDiffPatchCacheLocked(limit int) {
	if len(u.patchCache) <= limit {
		return
	}
	var oldestKey string
	var oldestUse uint64
	first := true
	for key, entry := range u.patchCache {
		if first || entry.lastUsed < oldestUse {
			first = false
			oldestKey = key
			oldestUse = entry.lastUsed
		}
	}
	delete(u.patchCache, oldestKey)
}

func (u *tuiState) renderDiffDetail() {
	u.renderDetails()
}

func (u *tuiState) syncDiffFiles(path string, files []DiffFile) {
	switchedWorktree := path != u.diffPath
	prev := ""
	if !switchedWorktree && u.diffSel >= 0 && u.diffSel < len(u.diffItems) {
		prev = u.diffItems[u.diffSel].Path
	}

	u.diffPath = path
	u.diffItems = files

	if len(u.diffItems) == 0 {
		u.diffSel = 0
		u.diffTreeRowSel = 0
		return
	}

	if switchedWorktree {
		u.diffSel = 0
		u.diffTreeRowSel = 0
	}
	if prev != "" {
		for i := range u.diffItems {
			if u.diffItems[i].Path == prev {
				u.diffSel = i
				break
			}
		}
	}
	if u.diffSel < 0 {
		u.diffSel = 0
	}
	if u.diffSel >= len(u.diffItems) {
		u.diffSel = len(u.diffItems) - 1
	}
}

func diffStatusColor(status string) tcell.Color {
	s := strings.TrimSpace(status)
	switch {
	case strings.Contains(s, "D"):
		return ansiColor(ansiRed)
	case strings.Contains(s, "A"), s == "??":
		return ansiColor(ansiGreen)
	case strings.Contains(s, "R"), strings.Contains(s, "C"):
		return ansiColor(ansiBlue)
	case strings.Contains(s, "M"), strings.Contains(s, "U"):
		return ansiColor(ansiYellow)
	default:
		return ansiColor(ansiCyan)
	}
}

type diffTreeNode struct {
	name     string
	children map[string]*diffTreeNode
	files    []int
}

func buildDiffTreeRows(files []DiffFile, selected int, collapsed map[string]bool, treeMode bool) ([]diffTreeRow, int) {
	if !treeMode {
		rows := make([]diffTreeRow, 0, len(files))
		selectedRow := 0
		for i, file := range files {
			if i == selected {
				selectedRow = len(rows)
			}
			rows = append(rows, diffTreeRow{
				label:     file.Path,
				status:    file.Status,
				fileIndex: i,
				key:       file.Path,
			})
		}
		return rows, selectedRow
	}

	root := &diffTreeNode{children: map[string]*diffTreeNode{}}
	for i, file := range files {
		parts := strings.Split(file.Path, "/")
		node := root
		for _, part := range parts[:len(parts)-1] {
			if node.children[part] == nil {
				node.children[part] = &diffTreeNode{name: part, children: map[string]*diffTreeNode{}}
			}
			node = node.children[part]
		}
		node.files = append(node.files, i)
	}

	rows := make([]diffTreeRow, 0, len(files))
	selectedRow := 0
	var walk func(node *diffTreeNode, depth int, parentKey string)
	compress := func(node *diffTreeNode) (string, *diffTreeNode) {
		parts := []string{node.name}
		current := node
		for len(current.files) == 0 && len(current.children) == 1 {
			for _, child := range current.children {
				parts = append(parts, child.name)
				current = child
				break
			}
		}
		return strings.Join(parts, "/"), current
	}
	walk = func(node *diffTreeNode, depth int, parentKey string) {
		dirNames := make([]string, 0, len(node.children))
		for name := range node.children {
			dirNames = append(dirNames, name)
		}
		sort.Strings(dirNames)
		for _, name := range dirNames {
			compacted, current := compress(node.children[name])
			dirKey := strings.Trim(strings.Trim(parentKey, "/")+"/"+compacted, "/")
			rows = append(rows, diffTreeRow{
				label:     compacted + "/",
				fileIndex: -1,
				isDir:     true,
				depth:     depth,
				key:       dirKey,
			})
			if !collapsed[dirKey] {
				walk(current, depth+1, dirKey)
			}
		}
		for _, fileIndex := range node.files {
			if selected == fileIndex {
				selectedRow = len(rows)
			}
			rows = append(rows, diffTreeRow{
				label:     filepath.Base(files[fileIndex].Path),
				status:    files[fileIndex].Status,
				fileIndex: fileIndex,
				isDir:     false,
				depth:     depth,
				key:       files[fileIndex].Path,
			})
		}
	}
	walk(root, 0, "")
	return rows, selectedRow
}

func (u *tuiState) renderDiffFileList() {
	u.diffFiles.Clear()
	headers := []string{"", "FILES"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetAttributes(tcell.AttrBold).
			SetTextColor(ansiColor(ansiCyan)).
			SetSelectable(false)
		if col == 1 {
			cell.SetExpansion(1)
		}
		u.diffFiles.SetCell(0, col, cell)
	}

	if len(u.diffItems) == 0 {
		u.diffFiles.SetCell(1, 0, tview.NewTableCell("").SetSelectable(false))
		u.diffFiles.SetCell(1, 1, tview.NewTableCell("(no changed files)").SetTextColor(ansiColor(ansiMagenta)).SetSelectable(false))
		u.diffFiles.SetCounter("0 of 0")
		u.diffFiles.SetOffset(0, 0)
		return
	}

	rows, selectedRow := buildDiffTreeRows(u.diffItems, u.diffSel, u.diffTreeCollapsed, u.diffTree)
	u.diffTreeRows = rows
	if u.diffTreeRowSel < 0 || u.diffTreeRowSel >= len(rows) {
		u.diffTreeRowSel = selectedRow
	}
	if len(rows) > 0 {
		selectedRow = u.diffTreeRowSel
	}
	for i, entry := range rows {
		row := i + 1
		selected := i == u.diffTreeRowSel
		marker := " "
		if selected {
			marker = ">"
		}
		status := strings.TrimSpace(entry.status)
		if !entry.isDir && status == "" {
			status = "??"
		}

		markerCell := tview.NewTableCell(marker).SetTextColor(ansiColor(ansiCyan))
		statusColor := diffStatusColor(status)
		if entry.isDir {
			statusColor = ansiColor(ansiCyan)
		}
		indent := strings.Repeat("  ", entry.depth)
		label := indent + entry.label
		if entry.isDir && u.diffTree {
			icon := "▾ "
			if u.diffTreeCollapsed[entry.key] {
				icon = "▸ "
			}
			label = indent + icon + entry.label
		}
		if !entry.isDir {
			label = fmt.Sprintf("%-2s %s", status, entry.label)
			label = indent + label
		}
		pathCell := tview.NewTableCell(truncatePath(label, 100)).SetExpansion(1).SetTextColor(statusColor)
		if selected {
			markerCell.SetAttributes(tcell.AttrReverse)
			pathCell.SetAttributes(tcell.AttrReverse)
		}
		u.diffFiles.SetCell(row, 0, markerCell)
		u.diffFiles.SetCell(row, 1, pathCell)
	}
	u.diffFiles.SetCounter(fmt.Sprintf("%d of %d", u.diffSel+1, len(u.diffItems)))
	u.ensureDiffSelectionVisible(selectedRow, len(rows))
}

func (u *tuiState) ensureDiffSelectionVisible(selectedRow int, totalRows int) {
	if totalRows == 0 {
		u.diffFiles.SetOffset(0, 0)
		return
	}
	_, _, _, h := u.diffFiles.GetInnerRect()
	visibleRows := h - 1
	if visibleRows < 1 {
		visibleRows = 1
	}
	maxOffset := totalRows - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := selectedRow - (visibleRows / 2)
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	u.diffFiles.SetOffset(offset, 0)
}

func (u *tuiState) renderSelectedFileDiff() {
	u.renderDetails()
}

func (u *tuiState) renderDirectoryPreview(row diffTreeRow) {
	if !row.isDir {
		return
	}
	prefix := strings.TrimSuffix(row.key, "/")
	if prefix != "" {
		prefix += "/"
	}
	count := 0
	for _, file := range u.diffItems {
		if strings.HasPrefix(file.Path, prefix) {
			count++
		}
	}
	state := "expanded"
	if u.diffTreeCollapsed[row.key] {
		state = "collapsed"
	}
	u.setDiffText(fmt.Sprintf("%s (%s)\n\n%d changed files\n\nenter: toggle directory\n`: tree/flat\n-: collapse all\n=: expand all", row.label, state, count), false)
}

func (u *tuiState) detailDiffWidth() int {
	_, _, w, _ := u.diffView.GetInnerRect()
	if w <= 0 {
		return 0
	}
	return w
}

func (u *tuiState) agentTerminalInnerSize() paneSize {
	_, _, w, h := u.agentTerm.GetInnerRect()
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return paneSize{w: w, h: h}
}

func (u *tuiState) syncDetailPaneSize(item *Worktree) {
	if item == nil {
		return
	}
	target := u.detail
	if u.detailTab == detailTabAgent {
		target = nil
	}
	var w, h int
	if target == nil {
		_, _, w, h = u.agentTerm.GetInnerRect()
	} else {
		_, _, w, h = target.GetInnerRect()
	}
	if w <= 0 || h <= 0 {
		return
	}
	if w < 20 {
		w = 20
	}
	if h < 4 {
		h = 4
	}

	paneTarget := u.mgr.agentPaneTarget(u.repoRoot, item)
	if paneTarget == "" {
		return
	}

	if last, ok := u.paneSizes[paneTarget]; ok && last.w == w && last.h == h {
		return
	}
	if err := tmuxResizePane(paneTarget, w, h); err != nil {
		return
	}
	u.paneSizes[paneTarget] = paneSize{w: w, h: h}
}

func (u *tuiState) setDetailText(text string, follow bool) {
	u.agentPages.ShowPage("text")
	u.agentPages.HidePage("terminal")
	u.agentDetailTextMode = true
	u.setDetailRenderedText(tview.Escape(text), follow)
}

func (u *tuiState) setDetailANSI(text string, follow bool) {
	u.agentPages.ShowPage("text")
	u.agentPages.HidePage("terminal")
	u.agentDetailTextMode = true
	u.setDetailRenderedText(tview.TranslateANSI(normalizeTerminalStream(text)), follow)
}

func (u *tuiState) setDetailRenderedText(text string, follow bool) {
	if text == u.lastDetail {
		return
	}
	row, col := u.detail.GetScrollOffset()
	stickToEnd := follow && u.detailIsPinnedToBottom()
	u.detail.SetText(text)
	u.lastDetail = text
	if stickToEnd {
		u.detail.ScrollToEnd()
		return
	}
	if u.app.GetFocus() == u.detail {
		u.detail.ScrollTo(row, col)
		return
	}
	if follow {
		u.detail.ScrollToEnd()
	} else {
		u.detail.ScrollToBeginning()
	}
}

func (u *tuiState) setAgentTerminalData(paneTarget string, data []byte, reset bool) {
	u.agentPages.ShowPage("terminal")
	u.agentPages.HidePage("text")
	u.agentDetailTextMode = false
	_, _, w, h := u.agentTerm.GetInnerRect()
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	if reset {
		u.agentTerm.ResetWithData(paneTarget, data, w, h)
		return
	}
	u.agentTerm.AppendData(paneTarget, data, w, h)
}

func (u *tuiState) detailIsPinnedToBottom() bool {
	row, _ := u.detail.GetScrollOffset()
	_, _, _, h := u.detail.GetInnerRect()
	if h <= 0 {
		return true
	}
	lineCount := strings.Count(u.lastDetail, "\n") + 1
	maxTop := lineCount - h
	if maxTop < 0 {
		maxTop = 0
	}
	return row >= maxTop-1
}

func (u *tuiState) setDiffText(text string, keepScroll bool) {
	u.setDiffRenderedText(tview.Escape(text), keepScroll)
}

func readFileChunk(path string, offset int64) (fileChunkResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileChunkResult{nextOffset: offset}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fileChunkResult{nextOffset: offset}, err
	}
	size := info.Size()
	rolledBack := false
	if offset < 0 {
		offset = 0
	}
	if offset > size {
		offset = 0
		rolledBack = true
	}
	buf := make([]byte, size-offset)
	if len(buf) == 0 {
		return fileChunkResult{nextOffset: size, reset: rolledBack}, nil
	}
	if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
		return fileChunkResult{nextOffset: offset, reset: rolledBack}, err
	}
	return fileChunkResult{data: buf, nextOffset: size, reset: rolledBack}, nil
}

func agentTerminalNeedsReseed(state agentTerminalViewState, paneTarget string, size paneSize, streamReset bool) bool {
	if state.needsReset {
		return true
	}
	if strings.TrimSpace(state.paneTarget) == "" {
		return true
	}
	if state.paneTarget != paneTarget {
		return true
	}
	if state.size != size {
		return true
	}
	return streamReset
}

func hasTerminalControlData(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	text := string(data)
	if strings.Contains(text, "\x1b") {
		return true
	}
	if strings.Contains(text, "\r") || strings.Contains(text, "\b") {
		return true
	}
	return false
}

func syncAgentTerminalMirror(state agentTerminalViewState, paneTarget string, size paneSize, captureLines int, deps agentTerminalSyncDeps) (agentTerminalSyncResult, error) {
	result := agentTerminalSyncResult{
		nextState: agentTerminalViewState{
			paneTarget:   paneTarget,
			streamOffset: state.streamOffset,
			size:         size,
		},
		ready: agentPromptUnknown,
	}
	reset := agentTerminalNeedsReseed(state, paneTarget, size, false)

	var (
		chunk      fileChunkResult
		streamErr  error
		streamPath string
	)
	if deps.ensureLog != nil {
		streamPath, streamErr = deps.ensureLog(false)
		if streamErr == nil && deps.readChunk != nil {
			chunk, streamErr = deps.readChunk(streamPath, state.streamOffset)
			if streamErr == nil && chunk.reset {
				reset = true
				if reseedPath, err := deps.ensureLog(true); err == nil {
					streamPath = reseedPath
					chunk, _ = deps.readChunk(streamPath, 0)
				}
			}
		}
	}
	if errors.Is(streamErr, os.ErrNotExist) {
		streamErr = nil
		reset = true
		result.nextState.needsReset = true
	}
	if streamErr != nil {
		reset = true
		result.nextState.needsReset = true
	}
	if streamErr == nil {
		result.nextState.streamOffset = chunk.nextOffset
	}

	if reset {
		if len(chunk.data) > 0 && hasTerminalControlData(chunk.data) {
			result.ready = agentPromptStateForOutput(string(chunk.data))
			result.data = chunk.data
			result.reset = true
			return result, nil
		}
		if deps.capture == nil {
			if streamErr != nil {
				return result, streamErr
			}
			return result, fmt.Errorf("agent snapshot capture is not available")
		}
		snapshot, err := deps.capture(captureLines)
		if err != nil {
			if streamErr != nil {
				return result, streamErr
			}
			return result, err
		}
		if strings.TrimSpace(snapshot) == "" {
			snapshot = "(agent pane is running, but no output yet)"
			result.ready = agentPromptBusy
		} else {
			result.ready = agentPromptStateForOutput(snapshot)
		}
		result.data = []byte(snapshot)
		result.reset = true
		return result, nil
	}

	if len(chunk.data) > 0 {
		result.data = chunk.data
		result.ready = agentPromptStateForOutput(string(chunk.data))
	}
	result.reset = false
	return result, nil
}

func (u *tuiState) setDiffANSI(text string, keepScroll bool) {
	u.setDiffRenderedText(tview.TranslateANSI(text), keepScroll)
}

func (u *tuiState) setDiffRenderedText(text string, keepScroll bool) {
	if text == u.lastDiff {
		return
	}
	row, col := u.diffView.GetScrollOffset()
	u.diffView.SetText(text)
	u.lastDiff = text
	if keepScroll {
		u.diffView.ScrollTo(row, col)
		return
	}
	u.diffView.ScrollToBeginning()
}

func (u *tuiState) detailCaptureLineCount() int {
	_, _, _, h := u.detail.GetInnerRect()
	if h <= 0 {
		return detailCaptureLines
	}
	lines := h + 6
	if lines > detailCaptureLines {
		lines = detailCaptureLines
	}
	if lines < 20 {
		lines = 20
	}
	return lines
}

func (u *tuiState) scrollTextView(view *tview.TextView, delta int) {
	if view == nil {
		return
	}
	row, col := view.GetScrollOffset()
	next := row + delta
	if next < 0 {
		next = 0
	}
	view.ScrollTo(next, col)
}

func (u *tuiState) worktreeGraphic(selectedPath string) string {
	if len(u.items) == 0 {
		return lipgloss.NewStyle().Foreground(ColorPurple).Render("(no worktrees)")
	}

	ordered := append([]Worktree(nil), u.items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Current != ordered[j].Current {
			return ordered[i].Current
		}
		if ordered[i].Branch == ordered[j].Branch {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Branch < ordered[j].Branch
	})

	repoLabel := u.repoName
	if strings.TrimSpace(u.repoSlug) != "" {
		repoLabel = u.repoSlug
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(ColorGreen).Render(repoLabel),
		lipgloss.NewStyle().Foreground(ColorCyan).Render("│"),
	}

	for i, wt := range ordered {
		branch := wt.Branch
		if branch == "" {
			branch = "detached"
		}

		arm := lipgloss.NewStyle().Foreground(ColorCyan).Render("├─")
		stem := lipgloss.NewStyle().Foreground(ColorCyan).Render("│ ")
		if i == len(ordered)-1 {
			arm = lipgloss.NewStyle().Foreground(ColorCyan).Render("└─")
			stem = "  "
		}

		marker := "○"
		markerColor := ColorCyan
		if wt.Current {
			marker = "●"
			markerColor = ColorGreen
		}
		if wt.Path == selectedPath {
			marker = "◆"
			markerColor = ColorBlue
		}

		branchColor := ColorCyan
		if wt.Dirty {
			branchColor = ColorRed
		} else if wt.Current {
			branchColor = ColorGreen
		}

		state := "clean"
		stateColor := ColorGreen
		if wt.Dirty {
			state = "dirty"
			stateColor = ColorRed
		}

		tmuxState := lipgloss.NewStyle().Foreground(ColorCyan).Render("·")
		switch wt.TmuxState {
		case "yes":
			tmuxState = lipgloss.NewStyle().Foreground(ColorGreen).Render("●")
		case "no":
			tmuxState = lipgloss.NewStyle().Foreground(ColorRed).Render("○")
		}

		branchText := lipgloss.NewStyle().Bold(true).Foreground(branchColor).Render(truncate(branch, 42))
		stateText := lipgloss.NewStyle().Foreground(stateColor).Render("(" + state + ")")
		markerText := lipgloss.NewStyle().Foreground(markerColor).Render(marker)

		line := fmt.Sprintf(
			"%s%s %s %s tmux:%s",
			arm, markerText, branchText, stateText, tmuxState,
		)
		lines = append(lines, line)

		pathColor := ColorPurple
		if wt.Path == selectedPath {
			pathColor = ColorBlue
		}
		pathArm := lipgloss.NewStyle().Foreground(ColorCyan).Render("└─")
		pathText := lipgloss.NewStyle().Foreground(pathColor).Render(truncatePath(wt.Path, 74))
		lines = append(lines, fmt.Sprintf("%s%s %s", stem, pathArm, pathText))
	}

	return tview.TranslateANSI(strings.Join(lines, "\n"))
}

func (u *tuiState) setStatus(format string, args ...any) {
	u.renderFooter("STATUS", fmt.Sprintf(format, args...))
}

func (u *tuiState) setInfo(format string, args ...any) {
	u.renderFooter("INFO", fmt.Sprintf(format, args...))
}

func (u *tuiState) setWarn(format string, args ...any) {
	u.renderFooter("WARN", fmt.Sprintf(format, args...))
}

func (u *tuiState) setError(format string, args ...any) {
	u.renderFooter("ERROR", fmt.Sprintf(format, args...))
}

func (u *tuiState) footerKeymap() string {
	base := "[::b]tab[::-] pane | [::b]R[::-] refresh | [::b]?[::-] help | [::b]q[::-] quit"
	focus := u.app.GetFocus()
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.agentTerm || focus == u.diffFiles || focus == u.diffView

	switch {
	case focus == u.statusPane:
		return "[::b]enter[::-] repos | " + base
	case focus == u.table:
		return "[::b]j/k[::-] move | [::b]p[::-] preview | [::b]enter[::-] attach | " + base
	case inDetail:
		return "[::b]j/k[::-] rows | [::b]enter[::-] toggle dir | [::b]`[::-] tree/flat | [::b]-/=[::-] fold | [::b]J/K[::-] patch scroll | " + base
	default:
		return "[::b]tab[::-] cycle modal focus | [::b]esc[::-] close modal"
	}
}

func (u *tuiState) renderFooter(level, message string) {
	if strings.TrimSpace(level) == "" {
		level = "INFO"
	}
	if strings.TrimSpace(message) == "" {
		message = "ready"
	}
	u.footerLevel = level
	u.footerMsg = message
	u.redrawFooter()
}

func (u *tuiState) redrawFooter() {
	level := u.footerLevel
	if strings.TrimSpace(level) == "" {
		level = "INFO"
	}
	message := u.footerMsg
	if strings.TrimSpace(message) == "" {
		message = "ready"
	}
	levelColor := ColorCyan
	switch level {
	case "ERROR":
		levelColor = ColorRed
	case "WARN":
		levelColor = ColorPurple
	case "INFO":
		levelColor = ColorBlue
	}

	keymapStyle := lipgloss.NewStyle().Foreground(ColorGray)
	levelStyle := lipgloss.NewStyle().Foreground(levelColor).Bold(true)
	msgStyle := lipgloss.NewStyle()
	versionStyle := lipgloss.NewStyle().Foreground(ColorCyan)

	left := fmt.Sprintf("╰─ %s  %s: %s",
		keymapStyle.Render(u.footerKeymap()),
		levelStyle.Render(level),
		msgStyle.Render(message),
	)
	right := fmt.Sprintf("─ %s ╯", versionStyle.Render("v"+Version))

	u.footerLeft.SetText(tview.TranslateANSI(left))
	u.footerRight.SetText(tview.TranslateANSI(right))
}

func (u *tuiState) showModal(name string, p tview.Primitive, width, height int) {
	u.pages.AddPage(name, centered(width, height, p), true, true)
	u.app.SetFocus(p)
	u.updatePaneFocusStyles()
}

func (u *tuiState) closeModal(name string) {
	u.pages.RemovePage(name)
	u.app.SetFocus(u.table)
	u.updatePaneFocusStyles()
}

func formatByteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	uIdx := 0
	for v >= 1024 && uIdx < len(units)-1 {
		v /= 1024
		uIdx++
	}
	return fmt.Sprintf("%.1f %s", v, units[uIdx])
}

func (u *tuiState) showProgressModal(name, title string, totalSteps int) (func(string), func(string), func(float64), func()) {
	const barWidth = 44
	const modalWidth = 64

	titleView := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	titleView.SetBackgroundColor(tcell.ColorDefault)
	titleStyle := lipgloss.NewStyle().Foreground(ThemeColorPrimary).Bold(true)
	titleView.SetText(tview.TranslateANSI(" " + titleStyle.Render(strings.TrimSpace(title))))

	stepView := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	stepView.SetBackgroundColor(tcell.ColorDefault)
	stepView.SetTextColor(tcell.ColorDefault)

	barView := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	barView.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(titleView, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(stepView, 1, 0, false).
		AddItem(barView, 1, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	u.showModal(name, layout, modalWidth, 7)
	u.app.SetFocus(layout)

	spinChars := []string{"|", "/", "-", "\\"}

	var mu sync.Mutex
	var step int
	var stepProgress float64
	label := "Working..."
	var frame int

	render := func() {
		mu.Lock()
		s, sp, l, f := step, stepProgress, label, frame
		mu.Unlock()

		pct := 0.0
		if totalSteps > 0 {
			if sp < 0 {
				sp = 0
			}
			if sp > 1 {
				sp = 1
			}
			if s > 0 {
				base := float64(s - 1)
				pct = (base + sp) / float64(totalSteps)
			}
			if pct > 1.0 {
				pct = 1.0
			}
		}

		filled := int(float64(barWidth) * pct)
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		spin := spinChars[f%len(spinChars)]
		spinStyle := lipgloss.NewStyle().Foreground(ThemeColorPrimary)
		stepView.SetText(tview.TranslateANSI(fmt.Sprintf(" %s %s", spinStyle.Render(spin), l)))

		filledStyle := lipgloss.NewStyle().Foreground(ThemeColorPrimary)
		emptyStyle := lipgloss.NewStyle().Foreground(ColorGray)
		pctStyle := lipgloss.NewStyle().Foreground(ThemeColorPrimary).Bold(true)

		var pctText string
		if s == 0 {
			pctText = pctStyle.Render("  --%")
		} else {
			pctText = pctStyle.Render(fmt.Sprintf(" %3d%%", int(pct*100)))
		}

		barText := " " +
			filledStyle.Render(strings.Repeat("█", filled)) +
			emptyStyle.Render(strings.Repeat("░", empty)) +
			pctText
		barView.SetText(tview.TranslateANSI(barText))
	}
	render()

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				frame++
				mu.Unlock()
				u.app.QueueUpdateDraw(func() {
					render()
				})
			}
		}
	}()

	advance := func(next string) {
		mu.Lock()
		step++
		stepProgress = 0
		if strings.TrimSpace(next) != "" {
			label = strings.TrimSpace(next)
		}
		mu.Unlock()
		u.app.QueueUpdateDraw(func() {
			render()
		})
	}
	setLabel := func(next string) {
		mu.Lock()
		if strings.TrimSpace(next) != "" {
			label = strings.TrimSpace(next)
		}
		mu.Unlock()
		u.app.QueueUpdateDraw(func() {
			render()
		})
	}
	setStepProgress := func(progress float64) {
		mu.Lock()
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
		stepProgress = progress
		mu.Unlock()
		u.app.QueueUpdateDraw(func() {
			render()
		})
	}
	stop := func() {
		close(done)
	}
	return advance, setLabel, setStepProgress, stop
}

func centered(width, height int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(
			tview.NewFlex().
				SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(p, height, 1, true).
				AddItem(nil, 0, 1, false),
			width, 1, true,
		).
		AddItem(nil, 0, 1, false)
}

func styleModalInputField(field *tview.InputField) {
	field.
		SetLabel("").
		SetFieldTextColor(tcell.ColorDefault).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetBackgroundColor(tcell.ColorDefault)
}

func styleModalCheckbox(field *tview.Checkbox) {
	field.
		SetLabelColor(ansiColor(ansiCyan)).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(tcell.ColorDefault).
		SetBackgroundColor(tcell.ColorDefault)
}

func styleModalDropDown(field *tview.DropDown) {
	base := tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault)
	focus := base.Reverse(true)
	field.
		SetLabel("").
		SetLabelColor(ansiColor(ansiCyan)).
		SetFieldTextColor(tcell.ColorDefault).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldStyle(base).
		SetFocusedStyle(focus).
		SetListStyles(base, focus).
		SetTextOptions("", "", "> ", "", "(select)").
		SetBackgroundColor(tcell.ColorDefault)
}

func modalHeader(title string) *tview.TextView {
	header := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	header.SetBackgroundColor(tcell.ColorDefault)
	header.SetTextColor(paneBorderColor())
	header.SetText(" " + title)
	return header
}

func modalFieldBox(title string, inner tview.Primitive) *tview.Flex {
	box := tview.NewFlex().SetDirection(tview.FlexRow)
	box.AddItem(inner, 1, 1, false)
	box.SetBackgroundColor(tcell.ColorDefault)
	box.SetBorder(true)
	box.SetBorderColor(paneBorderColor())
	box.SetTitle(" " + title + " ")
	box.SetTitleColor(ansiColor(ansiCyan))
	return box
}

func modalButton(label string, selected func()) *tview.Button {
	btn := tview.NewButton(label).SetSelectedFunc(selected)
	btn.SetLabelColor(tcell.ColorDefault)
	btn.SetLabelColorActivated(ansiColor(ansiCyan))
	btn.SetBackgroundColor(tcell.ColorDefault)
	btn.SetBackgroundColorActivated(tcell.ColorDefault)
	return btn
}

func setPrimitiveInputCapture(p tview.Primitive, capture func(ev *tcell.EventKey) *tcell.EventKey) {
	switch v := p.(type) {
	case *tview.InputField:
		v.SetInputCapture(capture)
	case *tview.DropDown:
		v.SetInputCapture(capture)
	case *tview.Checkbox:
		v.SetInputCapture(capture)
	case *tview.Button:
		v.SetInputCapture(capture)
	case *tview.Table:
		v.SetInputCapture(capture)
	}
}

func cycleModalFocus(app *tview.Application, focusables []tview.Primitive, delta int) {
	if len(focusables) == 0 {
		return
	}
	cur := app.GetFocus()
	idx := 0
	for i, f := range focusables {
		if cur == f {
			idx = i
			break
		}
	}
	next := (idx + delta) % len(focusables)
	if next < 0 {
		next += len(focusables)
	}
	app.SetFocus(focusables[next])
}

func modalCapture(
	app *tview.Application,
	focusables []tview.Primitive,
	onEsc func(),
	shortcuts map[rune]func(),
) func(ev *tcell.EventKey) *tcell.EventKey {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			onEsc()
			return nil
		case tcell.KeyTAB:
			cycleModalFocus(app, focusables, 1)
			return nil
		case tcell.KeyBacktab:
			cycleModalFocus(app, focusables, -1)
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			key := unicode.ToLower(ev.Rune())
			if fn, ok := shortcuts[key]; ok {
				if ev.Modifiers()&tcell.ModAlt != 0 {
					fn()
					return nil
				}
				switch app.GetFocus().(type) {
				case *tview.InputField, *tview.DropDown:
					return ev
				default:
					fn()
					return nil
				}
			}
		}
		return ev
	}
}

func (u *tuiState) showRepoSwitchModal() {
	u.refreshRepoChoices(true)
	if len(u.repos) <= 1 {
		u.setWarn("no other repositories found near current repo")
		return
	}

	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorders(false)
	table.SetSeparator(' ')
	table.SetBackgroundColor(tcell.ColorDefault)
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	table.SetBorder(true)
	table.SetBorderColor(paneBorderColor())

	headers := []string{"", "Repository", "Branch", "Path"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetAttributes(tcell.AttrBold).
			SetTextColor(ansiColor(ansiCyan)).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, col, cell)
	}

	currentRow := 1
	for i, repo := range u.repos {
		row := i + 1
		mark := " "
		if repo.Root == u.repoRoot {
			mark = "*"
			currentRow = row
		}

		nameCell := tview.NewTableCell(repo.Name).SetExpansion(1)
		if repo.Root == u.repoRoot {
			nameCell.SetAttributes(tcell.AttrBold)
		}

		table.SetCell(row, 0, tview.NewTableCell(mark).SetTextColor(ansiColor(ansiGreen)).SetExpansion(1))
		table.SetCell(row, 1, nameCell)
		table.SetCell(row, 2, tview.NewTableCell(repo.Branch).SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
		table.SetCell(row, 3, tview.NewTableCell(repo.Root).SetTextColor(ansiColor(ansiMagenta)).SetExpansion(1))
	}

	cancelRow := len(u.repos) + 1
	table.SetCell(cancelRow, 0, tview.NewTableCell(""))
	table.SetCell(cancelRow, 1, tview.NewTableCell("Cancel").SetTextColor(tcell.ColorDefault))

	counter := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetTextAlign(tview.AlignRight)
	counter.SetTextColor(paneBorderColor())
	counter.SetBackgroundColor(tcell.ColorDefault)

	updateCounter := func(row int) {
		if row < 1 {
			row = 1
		}
		total := len(u.repos) + 1
		if row > total {
			row = total
		}
		counter.SetText(fmt.Sprintf("%d of %d", row, total))
	}

	selectRow := func(row int) {
		if row <= 0 {
			return
		}
		if row == cancelRow {
			u.closeModal("repos")
			u.setInfo("repo switch canceled")
			return
		}
		idx := row - 1
		if idx < 0 || idx >= len(u.repos) {
			return
		}
		u.closeModal("repos")
		u.switchRepo(u.repos[idx])
	}

	table.SetSelectionChangedFunc(func(row, col int) {
		updateCounter(row)
	})
	table.SetSelectedFunc(func(row, col int) {
		selectRow(row)
	})
	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			u.closeModal("repos")
			u.setInfo("repo switch canceled")
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'c':
				u.closeModal("repos")
				u.setInfo("repo switch canceled")
				return nil
			case 'j':
				row, _ := table.GetSelection()
				if row < cancelRow {
					table.Select(row+1, 0)
				}
				return nil
			case 'k':
				row, _ := table.GetSelection()
				if row > 1 {
					table.Select(row-1, 0)
				}
				return nil
			}
		}
		return ev
	})

	meta := tview.NewFlex().
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(counter, 10, 0, false)

	picker := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 1, true).
		AddItem(meta, 1, 0, false)
	picker.SetBackgroundColor(tcell.ColorDefault)

	u.showModal("repos", picker, 150, 22)
	table.Select(currentRow, 0)
	updateCounter(currentRow)
	u.app.SetFocus(table)
}

func (u *tuiState) switchRepo(repo repoChoice) {
	if repo.Root == "" || repo.Root == u.repoRoot {
		return
	}
	if err := os.Chdir(repo.Root); err != nil {
		u.setError("switch failed: %v", err)
		return
	}
	u.repoRoot = repo.Root
	u.repoName = repo.Name
	u.repoSlug = repo.GitHubRepo
	u.filter = ""
	u.selected = 0
	if err := u.refresh(); err != nil {
		u.setError("switched repo, refresh failed: %v", err)
		return
	}
	u.setInfo("switched repo: %s", repoChoiceLabel(repo))
}

func (u *tuiState) showFilterModal() {
	input := tview.NewInputField().SetText(u.filter)
	styleModalInputField(input)

	applyFilter := func() {
		u.filter = strings.TrimSpace(input.GetText())
		u.applyFilter()
		u.renderTable()
		u.renderDetails()
		u.setInfo("filter updated")
		u.closeModal("filter")
	}
	clearFilter := func() {
		u.filter = ""
		u.applyFilter()
		u.renderTable()
		u.renderDetails()
		u.setInfo("filter cleared")
		u.closeModal("filter")
	}
	cancel := func() {
		u.closeModal("filter")
	}

	applyBtn := modalButton("<a> Apply", applyFilter)
	clearBtn := modalButton("<l> Clear", clearFilter)
	cancelBtn := modalButton("<c> Cancel", cancel)

	row := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(applyBtn, 12, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(clearBtn, 12, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(cancelBtn, 12, 0, false).
		AddItem(nil, 0, 1, false)

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(modalHeader("Filter Worktrees"), 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(modalFieldBox("Filter Query", input), 3, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(row, 1, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	focusables := []tview.Primitive{input, applyBtn, clearBtn, cancelBtn}
	capture := modalCapture(u.app, focusables, cancel, map[rune]func(){
		'a': applyFilter,
		'l': clearFilter,
		'c': cancel,
	})
	for _, p := range focusables {
		setPrimitiveInputCapture(p, capture)
	}
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			applyFilter()
		}
	})

	u.showModal("filter", layout, 76, 11)
	u.app.SetFocus(input)
}

func (u *tuiState) showCreateModal() {
	repoRoot, err := u.mgr.RequireRepo()
	if err != nil {
		u.setError("not in a git repo: %v", err)
		return
	}

	allBranches, _ := u.mgr.ListBranches(repoRoot)
	creating := false

	type branchRow struct {
		name     string
		isNew    bool
		isRemote bool
	}
	var displayRows []branchRow

	input := tview.NewInputField()
	styleModalInputField(input)
	input.SetPlaceholder("type to filter or enter a new branch name")
	input.SetPlaceholderTextColor(paneBorderColor())

	branchTable := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorders(false)
	branchTable.SetSeparator(' ')
	branchTable.SetBackgroundColor(tcell.ColorDefault)
	branchTable.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	branchTable.SetBorder(true)
	branchTable.SetBorderColor(paneBorderColor())

	counter := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetTextAlign(tview.AlignRight)
	counter.SetTextColor(paneBorderColor())
	counter.SetBackgroundColor(tcell.ColorDefault)

	hints := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	hints.SetTextColor(paneBorderColor())
	hints.SetBackgroundColor(tcell.ColorDefault)
	hints.SetText(" ↑↓/jk navigate  enter select  c/esc cancel")

	updateCounter := func(dataIdx int) {
		total := len(displayRows)
		if total == 0 {
			counter.SetText("")
			return
		}
		n := dataIdx + 1
		if n < 1 {
			n = 1
		}
		counter.SetText(fmt.Sprintf("%d of %d  ", n, total))
	}

	rebuildTable := func(query string) {
		displayRows = nil
		branchTable.Clear()

		branchTable.SetCell(0, 0, tview.NewTableCell("").SetSelectable(false))
		branchTable.SetCell(0, 1, tview.NewTableCell("BRANCH").
			SetTextColor(ansiColor(ansiCyan)).SetSelectable(false).SetExpansion(1))
		branchTable.SetCell(0, 2, tview.NewTableCell("").SetSelectable(false))

		rowIdx := 1
		lq := strings.ToLower(strings.TrimSpace(query))

		// Synthetic "new branch" entry when query doesn't exactly match any existing branch
		if lq != "" {
			exactMatch := false
			for _, b := range allBranches {
				if strings.ToLower(b.Name) == lq {
					exactMatch = true
					break
				}
			}
			if !exactMatch {
				name := strings.TrimSpace(query)
				branchTable.SetCell(rowIdx, 0, tview.NewTableCell("✦").SetTextColor(ansiColor(ansiGreen)).SetSelectable(true))
				branchTable.SetCell(rowIdx, 1, tview.NewTableCell(name).SetTextColor(tcell.ColorDefault).SetSelectable(true).SetExpansion(1))
				branchTable.SetCell(rowIdx, 2, tview.NewTableCell("new").SetTextColor(paneBorderColor()).SetSelectable(true))
				displayRows = append(displayRows, branchRow{name: name, isNew: true})
				rowIdx++
			}
		}

		for _, b := range allBranches {
			if lq != "" && !strings.Contains(strings.ToLower(b.Name), lq) {
				continue
			}
			typeLabel := ""
			typeColor := paneBorderColor()
			if b.Remote {
				typeLabel = "remote"
				typeColor = ansiColor(ansiMagenta)
			}
			branchTable.SetCell(rowIdx, 0, tview.NewTableCell("").SetSelectable(true))
			branchTable.SetCell(rowIdx, 1, tview.NewTableCell(b.Name).SetTextColor(tcell.ColorDefault).SetSelectable(true).SetExpansion(1))
			branchTable.SetCell(rowIdx, 2, tview.NewTableCell(typeLabel).SetTextColor(typeColor).SetSelectable(true))
			displayRows = append(displayRows, branchRow{name: b.Name, isRemote: b.Remote})
			rowIdx++
		}

		if len(displayRows) == 0 && lq == "" {
			branchTable.SetCell(1, 0, tview.NewTableCell(""))
			branchTable.SetCell(1, 1, tview.NewTableCell("no branches available — type a name to create one").
				SetTextColor(paneBorderColor()).SetSelectable(false).SetExpansion(1))
			branchTable.SetCell(1, 2, tview.NewTableCell(""))
		}

		if len(displayRows) > 0 {
			branchTable.Select(1, 0)
			updateCounter(0)
		} else {
			counter.SetText("")
		}
	}

	doCreate := func(branch string, fromExisting bool) {
		if creating {
			return
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			u.setWarn("branch name is required")
			return
		}
		u.closeModal("create")
		creating = true

		// Creation runs entirely in the background so the UI stays interactive.
		// Progress is surfaced through the footer status line instead of a
		// blocking modal.
		setBgStatus := func(msg string) {
			u.app.QueueUpdateDraw(func() {
				u.setStatus("%s", msg)
			})
		}

		go func(branch string, fromExisting bool) {
			var path string
			var createErr error
			warnings := []string{}
			var refreshed []Worktree
			var refreshErr error

			var opts NewOptions
			if fromExisting {
				opts = NewOptions{FromBranch: branch, Launch: false}
			} else {
				opts = NewOptions{Branch: branch, Launch: false}
			}

			debugLogf("ui_create start branch=%q existing=%t auto_launch=%t auto_start_agent=%t", branch, fromExisting, u.mgr.Cfg.AutoLaunch, u.mgr.Cfg.AutoStartAgent)
			setBgStatus(fmt.Sprintf("Creating worktree %s...", branch))
			_, path, createErr = u.mgr.NewWorktree(opts)
			if createErr != nil {
				debugLogf("ui_create new_worktree failed branch=%q: %v", branch, createErr)
			}

			if createErr == nil && u.mgr.HasBootstrap() {
				setBgStatus(fmt.Sprintf("Bootstrapping worktree %s...", branch))
				lastLabel := time.Time{}
				bootstrapOut := newLineWriter(func(line string) {
					line = strings.TrimSpace(line)
					if line == "" {
						return
					}
					// Throttle UI redraws on noisy install output.
					now := time.Now()
					if !lastLabel.IsZero() && now.Sub(lastLabel) < 120*time.Millisecond {
						return
					}
					lastLabel = now
					setBgStatus(fmt.Sprintf("Bootstrapping %s: %s", branch, truncate(line, 60)))
				})
				if _, err := u.mgr.Bootstrap(BootstrapOptions{
					Target: path,
					OnStep: func(dir, run string) {
						setBgStatus(fmt.Sprintf("Bootstrapping %s: %s", branch, truncate(run, 60)))
					},
					Out: bootstrapOut,
				}); err != nil {
					debugLogf("ui_create bootstrap failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("bootstrap failed: %v", err))
				}
			}

			if createErr == nil && u.mgr.Cfg.AutoLaunch {
				setBgStatus(fmt.Sprintf("Launching tmux tools for %s...", branch))
				if _, err := u.mgr.Launch(LaunchOptions{Target: path, NoAttach: true}); err != nil {
					debugLogf("ui_create auto_launch failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("launch failed: %v", err))
				}
			}
			if createErr == nil && u.mgr.Cfg.AutoStartAgent {
				setBgStatus(fmt.Sprintf("Starting agent for %s...", branch))
				if _, _, err := u.mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
					debugLogf("ui_create auto_agent failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("agent start failed: %v", err))
				}
			}

			if createErr == nil {
				refreshed, refreshErr = u.mgr.ListWorktrees()
				if refreshErr != nil {
					debugLogf("ui_create refresh failed path=%q: %v", path, refreshErr)
				}
			}

			u.app.QueueUpdateDraw(func() {
				if createErr != nil {
					u.setError("create failed (%s): %v", branch, createErr)
					return
				}

				if refreshErr == nil {
					u.refreshRepoChoices(false)
					u.repoBranch = u.mgr.CurrentBranch(u.repoRoot)
					u.items = refreshed
					u.applyFilter()
					u.renderTable()
					u.renderTableMeta()
					u.renderDetails()
					u.renderStatusPane()
					u.selectPath(path)
				}

				if len(warnings) > 0 {
					u.setWarn("created: %s (warnings: %s)", path, strings.Join(warnings, " | "))
					return
				}
				if refreshErr != nil {
					u.setWarn("created: %s (refresh failed: %v)", path, refreshErr)
					return
				}
				debugLogf("ui_create success path=%q warnings=%d", path, len(warnings))
				u.setInfo("created: %s", path)
			})
		}(branch, fromExisting)
	}

	selectCurrentRow := func() {
		row, _ := branchTable.GetSelection()
		if row < 1 || row-1 >= len(displayRows) {
			return
		}
		r := displayRows[row-1]
		doCreate(r.name, !r.isNew)
	}

	cancel := func() {
		u.closeModal("create")
	}

	input.SetChangedFunc(func(text string) {
		rebuildTable(text)
	})
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cancel()
			return nil
		case tcell.KeyEnter:
			if len(displayRows) > 0 {
				r := displayRows[0]
				doCreate(r.name, !r.isNew)
			} else {
				doCreate(strings.TrimSpace(input.GetText()), false)
			}
			return nil
		case tcell.KeyDown:
			if len(displayRows) > 0 {
				u.app.SetFocus(branchTable)
				branchTable.Select(1, 0)
				updateCounter(0)
			}
			return nil
		case tcell.KeyTab:
			if len(displayRows) > 0 {
				u.app.SetFocus(branchTable)
				branchTable.Select(1, 0)
				updateCounter(0)
			}
			return nil
		}
		return ev
	})

	branchTable.SetSelectionChangedFunc(func(row, col int) {
		if row >= 1 {
			updateCounter(row - 1)
		}
	})
	branchTable.SetSelectedFunc(func(row, col int) {
		selectCurrentRow()
	})
	branchTable.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cancel()
			return nil
		case tcell.KeyEnter:
			selectCurrentRow()
			return nil
		case tcell.KeyUp:
			row, _ := branchTable.GetSelection()
			if row <= 1 {
				u.app.SetFocus(input)
				return nil
			}
		case tcell.KeyBacktab:
			u.app.SetFocus(input)
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'c':
				cancel()
				return nil
			case 'j':
				row, _ := branchTable.GetSelection()
				if row < branchTable.GetRowCount()-1 {
					branchTable.Select(row+1, 0)
				}
				return nil
			case 'k':
				row, _ := branchTable.GetSelection()
				if row > 1 {
					branchTable.Select(row-1, 0)
				} else {
					u.app.SetFocus(input)
				}
				return nil
			}
		}
		return ev
	})

	footer := tview.NewFlex().
		AddItem(hints, 0, 1, false).
		AddItem(counter, 12, 0, false)
	footer.SetBackgroundColor(tcell.ColorDefault)

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(modalHeader("Create Worktree"), 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(modalFieldBox("Branch", input), 3, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(branchTable, 0, 1, false).
		AddItem(nil, 1, 0, false).
		AddItem(footer, 1, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	rebuildTable("")
	u.showModal("create", layout, 86, 24)
	u.app.SetFocus(input)
}

func (u *tuiState) selectPath(path string) {
	for pos, idx := range u.visible {
		if u.items[idx].Path == path {
			u.selected = pos
			u.selectTableRow(u.selected+1, true)
			u.renderDetails()
			return
		}
	}
}

func (u *tuiState) selectTableRow(row int, force bool) {
	if row < 0 {
		row = 0
	}
	prev := u.forceTableSelect
	u.forceTableSelect = force
	u.table.Select(row, 0)
	u.forceTableSelect = prev
}

func (u *tuiState) showDeleteModal() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}

	branch := item.Branch
	if branch == "" {
		branch = filepath.Base(item.Path)
	}

	removing := false
	remove := func() {
		if removing {
			return
		}
		removing = true
		u.closeModal("delete")

		go func() {
			// Async removal renames the worktree aside instantly and reaps its
			// files in the background, so the list refreshes immediately instead
			// of blocking on a large tree (node_modules, build output).
			_, warnings, removeErr := u.mgr.Remove(RemoveOptions{
				Target:       item.Path,
				Force:        item.Dirty,
				DeleteBranch: false,
				Async:        true,
			})

			var refreshed []Worktree
			var refreshErr error
			if removeErr == nil {
				refreshed, refreshErr = u.mgr.ListWorktrees()
			}

			u.app.QueueUpdateDraw(func() {
				if removeErr != nil {
					u.setError("remove failed: %v", removeErr)
					return
				}

				if refreshErr == nil {
					u.refreshRepoChoices(false)
					u.repoBranch = u.mgr.CurrentBranch(u.repoRoot)
					u.items = refreshed
					u.applyFilter()
					u.renderTable()
					u.renderTableMeta()
					u.renderDetails()
					u.renderStatusPane()
				}

				if len(warnings) > 0 {
					u.setWarn("removed with warning: %s", warnings[0])
				} else {
					u.setInfo("removed: %s", branch)
				}
			})
		}()
	}
	cancel := func() {
		u.closeModal("delete")
	}

	msg := tview.NewTextView().SetDynamicColors(true)
	msg.SetBackgroundColor(tcell.ColorDefault)
	msg.SetTextColor(tcell.ColorDefault)
	msg.SetWrap(true)
	msg.SetText(fmt.Sprintf(
		"Remove worktree [::b]%s[::-]?\n\n[cyan]%s[-]",
		branch,
		truncatePath(item.Path, 96),
	))
	msg.SetBorder(true)
	msg.SetBorderColor(paneBorderColor())

	action := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	action.SetBackgroundColor(tcell.ColorDefault)
	action.SetTextColor(ansiColor(ansiCyan))
	action.SetText(fmt.Sprintf(" r - Remove worktree [::b]%s[::-]", branch))

	options := tview.NewTable().
		SetSelectable(true, false).
		SetBorders(false)
	options.SetSeparator(' ')
	options.SetBackgroundColor(tcell.ColorDefault)
	options.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	options.SetBorder(true)
	options.SetBorderColor(paneBorderColor())
	options.SetCell(0, 0, tview.NewTableCell("r").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
	options.SetCell(0, 1, tview.NewTableCell("Remove worktree").SetTextColor(tcell.ColorDefault).SetExpansion(1))
	options.SetCell(1, 0, tview.NewTableCell("c").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
	options.SetCell(1, 1, tview.NewTableCell("Cancel").SetTextColor(tcell.ColorDefault).SetExpansion(1))

	selectOption := func(row int) {
		switch row {
		case 0:
			remove()
		default:
			cancel()
		}
	}
	options.SetSelectedFunc(func(row, _ int) {
		selectOption(row)
	})
	options.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			row, _ := options.GetSelection()
			selectOption(row)
			return nil
		case tcell.KeyEscape:
			cancel()
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			switch unicode.ToLower(ev.Rune()) {
			case 'r':
				remove()
				return nil
			case 'c':
				cancel()
				return nil
			case 'j':
				row, _ := options.GetSelection()
				if row < 1 {
					options.Select(row+1, 0)
				}
				return nil
			case 'k':
				row, _ := options.GetSelection()
				if row > 0 {
					options.Select(row-1, 0)
				}
				return nil
			}
		}
		return ev
	})

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(action, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(options, 4, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(msg, 4, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	u.showModal("delete", layout, 96, 12)
	options.Select(0, 0)
	u.app.SetFocus(options)
}

func (u *tuiState) showDetachModal() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}

	branch := item.Branch
	if branch == "" {
		branch = filepath.Base(item.Path)
	}

	detach := func() {
		path, detached, err := u.mgr.Detach(item.Path)
		if err != nil {
			u.setError("detach failed: %v", err)
			return
		}
		u.closeModal("detach")
		if err := u.refresh(); err != nil {
			u.setWarn("detached, but refresh failed: %v", err)
			return
		}
		if !detached {
			u.setInfo("session was not running: %s", path)
			return
		}
		u.setInfo("detached: %s", path)
	}
	cancel := func() {
		u.closeModal("detach")
	}

	msg := tview.NewTextView().SetDynamicColors(true)
	msg.SetBackgroundColor(tcell.ColorDefault)
	msg.SetTextColor(tcell.ColorDefault)
	msg.SetWrap(true)
	msg.SetText(fmt.Sprintf(
		"Detach from worktree [::b]%s[::-]?\n\nThis will kill the tmux session for this worktree only.\n\n[cyan]%s[-]",
		branch,
		truncatePath(item.Path, 96),
	))
	msg.SetBorder(true)
	msg.SetBorderColor(paneBorderColor())

	action := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	action.SetBackgroundColor(tcell.ColorDefault)
	action.SetTextColor(ansiColor(ansiCyan))
	action.SetText(fmt.Sprintf(" x - Detach worktree [::b]%s[::-]", branch))

	options := tview.NewTable().
		SetSelectable(true, false).
		SetBorders(false)
	options.SetSeparator(' ')
	options.SetBackgroundColor(tcell.ColorDefault)
	options.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	options.SetBorder(true)
	options.SetBorderColor(paneBorderColor())
	options.SetCell(0, 0, tview.NewTableCell("x").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
	options.SetCell(0, 1, tview.NewTableCell("Detach session").SetTextColor(tcell.ColorDefault).SetExpansion(1))
	options.SetCell(1, 0, tview.NewTableCell("c").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
	options.SetCell(1, 1, tview.NewTableCell("Cancel").SetTextColor(tcell.ColorDefault).SetExpansion(1))

	selectOption := func(row int) {
		switch row {
		case 0:
			detach()
		default:
			cancel()
		}
	}
	options.SetSelectedFunc(func(row, _ int) {
		selectOption(row)
	})
	options.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			row, _ := options.GetSelection()
			selectOption(row)
			return nil
		case tcell.KeyEscape:
			cancel()
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			switch unicode.ToLower(ev.Rune()) {
			case 'x':
				detach()
				return nil
			case 'c':
				cancel()
				return nil
			case 'j':
				row, _ := options.GetSelection()
				if row < 1 {
					options.Select(row+1, 0)
				}
				return nil
			case 'k':
				row, _ := options.GetSelection()
				if row > 0 {
					options.Select(row-1, 0)
				}
				return nil
			}
		}
		return ev
	})

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(action, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(options, 4, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(msg, 5, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	u.showModal("detach", layout, 96, 13)
	options.Select(0, 0)
	u.app.SetFocus(options)
}

func (u *tuiState) showHelpModal() {
	type binding struct {
		Key   string
		What  string
		Short string
	}

	focus := u.app.GetFocus()
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.agentTerm || focus == u.diffFiles || focus == u.diffView
	inTable := focus == u.table

	var bindings []binding
	var title string

	// General bindings (always relevant)
	general := []binding{
		{Key: "tab / shift+tab", What: "Switch pane focus", Short: "Cycle focus across status, details, and worktrees panes."},
		{Key: "R", What: "Refresh", Short: "Reload worktrees and repository metadata."},
		{Key: "?", What: "Open keybindings", Short: "Open this contextual help window."},
		{Key: "esc", What: "Close modal", Short: "Cancel and close the current modal window."},
		{Key: "q / ctrl+c", What: "Quit", Short: "Exit the TUI."},
	}

	if inTable {
		title = "Worktree List Help"
		bindings = []binding{
			{Key: "j / k, up / down", What: "Move selection", Short: "Navigate through your list of git worktrees."},
			{Key: "enter / g", What: "Attach to worktree", Short: "Open/focus the tmux session for the selected worktree."},
			{Key: "p", What: "Promote to preview", Short: "Run the configured preview services from the selected worktree (▶ marks the current preview)."},
			{Key: "d", What: "Detach session", Short: "Stop the selected worktree's tmux session (keeps worktree)."},
			{Key: "n", What: "New worktree", Short: "Create a new branch and worktree from this repo."},
			{Key: "x", What: "Remove worktree", Short: "Delete the selected worktree (and optionally its branch)."},
			{Key: "/", What: "Filter list", Short: "Narrow down the list by branch name or path."},
		}
	} else if inDetail && u.detailTab == detailTabDiff {
		title = "Git Diff Help"
		bindings = []binding{
			{Key: "j / k", What: "Move rows", Short: "Move through files and directories in the diff list."},
			{Key: "enter / space", What: "Toggle directory", Short: "Collapse or expand the selected directory row."},
			{Key: "`", What: "Tree or flat view", Short: "Switch between a LazyGit-style tree and a flat file list."},
			{Key: "- / =", What: "Collapse or expand all", Short: "Fold or unfold all directories in the tree view."},
			{Key: "J / K", What: "Scroll patch", Short: "Scroll the patch view for the current file."},
			{Key: "ctrl+u / ctrl+d", What: "Fast scroll", Short: "Scroll the patch view faster (10 lines)."},
		}
	} else {
		title = "General Help"
	}

	bindings = append(bindings, general...)

	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorders(false)
	table.SetSeparator(' ')
	table.SetBackgroundColor(tcell.ColorDefault)
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
	table.SetBorder(true)
	table.SetBorderColor(paneBorderColor())
	table.SetTitle(fmt.Sprintf(" %s ", title))

	headers := []string{"Key", "Action"}
	for col, h := range headers {
		table.SetCell(
			0,
			col,
			tview.NewTableCell(h).
				SetTextColor(ansiColor(ansiCyan)).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false).
				SetExpansion(1),
		)
	}
	for i, b := range bindings {
		row := i + 1
		table.SetCell(row, 0, tview.NewTableCell(b.Key).SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
		table.SetCell(row, 1, tview.NewTableCell(b.What).SetTextColor(tcell.ColorDefault).SetExpansion(1))
	}

	desc := tview.NewTextView().SetDynamicColors(true)
	desc.SetWrap(true)
	desc.SetTextColor(tcell.ColorDefault)
	desc.SetBackgroundColor(tcell.ColorDefault)
	desc.SetBorder(true)
	desc.SetBorderColor(paneBorderColor())

	hint := tview.NewTextView().SetDynamicColors(true)
	hint.SetWrap(false)
	hint.SetTextColor(ansiColor(ansiCyan))
	hint.SetBackgroundColor(tcell.ColorDefault)
	hint.SetText("j/k scroll | enter select | esc close")

	counter := tview.NewTextView().SetDynamicColors(true)
	counter.SetWrap(false)
	counter.SetTextAlign(tview.AlignRight)
	counter.SetTextColor(paneBorderColor())
	counter.SetBackgroundColor(tcell.ColorDefault)

	updateSelection := func(row int) {
		if row < 1 {
			row = 1
		}
		if row > len(bindings) {
			row = len(bindings)
		}
		idx := row - 1
		counter.SetText(fmt.Sprintf("%d of %d", row, len(bindings)))
		desc.SetText(bindings[idx].Short)
	}

	table.SetSelectionChangedFunc(func(row, col int) {
		updateSelection(row)
	})
	table.SetSelectedFunc(func(row, col int) {
		updateSelection(row)
	})
	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			u.closeModal("help")
			return nil
		case tcell.KeyPgDn:
			row, _ := table.GetSelection()
			row += 8
			if row > len(bindings) {
				row = len(bindings)
			}
			table.Select(row, 0)
			return nil
		case tcell.KeyPgUp:
			row, _ := table.GetSelection()
			row -= 8
			if row < 1 {
				row = 1
			}
			table.Select(row, 0)
			return nil
		}
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'c':
				u.closeModal("help")
				return nil
			case 'j':
				row, _ := table.GetSelection()
				if row < len(bindings) {
					table.Select(row+1, 0)
				}
				return nil
			case 'k':
				row, _ := table.GetSelection()
				if row > 1 {
					table.Select(row-1, 0)
				}
				return nil
			}
		}
		return ev
	})

	meta := tview.NewFlex().
		AddItem(hint, 0, 1, false).
		AddItem(counter, 12, 0, false)

	modal := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(table, 0, 3, true).
		AddItem(desc, 4, 0, false).
		AddItem(meta, 1, 0, false)
	modal.SetBackgroundColor(tcell.ColorDefault)

	u.showModal("help", modal, 118, 24)
	table.Select(1, 0)
	updateSelection(1)
	u.app.SetFocus(table)
}

func (u *tuiState) goCurrent() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}
	var path string
	var err error
	u.app.Suspend(func() {
		path, err = u.mgr.Go(GoOptions{Target: item.Path, Launch: true, Attach: true})
	})
	if err != nil {
		u.setError("attach failed: %v", err)
		return
	}
	u.setInfo("attached: %s", path)
	if err := u.refresh(); err != nil {
		u.setWarn("attach succeeded, refresh failed: %v", err)
	}
}

func (u *tuiState) promoteSelectedPreview() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}
	if len(u.mgr.Cfg.PreviewWindows) == 0 {
		u.setWarn("no preview services configured; add [[preview_windows]] to .sprout.toml")
		return
	}
	branch := item.Branch
	if branch == "" {
		branch = filepath.Base(item.Path)
	}
	target := item.Path

	// Promotion can block for a while (tmux window rebuilds plus waiting on the
	// tunnel API), so run it off the event thread to keep the UI responsive and
	// apply the result via QueueUpdateDraw.
	u.setStatus("Promoting preview to %s...", branch)
	go func(branch, target string) {
		st, err := u.mgr.PromotePreview(PreviewOptions{Target: target})
		u.app.QueueUpdateDraw(func() {
			if err != nil {
				u.setError("preview failed: %v", err)
				return
			}
			u.previewWorktreePath = st.Path
			if err := u.refresh(); err != nil {
				u.setWarn("preview running, refresh failed: %v", err)
				return
			}
			if len(st.SyncWarnings) > 0 {
				u.setWarn("preview running from %s (%s)", branch, strings.Join(st.SyncWarnings, "; "))
				return
			}
			u.setInfo("preview now running from %s", branch)
		})
	}(branch, target)
}

func (u *tuiState) launchCurrent() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}
	_, err := u.mgr.Launch(LaunchOptions{Target: item.Path, NoAttach: true})
	if err != nil {
		u.setError("launch failed: %v", err)
		return
	}
	u.setInfo("launched: %s", item.Path)
}

func (u *tuiState) startAgentCurrent() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}

	path, already, err := u.mgr.StartAgent(AgentOptions{Target: item.Path, Attach: false})
	if err != nil {
		u.setError("agent start failed: %v", err)
		return
	}
	if err := u.refresh(); err != nil {
		u.setWarn("agent updated, refresh failed: %v", err)
	}
	if already {
		u.setInfo("agent already running: %s", path)
		return
	}
	u.setInfo("agent started: %s", path)
}

func (u *tuiState) attachAgentCurrent() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}

	var path string
	var err error
	u.app.Suspend(func() {
		path, err = u.mgr.AttachAgent(item.Path)
	})
	if err != nil {
		u.setError("agent attach failed: %v", err)
		return
	}
	if err := u.refresh(); err != nil {
		u.setWarn("agent attached, refresh failed: %v", err)
		return
	}
	u.setInfo("agent attached: %s", path)
}

func (u *tuiState) stopAgentCurrent() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}

	path, stopped, err := u.mgr.StopAgent(item.Path)
	if err != nil {
		u.setError("agent stop failed: %v", err)
		return
	}
	if err := u.refresh(); err != nil {
		u.setWarn("agent updated, refresh failed: %v", err)
	}
	if !stopped {
		u.setInfo("agent was not running: %s", path)
		return
	}
	u.setInfo("agent stopped: %s", path)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func truncatePath(path string, max int) string {
	if len(path) <= max {
		return path
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) <= 2 {
		return truncate(path, max)
	}
	for len(parts) > 2 {
		cand := filepath.Join(parts[0], "...", filepath.Join(parts[len(parts)-2:]...))
		if len(cand) <= max {
			return cand
		}
		parts = append(parts[:1], parts[2:]...)
	}
	return truncate(path, max)
}
