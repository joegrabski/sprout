package sprout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type modalView struct {
	title  string
	lines  []string
	width  int
	footer string
}

// modal is a centered dialog drawn over a dimmed main view. While one is open
// it receives every key press.
type modal interface {
	update(u *model, msg tea.KeyMsg) tea.Cmd
	view(u *model) modalView
}

func (u *model) closeModal() { u.modal = nil }

func modalWidth(u *model, want int) int { return max(30, min(want, u.width-10)) }

// pickerRow renders one selectable row: a selection bar, an optional mark,
// the label, and a right-aligned dim detail.
func pickerRow(sel bool, mark, label, detail string, detailStyle lipgloss.Style, w int) string {
	base := lipgloss.NewStyle()
	bar := base.Render(" ")
	labelStyle := base
	if sel {
		base = base.Background(uiSelBg)
		bar = base.Foreground(uiAccent).Render("▌")
		labelStyle = base.Bold(true)
	}
	head := bar + base.Render(" "+mark+" ")
	right := ""
	if detail != "" {
		right = detailStyle.Inherit(base).Render(detail) + base.Render(" ")
	}
	avail := w - cell(head) - cell(right) - 1
	label = ellipsize(label, max(1, avail))
	line := head + labelStyle.Render(label)
	return line + base.Render(strings.Repeat(" ", max(0, w-cell(line)-cell(right)))) + right
}

// ---- confirm --------------------------------------------------------------

type confirmModal struct {
	title        string
	prompt       []string
	confirmKey   string
	confirmLabel string
	danger       bool
	cursor       int
	onConfirm    func(u *model) tea.Cmd
}

func (m *confirmModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "esc", "c", "q", "ctrl+c":
		u.closeModal()
	case "j", "down", "tab":
		m.cursor = 1
	case "k", "up", "shift+tab":
		m.cursor = 0
	case "enter":
		u.closeModal()
		if m.cursor == 0 {
			return m.onConfirm(u)
		}
	case m.confirmKey:
		u.closeModal()
		return m.onConfirm(u)
	}
	return nil
}

func (m *confirmModal) view(u *model) modalView {
	w := modalWidth(u, 64)
	iw := w - 2
	accent := styleUIAccent.Bold(true)
	if m.danger {
		accent = fg(uiRed).Bold(true)
	}
	lines := []string{""}
	for _, p := range m.prompt {
		lines = append(lines, "  "+p)
	}
	lines = append(lines, "")
	options := []struct{ key, label string }{{m.confirmKey, m.confirmLabel}, {"c", "Cancel"}}
	for i, o := range options {
		sel := i == m.cursor
		base := lipgloss.NewStyle()
		bar := " "
		if sel {
			base = base.Background(uiSelBg)
			bar = accent.Background(uiSelBg).Render("▌")
		}
		row := bar + base.Render(" ") + accent.Inherit(base).Render(o.key) + base.Render("  "+o.label)
		if i == 1 {
			row = bar + base.Render(" ") + styleUIDim.Inherit(base).Render(o.key) + base.Render("  "+o.label)
		}
		lines = append(lines, row+base.Render(strings.Repeat(" ", max(0, iw-cell(row)))))
	}
	lines = append(lines, "")
	return modalView{title: m.title, lines: lines, width: w, footer: "enter select · esc cancel"}
}

func (u *model) openDeleteModal() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}
	it := *item
	prompt := []string{
		"Remove worktree " + styleUIBold.Render(branchLabel(it)) + "?",
		styleUIDim.Render(ellipsizeMiddle(tildePath(it.Path), 56)),
	}
	if it.Dirty {
		prompt = append(prompt, "", fg(uiAmber).Render("! uncommitted changes will be discarded"))
	}
	u.modal = &confirmModal{
		title:        "Remove worktree",
		prompt:       prompt,
		confirmKey:   "r",
		confirmLabel: "Remove worktree",
		danger:       true,
		onConfirm:    func(u *model) tea.Cmd { return u.removeCmd(it) },
	}
}

func (u *model) openDetachModal() {
	item := u.selectedItem()
	if item == nil {
		u.setWarn("nothing selected")
		return
	}
	it := *item
	u.modal = &confirmModal{
		title: "Detach session",
		prompt: []string{
			"Detach from " + styleUIBold.Render(branchLabel(it)) + "?",
			styleUIDim.Render("Kills this worktree's tmux session only."),
			styleUIDim.Render(ellipsizeMiddle(tildePath(it.Path), 56)),
		},
		confirmKey:   "x",
		confirmLabel: "Detach session",
		onConfirm:    func(u *model) tea.Cmd { return u.detachCmd(it) },
	}
}

