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
| `update_check` | bool | `true` | `SPROUT_UPDATE_CHECK` | Check GitHub for updates once per day |
| `launch_nvim` | bool | `true` | `SPROUT_LAUNCH_NVIM` | Launch Neovim in tmux session |
| `launch_lazygit` | bool | `true` | `SPROUT_LAUNCH_LAZYGIT` | Launch Lazygit in tmux session |
| `agent_command` | string | `codex` | `SPROUT_AGENT_COMMAND` | Default agent command (deprecated: use default_agent_type) |
| `default_agent_type` | string | `codex` | `SPROUT_DEFAULT_AGENT_TYPE` | Default AI agent type (codex, aider, claude, gemini) |
| `session_prefix` | string | `sprout` | `SPROUT_SESSION_PREFIX` | Prefix for tmux session names |
| `agent_command_*` | string | `varies` | `SPROUT_AGENT_COMMAND_*` | Custom command for specific agent type (* = agent type) |
| `layout_<repo>_win_<name>_pane_<idx>` | string | `-` | `` | Custom multi-pane tmux window configuration |
| `[[windows]]` | table array | `-` | `` | Structured tmux window layout for child worktree sessions |
| `preview_session_suffix` | string | `preview` | `SPROUT_PREVIEW_SESSION_SUFFIX` | Suffix for the dedicated preview tmux session name |
| `preview_command_prefix` | string | `` | `SPROUT_PREVIEW_COMMAND_PREFIX` | Optional prefix wrapped around each preview service command (e.g. "portless run") |
| `preview_auto_attach` | bool | `false` | `SPROUT_PREVIEW_AUTO_ATTACH` | Attach to the preview session after promoting (default: run detached) |
| `[[preview_windows]]` | table array | `-` | `` | Long-running services run from the preview worktree |
| `preview_tunnel_api` | string | `http://127.0.0.1:4040/api/tunnels` | `SPROUT_PREVIEW_TUNNEL_API` | ngrok agent local API used by [[preview_sync]] to resolve tunnel URLs |
| `[[preview_sync]]` | table array | `-` | `` | Config files kept in sync with live tunnel URLs on each promote |
| `bootstrap_links` | array | `[]` | `SPROUT_BOOTSTRAP_LINKS` | Gitignored paths to symlink from the main worktree into each new worktree |
| `[[bootstrap]]` | table array | `-` | `` | Setup commands (dir + run) to run when bootstrapping a worktree |


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

# Check for updates (disable with SPROUT_UPDATE_CHECK=0)
update_check = true

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
export SPROUT_UPDATE_CHECK="true"
export SPROUT_LAUNCH_NVIM="true"
export SPROUT_LAUNCH_LAZYGIT="true"
export SPROUT_AGENT_COMMAND="codex"
export SPROUT_DEFAULT_AGENT_TYPE="codex"
export SPROUT_SESSION_PREFIX="sprout"
export SPROUT_AGENT_COMMAND_*="varies"
export SPROUT_PREVIEW_SESSION_SUFFIX="preview"
export SPROUT_PREVIEW_COMMAND_PREFIX=""
export SPROUT_PREVIEW_AUTO_ATTACH="false"
export SPROUT_PREVIEW_TUNNEL_API="http://127.0.0.1:4040/api/tunnels"
export SPROUT_BOOTSTRAP_LINKS="[]"
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

### update_check

When `true`, Sprout checks GitHub for updates once per day. Disable by setting `SPROUT_UPDATE_CHECK=0`.

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

Prefix for tmux session names. Worktree sessions are named from a stable worktree-path token (for example, `{prefix}-{repo}-{worktree}`) so switching branches inside the same worktree does not change the tmux session name.

### agent_command_*

Custom commands for different AI agent types. Replace `*` with the agent type (e.g., `agent_command_codex`).

Examples:
- `agent_command_codex = "codex"`
- `agent_command_aider = "aider --model gpt-4"`
- `agent_command_claude = "claude-code"`

## Structured tmux windows

Use structured tmux window definitions when you want Sprout to launch specific windows and panes instead of the default tool-based layout.

- `[[windows]]` configures child worktree sessions.
- Set `role = "agent"` on a child window when that window should be treated as the agent window for attach.

```toml
[[windows]]
name = "editor"
role = "agent"
layout = "main-vertical"

  [[windows.panes]]
  run = "nvim ."

  [[windows.panes]]
  run = "pnpm dev"
```

## Preview worktree

Designate one worktree at a time as the **preview**: a dedicated tmux session (`{prefix}-{repo}-{preview_session_suffix}`) runs a configured set of long-running services from that worktree. Promoting another worktree tears the session down and rebuilds it pointed at the new worktree path, so you can validate an agent's branch by running your real stack against it.

