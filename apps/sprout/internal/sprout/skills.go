package sprout

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SkillStatus is the outcome of installing one skill.
type SkillStatus int

const (
	// SkillInstalled means the skill was not present and has been copied.
	SkillInstalled SkillStatus = iota
	// SkillUpdated means an installed skill was replaced (force).
	SkillUpdated
	// SkillExists means the skill is already installed and was left alone.
	SkillExists
	// SkillSymlink means the destination is a symlink and was left alone.
	SkillSymlink
)

// SkillInstall reports what InstallSkills did for one skill.
type SkillInstall struct {
	Name   string
	Path   string
	Status SkillStatus
}

// InstallSkills copies skills from src (one directory per skill) into
// destRoot. With no names it installs every skill in src. An installed skill
// is only replaced when force is set, and a symlinked skill is never written
// through, so a skill linked to a working checkout is not overwritten.
func InstallSkills(src fs.FS, destRoot string, names []string, force bool) ([]SkillInstall, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	var available []string
	for _, e := range entries {
		if e.IsDir() {
			available = append(available, e.Name())
		}
	}
	if len(names) == 0 {
		names = available
	}
	for _, name := range names {
		if !slices.Contains(available, name) {
			return nil, fmt.Errorf("unknown skill %q (available: %s)", name, strings.Join(available, ", "))
		}
	}

	var results []SkillInstall
	for _, name := range names {
		dest := filepath.Join(destRoot, name)
		res := SkillInstall{Name: name, Path: dest}

		info, err := os.Lstat(dest)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return results, err
		}

		switch {
		case exists && info.Mode()&os.ModeSymlink != 0:
			res.Status = SkillSymlink
		case exists && !force:
			res.Status = SkillExists
		default:
			if exists {
				if err := os.RemoveAll(dest); err != nil {
					return results, err
				}
				res.Status = SkillUpdated
			}
			sub, err := fs.Sub(src, name)
			if err != nil {
				return results, err
			}
			if err := os.CopyFS(dest, sub); err != nil {
				return results, err
			}
		}
		results = append(results, res)
	}
	return results, nil
}
