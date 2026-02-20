package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

type Command struct {
	Name        string
	Usage       string
	Description string
	HelpText    string
}

const docTemplate = `---
sidebar_position: 1
---

# CLI Commands Reference

Complete reference for all Sprout CLI commands.

## Overview

Sprout provides a comprehensive set of commands for managing git worktrees. You can either use the interactive TUI or individual commands for scripting and automation.

{{ range .Commands }}
## {{ .Name }}

**Usage:** {{ backtick }}{{ .Usage }}{{ backtick }}

{{ .Description }}

{{ if .HelpText }}
{{ backtick }}{{ backtick }}{{ backtick }}
{{ .HelpText }}
{{ backtick }}{{ backtick }}{{ backtick }}
{{ end }}

{{ end }}
`

func main() {
	// Find the sprout binary
	sproutBinary := findSproutBinary()
	if sproutBinary == "" {
		fmt.Fprintln(os.Stderr, "error: sprout binary not found. Please build it first with: cd ../sprout && go build -o ../../sprout ./cmd/sprout")
		os.Exit(1)
	}

	commands := []Command{}

	// Parse help text for each command
	for _, cmd := range []string{"ui", "new", "list", "go", "path", "launch", "detach", "agent", "rm", "doctor", "shell-hook"} {
		helpText, usage, description := getCommandHelp(sproutBinary, cmd)
		commands = append(commands, Command{
			Name:        cmd,
			Usage:       usage,
			Description: description,
			HelpText:    helpText,
		})
	}

	// Generate markdown
	tmpl := template.Must(template.New("doc").Funcs(template.FuncMap{
		"backtick": func() string { return "`" },
	}).Parse(docTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"Commands": commands,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error generating docs: %v\n", err)
		os.Exit(1)
	}

	// Write to docs
	outputPath := filepath.Join("..", "docs", "cli", "commands.md")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating docs directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing docs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated CLI documentation: %s\n", outputPath)
}

func findSproutBinary() string {
	// Try relative path first
	paths := []string{
		"../../sprout",
		"../../../sprout",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try PATH
	if path, err := exec.LookPath("sprout"); err == nil {
		return path
	}

	return ""
}

func getCommandHelp(binary, cmd string) (helpText, usage, description string) {
	// Special handling for different commands
	switch cmd {
	case "ui":
		usage = "sprout ui"
		description = "Launch the interactive TUI for managing worktrees."
		helpText = "The UI command launches an interactive terminal user interface where you can:\n- View all worktrees\n- Create new worktrees\n- Launch tmux sessions\n- Start/stop AI agents\n- Remove worktrees\n\nPrimary Hotkeys:\n- Enter / g : Attach to worktree session\n- d         : Detach from session\n- x         : Remove worktree (confirmation modal)\n- n         : Create new worktree\n- /         : Filter worktree list\n- r         : Refresh state\n- ?         : Open contextual help\n- q         : Quit"
	case "new":
		usage = "sprout new <type> <name> [--from <base>] [--from-branch <branch>] [--no-launch]"
		description = "Create a new worktree."
		helpText = `Creates a new git worktree and branch.

Arguments:
  <type>  Branch type prefix (e.g., feat, fix, chore)
  <name>  Branch name (spaces allowed)

Flags:
  --from <base>           Base branch to create from (default: config.base_branch)
  --from-branch <branch>  Existing local or remote branch to create worktree from
  --no-launch             Don't auto-launch tmux session

Examples:
  sprout new feat checkout-redesign
  sprout new fix urgent-bug --from main
  sprout new --from-branch feat/existing-branch`
	case "list":
		usage = "sprout list [--json]"
		description = "List all worktrees with their status."
		helpText = `Lists all git worktrees with their current status.

Flags:
  --json  Output as JSON

Output columns:
  CUR     - * if current worktree
  BRANCH  - Branch name
  STATUS  - clean or dirty
  TMUX    - Tmux session state (active, inactive, or -)
  AGENT   - AI agent state (active, inactive, or -)
  PATH    - Worktree path`
	case "go":
		usage = "sprout go <branch-or-worktree> [--attach] [--no-launch]"
		description = "Switch to a worktree (optionally launching or attaching to tmux)."
		helpText = `Navigate to a worktree and optionally manage tmux session.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --attach      Attach to existing tmux session if running
  --no-launch   Don't launch tmux session if not running

Examples:
  sprout go feat/checkout-redesign
  sprout go main --attach`
	case "path":
		usage = "sprout path <branch-or-worktree>"
		description = "Print the absolute path to a worktree."
		helpText = `Outputs the absolute path to a worktree. Useful for scripting.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Examples:
  cd $(sprout path feat/checkout)
  code $(sprout path main)`
	case "launch":
		usage = "sprout launch <branch-or-worktree> [--no-attach]"
		description = "Launch a tmux session for a worktree."
		helpText = `Creates and optionally attaches to a tmux session for a worktree.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --no-attach  Launch session without attaching

The tmux session includes:
- Neovim (if launch_nvim is enabled)
- Lazygit (if launch_lazygit is enabled)
- Shell in worktree directory`
	case "detach":
		usage = "sprout detach <branch-or-worktree>"
		description = "Detach from and kill the tmux session for a worktree."
		helpText = `Kills the tmux session associated with a worktree.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Note: This does not remove the worktree itself, only stops the tmux session.`
	case "agent":
		usage = "sprout agent <start|stop|attach> <branch-or-worktree>"
		description = "Manage AI coding agents for a worktree."
		helpText = `Start, stop, or attach to AI coding agents.

Subcommands:
  start   - Start an agent in a new tmux window
  stop    - Stop the agent tmux window
  attach  - Attach to running agent window

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Supported agents (via config):
  - codex   (default)
  - aider
  - claude
  - gemini

Examples:
  sprout agent start feat/new-feature
  sprout agent attach main
  sprout agent stop feat/new-feature`
	case "rm":
		usage = "sprout rm <branch-or-worktree> [--delete-branch] [--force]"
		description = "Remove a worktree (and optionally its branch)."
		helpText = `Removes a git worktree and optionally deletes the branch.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --delete-branch  Also delete the git branch
  --force          Force removal even if worktree is dirty

Warning: This will stop any running tmux sessions and agents.

Examples:
  sprout rm feat/old-feature
  sprout rm fix/bug --delete-branch
  sprout rm dirty-worktree --force`
	case "doctor":
		usage = "sprout doctor"
		description = "Check system dependencies and configuration."
		helpText = `Runs diagnostics to verify sprout's environment.

Checks:
  - Git installation and version
  - Tmux installation and version
  - Configured agent commands availability
  - Git repository detection
  - Configuration file validity

Exit codes:
  0 - All checks passed
  1 - One or more checks failed`
	case "shell-hook":
		usage = "sprout shell-hook <zsh|bash|fish>"
		description = "Output shell integration code for auto-cd functionality."
		helpText = `Generates shell integration code for your shell.

Arguments:
  <shell>  Shell type (zsh, bash, or fish)

The shell hook enables automatic directory changing when using sprout commands.

Installation:
  # For Zsh (add to ~/.zshrc)
  eval "$(sprout shell-hook zsh)"

  # For Bash (add to ~/.bashrc)
  eval "$(sprout shell-hook bash)"

  # For Fish (add to ~/.config/fish/config.fish)
  sprout shell-hook fish | source`
	}

	return
}
