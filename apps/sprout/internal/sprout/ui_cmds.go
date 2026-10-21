package sprout

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Message levels shown in the footer.
const (
	levelStatus = "STATUS"
	levelInfo   = "INFO"
	levelWarn   = "WARN"
	levelError  = "ERROR"
)

// Messages produced by background commands.

type refreshedMsg struct {
	items      []Worktree
	previewDir string
	repoBranch string
	repos      []repoChoice // nil: keep the current list
	repoSlug   string
	repoCache  map[string]repoChoice
	err        error
	selectPath string
	level      string
	notice     string
}

type diffLoadedMsg struct {
	seq      int64
	path     string
	files    []DiffFile
	selected int
	text     string
	err      error
}

// agentStatesMsg carries the result of a sweep over all running agents.
type agentStatesMsg struct {
	repoRoot string
	states   map[string]agentPromptState
}

// agentScreenMsg carries a captured agent screen for the Agent tab.
type agentScreenMsg struct {
	repoRoot string
	path     string
	lines    []string
	err      error
}

// prsMsg carries pull request info keyed by branch.
type prsMsg struct {
	repoRoot string
	prs      map[string]PRInfo
	err      error
}

// workItemsMsg delivers issues and PRs to the new-worktree dialog that asked
// for them; it is dropped if that dialog is no longer open.
type workItemsMsg struct {
	modal *createModal
	items []ghWorkItem
	err   error
}

type tickMsg time.Time

// noticeMsg is progress reported from a long-running command via Program.Send.
type noticeMsg struct{ level, text string }

// opDoneMsg completes a background operation started with startOp.
type opDoneMsg struct {
	level, text string
	refresh     bool
	fresh       bool
	selectPath  string
}

type updateAvailableMsg struct{ latest string }

type execDoneMsg struct {
	err  error
	path string
	what string
}

func tickCmd() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// funcExec adapts a blocking function (such as attaching to tmux) to tea.ExecCommand.
type funcExec struct{ fn func() error }

func (f *funcExec) Run() error          { return f.fn() }
func (f *funcExec) SetStdin(io.Reader)  {}
func (f *funcExec) SetStdout(io.Writer) {}
func (f *funcExec) SetStderr(io.Writer) {}

// ---- refresh --------------------------------------------------------------

type refreshReq struct {
	mgr        *Manager
	repoRoot   string
	fresh      bool
	skipRepos  bool
	repoCache  map[string]repoChoice
	selectPath string
	level      string
	notice     string
}

func (r refreshReq) run() tea.Msg {
	msg := refreshedMsg{selectPath: r.selectPath, level: r.level, notice: r.notice}
	var (
		items []Worktree
		err   error
	)
	if r.fresh {
		items, err = r.mgr.ListWorktreesForRepo(r.repoRoot)
	} else {
		var state daemonState
		state, err = listWorktreesFastState(r.mgr, r.repoRoot)
		items = state.Worktrees
	}
	if err != nil {
		msg.err = err
		return msg
	}
	msg.items = items
	msg.repoBranch = r.mgr.CurrentBranch(r.repoRoot)
	if st, err := r.mgr.readPreviewState(r.repoRoot); err == nil && st != nil {
		msg.previewDir = st.Path
	}
	if !r.skipRepos {
		msg.repos, msg.repoSlug, msg.repoCache = scanRepos(r.repoRoot, msg.repoBranch, r.repoCache)
	}
	return msg
}

// scanRepos lists sibling git repositories of repoRoot for the repo switcher.
func scanRepos(repoRoot, currentBranch string, cache map[string]repoChoice) ([]repoChoice, string, map[string]repoChoice) {
	parent := filepath.Dir(repoRoot)
	roots := []string{repoRoot}
	if entries, err := os.ReadDir(parent); err == nil {
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			root := filepath.Join(parent, ent.Name())
			if root == repoRoot || !isGitRepoDir(root) {
				continue
			}
			roots = append(roots, root)
		}
	}

	next := make(map[string]repoChoice, len(roots))
	repos := make([]repoChoice, len(roots))
	var wg sync.WaitGroup
	for i, root := range roots {
		if cached, ok := cache[root]; ok {
			repos[i] = cached
			continue
		}
		wg.Add(1)
		go func(i int, root string) {
			defer wg.Done()
			repos[i] = buildRepoChoice(root)
		}(i, root)
	}
	wg.Wait()
	for i, root := range roots {
		if root == repoRoot && strings.TrimSpace(currentBranch) != "" {
			repos[i].Branch = currentBranch
		}
		next[root] = repos[i]
	}

	sort.Slice(repos, func(i, j int) bool {
		if repos[i].Root == repoRoot {
			return true
		}
		if repos[j].Root == repoRoot {
			return false
		}
		return repoChoiceLabel(repos[i]) < repoChoiceLabel(repos[j])
	})
	slug := ""
	for _, r := range repos {
		if r.Root == repoRoot {
			slug = r.GitHubRepo
			break
		}
	}
	return repos, slug, next
}