// ---- discard / commit -----------------------------------------------------

func (u *model) openDiscardModal() {
	files, dir := u.selectedFiles()
	if len(files) == 0 {
		return
	}
	untracked := 0
	for _, f := range files {
		if parseFileStage(f.Status).untracked {
			untracked++
		}
	}
	var prompt []string
	if len(files) == 1 {
		prompt = append(prompt, "Discard all changes to "+styleUIBold.Render(files[0].Path)+"?")
	} else {
		prompt = append(prompt, fmt.Sprintf("Discard all changes to %s?", styleUIBold.Render(fmt.Sprintf("%d files", len(files)))))
		for i, f := range files {
			if i == 4 {
				prompt = append(prompt, styleUIDim.Render(fmt.Sprintf("… and %d more", len(files)-4)))
				break
			}
			prompt = append(prompt, styleUIDim.Render(ellipsizeMiddle(f.Path, 56)))
		}
	}
	if untracked > 0 {
		prompt = append(prompt, "", fg(uiAmber).Render(fmt.Sprintf("! %d untracked %s will be deleted", untracked, plural(untracked, "file", "files"))))
	}
	prompt = append(prompt, "", fg(uiRed).Render("This cannot be undone."))
	u.modal = &confirmModal{
		title:        "Discard changes",
		prompt:       prompt,
		confirmKey:   "d",
		confirmLabel: "Discard changes",
		danger:       true,
		onConfirm:    func(u *model) tea.Cmd { return u.discardFiles(dir, files) },
	}
}

type commitModal struct {
	input  textinput.Model
	dir    string
	staged []string
}

func (u *model) openCommitModal() tea.Cmd {
	item := u.selectedItem()
	if item == nil || u.diffPath != item.Path {
		return nil
	}
	var staged []string
	for _, f := range u.diffItems {
		if parseFileStage(f.Status).staged {
			staged = append(staged, f.Path)
		}
	}
	if len(staged) == 0 {
		if len(u.diffItems) == 0 {
			u.setWarn("nothing to commit")
		} else {
			u.setWarn("nothing staged — space stages a file, a stages everything")
		}
		return nil
	}
	u.modal = &commitModal{input: newInput("commit message"), dir: item.Path, staged: staged}
	return nil
}

func (m *commitModal) takesText() bool { return true }

func (m *commitModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		u.closeModal()
		return nil
	case "enter":
		message := strings.TrimSpace(m.input.Value())
		if message == "" {
			u.setWarn("commit message is required")
			return nil
		}
		u.closeModal()
		return u.commitCmd(m.dir, message)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *commitModal) view(u *model) modalView {
	w := modalWidth(u, 72)
	iw := w - 2
	m.input.Width = iw - 6
	lines := []string{""}
	for i, p := range m.staged {
		if i == 4 {
			lines = append(lines, "  "+styleUIDim.Render(fmt.Sprintf("… and %d more", len(m.staged)-4)))
			break
		}
		lines = append(lines, "  "+fg(uiSuccess).Render("●")+" "+styleUIDim.Render(ellipsizeMiddle(p, iw-8)))
	}
	lines = append(lines, "", "  "+styleUIAccent.Render("› ")+m.input.View(), "")
	title := fmt.Sprintf("Commit %d staged %s", len(m.staged), plural(len(m.staged), "file", "files"))
	return modalView{title: title, lines: lines, width: w, footer: "enter commit · esc cancel"}
}

// ---- filter ---------------------------------------------------------------

type filterModal struct{ input textinput.Model }

func newInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.PlaceholderStyle = styleUIDim
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Cursor.Style = lipgloss.NewStyle().Reverse(true)
	ti.CharLimit = 200
	ti.Focus()
	return ti
}

func newFilterModal(u *model) *filterModal {
	m := &filterModal{input: newInput("branch or path")}
	m.input.SetValue(u.filter)
	m.input.CursorEnd()
	return m
}

func (m *filterModal) takesText() bool { return true }

func (m *filterModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		u.closeModal()
		return nil
	case "enter":
		u.filter = strings.TrimSpace(m.input.Value())
		u.closeModal()
		prev := ""
		if it := u.selectedItem(); it != nil {
			prev = it.Path
		}
		u.applyFilter()
		u.selectPathIfVisible(prev)
		if u.filter == "" {
			u.setInfo("filter cleared")
		} else {
			u.setInfo("filter: %s", u.filter)
		}
		return u.requestDiff(true)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *filterModal) view(u *model) modalView {
	w := modalWidth(u, 56)
	m.input.Width = w - 8
	return modalView{
		title: "Filter worktrees",
		lines: []string{"", "  " + styleUIAccent.Render("⌕ ") + m.input.View(), ""},
		width: w, footer: "enter apply · esc cancel",
	}
}

