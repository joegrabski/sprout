---
sidebar_position: 3
---

# Configuration

Config file: `~/.config/sprout/config.toml` (override with `SPROUT_CONFIG`).

## Options

| Option | Type | Default | Env | Description |
| --- | --- | --- | --- | --- |
| `base_branch` | string | `dev` | `SPROUT_BASE_BRANCH` | Default base branch for `sprout new` |
| `worktree_root_template` | string | `../{repo}.worktrees` | `SPROUT_WORKTREE_ROOT_TEMPLATE` | Worktree directory template |
| `auto_launch` | bool | `true` | `SPROUT_AUTO_LAUNCH` | Auto-launch tmux on new worktree |
| `auto_start_agent` | bool | `true` | `SPROUT_AUTO_START_AGENT` | Auto-start agent on new worktree |
| `session_tools` | array | `["agent","lazygit","nvim"]` | `SPROUT_SESSION_TOOLS` | Ordered tmux windows per session |
| `agent_command` | string | `codex` | `SPROUT_AGENT_COMMAND` | Agent command |
| `default_agent_type` | string | `codex` | `SPROUT_DEFAULT_AGENT_TYPE` | Default agent type |
| `agent_command_*` | string | varies | `SPROUT_AGENT_COMMAND_*` | Agent command per type |
| `session_prefix` | string | `sprout` | `SPROUT_SESSION_PREFIX` | Tmux session name prefix |

## Example

```toml
base_branch = "main"
worktree_root_template = "../{repo}.worktrees"
auto_launch = true
auto_start_agent = true
session_tools = ["agent", "lazygit", "nvim", "pnpm dev"]
default_agent_type = "codex"
agent_command_codex = "codex"
agent_command_aider = "aider --model gpt-4"
session_prefix = "sprout"
```

## `session_tools`

Built-in values:
- `agent` — runs `agent_command`
- `lazygit` — runs `lazygit -p .`
- `nvim` — runs `nvim .`

Any other value is run as a shell command in its own tmux window (e.g. `"pnpm dev"`).

## Environment variables

`SPROUT_SESSION_TOOLS` accepts comma-separated or TOML-array syntax:

```bash
export SPROUT_SESSION_TOOLS="agent,lazygit,nvim"
# or
export SPROUT_SESSION_TOOLS='["agent","lazygit","nvim","pnpm dev"]'
```
