package sprout

import (
	"os"
	"testing"
)

func TestParsePRList(t *testing.T) {
	data, err := os.ReadFile("testdata/gh_pr_list.json")
	if err != nil {
		t.Fatal(err)
	}
	prs, err := parsePRList(data)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]PRInfo{
		"feat/billing":     {Number: 12, State: "OPEN", Checks: "pass", URL: "https://github.com/acme/api/pull/12"},
		"fix/auth-timeout": {Number: 14, State: "OPEN", Draft: true, Checks: "fail", URL: "https://github.com/acme/api/pull/14"},
		"chore/deps":       {Number: 15, State: "OPEN", Checks: "pending", URL: "https://github.com/acme/api/pull/15"}, // open beats the older merged PR
		"feat/old-branch":  {Number: 4, State: "CLOSED", URL: "https://github.com/acme/api/pull/4"},                    // newest of two closed PRs
	}
	for branch, want := range cases {
		if got := prs[branch]; got != want {
			t.Errorf("prs[%q] = %+v, want %+v", branch, got, want)
		}
	}
	if _, ok := prs["main"]; ok {
		t.Error("fork PR must not be matched to the local main branch")
	}
}

func TestRollupChecks(t *testing.T) {
	run := func(status, conclusion string) ghCheckResult {
		return ghCheckResult{Typename: "CheckRun", Status: status, Conclusion: conclusion}
	}
	ctx := func(state string) ghCheckResult { return ghCheckResult{Typename: "StatusContext", State: state} }
	cases := []struct {
		name   string
		checks []ghCheckResult
		want   string
	}{
		{"none", nil, ""},
		{"all pass", []ghCheckResult{run("COMPLETED", "SUCCESS"), run("COMPLETED", "SKIPPED"), ctx("SUCCESS")}, "pass"},
		{"one failing", []ghCheckResult{run("COMPLETED", "SUCCESS"), run("COMPLETED", "TIMED_OUT")}, "fail"},
		{"context error", []ghCheckResult{ctx("ERROR")}, "fail"},
		{"running", []ghCheckResult{run("COMPLETED", "SUCCESS"), run("IN_PROGRESS", "")}, "pending"},
		{"queued context", []ghCheckResult{ctx("PENDING")}, "pending"},
		{"failure beats pending", []ghCheckResult{run("QUEUED", ""), run("COMPLETED", "FAILURE")}, "fail"},
	}
	for _, tc := range cases {
		if got := rollupChecks(tc.checks); got != tc.want {
			t.Errorf("%s: rollupChecks() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIssueBranchName(t *testing.T) {
	cases := []struct {
		item ghWorkItem
		want string
	}{
		{ghWorkItem{Number: 2688, Title: "Fix duplicate user on signup when email has plus", Labels: []string{"bug"}}, "fix/2688-fix-duplicate-user-on-signup"},
		{ghWorkItem{Number: 7, Title: "Add dark mode!", Labels: []string{"enhancement"}}, "feat/7-add-dark-mode"},
		{ghWorkItem{Number: 9, Title: "Update README", Labels: []string{"Documentation"}}, "docs/9-update-readme"},
		{ghWorkItem{Number: 3, Title: "???"}, "feat/3"},
	}
	for _, tc := range cases {
		if got := issueBranchName(tc.item); got != tc.want {
			t.Errorf("issueBranchName(%q) = %q, want %q", tc.item.Title, got, tc.want)
		}
	}
}

func TestParseWorkItems(t *testing.T) {
	issues := []byte(`[{"number":5,"title":"Crash on start","labels":[{"name":"bug"}]}]`)
	prs := []byte(`[{"number":8,"title":"Add billing","headRefName":"feat/billing","isCrossRepository":false},{"number":6,"title":"From a fork","headRefName":"main","isCrossRepository":true}]`)
	items := parseWorkItems(issues, prs)
	if len(items) != 3 || items[0].Number != 8 || items[0].Kind != "pr" || items[0].Head != "feat/billing" {
		t.Fatalf("items = %+v", items)
	}
	if items[2].Kind != "issue" || items[2].BranchGuess != "fix/5-crash-on-start" {
		t.Fatalf("issue item = %+v", items[2])
	}
	if !items[1].CrossRepo {
		t.Fatalf("fork PR not flagged: %+v", items[1])
	}
}