// ---- repo switcher --------------------------------------------------------

type repoModal struct {
	repos  []repoChoice
	cursor int
	top    int
}

func (u *model) openRepoModal() {
	if len(u.repos) <= 1 {
		u.setWarn("no other repositories found near current repo")
		return
	}
	m := &repoModal{repos: u.repos}
	for i, r := range u.repos {
		if r.Root == u.repoRoot {
			m.cursor = i
		}
	}
	u.modal = m
}

func (m *repoModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "c", "q", "ctrl+c":
		u.closeModal()
		u.setInfo("repo switch canceled")
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.repos)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.repos) - 1
	case "enter":
		repo := m.repos[m.cursor]
		u.closeModal()
		return u.switchRepo(repo)
	}
	return nil
}

func (m *repoModal) view(u *model) modalView {
	w := modalWidth(u, 88)
	iw := w - 2
	const maxRows = 12
	rows := min(maxRows, len(m.repos))
	m.top = windowTop(m.top, m.cursor, rows, len(m.repos))
	lines := []string{""}
	for i := m.top; i < m.top+rows; i++ {
		r := m.repos[i]
		mark := " "
		if r.Root == u.repoRoot {
			mark = styleUIAccent.Render("●")
		}
		lines = append(lines, pickerRow(i == m.cursor, mark, repoChoiceLabel(r), r.Branch, styleUIAccent, iw))
	}
	lines = append(lines, "")
	return modalView{title: "Switch repository", lines: lines, width: w, footer: fmt.Sprintf("%d of %d · enter switch · esc cancel", m.cursor+1, len(m.repos))}
}

// ---- create worktree ------------------------------------------------------

type branchRow struct {
	name     string
	isNew    bool
	isRemote bool
	item     *ghWorkItem // set for rows that come from an issue or pull request
}

type createModal struct {
	input    textinput.Model
	branches []BranchInfo
	items    []ghWorkItem
	itemsErr error
	itemsOK  bool // work items have arrived (or failed)
	rows     []branchRow
	cursor   int
	top      int
	inList   bool
	repoRoot string
}

func (u *model) openCreateModal() tea.Cmd {
	repoRoot, err := u.mgr.RequireRepo()
	if err != nil {
		u.setError("not in a git repo: %v", err)
		return nil
	}
	branches, _ := u.mgr.ListBranches(repoRoot)
	m := &createModal{input: newInput("type to filter, #123, or name a new branch"), branches: branches, repoRoot: repoRoot}
	m.rebuild("")
	u.modal = m
	return func() tea.Msg {
		items, err := fetchWorkItems(context.Background(), repoRoot)
		return workItemsMsg{modal: m, items: items, err: err}
	}
}

func (m *createModal) setWorkItems(items []ghWorkItem, err error) {
	m.items, m.itemsErr, m.itemsOK = items, err, true
	m.rebuild(m.input.Value())
}

func (m *createModal) rebuild(query string) {
	m.rows = nil
	q := strings.TrimSpace(query)
	lq := strings.ToLower(q)
	if lq != "" {
		exact := false
		for _, b := range m.branches {
			if strings.ToLower(b.Name) == lq {
				exact = true
				break
			}
		}
		if !exact && !strings.HasPrefix(q, "#") {
			m.rows = append(m.rows, branchRow{name: q, isNew: true})
		}
	}
	for _, b := range m.branches {
		if lq == "" || strings.Contains(strings.ToLower(b.Name), lq) {
			m.rows = append(m.rows, branchRow{name: b.Name, isRemote: b.Remote})
		}
	}
	num := strings.TrimPrefix(lq, "#")
	for i := range m.items {
		it := &m.items[i]
		hay := strings.ToLower(fmt.Sprintf("%d %s %s", it.Number, it.Title, it.Head))
		if lq == "" || strings.Contains(hay, lq) || (num != "" && strings.HasPrefix(fmt.Sprint(it.Number), num)) {
			m.rows = append(m.rows, branchRow{name: it.Title, item: it})
		}
	}
	m.cursor, m.top = 0, 0
}

