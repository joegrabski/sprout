---
sidebar_position: 4
---

# CLI Reference

## `sprout` / `sprout ui`

Launch the interactive TUI.

## `sprout new`

```
sprout new <type> <name> [--from <base>] [--no-launch]
```

Create a worktree and branch. The branch name is `<type>/<name>`.

```bash
sprout new feat checkout-redesign
sprout new fix urgent-bug --from main
sprout new chore update-deps --no-launch
```

## `sprout go`

```
sprout go <branch> [--attach] [--no-launch]
```

Switch to a worktree. With shell integration, changes your shell's directory.

```bash
sprout go main
sprout go feat/checkout-redesign --attach
```

## `sprout list`

```
sprout list [--json]
```

List all worktrees with branch, status, tmux, and agent state.

## `sprout path`

```
sprout path <branch>
```

Print the absolute path to a worktree. Useful for scripting.

```bash
cd $(sprout path feat/checkout)
code $(sprout path main)
```

## `sprout launch`

```
sprout launch <branch> [--no-attach]
```

Start a tmux session for a worktree.

## `sprout detach`

```
sprout detach <branch>
```

Kill the tmux session for a worktree (worktree is not removed).

## `sprout agent`

```
sprout agent <start|stop|attach> <branch>
```

Manage AI agent windows.

```bash
sprout agent start feat/my-feature
sprout agent attach feat/my-feature
sprout agent stop feat/my-feature
```

## `sprout rm`

```
sprout rm <branch> [--delete-branch] [--force]
```

Remove a worktree. Stops any running sessions and agents.

```bash
sprout rm feat/old-feature
sprout rm fix/bug --delete-branch
sprout rm dirty-worktree --force
```

## `sprout doctor`

Check system dependencies and configuration. Exits `0` if all checks pass, `1` if any fail.

## `sprout shell-hook`

```
sprout shell-hook <zsh|bash|fish>
```

Output shell integration code. See [Installation](installation.md) for setup.
