# sprout CLI app

`sprout` is a Go-based git worktree manager with a terminal UI.

## Build

From repo root:

```bash
go build -o /usr/local/bin/sprout ./apps/sprout/cmd/sprout
```

From `apps/sprout`:

```bash
go build -o /usr/local/bin/sprout ./cmd/sprout
```

For development:

```bash
./bin/sprout
```

## Test

From `apps/sprout`:

```bash
go test ./...
```

## Commands

- `sprout` or `sprout ui`
- `sprout new <type> <name> [--from <base>] [--no-launch]`
- `sprout list [--json]`
- `sprout go <branch-or-worktree> [--attach] [--no-launch]`
- `sprout path <branch-or-worktree>`
- `sprout launch <branch-or-worktree> [--no-attach]`
- `sprout detach <branch-or-worktree>`
- `sprout agent <start|stop|attach> <branch-or-worktree>`
- `sprout rm <branch-or-worktree> [--delete-branch] [--force]`
- `sprout doctor`
- `sprout shell-hook <zsh|bash|fish>`
- `sprout version`

## Config

Config file: `~/.config/sprout/config.toml`

```toml
base_branch = "dev"
worktree_root_template = "../{repo}.worktrees"
auto_launch = true
auto_start_agent = true
session_tools = ["agent", "lazygit", "nvim"]
agent_command = "codex"
session_prefix = "sprout"
```

## Shell completion

Zsh completion file:

```bash
cp apps/sprout/completions/sprout.zsh ~/.zsh/completions/_sprout
```