func (m *createModal) create(u *model, r branchRow) tea.Cmd {
	name := strings.TrimSpace(r.name)
	if name == "" {
		u.setWarn("branch name is required")
		return nil
	}
	u.closeModal()
	it := r.item
	switch {
	case it == nil:
		return u.createCmd(name, !r.isNew, nil)
	case it.Kind == "issue":
		return u.createCmd(it.BranchGuess, false, nil)
	case it.CrossRepo:
		local := fmt.Sprintf("pr/%d", it.Number)
		if slug := titleSlug(it.Title, 5); slug != "" {
			local += "-" + slug
		}
		root, ref := m.repoRoot, fmt.Sprintf("+pull/%d/head:%s", it.Number, local)
		return u.createCmd(local, true, func() error {
			return runCmdQuietTimeout(root, 60*time.Second, "git", "fetch", "origin", ref)
		})
	default:
		root, head := m.repoRoot, it.Head
		return u.createCmd(head, true, func() error {
			return runCmdQuietTimeout(root, 60*time.Second, "git", "fetch", "origin", head)
		})
	}
}

func (m *createModal) takesText() bool { return !m.inList }

func (m *createModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "esc" || key == "ctrl+c" {
		u.closeModal()
		return nil
	}
	if !m.inList {
		switch key {
		case "enter":
			if len(m.rows) > 0 {
				return m.create(u, m.rows[0])
			}
			return m.create(u, branchRow{name: m.input.Value(), isNew: true})
		case "down", "tab", "ctrl+n":
			if len(m.rows) > 0 {
				m.inList = true
				m.input.Blur()
			}
			return nil
		}
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.rebuild(m.input.Value())
		}
		return cmd
	}

	switch key {
	case "enter":
		if m.cursor < len(m.rows) {
			return m.create(u, m.rows[m.cursor])
		}
	case "c":
		u.closeModal()
	case "j", "down", "ctrl+n":
		m.cursor = min(m.cursor+1, len(m.rows)-1)
	case "k", "up", "ctrl+p":
		if m.cursor == 0 {
			m.inList = false
			m.input.Focus()
		} else {
			m.cursor--
		}
	case "shift+tab":
		m.inList = false
		m.input.Focus()
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.rows) - 1
	}
	return nil
}

func (m *createModal) view(u *model) modalView {
	w := modalWidth(u, 84)
	iw := w - 2
	m.input.Width = iw - 6
	maxRows := max(3, min(12, u.height-12))
	rows := min(maxRows, max(1, len(m.rows)))
	m.top = windowTop(m.top, m.cursor, rows, len(m.rows))

	prompt := styleUIDim.Render("+ ")
	if !m.inList {
		prompt = styleUIAccent.Render("+ ")
	}
	lines := []string{"", "  " + prompt + m.input.View(), "  " + styleUIFaint.Render(strings.Repeat("─", iw-4))}
	if len(m.rows) == 0 {
		lines = append(lines, "", "  "+styleUIDim.Render("no matches — press enter to create a new branch"))
	}
	for i := m.top; i < min(len(m.rows), m.top+rows); i++ {
		r := m.rows[i]
		mark, detail, ds := " ", "", styleUIDim
		switch {
		case r.isNew:
			mark, detail, ds = styleUIAccent.Render("✦"), "new branch", styleUIAccent
		case r.item != nil && r.item.Kind == "issue":
			mark, detail, ds = fg(uiAmber).Render("◇"), fmt.Sprintf("issue #%d", r.item.Number), styleUIDim
		case r.item != nil:
			mark, detail, ds = fg(uiBlue).Render("⇄"), fmt.Sprintf("PR #%d · %s", r.item.Number, r.item.Head), styleUIDim
		case r.isRemote:
			detail, ds = "remote", fg(uiViolet)
		}
		lines = append(lines, pickerRow(m.inList && i == m.cursor, mark, r.name, detail, ds, iw))
	}
	lines = append(lines, "")
	footer := "enter create · ↓ list · esc cancel"
	if m.inList {
		footer = fmt.Sprintf("%d of %d · enter create · ↑ filter · esc cancel", m.cursor+1, len(m.rows))
		if m.cursor < len(m.rows) {
			if it := m.rows[m.cursor].item; it != nil && it.Kind == "issue" {
				footer = "creates " + it.BranchGuess + " · enter create · esc cancel"
			}
		}
	}
	switch {
	case !m.itemsOK:
		footer = "loading issues & PRs… · " + footer
	case m.itemsErr != nil && !errors.Is(m.itemsErr, errGHUnavailable):
		footer = "gh: " + ellipsize(m.itemsErr.Error(), 30) + " · " + footer
	}
	return modalView{title: "New worktree", lines: lines, width: w, footer: footer}
}