// ---- diff -----------------------------------------------------------------

type diffFilesCacheEntry struct {
	digest   string
	files    []DiffFile
	fetched  time.Time
	lastUsed uint64
}

type diffPatchCacheEntry struct {
	text     string
	lastUsed uint64
}

type diffSnapshotResult struct {
	digest string
	files  []DiffFile
}

// diffStore caches git status/patch lookups. It is shared with background
// commands, so everything goes through mu.
type diffStore struct {
	mu      sync.Mutex
	files   map[string]diffFilesCacheEntry
	patches map[string]diffPatchCacheEntry
	use     uint64
}

func newDiffStore() *diffStore {
	return &diffStore{
		files:   map[string]diffFilesCacheEntry{},
		patches: map[string]diffPatchCacheEntry{},
	}
}

func (s *diffStore) clear() {
	s.mu.Lock()
	s.files = map[string]diffFilesCacheEntry{}
	s.patches = map[string]diffPatchCacheEntry{}
	s.mu.Unlock()
}

func (s *diffStore) has(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.files[path]
	return ok
}

// snapshot returns the changed-file list for path, reusing a cached copy
// younger than maxAge.
func (s *diffStore) snapshot(ctx context.Context, mgr *Manager, path string, maxAge time.Duration) (diffSnapshotResult, error) {
	s.mu.Lock()
	if e, ok := s.files[path]; ok && time.Since(e.fetched) <= maxAge {
		s.use++
		e.lastUsed = s.use
		s.files[path] = e
		s.mu.Unlock()
		return diffSnapshotResult{digest: e.digest, files: e.files}, nil
	}
	s.mu.Unlock()

	snap, err := mgr.WorktreeDiffSnapshotContext(ctx, path)
	if err != nil {
		return diffSnapshotResult{}, err
	}
	s.mu.Lock()
	s.use++
	s.files[path] = diffFilesCacheEntry{digest: snap.Digest, files: snap.Files, fetched: time.Now(), lastUsed: s.use}
	evictOldest(s.files, 128, func(e diffFilesCacheEntry) uint64 { return e.lastUsed })
	s.mu.Unlock()
	return diffSnapshotResult{digest: snap.Digest, files: snap.Files}, nil
}

func diffPatchCacheKey(path, digest string, file DiffFile, width int) string {
	return strings.Join([]string{path, digest, file.Path, file.PreviousPath, file.Status, strconv.Itoa(width)}, "\x00")
}

func (s *diffStore) patch(ctx context.Context, mgr *Manager, path, digest string, file DiffFile, width int) (string, error) {
	key := diffPatchCacheKey(path, digest, file, width)
	s.mu.Lock()
	if e, ok := s.patches[key]; ok {
		s.use++
		e.lastUsed = s.use
		s.patches[key] = e
		s.mu.Unlock()
		return e.text, nil
	}
	s.mu.Unlock()

	text, err := mgr.WorktreeDiffForFileContext(ctx, path, file, width)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.use++
	s.patches[key] = diffPatchCacheEntry{text: text, lastUsed: s.use}
	evictOldest(s.patches, 512, func(e diffPatchCacheEntry) uint64 { return e.lastUsed })
	s.mu.Unlock()
	return text, nil
}

func evictOldest[V any](m map[string]V, limit int, used func(V) uint64) {
	for len(m) > limit {
		var oldest string
		var oldestUse uint64
		first := true
		for k, v := range m {
			if u := used(v); first || u < oldestUse {
				first, oldest, oldestUse = false, k, u
			}
		}
		delete(m, oldest)
	}
}

