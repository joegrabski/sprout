package sprout

import (
	"fmt"
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
	mgr      *Manager
	repoName string
	repoRoot string
	repoSlug string

	app         *tview.Application
	pages       *tview.Pages
	table       *counterTable
	statusPane  *tview.TextView
	detailPane  *tview.Flex
	detailPages *tview.Pages
	detailTabs  *tview.TextView
	detail      *tview.TextView
	diffFiles   *counterTable
	diffView    *tview.TextView
	footerLeft  *tview.TextView
	footerRight *tview.TextView

	items    []Worktree
	visible  []int
	selected int
	filter   string
	repos    []repoChoice

	focusables          []tview.Primitive
	lastDetail          string
	lastDiff            string
	detailTab           detailTab
	diffItems           []DiffFile
	diffSel             int
	diffPath            string
	diffCache           map[string]diffFilesCacheEntry
	patchCache          map[string]diffPatchCacheEntry
	agentPrompt         map[string]agentPromptState
	agentOutputCache    map[string]string
	agentOutputActivity map[string]int64
	paneSizes           map[string]paneSize
	paneActivity        map[string]int64
	panePromptActivity  map[string]int64
	forceTableSelect    bool
	footerLevel         string
	footerMsg           string
}

type paneSize struct {
	w int
	h int
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
)

var agentPromptOnlyRe = regexp.MustCompile(`^(>|>>|>>>|\$|#|:|›|❯|➜)\s*$`)
var agentPromptInputRe = regexp.MustCompile(`^(>|>>|>>>|\$|#|:|›|❯|➜)\s+.*$`)

type diffFilesCacheEntry struct {
	files     []DiffFile
	fetchedAt time.Time
}

type diffPatchCacheEntry struct {
	text      string
	fetchedAt time.Time
}

const (
	detailPollInterval = 150 * time.Millisecond
	detailCaptureLines = 60
	diffFilesCacheTTL  = 900 * time.Millisecond
	diffPatchCacheTTL  = 2 * time.Second
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
	repoRoot, err := mgr.RequireRepo()
	if err != nil {
		fmt.Println("error: run this command inside a git worktree")
		return 1
	}

	u := newTUI(mgr, repoRoot)
	if err := u.refresh(); err != nil {
		u.setError("refresh failed: %v", err)
	}
	u.startUpdateCheck()
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

	detailPages := tview.NewPages().
		AddPage("agent", detail, true, true).
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
		detailTabs:          detailTabs,
		detail:              detail,
		diffFiles:           diffFiles,
		diffView:            diffView,
		footerLeft:          footerLeft,
		footerRight:         footerRight,
		detailTab:           detailTabAgent,
		diffSel:             0,
		diffCache:           map[string]diffFilesCacheEntry{},
		patchCache:          map[string]diffPatchCacheEntry{},
		agentPrompt:         map[string]agentPromptState{},
		agentOutputCache:    map[string]string{},
		agentOutputActivity: map[string]int64{},
		paneSizes:           map[string]paneSize{},
		paneActivity:        map[string]int64{},
		panePromptActivity:  map[string]int64{},
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

	u.footerRight.SetText(fmt.Sprintf("v%s", Version))
	u.refreshRepoChoices()
	u.app.SetFocus(u.statusPane)
	u.updatePaneFocusStyles()
	u.setInfo("ready")
	return u
}

func (u *tuiState) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	mainFocus := u.isMainFocus()
	focus := u.app.GetFocus()
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.diffFiles || focus == u.diffView

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
		case 'r':
			if err := u.refresh(); err != nil {
				u.setError("refresh failed: %v", err)
			}
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
		return nil
	case tcell.KeyUp:
		u.scrollTextView(u.detail, -1)
		return nil
	case tcell.KeyDown:
		u.scrollTextView(u.detail, 1)
		return nil
	case tcell.KeyPgUp:
		u.scrollTextView(u.detail, -10)
		return nil
	case tcell.KeyPgDn:
		u.scrollTextView(u.detail, 10)
		return nil
	case tcell.KeyHome:
		u.detail.ScrollToBeginning()
		return nil
	case tcell.KeyEnd:
		u.detail.ScrollToEnd()
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
			u.scrollTextView(u.detail, 1)
		case 'k':
			u.scrollTextView(u.detail, -1)
		case 'g':
			u.detail.ScrollToBeginning()
		case 'G':
			u.detail.ScrollToEnd()
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
		u.selectDiffFile(0)
		return nil
	case tcell.KeyEnd:
		u.selectDiffFile(len(u.diffItems) - 1)
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
			u.moveDiffSelection(1)
		case 'k':
			u.moveDiffSelection(-1)
		case 'J':
			u.scrollTextView(u.diffView, 10)
		case 'K':
			u.scrollTextView(u.diffView, -10)
		case 'g':
			u.selectDiffFile(0)
		case 'G':
			u.selectDiffFile(len(u.diffItems) - 1)
		case 'h', '[':
			u.cycleDetailTab(-1)
		case 'l', ']':
			u.cycleDetailTab(1)
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
	if current == u.diffFiles || current == u.diffView || current == u.detail {
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
	tabs := []detailTab{detailTabAgent, detailTabDiff}
	idx := 0
	for i, tab := range tabs {
		if u.detailTab == tab {
			idx = i
			break
		}
	}
	next := (idx + delta) % len(tabs)
	if next < 0 {
		next += len(tabs)
	}
	u.setDetailTab(tabs[next])
}