// ---- help -----------------------------------------------------------------

type binding struct{ key, what, detail string }

type helpModal struct {
	title    string
	bindings []binding
	cursor   int
	top      int
}

func newHelpModal(u *model) *helpModal {
	general := []binding{
		{"tab / shift+tab", "Switch pane", "Cycle focus through status, worktrees, files and patch. 1–4 jump straight to a pane."},
		{"R", "Refresh", "Reload worktrees, repository metadata and diffs."},
		{"?", "Keybindings", "Open this contextual help."},
		{"q / ctrl+c", "Quit", "Exit sprout."},
	}
	var title string
	var specific []binding
	switch u.focus {
	case paneWorktrees:
		title = "Worktrees"
		specific = []binding{
			{"j / k", "Move selection", "Move through your git worktrees."},
			{"enter / g", "Attach", "Open or focus the tmux session for the selected worktree."},
			{"p", "Promote to preview", "Run the configured preview services from the selected worktree (▶ marks the current preview)."},
			{"n", "New worktree", "Create a branch and worktree from this repo."},
			{"o", "Open pull request", "Open the selected branch's pull request in the browser."},
			{"x", "Remove worktree", "Delete the selected worktree. Its branch is kept."},
			{"d", "Detach session", "Stop the selected worktree's tmux session (keeps the worktree)."},
			{"/", "Filter", "Narrow the list by branch name or path. esc clears it."},
		}
	case paneFiles:
		title = "Files"
		specific = []binding{
			{"j / k", "Move rows", "Move through changed files and directories."},
			{"space", "Stage / unstage", "Stage or unstage the file, or everything under a directory."},
			{"a", "Stage all / unstage all", "Stage every change, or unstage everything when all of it is staged."},
			{"c", "Commit", "Commit what is staged. Signed commits borrow the terminal for the passphrase prompt."},
			{"d", "Discard", "Throw away changes to the file (or directory). Asks first. Untracked files are deleted."},
			{"enter", "Open / fold", "Fold or unfold a directory, or jump into the patch for a file."},
			{"`", "Tree or flat", "Switch between a tree and a flat file list."},
			{"- / =", "Fold all / unfold all", "Collapse or expand every directory in tree view."},
			{"J / K", "Scroll patch", "Scroll the patch without leaving the file list."},
		}
	case panePatch:
		title = "Patch"
		specific = []binding{
			{"j / k", "Scroll", "Scroll the patch one line at a time."},
			{"ctrl+d / ctrl+u", "Half page", "Scroll the patch half a page."},
			{"g / G", "Top / bottom", "Jump to the start or end of the patch."},
			{"h / l", "Pan", "Pan long lines left and right."},
			{"esc", "Back", "Return to the file list."},
		}
	default:
		title = "General"
		specific = []binding{{"enter", "Switch repository", "Pick another repository next to this one."}}
	}
	return &helpModal{title: title, bindings: append(specific, general...)}
}

func (m *helpModal) update(u *model, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "c", "q", "?", "enter", "ctrl+c":
		u.closeModal()
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.bindings)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "pgdown", "ctrl+d":
		m.cursor = min(m.cursor+8, len(m.bindings)-1)
	case "pgup", "ctrl+u":
		m.cursor = max(m.cursor-8, 0)
	}
	return nil
}

func (m *helpModal) view(u *model) modalView {
	w := modalWidth(u, 76)
	iw := w - 2
	rows := min(len(m.bindings), max(4, min(14, u.height-14)))
	m.top = windowTop(m.top, m.cursor, rows, len(m.bindings))
	lines := []string{""}
	for i := m.top; i < m.top+rows; i++ {
		b := m.bindings[i]
		sel := i == m.cursor
		base := lipgloss.NewStyle()
		bar := " "
		if sel {
			base = base.Background(uiSelBg)
			bar = styleUIAccent.Background(uiSelBg).Render("▌")
		}
		keyCol := fit(b.key, 18)
		row := bar + base.Render(" ") + styleUIAccent.Bold(true).Inherit(base).Render(keyCol) + base.Render(" ") + base.Render(b.what)
		lines = append(lines, row+base.Render(strings.Repeat(" ", max(0, iw-cell(row)))))
	}
	lines = append(lines, "", "  "+styleUIDim.Render(ellipsize(m.bindings[m.cursor].detail, iw-4)), "")
	return modalView{title: m.title + " keys", lines: lines, width: w, footer: fmt.Sprintf("%d of %d · esc close", m.cursor+1, len(m.bindings))}
}
