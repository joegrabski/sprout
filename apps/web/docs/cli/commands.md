---
sidebar_position: 1
---

# CLI Commands Reference

Complete reference for all Sprout CLI commands.

## Overview

Sprout provides a comprehensive set of commands for managing git worktrees. You can either use the interactive TUI or individual commands for scripting and automation.


## ui

**Usage:** `sprout ui`

Launch the interactive TUI for managing worktrees.


```
The UI command launches an interactive terminal user interface where you can:
- View all worktrees
- Create new worktrees
- Launch tmux sessions
- Start/stop AI agents
- Remove worktrees

Primary Hotkeys:
- Enter / g : Attach to worktree session
- d         : Detach from session
- x         : Remove worktree (confirmation modal)
- n         : Create new worktree
- /         : Filter worktree list
- r         : Refresh state
- ?         : Open contextual help
- q         : Quit
```



## new

**Usage:** `sprout new <type> <name> [--from <base>] [--from-branch <branch>] [--no-launch]`

Create a new worktree.


```
Creates a new git worktree and branch.

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
  sprout new --from-branch feat/existing-branch
```



## list

**Usage:** `sprout list [--json]`

List all worktrees with their status.


```
Lists all git worktrees with their current status.

Flags:
  --json  Output as JSON

Output columns:
  CUR     - * if current worktree
  BRANCH  - Branch name
  STATUS  - clean or dirty
  TMUX    - Tmux session state (active, inactive, or -)
  AGENT   - AI agent state (active, inactive, or -)
  PATH    - Worktree path
```



## go

**Usage:** `sprout go <branch-or-worktree> [--attach] [--no-launch]`

Switch to a worktree (optionally launching or attaching to tmux).


```
Navigate to a worktree and optionally manage tmux session.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --attach      Attach to existing tmux session if running
  --no-launch   Don't launch tmux session if not running

Examples:
  sprout go feat/checkout-redesign
  sprout go main --attach
```



## path

**Usage:** `sprout path <branch-or-worktree>`

Print the absolute path to a worktree.


```
Outputs the absolute path to a worktree. Useful for scripting.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Examples:
  cd $(sprout path feat/checkout)
  code $(sprout path main)
```



## launch

**Usage:** `sprout launch <branch-or-worktree> [--no-attach]`

Launch a tmux session for a worktree.


```
Creates and optionally attaches to a tmux session for a worktree.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --no-attach  Launch session without attaching

The tmux session includes:
- Neovim (if launch_nvim is enabled)
- Lazygit (if launch_lazygit is enabled)
- Shell in worktree directory
```



## detach

**Usage:** `sprout detach <branch-or-worktree>`

Detach from and kill the tmux session for a worktree.


```
Kills the tmux session associated with a worktree.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Note: This does not remove the worktree itself, only stops the tmux session.
```



## agent

**Usage:** `sprout agent <start|stop|attach> <branch-or-worktree>`

Manage AI coding agents for a worktree.


```
Start, stop, or attach to AI coding agents.

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
  sprout agent stop feat/new-feature
```



## rm

**Usage:** `sprout rm <branch-or-worktree> [--delete-branch] [--force]`

Remove a worktree (and optionally its branch).


```
Removes a git worktree and optionally deletes the branch.

Arguments:
  <branch-or-worktree>  Branch name or worktree path

Flags:
  --delete-branch  Also delete the git branch
  --force          Force removal even if worktree is dirty

Warning: This will stop any running tmux sessions and agents.

Examples:
  sprout rm feat/old-feature
  sprout rm fix/bug --delete-branch
  sprout rm dirty-worktree --force
```



## doctor

**Usage:** `sprout doctor`

Check system dependencies and configuration.


```
Runs diagnostics to verify sprout's environment.

Checks:
  - Git installation and version
  - Tmux installation and version
  - Configured agent commands availability
  - Git repository detection
  - Configuration file validity

Exit codes:
  0 - All checks passed
  1 - One or more checks failed
```



## shell-hook

**Usage:** `sprout shell-hook <zsh|bash|fish>`

Output shell integration code for auto-cd functionality.


```
Generates shell integration code for your shell.

Arguments:
  <shell>  Shell type (zsh, bash, or fish)

The shell hook enables automatic directory changing when using sprout commands.

Installation:
  # For Zsh (add to ~/.zshrc)
  brew tap joegrabski/sprout https://github.com/joegrabski/sprout
  brew install sprout
  eval "$(sprout shell-hook zsh)"

  # For Bash (add to ~/.bashrc)
  eval "$(sprout shell-hook bash)"

  # For Fish (add to ~/.config/fish/config.fish)
  sprout shell-hook fish | source
```