func (u *tuiState) setDetailTab(tab detailTab) {
	if u.detailTab == tab {
		return
	}
	u.detailTab = tab
	if tab == detailTabAgent {
		u.detailPages.ShowPage("agent")
		u.detailPages.HidePage("diff")
		u.lastDetail = ""
		u.detail.ScrollToEnd()
		if u.app.GetFocus() == u.diffFiles || u.app.GetFocus() == u.diffView {
			u.app.SetFocus(u.detail)
		}
	} else {
		u.detailPages.ShowPage("diff")
		u.detailPages.HidePage("agent")
		u.lastDiff = ""
		u.diffView.ScrollToBeginning()
		if u.app.GetFocus() == u.detail {
			u.app.SetFocus(u.diffFiles)
		}
	}
	u.renderDetailTabs()
	u.renderDetails()
	u.updatePaneFocusStyles()
	u.redrawFooter()
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
		"[3]-Worktrees",
	)
	stylePane(
		focus == u.detailPane || focus == u.detail || focus == u.diffFiles || focus == u.diffView,
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
	u.renderDetails()
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
	if len(u.diffItems) == 0 {
		return
	}
	next := u.diffSel + delta
	if next < 0 {
		next = 0
	}
	if next >= len(u.diffItems) {
		next = len(u.diffItems) - 1
	}
	u.selectDiffFile(next)
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
	u.renderSelectedFileDiff()
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
	u.refreshRepoChoices()
	items, err := u.mgr.ListWorktrees()
	if err != nil {
		return err
	}
	u.clearDiffCaches()
	u.items = items
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
	activity, err := u.mgr.agentPaneActivity(u.repoRoot, item)
	if err != nil {
		return true
	}
	paneTarget := u.mgr.agentPaneTarget(u.repoRoot, item)
	if paneTarget == "" {
		return true
	}
	if last, ok := u.paneActivity[paneTarget]; ok && last == activity {
		return false
	}
	u.paneActivity[paneTarget] = activity
	return true
}

func (u *tuiState) renderDetailTabs() {
	agentStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	diffStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	separator := lipgloss.NewStyle().Foreground(ColorCyan).Render("|")

	switch u.detailTab {
	case detailTabDiff:
		diffStyle = diffStyle.Reverse(true)
	default:
		agentStyle = agentStyle.Reverse(true)
	}

	agent := agentStyle.Render(" AGENT OUTPUT ")
	diff := diffStyle.Render(" GIT DIFF ")

	u.detailTabs.SetText(tview.TranslateANSI(fmt.Sprintf(" %s %s %s", agent, separator, diff)))
}

func (u *tuiState) currentFilterLabel() string {
	if strings.TrimSpace(u.filter) == "" {
		return "(none)"
	}
	return u.filter
}