// loadDiff gathers the file list and the selected file's patch for item.
// It returns nil when ctx is canceled so superseded requests vanish quietly.
func loadDiff(ctx context.Context, seq int64, mgr *Manager, store *diffStore, path string, sel, width int) tea.Msg {
	snap, err := store.snapshot(ctx, mgr, path, 1500*time.Millisecond)
	if err != nil {
		if errorsIsCanceled(err) || ctx.Err() != nil {
			return nil
		}
		return diffLoadedMsg{seq: seq, path: path, err: err}
	}
	msg := diffLoadedMsg{seq: seq, path: path, files: snap.files}
	if len(snap.files) == 0 {
		return msg
	}
	sel = max(0, min(sel, len(snap.files)-1))
	msg.selected = sel
	text, err := store.patch(ctx, mgr, path, snap.digest, snap.files[sel], width)
	if err != nil {
		if errorsIsCanceled(err) || ctx.Err() != nil {
			return nil
		}
		text = fmt.Sprintf("Unable to read file diff.\n\n%s", err)
	}
	msg.text = text
	return msg
}

// ---- agent prompt probing -------------------------------------------------

// agentProbe classifies agent panes by capturing their screens. It never
// touches the panes (no pipe-pane, no resize), so it is safe to sweep every
// agent worktree.
type agentProbe struct {
	mu       sync.Mutex
	targets  map[string]string
	activity map[string]int64
}

func newAgentProbe() *agentProbe {
	return &agentProbe{
		targets:  map[string]string{},
		activity: map[string]int64{},
	}
}

func (p *agentProbe) forget(path string) {
	p.mu.Lock()
	delete(p.targets, path)
	delete(p.activity, path)
	p.mu.Unlock()
}

func (p *agentProbe) paneTarget(mgr *Manager, repoRoot string, item Worktree) string {
	if strings.TrimSpace(item.Path) == "" || item.AgentState != "yes" {
		p.forget(item.Path)
		return ""
	}
	p.mu.Lock()
	if t := p.targets[item.Path]; strings.TrimSpace(t) != "" {
		p.mu.Unlock()
		return t
	}
	p.mu.Unlock()
	t := strings.TrimSpace(mgr.agentPaneTarget(repoRoot, &item))
	if t == "" {
		return ""
	}
	p.mu.Lock()
	p.targets[item.Path] = t
	p.mu.Unlock()
	return t
}

// trimBlankTail drops empty rows from the bottom of a captured screen.
func trimBlankTail(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(stripANSI(rows[len(rows)-1])) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// classify captures the agent pane and reports its prompt state. changed is
// false when the pane has not been active since the last look.
func (p *agentProbe) classify(mgr *Manager, repoRoot string, item Worktree) (state agentPromptState, changed bool) {
	target := p.paneTarget(mgr, repoRoot, item)
	if target == "" {
		return agentPromptUnknown, false
	}
	if activity, err := tmuxPaneActivityFn(target); err == nil {
		p.mu.Lock()
		last, seen := p.activity[item.Path]
		p.activity[item.Path] = activity
		p.mu.Unlock()
		// pane_activity has one-second resolution, so keep looking while the
		// pane is fresh instead of trusting an equal timestamp.
		if seen && last == activity && time.Now().Unix()-activity > 2 {
			return agentPromptUnknown, false
		}
	}
	out, err := tmuxCapturePaneFn(target, 40, false)
	if err != nil {
		return agentPromptUnknown, false
	}
	text := strings.Join(trimBlankTail(strings.Split(out, "\n")), "\n")
	if strings.TrimSpace(text) == "" {
		return agentPromptBusy, true
	}
	return agentPromptStateForOutput(text), true
}

// sweep classifies every running agent in items, a few at a time.
func (p *agentProbe) sweep(mgr *Manager, repoRoot string, items []Worktree) map[string]agentPromptState {
	var agents []Worktree
	for _, it := range items {
		if it.AgentState == "yes" {
			agents = append(agents, it)
		}
	}
	out := map[string]agentPromptState{}
	var outMu sync.Mutex
	jobs := make(chan Worktree)
	var wg sync.WaitGroup
	for w := 0; w < min(4, len(agents)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				if state, changed := p.classify(mgr, repoRoot, it); changed {
					outMu.Lock()
					out[it.Path] = state
					outMu.Unlock()
				}
			}
		}()
	}
	for _, it := range agents {
		jobs <- it
	}
	close(jobs)
	wg.Wait()
	return out
}

// screen captures the agent pane for display: the last rows lines, blank tail
// removed, escape sequences kept so colours survive.
func (p *agentProbe) screen(mgr *Manager, repoRoot string, item Worktree, rows int) ([]string, error) {
	target := p.paneTarget(mgr, repoRoot, item)
	if target == "" {
		return nil, fmt.Errorf("agent pane not found")
	}
	out, err := tmuxCapturePaneFn(target, rows, false)
	if err != nil {
		return nil, err
	}
	lines := trimBlankTail(strings.Split(strings.ReplaceAll(out, "\t", "    "), "\n"))
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	return lines, nil
}
