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

	focusables  []tview.Primitive
	lastDetail  string
	lastDiff    string
	detailTab   detailTab
	diffItems   []DiffFile
	diffSel     int
	diffPath    string
	diffCache   map[string]diffFilesCacheEntry
	patchCache  map[string]diffPatchCacheEntry
	agentPrompt map[string]agentPromptState
	paneSizes   map[string]paneSize
	footerLevel string
	footerMsg   string
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
	detailPollInterval = 350 * time.Millisecond
	detailCaptureLines = 90
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
	return tcell.ColorDefault
}

func paneFocusColor() tcell.Color {
	return tcell.ColorDefault
}

func ansiColor(code int) tcell.Color {
	return tcell.PaletteColor(code)
}

func paletteLevelColor(level string) tcell.Color {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "ERROR":
		return ansiColor(ansiRed)
	case "WARN":
		return ansiColor(ansiMagenta)
	case "INFO":
		return ansiColor(ansiBlue)
	default:
		return ansiColor(ansiCyan)
	}
}

func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = paneBorderColor()
	tview.Styles.TitleColor = paneBorderColor()
	tview.Styles.GraphicsColor = paneBorderColor()
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = ansiColor(ansiCyan)
	tview.Styles.TertiaryTextColor = ansiColor(ansiBlue)
	tview.Styles.InverseTextColor = tcell.ColorDefault
	tview.Styles.ContrastSecondaryTextColor = ansiColor(ansiRed)

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
		mgr:         mgr,
		repoName:    mgr.RepoName(repoRoot),
		repoRoot:    repoRoot,
		app:         tview.NewApplication(),
		pages:       pages,
		table:       table,
		statusPane:  statusPane,
		detailPane:  detailPane,
		detailPages: detailPages,
		detailTabs:  detailTabs,
		detail:      detail,
		diffFiles:   diffFiles,
		diffView:    diffView,
		footerLeft:  footerLeft,
		footerRight: footerRight,
		detailTab:   detailTabAgent,
		diffSel:     0,
		diffCache:   map[string]diffFilesCacheEntry{},
		patchCache:  map[string]diffPatchCacheEntry{},
		agentPrompt: map[string]agentPromptState{},
		paneSizes:   map[string]paneSize{},
	}
	u.focusables = []tview.Primitive{u.statusPane, u.detail, u.table}

	table.SetSelectionChangedFunc(func(row, _ int) {
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
	u.updatePaneFocusStyles()
	u.setInfo("ready")
	return u
}

func (u *tuiState) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	mainFocus := u.isMainFocus()
	if mainFocus && u.app.GetFocus() == u.detail {
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
		case 'g':
			u.goCurrent()
			return nil
		case 'n':
			u.showCreateModal()
			return nil
		case 'd':
			u.showDeleteModal()
			return nil
		case 'x':
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
		case '1':
			u.setDetailTab(detailTabAgent)
		case '2':
			u.setDetailTab(detailTabDiff)
		}
		return nil
	default:
		return nil
	}
}

func (u *tuiState) handleDiffBrowseKey(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyCtrlC:
		u.app.Stop()
		return nil
	case tcell.KeyCtrlU:
		u.scrollTextView(u.diffView, -10)
		return nil
	case tcell.KeyCtrlD:
		u.scrollTextView(u.diffView, 10)
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
		u.diffView.ScrollToBeginning()
		return nil
	case tcell.KeyEnd:
		u.diffView.ScrollToEnd()
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
		case '1':
			u.setDetailTab(detailTabAgent)
		case '2':
			u.setDetailTab(detailTabDiff)
		}
		return nil
	default:
		return nil
	}
}

