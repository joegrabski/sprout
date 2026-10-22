package sprout

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Pure (UI-framework independent) logic used by the TUI: agent prompt
// detection, diff tree building and repo discovery.

type repoChoice struct {
	Root       string
	Name       string
	GitHubRepo string
	Branch     string
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
)

type diffTreeRow struct {
	label     string
	status    string
	fileIndex int
	isDir     bool
	depth     int
	key       string
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

func errorsIsCanceled(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
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