func (u *tuiState) renderStatusPane() {
	repoBranch := u.mgr.CurrentBranch(u.repoRoot)
	if repoBranch == "" {
		repoBranch = "(detached)"
	}
	selectedBranch := "(none)"
	agentLabel := "n/a"
	agentColor := ColorCyan
	if item := u.selectedItem(); item != nil {
		selectedBranch = item.Branch
		if strings.TrimSpace(selectedBranch) == "" {
			selectedBranch = "(detached)"
		}
		label, colorName := u.selectedAgentPromptLabel(item)
		agentLabel = label
		switch colorName {
		case "green":
			agentColor = ColorGreen
		case "yellow":
			agentColor = ColorEmerald // use emerald for busy
		case "red":
			agentColor = ColorRed
		case "blue":
			agentColor = ColorBlue
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
	agLabel := lipgloss.NewStyle().Foreground(ColorBlue).Render("agent:")
	agStatus := lipgloss.NewStyle().Foreground(agentColor).Render(agentLabel)

	status := fmt.Sprintf(
		"%s %s %s %s  %s %s  %s %s",
		check, repoStr, arrow, branchStr, selLabel, selBranch, agLabel, agStatus,
	)

	if u.app.GetFocus() == u.statusPane {
		status = lipgloss.NewStyle().Reverse(true).Render(
			fmt.Sprintf("✓ %s -> %s   selected: %s   agent: %s   (enter to switch repo)", repo, repoBranch, selectedBranch, agentLabel),
		)
	}

	u.statusPane.SetText(tview.TranslateANSI(status))
}

func (u *tuiState) refreshRepoChoices() {
	parent := filepath.Dir(u.repoRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		u.repos = []repoChoice{buildRepoChoice(u.repoRoot)}
		u.repoSlug = u.repos[0].GitHubRepo
		return
	}

	choices := map[string]repoChoice{}
	choices[u.repoRoot] = buildRepoChoice(u.repoRoot)

	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		root := filepath.Join(parent, ent.Name())
		if !isGitRepoDir(root) {
			continue
		}
		choices[root] = buildRepoChoice(root)
	}

	u.repos = u.repos[:0]
	for _, choice := range choices {
		u.repos = append(u.repos, choice)
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
		status := "clean"
		if item.Dirty {
			status = "dirty"
		}
		agent := u.tableAgentLabel(item)

		values := []string{cur, truncate(branch, 35), status, item.TmuxState, agent, truncatePath(item.Path, 120)}
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
				cell.SetTextColor(tableAgentColor(val))
			}
			if item.Current && col == 1 {
				cell.SetTextColor(ColorToTcell(ThemeColorAccent))
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
	item := u.selectedItem()
	if item == nil {
		return
	}
	if u.selected < 0 || u.selected >= len(u.visible) {
		return
	}
	row := u.selected + 1
	if row <= 0 {
		return
	}
	label := u.tableAgentLabel(*item)
	cell := u.table.GetCell(row, 4)
	if cell == nil {
		return
	}
	cell.SetText(label)
	cell.SetTextColor(tableAgentColor(label))
	u.table.SetCell(row, 4, cell)
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
	if next == agentPromptReady && (!hadPrev || prev != agentPromptReady) {
		branch := item.Branch
		if strings.TrimSpace(branch) == "" {
			branch = filepath.Base(item.Path)
		}
		u.setInfo("agent ready for input: %s", branch)
	}
	u.renderStatusPane()
	u.updateSelectedAgentCell()
}

func (u *tuiState) captureAgentPromptState(item *Worktree, lines int) {
	if item == nil || item.AgentState != "yes" {
		return
	}
	activity, err := u.mgr.agentPaneActivity(u.repoRoot, item)
	if err == nil {
		paneTarget := u.mgr.agentPaneTarget(u.repoRoot, item)
		if paneTarget != "" {
			if last, ok := u.panePromptActivity[paneTarget]; ok && last == activity {
				return
			}
			u.panePromptActivity[paneTarget] = activity
		}
	}
	out, err := u.mgr.agentOutputForWorktree(u.repoRoot, item, lines)
	if err != nil {
		return
	}
	if agentReadyForInstruction(out) {
		u.setAgentPromptState(item, agentPromptReady)
		return
	}
	u.setAgentPromptState(item, agentPromptBusy)
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

func (u *tuiState) renderDetails() {
	switch u.detailTab {
	case detailTabDiff:
		u.renderDiffDetail()
	default:
		u.renderAgentDetail()
	}
}

func (u *tuiState) renderAgentDetail() {
	item := u.selectedItem()
	if item == nil {
		u.setDetailText("Select a worktree to view agent output.", false)
		return
	}

	captureLines := u.detailCaptureLineCount()
	if item.AgentState != "yes" {
		u.setAgentPromptState(item, agentPromptUnknown)
		u.setDetailText(
			"Agent pane is not available for this worktree.\n\n"+
				"Press enter on the worktree list to attach.\n"+
				"A tmux session will open with your configured session tools.",
			false,
		)
		return
	}

	u.syncDetailPaneSize(item)
	paneTarget := u.mgr.agentPaneTarget(u.repoRoot, item)
	var out string
	activity, activityErr := u.mgr.agentPaneActivity(u.repoRoot, item)
	if paneTarget != "" && activityErr == nil {
		if last, ok := u.agentOutputActivity[paneTarget]; ok && last == activity {
			if cached, ok := u.agentOutputCache[paneTarget]; ok {
				out = cached
			}
		}
	}
	if out == "" {
		fetched, err := u.mgr.agentOutputForWorktree(u.repoRoot, item, captureLines)
		if err != nil {
			u.setAgentPromptState(item, agentPromptUnknown)
			u.setDetailText(fmt.Sprintf("Unable to read agent output.\n\n%s", err), false)
			return
		}
		out = fetched
		if paneTarget != "" {
			u.agentOutputCache[paneTarget] = out
			if activityErr == nil {
				u.agentOutputActivity[paneTarget] = activity
			}
		}
	}
	if strings.TrimSpace(out) == "" {
		u.setAgentPromptState(item, agentPromptBusy)
		out = "(agent pane is running, but no output yet)"
	} else if agentReadyForInstruction(out) {
		u.setAgentPromptState(item, agentPromptReady)
	} else {
		u.setAgentPromptState(item, agentPromptBusy)
	}
	u.setDetailANSI(out, true)
}

func (u *tuiState) clearDiffCaches() {
	u.diffCache = map[string]diffFilesCacheEntry{}
	u.patchCache = map[string]diffPatchCacheEntry{}
	u.lastDiff = ""
}

func (u *tuiState) cachedDiffFiles(path string) ([]DiffFile, error) {
	now := time.Now()
	if entry, ok := u.diffCache[path]; ok && now.Sub(entry.fetchedAt) <= diffFilesCacheTTL {
		return entry.files, nil
	}
	files, err := u.mgr.WorktreeDiffFiles(path)
	if err != nil {
		return nil, err
	}
	u.diffCache[path] = diffFilesCacheEntry{
		files:     files,
		fetchedAt: now,
	}
	if len(u.diffCache) > 128 {
		u.diffCache = map[string]diffFilesCacheEntry{path: u.diffCache[path]}
	}
	return files, nil
}

func diffPatchCacheKey(path string, file DiffFile, width int) string {
	return strings.Join([]string{
		path,
		file.Path,
		file.Status,
		strconv.Itoa(width),
	}, "\x00")
}

func (u *tuiState) cachedFileDiff(path string, file DiffFile, width int) (string, error) {
	key := diffPatchCacheKey(path, file, width)
	now := time.Now()
	if entry, ok := u.patchCache[key]; ok && now.Sub(entry.fetchedAt) <= diffPatchCacheTTL {
		return entry.text, nil
	}
	diff, err := u.mgr.WorktreeDiffForFile(path, file, width)
	if err != nil {
		return "", err
	}
	u.patchCache[key] = diffPatchCacheEntry{
		text:      diff,
		fetchedAt: now,
	}
	if len(u.patchCache) > 512 {
		u.patchCache = map[string]diffPatchCacheEntry{key: u.patchCache[key]}
	}
	return diff, nil
}

func (u *tuiState) renderDiffDetail() {
	item := u.selectedItem()
	if item == nil {
		u.diffItems = nil
		u.diffSel = 0
		u.diffPath = ""
		u.renderDiffFileList()
		u.setDiffText("Select a worktree to view git diff.", false)
		return
	}
	files, err := u.cachedDiffFiles(item.Path)
	if err != nil {
		u.diffItems = nil
		u.diffSel = 0
		u.diffPath = item.Path
		u.renderDiffFileList()
		u.setDiffText(fmt.Sprintf("Unable to read git diff.\n\n%s", err), false)
		return
	}
	u.syncDiffFiles(item.Path, files)
	u.renderDiffFileList()
	if len(u.diffItems) == 0 {
		u.setDiffText("(working tree is clean)", false)
		return
	}
	u.renderSelectedFileDiff()
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
		return
	}

	if switchedWorktree {
		u.diffSel = 0
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

func (u *tuiState) renderDiffFileList() {
	u.diffFiles.Clear()
	headers := []string{"", "ST", "FILE"}
	for col, h := range headers {
		cell := tview.NewTableCell(h).
			SetAttributes(tcell.AttrBold).
			SetTextColor(ansiColor(ansiCyan)).
			SetExpansion(1).
			SetSelectable(false)
		u.diffFiles.SetCell(0, col, cell)
	}

	if len(u.diffItems) == 0 {
		u.diffFiles.SetCell(1, 0, tview.NewTableCell("").SetSelectable(false))
		u.diffFiles.SetCell(1, 1, tview.NewTableCell("").SetSelectable(false))
		u.diffFiles.SetCell(1, 2, tview.NewTableCell("(no changed files)").SetTextColor(ansiColor(ansiMagenta)).SetSelectable(false))
		u.diffFiles.SetCounter("0 of 0")
		u.diffFiles.SetOffset(0, 0)
		return
	}

	for i, f := range u.diffItems {
		row := i + 1
		selected := i == u.diffSel
		marker := " "
		if selected {
			marker = ">"
		}
		status := strings.TrimSpace(f.Status)
		if status == "" {
			status = "??"
		}

		markerCell := tview.NewTableCell(marker).SetExpansion(1).SetTextColor(ansiColor(ansiCyan))
		statusCell := tview.NewTableCell(status).SetExpansion(1).SetTextColor(diffStatusColor(status))
		pathCell := tview.NewTableCell(truncatePath(f.Path, 80)).SetExpansion(1).SetTextColor(tcell.ColorDefault)
		if selected {
			markerCell.SetAttributes(tcell.AttrReverse)
			statusCell.SetAttributes(tcell.AttrReverse)
			pathCell.SetAttributes(tcell.AttrReverse)
		}
		u.diffFiles.SetCell(row, 0, markerCell)
		u.diffFiles.SetCell(row, 1, statusCell)
		u.diffFiles.SetCell(row, 2, pathCell)
	}
	u.diffFiles.SetCounter(fmt.Sprintf("%d of %d", u.diffSel+1, len(u.diffItems)))
	u.ensureDiffSelectionVisible()
}

func (u *tuiState) ensureDiffSelectionVisible() {
	if len(u.diffItems) == 0 {
		u.diffFiles.SetOffset(0, 0)
		return
	}
	_, _, _, h := u.diffFiles.GetInnerRect()
	visibleRows := h - 1
	if visibleRows < 1 {
		visibleRows = 1
	}
	maxOffset := len(u.diffItems) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := u.diffSel - (visibleRows / 2)
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	u.diffFiles.SetOffset(offset, 0)
}

func (u *tuiState) renderSelectedFileDiff() {
	item := u.selectedItem()
	if item == nil {
		u.setDiffText("No worktree selected.", false)
		return
	}
	if len(u.diffItems) == 0 || u.diffSel < 0 || u.diffSel >= len(u.diffItems) {
		u.setDiffText("(working tree is clean)", false)
		return
	}
	diff, err := u.cachedFileDiff(item.Path, u.diffItems[u.diffSel], u.detailDiffWidth())
	if err != nil {
		u.setDiffText(fmt.Sprintf("Unable to read file diff.\n\n%s", err), false)
		return
	}
	u.setDiffANSI(diff, false)
}

func (u *tuiState) detailDiffWidth() int {
	_, _, w, _ := u.diffView.GetInnerRect()
	if w <= 0 {
		return 0
	}
	return w
}

func (u *tuiState) syncDetailPaneSize(item *Worktree) {
	if item == nil {
		return
	}
	_, _, w, h := u.detail.GetInnerRect()
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
	u.setDetailRenderedText(tview.Escape(text), follow)
}

func (u *tuiState) setDetailANSI(text string, follow bool) {
	u.setDetailRenderedText(tview.TranslateANSI(text), follow)
}

func (u *tuiState) setDetailRenderedText(text string, follow bool) {
	if text == u.lastDetail {
		return
	}
	row, col := u.detail.GetScrollOffset()
	u.detail.SetText(text)
	u.lastDetail = text
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

func (u *tuiState) setDiffText(text string, keepScroll bool) {
	u.setDiffRenderedText(tview.Escape(text), keepScroll)
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

		agentState := lipgloss.NewStyle().Foreground(ColorCyan).Render("·")
		switch wt.AgentState {
		case "yes":
			agentState = lipgloss.NewStyle().Foreground(ColorGreen).Render("●")
		case "no":
			agentState = lipgloss.NewStyle().Foreground(ColorRed).Render("○")
		}

		branchText := lipgloss.NewStyle().Bold(true).Foreground(branchColor).Render(truncate(branch, 42))
		stateText := lipgloss.NewStyle().Foreground(stateColor).Render("(" + state + ")")
		markerText := lipgloss.NewStyle().Foreground(markerColor).Render(marker)

		line := fmt.Sprintf(
			"%s%s %s %s tmux:%s agent:%s",
			arm, markerText, branchText, stateText, tmuxState, agentState,
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
	base := "[::b]tab[::-] pane | [::b]r[::-] refresh | [::b]?[::-] help | [::b]q[::-] quit"
	focus := u.app.GetFocus()
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.diffFiles || focus == u.diffView

	switch {
	case focus == u.statusPane:
		return "[::b]enter[::-] repos | " + base
	case focus == u.table:
		return "[::b]j/k[::-] move | [::b]enter[::-] attach | [::b]d[::-] detach | [::b]n[::-] new | [::b]x[::-] remove | [::b]/[::-] filter | " + base
	case inDetail:
		if u.detailTab == detailTabDiff {
			return "[::b]j/k[::-] files | [::b]J/K[::-] patch scroll | [::b]h/l[::-] tab | " + base
		}
		return "[::b]j/k/pgup/pgdn[::-] scroll | [::b]h/l/[[/]][::-] tab | " + base
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
	u.refreshRepoChoices()
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

	doCreate := func(branch string, fromExisting bool, copyUntracked bool) {
		if creating {
			return
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			u.setWarn("branch name is required")
			return
		}
		creating = true

		totalSteps := 2 // create + refresh
		if u.mgr.Cfg.AutoLaunch {
			totalSteps++
		}
		if u.mgr.Cfg.AutoStartAgent {
			totalSteps++
		}
		advance, setProgressLabel, setStepProgress, stopProgress := u.showProgressModal("create-progress", "Create Worktree", totalSteps)

		go func(branch string, fromExisting bool) {
			var path string
			var createErr error
			warnings := []string{}
			var refreshed []Worktree
			var refreshErr error

			var opts NewOptions
			lastCopyUpdate := time.Time{}
			renderCopyLabel := func(p CopyProgress) string {
				switch p.Phase {
				case "scan":
					if p.TotalFiles <= 0 {
						return "Scanning untracked files..."
					}
					return fmt.Sprintf("Scanning untracked files... %d files, %s total", p.TotalFiles, formatByteSize(p.TotalBytes))
				default:
					remainingFiles := p.TotalFiles - p.CopiedFiles
					if remainingFiles < 0 {
						remainingFiles = 0
					}
					remainingBytes := p.TotalBytes - p.CopiedBytes
					if remainingBytes < 0 {
						remainingBytes = 0
					}
					return fmt.Sprintf(
						"Copying untracked files... %d/%d files (%d remaining) • %s/%s (%s remaining)",
						p.CopiedFiles, p.TotalFiles, remainingFiles,
						formatByteSize(p.CopiedBytes), formatByteSize(p.TotalBytes), formatByteSize(remainingBytes),
					)
				}
			}
			onCopyProgress := func(p CopyProgress) {
				now := time.Now()
				// Throttle UI updates while still showing smooth progress.
				if p.CopiedFiles != p.TotalFiles && !lastCopyUpdate.IsZero() && now.Sub(lastCopyUpdate) < 120*time.Millisecond {
					return
				}
				lastCopyUpdate = now
				setProgressLabel(renderCopyLabel(p))
				progress := 0.0
				switch p.Phase {
				case "scan":
					progress = 0.05
				default:
					if p.TotalBytes > 0 {
						progress = float64(p.CopiedBytes) / float64(p.TotalBytes)
					} else if p.TotalFiles > 0 {
						progress = float64(p.CopiedFiles) / float64(p.TotalFiles)
					} else {
						progress = 1.0
					}
					// Reserve a tiny tail for worktree finalization in the same step.
					progress = 0.1 + (0.88 * progress)
				}
				setStepProgress(progress)
			}
			if fromExisting {
				opts = NewOptions{
					FromBranch:        branch,
					Launch:            false,
					SkipCopyUntracked: !copyUntracked,
					OnCopyProgress:    onCopyProgress,
				}
			} else {
				opts = NewOptions{
					Branch:            branch,
					Launch:            false,
					SkipCopyUntracked: !copyUntracked,
					OnCopyProgress:    onCopyProgress,
				}
			}

			debugLogf("ui_create start branch=%q existing=%t auto_launch=%t auto_start_agent=%t", branch, fromExisting, u.mgr.Cfg.AutoLaunch, u.mgr.Cfg.AutoStartAgent)
			advance("Creating worktree...")
			_, path, createErr = u.mgr.NewWorktree(opts)
			if createErr != nil {
				debugLogf("ui_create new_worktree failed branch=%q: %v", branch, createErr)
			}

			if createErr == nil && u.mgr.Cfg.AutoLaunch {
				advance("Launching tmux tools...")
				if _, err := u.mgr.Launch(LaunchOptions{Target: path, NoAttach: true}); err != nil {
					debugLogf("ui_create auto_launch failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("launch failed: %v", err))
				}
			}
			if createErr == nil && u.mgr.Cfg.AutoStartAgent {
				advance("Starting agent...")
				if _, _, err := u.mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
					debugLogf("ui_create auto_agent failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("agent start failed: %v", err))
				}
			}

			if createErr == nil {
				advance("Refreshing worktrees...")
				refreshed, refreshErr = u.mgr.ListWorktrees()
				if refreshErr != nil {
					debugLogf("ui_create refresh failed path=%q: %v", path, refreshErr)
				}
			}

			u.app.QueueUpdateDraw(func() {
				stopProgress()
				u.closeModal("create-progress")

				if createErr != nil {
					u.setError("create failed: %v", createErr)
					return
				}

				if refreshErr == nil {
					u.refreshRepoChoices()
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

	openCreateConfirm := func(branch string, fromExisting bool) {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			u.setWarn("branch name is required")
			return
		}

		u.closeModal("create")

		msg := tview.NewTextView().SetDynamicColors(true)
		msg.SetBackgroundColor(tcell.ColorDefault)
		msg.SetTextColor(tcell.ColorDefault)
		msg.SetWrap(true)
		mode := "new branch"
		if fromExisting {
			mode = "existing branch"
		}
		msg.SetText(fmt.Sprintf(
			"Create worktree [::b]%s[::-] (%s)?\n\nSelect whether to copy untracked + ignored files from the repo root.",
			branch,
			mode,
		))
		msg.SetBorder(true)
		msg.SetBorderColor(paneBorderColor())

		copyUntracked := true

		confirm := func() {
			u.closeModal("create-confirm")
			doCreate(branch, fromExisting, copyUntracked)
		}
		cancel := func() {
			u.closeModal("create-confirm")
			u.showCreateModal()
		}
		var render func()
		toggleCopy := func() {
			copyUntracked = !copyUntracked
			render()
		}

		action := tview.NewTextView().
			SetDynamicColors(true).
			SetWrap(false)
		action.SetBackgroundColor(tcell.ColorDefault)
		action.SetTextColor(ansiColor(ansiCyan))

		options := tview.NewTable().
			SetSelectable(true, false).
			SetBorders(false)
		options.SetSeparator(' ')
		options.SetBackgroundColor(tcell.ColorDefault)
		options.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorDefault).Background(tcell.ColorDefault).Reverse(true))
		options.SetBorder(true)
		options.SetBorderColor(paneBorderColor())

		render = func() {
			copyLabel := "off"
			if copyUntracked {
				copyLabel = "on"
			}
			action.SetText(fmt.Sprintf(" r - Create worktree [::b]%s[::-]   u - Toggle copy: [::b]%s[::-]", branch, copyLabel))

			options.SetCell(0, 0, tview.NewTableCell("r").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
			options.SetCell(0, 1, tview.NewTableCell("Create worktree").SetTextColor(tcell.ColorDefault).SetExpansion(1))
			options.SetCell(1, 0, tview.NewTableCell("u").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
			options.SetCell(1, 1, tview.NewTableCell(fmt.Sprintf("Copy untracked + ignored files: %s", copyLabel)).SetTextColor(tcell.ColorDefault).SetExpansion(1))
			options.SetCell(2, 0, tview.NewTableCell("c").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
			options.SetCell(2, 1, tview.NewTableCell("Cancel").SetTextColor(tcell.ColorDefault).SetExpansion(1))
		}
		render()

		selectOption := func(row int) {
			switch row {
			case 0:
				confirm()
			case 1:
				toggleCopy()
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
					confirm()
					return nil
				case 'u':
					toggleCopy()
					return nil
				case 'c':
					cancel()
					return nil
				case 'j':
					row, _ := options.GetSelection()
					if row < 2 {
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
			AddItem(options, 5, 0, true).
			AddItem(nil, 1, 0, false).
			AddItem(msg, 5, 0, false)
		layout.SetBackgroundColor(tcell.ColorDefault)

		u.showModal("create-confirm", layout, 96, 14)
		options.Select(0, 0)
		u.app.SetFocus(options)
	}

	selectCurrentRow := func() {
		row, _ := branchTable.GetSelection()
		if row < 1 || row-1 >= len(displayRows) {
			return
		}
		r := displayRows[row-1]
		openCreateConfirm(r.name, !r.isNew)
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
				openCreateConfirm(r.name, !r.isNew)
			} else {
				openCreateConfirm(strings.TrimSpace(input.GetText()), false)
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
		advance, setProgressLabel, setStepProgress, stopProgress := u.showProgressModal("delete-progress", "Remove Worktree", 2)

		go func() {
			lastDeleteUpdate := time.Time{}
			renderDeleteLabel := func(p DeleteProgress) string {
				switch p.Phase {
				case "scan":
					if p.TotalFiles <= 0 {
						return "Scanning worktree files..."
					}
					return fmt.Sprintf("Scanning worktree files... %d files, %s total", p.TotalFiles, formatByteSize(p.TotalBytes))
				default:
					remainingFiles := p.TotalFiles - p.DeletedFiles
					if remainingFiles < 0 {
						remainingFiles = 0
					}
					remainingBytes := p.TotalBytes - p.DeletedBytes
					if remainingBytes < 0 {
						remainingBytes = 0
					}
					label := fmt.Sprintf(
						"Deleting files... %d/%d files (%d remaining) • %s/%s (%s remaining)",
						p.DeletedFiles, p.TotalFiles, remainingFiles,
						formatByteSize(p.DeletedBytes), formatByteSize(p.TotalBytes), formatByteSize(remainingBytes),
					)
					if p.CurrentPath != "" {
						label = fmt.Sprintf("%s • %s", label, truncatePath(p.CurrentPath, 44))
					}
					return label
				}
			}
			onDeleteProgress := func(p DeleteProgress) {
				now := time.Now()
				if p.Phase == "delete" && p.DeletedFiles != p.TotalFiles && !lastDeleteUpdate.IsZero() && now.Sub(lastDeleteUpdate) < 120*time.Millisecond {
					return
				}
				lastDeleteUpdate = now
				setProgressLabel(renderDeleteLabel(p))
				progress := 0.0
				switch p.Phase {
				case "scan":
					progress = 0.05
				default:
					if p.TotalBytes > 0 {
						progress = float64(p.DeletedBytes) / float64(p.TotalBytes)
					} else if p.TotalFiles > 0 {
						progress = float64(p.DeletedFiles) / float64(p.TotalFiles)
					} else {
						progress = 1.0
					}
					progress = 0.1 + (0.88 * progress)
				}
				setStepProgress(progress)
			}
			advance("Removing worktree...")
			_, warnings, removeErr := u.mgr.Remove(RemoveOptions{
				Target:           item.Path,
				Force:            item.Dirty,
				DeleteBranch:     false,
				OnDeleteProgress: onDeleteProgress,
			})

			var refreshed []Worktree
			var refreshErr error
			if removeErr == nil {
				advance("Refreshing worktrees...")
				refreshed, refreshErr = u.mgr.ListWorktrees()
			}

			u.app.QueueUpdateDraw(func() {
				stopProgress()
				u.closeModal("delete-progress")

				if removeErr != nil {
					u.setError("remove failed: %v", removeErr)
					return
				}

				if refreshErr == nil {
					u.refreshRepoChoices()
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
	inDetail := focus == u.detailPane || focus == u.detail || focus == u.diffFiles || focus == u.diffView
	inTable := focus == u.table

	var bindings []binding
	var title string

	// General bindings (always relevant)
	general := []binding{
		{Key: "tab / shift+tab", What: "Switch pane focus", Short: "Cycle focus across status, details, and worktrees panes."},
		{Key: "r", What: "Refresh", Short: "Reload worktrees and repository metadata."},
		{Key: "?", What: "Open keybindings", Short: "Open this contextual help window."},
		{Key: "esc", What: "Close modal", Short: "Cancel and close the current modal window."},
		{Key: "q / ctrl+c", What: "Quit", Short: "Exit the TUI."},
	}

	if inTable {
		title = "Worktree List Help"
		bindings = []binding{
			{Key: "j / k, up / down", What: "Move selection", Short: "Navigate through your list of git worktrees."},
			{Key: "enter / g", What: "Attach to worktree", Short: "Open/focus the tmux session for the selected worktree."},
			{Key: "d", What: "Detach session", Short: "Stop the selected worktree's tmux session (keeps worktree)."},
			{Key: "n", What: "New worktree", Short: "Create a new branch and worktree from this repo."},
			{Key: "x", What: "Remove worktree", Short: "Delete the selected worktree (and optionally its branch)."},
			{Key: "/", What: "Filter list", Short: "Narrow down the list by branch name or path."},
		}
	} else if inDetail && u.detailTab == detailTabDiff {
		title = "Git Diff Help"
		bindings = []binding{
			{Key: "j / k", What: "Select file", Short: "Move through the list of changed files."},
			{Key: "J / K", What: "Scroll patch", Short: "Scroll the patch view for the current file."},
			{Key: "ctrl+u / ctrl+d", What: "Fast scroll", Short: "Scroll the patch view faster (10 lines)."},
			{Key: "h / l, [ / ]", What: "Switch tab", Short: "Switch back to Agent Output or next tab."},
		}
	} else if inDetail && u.detailTab == detailTabAgent {
		title = "Agent Output Help"
		bindings = []binding{
			{Key: "j / k, up / down", What: "Scroll output", Short: "Scroll through the agent's terminal output."},
			{Key: "pgup / pgdn", What: "Fast scroll", Short: "Scroll through output faster."},
			{Key: "h / l, [ / ]", What: "Switch tab", Short: "Switch to Git Diff or next tab."},
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
