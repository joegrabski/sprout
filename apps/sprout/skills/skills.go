// Package skills embeds the Claude Code skills that ship with sprout.
//
// Each subdirectory is one skill (a SKILL.md plus any supporting files). The
// directory is also what `npx skills add joegrabski/sprout` discovers.
package skills

import "embed"

// FS holds one directory per bundled skill.
//
//go:embed sprout-*
var FS embed.FS