func (u *tuiState) isMainFocus() bool {
	current := u.app.GetFocus()
	for _, p := range u.focusables {
		if current == p {
			return true
		}
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
	} else {
		u.detailPages.ShowPage("diff")
		u.detailPages.HidePage("agent")
		u.lastDiff = ""
		u.diffView.ScrollToBeginning()
	}
	u.renderDetailTabs()
	u.renderDetails()
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
		focus == u.detail,
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
	u.table.Select(u.selected+1, 0)
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
						u.renderDetails()
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

func (u *tuiState) renderDetailTabs() {
	agent := "[cyan][::b] AGENT OUTPUT [::-][-]"
	diff := "[cyan][::b] GIT DIFF [::-][-]"
	switch u.detailTab {
	case detailTabDiff:
		diff = "[::r][::b] GIT DIFF [::-][::-]"
	default:
		agent = "[::r][::b] AGENT OUTPUT [::-][::-]"
	}
	u.detailTabs.SetText(" " + agent + " [cyan]|[-] " + diff)
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
	agentColor := "cyan"
	if item := u.selectedItem(); item != nil {
		selectedBranch = item.Branch
		if strings.TrimSpace(selectedBranch) == "" {
			selectedBranch = "(detached)"
		}
		agentLabel, agentColor = u.selectedAgentPromptLabel(item)
	}
	repo := u.repoName
	status := fmt.Sprintf(
		"[green]✓[-] [::b]%s[::-] [cyan]->[-] [green]%s[-]  [cyan]selected:[-] [green]%s[-]  [cyan]agent:[-] [%s]%s[-]",
		repo,
		repoBranch,
		selectedBranch,
		agentColor,
		agentLabel,
	)
	if u.app.GetFocus() == u.statusPane {
		status = fmt.Sprintf(
			"[::r] %s [::-]",
			fmt.Sprintf("✓ %s -> %s   selected: %s   agent: %s   (enter to switch repo)", repo, repoBranch, selectedBranch, agentLabel),
		)
	}
	u.statusPane.SetText(status)
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
			SetTextColor(ansiColor(ansiCyan)).
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
					cell.SetTextColor(ansiColor(ansiMagenta))
				}
			case 2:
				if status == "dirty" {
					cell.SetTextColor(ansiColor(ansiRed))
				} else {
					cell.SetTextColor(ansiColor(ansiGreen))
				}
			case 3:
				if val == "yes" {
					cell.SetTextColor(ansiColor(ansiGreen))
				} else if val == "no" {
					cell.SetTextColor(ansiColor(ansiRed))
				} else {
					cell.SetTextColor(ansiColor(ansiCyan))
				}
			case 4:
				cell.SetTextColor(tableAgentColor(val))
			}
			if item.Current && col == 1 {
				cell.SetTextColor(ansiColor(ansiBlue))
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
		u.table.Select(1, 0)
		u.renderTableMeta()
		return
	}
	u.table.Select(u.selected+1, 0)
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
		return ansiColor(ansiGreen)
	case "busy", "running":
		return ansiColor(ansiYellow)
	case "no", "offline":
		return ansiColor(ansiRed)
	default:
		return ansiColor(ansiCyan)
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
				"Press enter or g on the worktree list to attach.\n"+
				"A tmux session will open with your configured session tools.",
			false,
		)
		return
	}

	u.syncDetailPaneSize(item)
	out, err := u.mgr.agentOutputForWorktree(u.repoRoot, item, captureLines)
	if err != nil {
		u.setAgentPromptState(item, agentPromptUnknown)
		u.setDetailText(fmt.Sprintf("Unable to read agent output.\n\n%s", err), false)
		return
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
	lines := h + 16
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
		return "[magenta](no worktrees)[-]"
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
		fmt.Sprintf("[::b][green]%s[-][::-]", repoLabel),
		"[cyan]│[-]",
	}

	for i, wt := range ordered {
		branch := wt.Branch
		if branch == "" {
			branch = "detached"
		}
		state := "clean"
		if wt.Dirty {
			state = "dirty"
		}

		arm := "├─"
		stem := "│ "
		if i == len(ordered)-1 {
			arm = "└─"
			stem = "  "
		}

		marker := "○"
		markerColor := "cyan"
		if wt.Current {
			marker = "●"
			markerColor = "green"
		}
		if wt.Path == selectedPath {
			marker = "◆"
			markerColor = "blue"
		}

		branchColor := "cyan"
		if wt.Dirty {
			branchColor = "red"
		} else if wt.Current {
			branchColor = "green"
		}

		stateColor := "green"
		if wt.Dirty {
			stateColor = "red"
		}

		tmuxState := "[cyan]·[-]"
		switch wt.TmuxState {
		case "yes":
			tmuxState = "[green]●[-]"
		case "no":
			tmuxState = "[red]○[-]"
		}
		agentState := "[cyan]·[-]"
		switch wt.AgentState {
		case "yes":
			agentState = "[green]●[-]"
		case "no":
			agentState = "[red]○[-]"
		}

		lines = append(
			lines,
			fmt.Sprintf(
				"[cyan]%s[-][%s]%s[-] [::b][%s]%s[-][::-] [%s](%s)[-] [cyan]tmux[-]:%s [cyan]agent[-]:%s",
				arm,
				markerColor,
				marker,
				branchColor,
				truncate(branch, 42),
				stateColor,
				state,
				tmuxState,
				agentState,
			),
		)

		pathColor := "magenta"
		if wt.Path == selectedPath {
			pathColor = "blue"
		}
		lines = append(lines, fmt.Sprintf("[cyan]%s└─[-] [%s]%s[-]", stem, pathColor, truncatePath(wt.Path, 74)))
	}

	return strings.Join(lines, "\n")
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

	switch {
	case focus == u.statusPane:
		return "[::b]enter[::-] repos | " + base
	case focus == u.table:
		return "[::b]j/k[::-] move | [::b]enter/g[::-] attach | [::b]x[::-] detach | [::b]n[::-] new | [::b]d[::-] remove | [::b]/[::-] filter | " + base
	case focus == u.detail:
		if u.detailTab == detailTabDiff {
			return "[::b]j/k[::-] files | [::b]ctrl+u/ctrl+d[::-] patch | [::b]h/l[::-] tab | " + base
		}
		return "[::b]j/k/pgup/pgdn[::-] scroll | [::b]h/l[::-] tab | [::b]1/2[::-] tab | " + base
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
	u.footerLeft.SetTextColor(paletteLevelColor(level))
	u.footerLeft.SetText(fmt.Sprintf("╰─ %s  %s: %s", u.footerKeymap(), level, message))
	u.footerRight.SetTextColor(ansiColor(ansiCyan))
	u.footerRight.SetText(fmt.Sprintf("─ v%s ╯", Version))
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

func (u *tuiState) showProgressModal(name, title, message string) (func(string), func()) {
	action := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	action.SetBackgroundColor(tcell.ColorDefault)
	action.SetTextColor(ansiColor(ansiCyan))
	actionLabel := strings.TrimSpace(title)
	if actionLabel == "" {
		actionLabel = "Working"
	}
	action.SetText(" " + actionLabel)

	body := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	body.SetBackgroundColor(tcell.ColorDefault)
	body.SetTextColor(tcell.ColorDefault)

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(action, 1, 0, false).
		AddItem(body, 1, 0, false)
	layout.SetBackgroundColor(tcell.ColorDefault)

	u.showModal(name, layout, 64, 5)
	u.app.SetFocus(layout)

	frames := []string{"|", "/", "-", "\\"}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "Working..."
	}

	var mu sync.Mutex
	render := func(frame int) {
		mu.Lock()
		current := msg
		mu.Unlock()
		body.SetText(fmt.Sprintf("[%s] %s", frames[frame%len(frames)], current))
	}
	render(0)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		frame := 1
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				f := frame
				frame++
				u.app.QueueUpdateDraw(func() {
					render(f)
				})
			}
		}
	}()

	update := func(next string) {
		trimmed := strings.TrimSpace(next)
		if trimmed == "" {
			return
		}
		mu.Lock()
		msg = trimmed
		mu.Unlock()
	}
	stop := func() {
		close(done)
	}
	return update, stop
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
	cancelBtn := modalButton("<x> Cancel", cancel)

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
		'x': cancel,
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
	branchField := tview.NewInputField()
	styleModalInputField(branchField)
	creating := false

	create := func() {
		if creating {
			return
		}
		branchValue := strings.TrimSpace(branchField.GetText())
		if branchValue == "" {
			u.setWarn("branch is required")
			return
		}
		creating = true

		u.closeModal("create")
		updateProgress, stopProgress := u.showProgressModal("create-progress", "Create Worktree", "Creating worktree...")

		go func(branch string) {
			var path string
			var createErr error
			warnings := []string{}
			var refreshed []Worktree
			var refreshErr error

			debugLogf("ui_create start branch=%q auto_launch=%t auto_start_agent=%t", branch, u.mgr.Cfg.AutoLaunch, u.mgr.Cfg.AutoStartAgent)
			updateProgress("Creating branch and worktree...")
			_, path, createErr = u.mgr.NewWorktree(NewOptions{
				Branch: branch,
				Launch: false,
			})
			if createErr != nil {
				debugLogf("ui_create new_worktree failed branch=%q: %v", branch, createErr)
			}

			if createErr == nil && u.mgr.Cfg.AutoLaunch {
				updateProgress("Launching tmux tools...")
				if _, err := u.mgr.Launch(LaunchOptions{Target: path, NoAttach: true}); err != nil {
					debugLogf("ui_create auto_launch failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("launch failed: %v", err))
				}
			}
			if createErr == nil && u.mgr.Cfg.AutoStartAgent {
				updateProgress("Starting agent...")
				if _, _, err := u.mgr.StartAgent(AgentOptions{Target: path, Attach: false}); err != nil {
					debugLogf("ui_create auto_agent failed path=%q: %v", path, err)
					warnings = append(warnings, fmt.Sprintf("agent start failed: %v", err))
				}
			}

			if createErr == nil {
				updateProgress("Refreshing worktrees...")
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
		}(branchValue)
	}
	cancel := func() {
		u.closeModal("create")
	}

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(modalHeader("Create Worktree"), 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(modalFieldBox("Branch Name", branchField), 3, 0, true)
	layout.SetBackgroundColor(tcell.ColorDefault)

	focusables := []tview.Primitive{branchField}
	capture := modalCapture(u.app, focusables, cancel, nil)
	branchField.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEnter {
			create()
			return nil
		}
		return capture(ev)
	})
	branchField.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			create()
		}
	})

	u.showModal("create", layout, 86, 9)
	u.app.SetFocus(branchField)
}

