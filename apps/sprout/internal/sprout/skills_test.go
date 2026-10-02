package sprout

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"sprout/skills"
)

func testSkillsFS() fstest.MapFS {
	return fstest.MapFS{
		"alpha/SKILL.md":      {Data: []byte("alpha skill")},
		"alpha/refs/guide.md": {Data: []byte("alpha guide")},
		"beta/SKILL.md":       {Data: []byte("beta skill")},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestInstallSkillsInstallsAll(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "claude", "skills")

	results, err := InstallSkills(testSkillsFS(), dest, nil, false)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != SkillInstalled {
			t.Errorf("%s status = %v, want installed", r.Name, r.Status)
		}
	}
	if got := readFile(t, filepath.Join(dest, "alpha", "refs", "guide.md")); got != "alpha guide" {
		t.Errorf("nested file = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "beta", "SKILL.md")); got != "beta skill" {
		t.Errorf("beta SKILL.md = %q", got)
	}
}

func TestInstallSkillsByName(t *testing.T) {
	dest := t.TempDir()

	results, err := InstallSkills(testSkillsFS(), dest, []string{"beta"}, false)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(results) != 1 || results[0].Name != "beta" {
		t.Fatalf("results = %+v, want only beta", results)
	}
	if _, err := os.Stat(filepath.Join(dest, "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha should not be installed, stat err = %v", err)
	}
}

func TestInstallSkillsUnknownName(t *testing.T) {
	_, err := InstallSkills(testSkillsFS(), t.TempDir(), []string{"nope"}, false)
	if err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "alpha, beta") {
		t.Errorf("error should name the skill and list the available ones, got %q", err)
	}
}

func TestInstallSkillsExistingNeedsForce(t *testing.T) {
	dest := t.TempDir()
	skillDir := filepath.Join(dest, "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "stale.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := InstallSkills(testSkillsFS(), dest, []string{"alpha"}, false)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if results[0].Status != SkillExists {
		t.Errorf("status = %v, want exists", results[0].Status)
	}
	if got := readFile(t, filepath.Join(skillDir, "SKILL.md")); got != "local edit" {
		t.Errorf("existing skill was modified without force: %q", got)
	}

	results, err = InstallSkills(testSkillsFS(), dest, []string{"alpha"}, true)
	if err != nil {
		t.Fatalf("InstallSkills force: %v", err)
	}
	if results[0].Status != SkillUpdated {
		t.Errorf("status = %v, want updated", results[0].Status)
	}
	if got := readFile(t, filepath.Join(skillDir, "SKILL.md")); got != "alpha skill" {
		t.Errorf("SKILL.md after force = %q", got)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "stale.md")); !os.IsNotExist(err) {
		t.Errorf("stale file should be removed by force, stat err = %v", err)
	}
}

func TestInstallSkillsLeavesSymlinkAlone(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout", "alpha")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "SKILL.md"), []byte("working copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "skills")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(checkout, filepath.Join(dest, "alpha")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	results, err := InstallSkills(testSkillsFS(), dest, []string{"alpha"}, true)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if results[0].Status != SkillSymlink {
		t.Errorf("status = %v, want symlink", results[0].Status)
	}
	if got := readFile(t, filepath.Join(checkout, "SKILL.md")); got != "working copy" {
		t.Errorf("symlink target was overwritten: %q", got)
	}
}

func TestEmbeddedSkillsAreValid(t *testing.T) {
	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		t.Fatalf("read embedded skills: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills are embedded")
	}
	for _, e := range entries {
		data, err := fs.ReadFile(skills.FS, e.Name()+"/SKILL.md")
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if !strings.HasPrefix(string(data), "---\n") || !strings.Contains(string(data), "\nname: "+e.Name()+"\n") {
			t.Errorf("%s/SKILL.md frontmatter should start with --- and declare name: %s", e.Name(), e.Name())
		}
	}
}
