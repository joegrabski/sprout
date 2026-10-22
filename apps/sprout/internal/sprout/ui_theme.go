package sprout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TUI palette. One strong accent (sprout green) on muted neutrals; every other
// colour carries meaning (amber = dirty/busy, violet = needs you, red = danger).
var (
	uiAccent  = lipgloss.AdaptiveColor{Light: "#2f9e44", Dark: "#9be564"}
	uiText    = lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#e6e9ef"}
	uiDim     = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#7d8590"}
	uiFaint   = lipgloss.AdaptiveColor{Light: "#b6bcc4", Dark: "#454c56"}
	uiSelBg   = lipgloss.AdaptiveColor{Light: "#dff3dc", Dark: "#233326"}
	uiAmber   = lipgloss.AdaptiveColor{Light: "#bf8700", Dark: "#e5c07b"}
	uiRed     = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f07178"}
	uiViolet  = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#c792ea"}
	uiBlue    = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#82aaff"}
	uiSuccess = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#9be564"}
)

func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

var (
	styleUIDim    = fg(uiDim)
	styleUIFaint  = fg(uiFaint)
	styleUIAccent = fg(uiAccent)
	styleUIBold   = lipgloss.NewStyle().Bold(true)
)

// cell is the display width of s, ANSI aware.
func cell(s string) int { return ansi.StringWidth(s) }

// fit truncates s to w cells (with an ellipsis) and right-pads it to exactly w.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if cell(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - cell(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// spread places left and right on one line of width w, truncating left first.
func spread(left, right string, w int) string {
	rw := cell(right)
	if rw >= w {
		return fit(right, w)
	}
	gap := w - rw - cell(left)
	if gap < 1 {
		left = ansi.Truncate(left, w-rw-1, "…")
		gap = w - rw - cell(left)
	}
	return left + strings.Repeat(" ", gap) + right
}

// ellipsize shortens plain text to at most w display cells.
func ellipsize(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if cell(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// ellipsizeMiddle keeps the head and tail of a path-like string.
func ellipsizeMiddle(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if cell(s) <= w {
		return s
	}
	if w <= 3 {
		return ansi.Truncate(s, w, "")
	}
	keep := w - 1
	head := keep / 3
	tail := keep - head
	return ansi.Truncate(s, head, "") + "…" + ansi.TruncateLeft(s, cell(s)-tail, "")
}

// box draws a lazygit-style bordered pane with the title set into the top
// border and an optional footer set into the bottom border. w and h are outer
// dimensions; lines are clipped/padded to the inner area.
func box(num, title, footer string, lines []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc := styleUIFaint
	if focused {
		bc = styleUIAccent
	}
	iw, ih := w-2, h-2

	numStyle, titleStyle := styleUIDim, styleUIDim
	if focused {
		numStyle = styleUIAccent.Bold(true)
		titleStyle = styleUIBold
	}
	label := ""
	if num != "" {
		label += numStyle.Render(num) + " "
	}
	label += titleStyle.Render(title)
	label = " " + ansi.Truncate(label, iw-3, "…") + " "

	top := bc.Render("╭─") + label
	top += bc.Render(strings.Repeat("─", max(0, w-1-cell(top)))) + bc.Render("╮")

	bottom := bc.Render(strings.Repeat("─", w-2))
	if footer != "" {
		f := " " + styleUIDim.Render(ansi.Truncate(footer, iw-4, "…")) + " "
		bottom = bc.Render(strings.Repeat("─", max(0, w-2-cell(f)-2))) + f + bc.Render("──")
	}
	bottom = bc.Render("╰") + bottom + bc.Render("╯")

	var b strings.Builder
	b.WriteString(top)
	side := bc.Render("│")
	for i := 0; i < ih; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString("\n")
		b.WriteString(side + fit(line, iw) + side)
	}
	b.WriteString("\n")
	b.WriteString(bottom)
	return b.String()
}

// joinHorizontal glues two blocks of equal height side by side.
func joinHorizontal(a, b string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, a, b)
}

// dim flattens a rendered frame to a single muted colour so an overlay reads
// as the only live thing on screen.
func dimLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// overlay splices fg over bg at (x, y). bg is dimmed first.
func overlay(bg string, fgBlock string, x, y int) string {
	bgLines := dimLines(strings.Split(bg, "\n"))
	fgLines := strings.Split(fgBlock, "\n")
	fw := 0
	for _, l := range fgLines {
		fw = max(fw, cell(l))
	}
	for i, fl := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		base := bgLines[row]
		left := ansi.Truncate(base, x, "")
		if pad := x - cell(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(base, x+fw, "")
		bgLines[row] = styleUIFaint.Render(left) + fit(fl, fw) + styleUIFaint.Render(right)
	}
	return strings.Join(bgLines, "\n")
}

// centerBlock returns lines centred in a w×h area.
func centerBlock(msg []string, w, h int) []string {
	out := make([]string, 0, h)
	top := max(0, (h-len(msg))/2)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, m := range msg {
		pad := max(0, (w-cell(m))/2)
		out = append(out, strings.Repeat(" ", pad)+m)
	}
	return out
}

// keyHint renders "key desc" the way the key bar and modals show shortcuts.
func keyHint(key, desc string) string {
	return styleUIAccent.Bold(true).Render(key) + " " + styleUIDim.Render(desc)
}

// windowTop returns a scroll offset that keeps sel visible in a window of cap rows.
func windowTop(top, sel, capacity, total int) int {
	if capacity <= 0 || total <= capacity {
		return 0
	}
	if sel < top {
		top = sel
	}
	if sel >= top+capacity {
		top = sel - capacity + 1
	}
	return max(0, min(top, total-capacity))
}