func (u *tuiState) selectPath(path string) {
	for pos, idx := range u.visible {
		if u.items[idx].Path == path {
			u.selected = pos
			u.table.Select(u.selected+1, 0)
			u.renderDetails()
			return
		}
	}
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

	remove := func() {
		_, warnings, err := u.mgr.Remove(RemoveOptions{
			Target:       item.Path,
			Force:        item.Dirty,
			DeleteBranch: false,
		})
		if err != nil {
			u.setError("remove failed: %v", err)
			return
		}
		u.closeModal("delete")
		if err := u.refresh(); err != nil {
			u.setWarn("removed, but refresh failed: %v", err)
			return
		}
		if len(warnings) > 0 {
			u.setWarn("removed with warning: %s", warnings[0])
		} else {
			u.setInfo("removed: %s", branch)
		}
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
	options.SetCell(1, 0, tview.NewTableCell("x").SetTextColor(ansiColor(ansiCyan)).SetExpansion(1))
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
			case 'x':
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
	bindings := []binding{
		{Key: "tab / shift+tab", What: "Switch pane focus", Short: "Cycle focus across status, details, and worktrees panes."},
		{Key: "j / k", What: "Move selection / scroll pane", Short: "Move worktree selection, or scroll when details is focused."},
		{Key: "up / down", What: "Move selection / scroll pane", Short: "Arrow-key alternative for worktree navigation and pane scrolling."},
		{Key: "pgup / pgdn", What: "Scroll details", Short: "Scroll faster through output; in diff tab this scrolls the patch pane."},
		{Key: "ctrl+u / ctrl+d in diff tab", What: "Scroll patch", Short: "Scroll the selected file patch up/down while staying in the diff inspector."},
		{Key: "h / l, left / right", What: "Switch detail tab", Short: "Switch between AGENT OUTPUT and GIT DIFF tabs in details pane."},
		{Key: "1 / 2", What: "Jump detail tab", Short: "Jump directly to AGENT OUTPUT (1) or GIT DIFF (2)."},
		{Key: "j / k in diff tab", What: "Select changed file", Short: "Move through changed files on the left; right pane updates to selected file diff."},
		{Key: "agent ready indicator", What: "Watch readiness", Short: "Status bar shows agent state (running/busy/ready), and footer notifies when selected agent becomes ready."},
		{Key: "enter", What: "Attach to worktree", Short: "From worktree list, open/focus tmux session with configured tool windows."},
		{Key: "g", What: "Attach to worktree", Short: "Shortcut to attach/focus the selected worktree tmux session."},
		{Key: "x", What: "Detach from worktree", Short: "Stop the selected worktree tmux session without removing the worktree itself."},
		{Key: "n", What: "Create worktree", Short: "Open create-worktree form and create a new branch + worktree."},
		{Key: "d", What: "Delete worktree", Short: "Open removal confirmation form for the selected worktree."},
		{Key: "/", What: "Filter worktrees", Short: "Open filter form to narrow the worktree list."},
		{Key: "r", What: "Refresh", Short: "Reload worktrees and repository metadata."},
		{Key: "?", What: "Open keybindings", Short: "Open this keybinding reference modal."},
		{Key: "esc", What: "Close modal", Short: "Cancel and close the current modal window."},
		{Key: "q / ctrl+c", What: "Quit", Short: "Exit the TUI."},
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
	hint.SetText("j/k or arrows scroll | enter keep focus | esc close")

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
			case 'q':
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

	u.showModal("help", modal, 118, 30)
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
