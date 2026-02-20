---
sidebar_position: 2
---

# Configuration Reference

Complete reference for all Sprout configuration options.

## Configuration Files

Sprout loads configuration in the following order, with each layer overriding the previous:

1. **Global config**: `~/.config/sprout/config.toml` (or `$SPROUT_CONFIG` if set)
2. **Repo config**: `.sprout.toml` at the root of the current git repository
3. **Environment variables**: highest priority, override everything

The repo config only needs to contain the keys you want to override. Everything else falls back to the global config.

### Example repo config

```toml
# .sprout.toml (committed to the repo)
base_branch = "main"
default_agent_type = "claude"
auto_start_agent = false
```

## Configuration Options

| Option | Type | Default | Environment Variable | Description |
|--------|------|---------|---------------------|-------------|
| `base_branch` | string | `main` | `SPROUT_BASE_BRANCH` | Default base branch for new worktrees |
| `worktree_root_template` | string | `../\{repo\}.worktrees` | `SPROUT_WORKTREE_ROOT_TEMPLATE` | Template for worktree root directory (\{repo\} is replaced with repo name) |
| `auto_launch` | bool | `true` | `SPROUT_AUTO_LAUNCH` | Automatically launch tmux session when creating worktrees |
| `auto_start_agent` | bool | `true` | `SPROUT_AUTO_START_AGENT` | Automatically start AI agent when creating worktrees |
| `launch_nvim` | bool | `true` | `SPROUT_LAUNCH_NVIM` | Launch Neovim in tmux session |
| `launch_lazygit` | bool | `true` | `SPROUT_LAUNCH_LAZYGIT` | Launch Lazygit in tmux session |
| `agent_command` | string | `codex` | `SPROUT_AGENT_COMMAND` | Default agent command (deprecated: use default_agent_type) |
| `default_agent_type` | string | `codex` | `SPROUT_DEFAULT_AGENT_TYPE` | Default AI agent type (codex, aider, claude, gemini) |
| `session_prefix` | string | `sprout` | `SPROUT_SESSION_PREFIX` | Prefix for tmux session names |
| `agent_command_*` | string | `varies` | `SPROUT_AGENT_COMMAND_*` | Custom command for specific agent type (* = agent type) |
| `layout_<repo>_win_<name>_pane_<idx>` | string | `-` | `-` | Custom multi-pane tmux window configuration |


## Example Configuration

```toml
# ~/.config/sprout/config.toml

# Base branch for new worktrees
base_branch = "main"

# Template for worktree root directory
# {repo} is replaced with repository name
worktree_root_template = "../{repo}.worktrees"

# Automatically launch tmux when creating new worktrees
auto_launch = true

# Automatically start AI agent when creating worktrees
auto_start_agent = true

# Launch nvim in tmux session
launch_nvim = true

# Launch lazygit in tmux session
launch_lazygit = true

# Default agent command (deprecated, use default_agent_type)
agent_command = "codex"

# Default agent type to use
default_agent_type = "codex"

# Tmux session prefix
session_prefix = "sprout"

# Agent commands by type
agent_command_codex = "codex"
agent_command_aider = "aider"
agent_command_claude = "claude"
agent_command_gemini = "gemini"
```

## Environment Variable Overrides

All configuration options can be overridden with environment variables:

```bash
export SPROUT_BASE_BRANCH="main"
export SPROUT_WORKTREE_ROOT_TEMPLATE="../\{repo\}.worktrees"
export SPROUT_AUTO_LAUNCH="true"
export SPROUT_AUTO_START_AGENT="true"
export SPROUT_LAUNCH_NVIM="true"
export SPROUT_LAUNCH_LAZYGIT="true"
export SPROUT_AGENT_COMMAND="codex"
export SPROUT_DEFAULT_AGENT_TYPE="codex"
export SPROUT_SESSION_PREFIX="sprout"
export SPROUT_AGENT_COMMAND_*="varies"
export -="-"
```

## Configuration Details

### base_branch

The default branch to use as the base when creating new worktrees. This is typically your main development branch (e.g., `main`, `dev`, `develop`).

### worktree_root_template

Template for the directory where worktrees will be created. The `{repo}` placeholder is replaced with the repository name.

For example, if your repo is `/home/user/myproject` and the template is `../{repo}.worktrees`, worktrees will be created in `/home/user/myproject.worktrees/`.

### auto_launch

When `true`, automatically creates and attaches to a tmux session when creating a new worktree with `sprout new`.

### auto_start_agent

When `true`, automatically starts an AI agent in a tmux window when creating a new worktree.

### launch_nvim

When `true`, opens Neovim in a tmux pane when launching a session.

### launch_lazygit

When `true`, opens Lazygit in a tmux pane when launching a session.

### agent_command

**Deprecated:** Use `default_agent_type` instead.

The command to run for starting an AI agent.

### default_agent_type

The default AI agent to use. Must match one of the agent types defined in `agent_command_*` options.

Supported values: `codex`, `aider`, `claude`, `gemini`

### session_prefix

Prefix for tmux session names. Sessions will be named `{prefix}-{branch}`.

### agent_command_*

Custom commands for different AI agent types. Replace `*` with the agent type (e.g., `agent_command_codex`).

Examples:
- `agent_command_codex = "codex"`
- `agent_command_aider = "aider --model gpt-4"`
- `agent_command_claude = "claude-code"`