The current preview is recorded in a state file in the repo's git common dir, so every worktree of the repo agrees on which one is the preview.

- `[[preview_windows]]` defines the services (same window/pane shape as `[[windows]]`, plus an optional `url` per service).
- `{worktree}` in a pane `dir` is replaced with the preview worktree path.
- `preview_command_prefix` optionally wraps each service command (e.g. a runner or profiler prefix). A pane whose `dir` resolves the same for every worktree (e.g. a fixed `~`) is left running across promotions instead of being restarted — use this for a shared tunnel agent like `ngrok start --all` so its URLs stay stable while you switch which worktree the code runs from.

Preview panes run under your login+interactive shell (`zsh`/`bash`), so `~/.zshrc` / `~/.zprofile` are sourced and tools installed via version managers (`mise`, `asdf`, `nvm`) or Homebrew are on `PATH`.

### Keeping app configs in sync (`[[preview_sync]]`)

When a frontend (e.g. a mobile app) reaches your backend through a tunnel, its config file holds the tunnel's public URL — and that URL changes whenever the tunnel restarts. `[[preview_sync]]` rewrites those values automatically after each promote by reading the live URLs from the tunnel agent's local API (`preview_tunnel_api`, an [ngrok](https://ngrok.com)-compatible `/api/tunnels` endpoint by default).

- `file` is the JSON file to update (supports `{worktree}`).
- Each `[[preview_sync.set]]` writes one dotted `path` (e.g. `api.baseUrl`) from a named `tunnel`, optionally through a `template` where `{url}` is the tunnel URL.
- The file is only rewritten when a value actually changes; `reload_windows` names preview windows to restart in that case (e.g. a bundler that must reload to pick up the new URL). On a plain feature switch the URLs are unchanged, so nothing is rewritten and nothing reloads — no rebuild.

Commands:

```bash
sprout preview              # show current preview + service URLs
sprout preview <target>     # promote a worktree to preview
sprout preview stop         # stop the preview session
sprout preview restart      # restart services from the current preview worktree
sprout preview sync         # rewrite [[preview_sync]] configs from live tunnel URLs
sprout preview logs <name>  # show recent output of a preview service
```

In the TUI, press `p` to promote the selected worktree; the current preview is marked with `▶`.

```toml
[[preview_windows]]
name = "tunnel"                     # shared tunnel agent, left running across promotions

  [[preview_windows.panes]]
  dir = "~"                         # fixed dir → not restarted when the preview worktree changes
  run = "ngrok start --all"

[[preview_windows]]
name = "api"
url = "http://localhost:8080"

  [[preview_windows.panes]]
  dir = "{worktree}"
  run = "make dev-api"

[[preview_windows]]
name = "web"

  [[preview_windows.panes]]
  dir = "{worktree}/web"
  run = "npm run dev"

# Rewrite the app's endpoint config from the live tunnel URLs on promote.
[[preview_sync]]
file = "{worktree}/web/config.json"
reload_windows = ["web"]           # restart the dev server only when a URL actually changed

  [[preview_sync.set]]
  path = "api.baseUrl"
  tunnel = "api"

  [[preview_sync.set]]
  path = "api.wsUrl"
  tunnel = "api"
  template = "{url}/socket"
```

## Bootstrapping new worktrees

Fresh git worktrees do not contain gitignored local state — `node_modules`, local config files, dev certs, cached data — so apps fail to start until the worktree is prepared. Sprout no longer copies untracked files into new worktrees; instead it bootstraps them:

- `bootstrap_links` is a list of gitignored paths to **symlink** from the main worktree into the new one. Use this for small, identical-everywhere local config (e.g. `.env`, `config.local.json`, `appsettings.Local.json`) and for heavy shared local state (local DB/storage dirs). Do **not** link `node_modules` — it is per-branch build state and sharing it corrupts Yarn's state file.
- `[[bootstrap]]` steps are commands (`dir` + `run`, same shape as panes) run in order to install dependencies, e.g. `yarn install` or `dotnet restore`. `{worktree}` in `dir` resolves to the worktree being bootstrapped.

Bootstrap runs automatically on `sprout new` (skip with `--no-bootstrap`). Run `sprout bootstrap [target]` to (re)bootstrap an existing worktree — handy for worktrees created before bootstrap was configured.

```toml
bootstrap_links = [
  "web/config.local.json",
  "api/appsettings.Local.json",
]

[[bootstrap]]
dir = "{worktree}/web"
run = "npm install"

[[bootstrap]]
dir = "{worktree}/api"
run = "dotnet restore"
```
