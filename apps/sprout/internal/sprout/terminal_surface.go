package sprout

import (
	"io"
	"strings"
	"sync"

	vt10x "github.com/ActiveState/vt10x"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type terminalSurface struct {
	*tview.Box
	mu         sync.Mutex
	state      *vt10x.State
	term       *vt10x.VT
	paneTarget string
	width      int
	height     int
}

func newTerminalSurface() *terminalSurface {
	s := &terminalSurface{Box: tview.NewBox()}
	s.SetBackgroundColor(tcell.ColorDefault)
	s.resetLocked("", 80, 24)
	return s
}

func (s *terminalSurface) resetLocked(paneTarget string, width int, height int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	state := &vt10x.State{}
	term, _ := vt10x.New(state, strings.NewReader(""), io.Discard)
	term.Resize(width, height)
	s.state = state
	s.term = term
	s.paneTarget = paneTarget
	s.width = width
	s.height = height
}

func (s *terminalSurface) ensureSizeLocked(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	if s.state == nil || s.term == nil {
		s.resetLocked(s.paneTarget, width, height)
		return
	}
	if s.width == width && s.height == height {
		return
	}
	s.term.Resize(width, height)
	s.width = width
	s.height = height
}

func (s *terminalSurface) ResetWithData(paneTarget string, data []byte, width int, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked(paneTarget, width, height)
	if len(data) > 0 {
		_, _ = s.term.Write(data)
	}
}

func (s *terminalSurface) AppendData(paneTarget string, data []byte, width int, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paneTarget != paneTarget || s.state == nil || s.term == nil {
		s.resetLocked(paneTarget, width, height)
	}
	s.ensureSizeLocked(width, height)
	if len(data) > 0 {
		_, _ = s.term.Write(data)
	}
}

func (s *terminalSurface) Draw(screen tcell.Screen) {
	s.Box.DrawForSubclass(screen, s)
	x, y, width, height := s.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	s.mu.Lock()
	s.ensureSizeLocked(width, height)
	state := s.state
	s.mu.Unlock()
	if state == nil {
		return
	}

	state.Lock()
	defer state.Unlock()
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			ch, fg, bg := state.Cell(col, row)
			if ch == 0 {
				ch = ' '
			}
			style := tcell.StyleDefault
			if fg != vt10x.DefaultFG {
				style = style.Foreground(tcell.Color(fg))
			}
			if bg != vt10x.DefaultBG {
				style = style.Background(tcell.Color(bg))
			}
			screen.SetContent(x+col, y+row, ch, nil, style)
		}
	}
	if state.CursorVisible() {
		curx, cury := state.Cursor()
		screen.ShowCursor(x+curx, y+cury)
	}
}

func (s *terminalSurface) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return s.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {})
}

func (s *terminalSurface) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return s.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if action == tview.MouseLeftClick {
			setFocus(s)
			return true, s
		}
		return false, nil
	})
}
