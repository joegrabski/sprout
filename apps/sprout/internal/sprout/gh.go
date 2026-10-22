package sprout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// GitHub integration for the TUI. Everything shells out to the gh CLI and
// degrades to "nothing to show" when gh is missing, signed out, or the repo is
// not on GitHub.

var errGHUnavailable = errors.New("gh unavailable")

const ghTimeout = 10 * time.Second

// ghRun runs `gh args...` in dir with no stdin and no prompts.
func ghRun(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errGHUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_PAGER=")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "gh auth login") || strings.Contains(msg, "not logged in") ||
			strings.Contains(msg, "none of the git remotes") || strings.Contains(msg, "no git remotes found") || strings.Contains(msg, "Could not resolve to a Repository") {
			return nil, errGHUnavailable
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", args[0], firstLine(msg))
	}
	return out.Bytes(), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---- pull requests ----------------------------------------------------------

// PRInfo is the pull request GitHub associates with a branch.
type PRInfo struct {
	Number int
	State  string // OPEN, MERGED or CLOSED
	Draft  bool
	Checks string // "pass", "fail", "pending" or "" when there are none
	URL    string
}

type ghPR struct {
	Number            int             `json:"number"`
	HeadRefName       string          `json:"headRefName"`
	State             string          `json:"state"`
	IsDraft           bool            `json:"isDraft"`
	IsCrossRepository bool            `json:"isCrossRepository"`
	URL               string          `json:"url"`
	UpdatedAt         string          `json:"updatedAt"`
	Title             string          `json:"title"`
	StatusCheckRollup []ghCheckResult `json:"statusCheckRollup"`
}

// ghCheckResult covers both rollup entry shapes: CheckRun (status/conclusion)
// and StatusContext (state).
type ghCheckResult struct {
	Typename   string `json:"__typename"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// rollupChecks reduces a check rollup to pass, fail, pending or "".
func rollupChecks(checks []ghCheckResult) string {
	pending, passed := false, false
	for _, c := range checks {
		switch c.Typename {
		case "StatusContext":
			switch strings.ToUpper(c.State) {
			case "FAILURE", "ERROR":
				return "fail"
			case "PENDING", "EXPECTED":
				pending = true
			case "SUCCESS":
				passed = true
			}
		default:
			if !strings.EqualFold(c.Status, "COMPLETED") {
				pending = true
				continue
			}
			switch strings.ToUpper(c.Conclusion) {
			case "FAILURE", "TIMED_OUT", "CANCELLED", "STARTUP_FAILURE", "ACTION_REQUIRED":
				return "fail"
			case "SUCCESS", "NEUTRAL", "SKIPPED":
				passed = true
			}
		}
	}
	switch {
	case pending:
		return "pending"
	case passed:
		return "pass"
	}
	return ""
}

// parsePRList maps branch name to its best pull request. Fork PRs are skipped
// (their head branch names collide with local ones); among several PRs for one
// branch an open one wins, then the most recently updated.
func parsePRList(data []byte) (map[string]PRInfo, error) {
	var prs []ghPR
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, err
	}
	best := map[string]ghPR{}
	for _, pr := range prs {
		if pr.IsCrossRepository || pr.HeadRefName == "" {
			continue
		}
		cur, ok := best[pr.HeadRefName]
		if !ok || prBetter(pr, cur) {
			best[pr.HeadRefName] = pr
		}
	}
	out := make(map[string]PRInfo, len(best))
	for branch, pr := range best {
		out[branch] = PRInfo{
			Number: pr.Number,
			State:  strings.ToUpper(pr.State),
			Draft:  pr.IsDraft,
			Checks: rollupChecks(pr.StatusCheckRollup),
			URL:    pr.URL,
		}
	}
	return out, nil
}

func prBetter(a, b ghPR) bool {
	aOpen, bOpen := strings.EqualFold(a.State, "OPEN"), strings.EqualFold(b.State, "OPEN")
	if aOpen != bOpen {
		return aOpen
	}
	return a.UpdatedAt > b.UpdatedAt
}

// fetchPRs returns the PR for each branch with one open-PR call (with checks)
// and one recent merged/closed call (cheap, no checks).
func fetchPRs(ctx context.Context, repoRoot string) (map[string]PRInfo, error) {
	var (
		wg                 sync.WaitGroup
		openOut, closedOut []byte
		openErr, closedErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		openOut, openErr = ghRun(ctx, repoRoot, "pr", "list", "--state", "open", "--limit", "60",
			"--json", "number,headRefName,state,isDraft,isCrossRepository,url,updatedAt,statusCheckRollup")
	}()
	go func() {
		defer wg.Done()
		closedOut, closedErr = ghRun(ctx, repoRoot, "pr", "list", "--state", "all", "--limit", "40",
			"--json", "number,headRefName,state,isDraft,isCrossRepository,url,updatedAt")
	}()
	wg.Wait()
	if openErr != nil {
		return nil, openErr
	}
	prs, err := parsePRList(openOut)
	if err != nil {
		return nil, err
	}
	if closedErr == nil {
		if recent, err := parsePRList(closedOut); err == nil {
			for branch, pr := range recent {
				if _, ok := prs[branch]; !ok {
					prs[branch] = pr
				}
			}
		}
	}
	return prs, nil
}

// ---- issues and pull requests to start work from ---------------------------------

// ghWorkItem is an issue or pull request offered by the new-worktree dialog.
type ghWorkItem struct {
	Kind        string // "issue" or "pr"
	Number      int
	Title       string
	Head        string // PR head branch
	CrossRepo   bool
	IssueType   string
	Labels      []string
	BranchGuess string
}

// fetchWorkItems lists open issues and pull requests for the new-worktree dialog.
func fetchWorkItems(ctx context.Context, repoRoot string) ([]ghWorkItem, error) {
	var (
		wg              sync.WaitGroup
		issueOut, prOut []byte
		issueErr, prErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		issueOut, issueErr = ghRun(ctx, repoRoot, "issue", "list", "--state", "open", "--limit", "40",
			"--json", "number,title,labels")
	}()
	go func() {
		defer wg.Done()
		prOut, prErr = ghRun(ctx, repoRoot, "pr", "list", "--state", "open", "--limit", "40",
			"--json", "number,title,headRefName,isCrossRepository")
	}()
	wg.Wait()
	if issueErr != nil && prErr != nil {
		return nil, issueErr
	}
	return parseWorkItems(issueOut, prOut), nil
}

func parseWorkItems(issueJSON, prJSON []byte) []ghWorkItem {
	var items []ghWorkItem
	var prs []ghPR
	if json.Unmarshal(prJSON, &prs) == nil {
		for _, pr := range prs {
			items = append(items, ghWorkItem{Kind: "pr", Number: pr.Number, Title: pr.Title, Head: pr.HeadRefName, CrossRepo: pr.IsCrossRepository})
		}
	}
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if json.Unmarshal(issueJSON, &issues) == nil {
		for _, is := range issues {
			it := ghWorkItem{Kind: "issue", Number: is.Number, Title: is.Title}
			for _, l := range is.Labels {
				it.Labels = append(it.Labels, l.Name)
			}
			it.BranchGuess = issueBranchName(it)
			items = append(items, it)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Number > items[j].Number })
	return items
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// issueBranchName follows the convention the sprout-issue skill falls back to
// when a repo defines none: <type>/<number>-<slug>, with sprout's branch types
// (feat, fix, chore, docs, refactor, test) picked from the issue's labels and a
// slug of the first five title words.
func issueBranchName(it ghWorkItem) string {
	prefix := "feat"
labels:
	for _, l := range it.Labels {
		switch strings.ToLower(l) {
		case "bug", "fix":
			prefix = "fix"
		case "docs", "documentation":
			prefix = "docs"
		case "chore", "maintenance":
			prefix = "chore"
		case "refactor":
			prefix = "refactor"
		case "test", "tests":
			prefix = "test"
		default:
			continue
		}
		break labels
	}
	if slug := titleSlug(it.Title, 5); slug != "" {
		return fmt.Sprintf("%s/%d-%s", prefix, it.Number, slug)
	}
	return fmt.Sprintf("%s/%d", prefix, it.Number)
}

// titleSlug lowercases the first n words of title into [a-z0-9-].
func titleSlug(title string, n int) string {
	words := strings.Fields(strings.ToLower(title))
	if len(words) > n {
		words = words[:n]
	}
	return strings.Trim(slugRe.ReplaceAllString(strings.Join(words, "-"), "-"), "-")
}
