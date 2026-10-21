package sprout

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestListBranchesSkipsRemoteHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", origin)
	repo := filepath.Join(root, "repo")
	gitT(t, root, "init", "-q", "-b", "main", repo)
	writeT(t, repo, "a.txt", "a\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-qm", "init")
	gitT(t, repo, "remote", "add", "origin", origin)
	gitT(t, repo, "push", "-q", "origin", "main")
	gitT(t, repo, "branch", "only-remote")
	gitT(t, repo, "push", "-q", "origin", "only-remote")
	gitT(t, repo, "branch", "-D", "only-remote")
	gitT(t, repo, "remote", "set-head", "origin", "main") // creates refs/remotes/origin/HEAD

	m := NewManager(DefaultConfig())
	branches, err := m.ListBranches(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "origin" || strings.Contains(b.Name, "HEAD") {
			t.Errorf("origin/HEAD leaked into the branch list: %+v", b)
		}
	}
	found := false
	for _, b := range branches {
		if b.Name == "only-remote" && b.Remote {
			found = true
		}
	}
	if !found {
		t.Errorf("remote-only branch missing from %+v", branches)
	}
}
