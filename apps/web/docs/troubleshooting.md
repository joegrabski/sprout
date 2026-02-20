---
sidebar_position: 6
---

# Troubleshooting

Run `sprout doctor` first, it checks git, tmux, your config, and configured agents.

## Command not found

The binary isn't in PATH. Add its directory to `~/.zshrc`:

```bash
export PATH="/usr/local/bin:$PATH"
```

## Auto-cd not working

Verify the shell hook is loaded:

```bash
type sprout
# Should output: "sprout is a shell function"
```

If not, ensure your shell config includes `eval "$(sprout shell-hook zsh)"` and reload:

```bash
source ~/.zshrc
```

## Tmux session won't launch

Check tmux is installed and the server is running:

```bash
tmux ls
brew install tmux  # if not installed
```

Avoid running `sprout` from inside an existing tmux session.

## "Branch already checked out"

A branch can only be in one worktree at a time:

```bash
git worktree list   # find which worktree has it
sprout rm <old-worktree>
```

## Can't remove worktree

If you're inside the worktree directory, navigate out first:

```bash
cd ~
sprout rm feat/my-feature
```

For dirty worktrees, use `--force`:

```bash
sprout rm feat/my-feature --force
```

## Agent won't start

Check the agent CLI is installed and your API key is set:

```bash
sprout doctor
echo $OPENAI_API_KEY
echo $ANTHROPIC_API_KEY
```

## Config not loading

Check the file exists at `~/.config/sprout/config.toml`. To use a custom path:

```bash
export SPROUT_CONFIG=~/my-config.toml
```

Validate with `sprout doctor`.

## Getting help

1. Run `sprout doctor` and note the output
2. Check [GitHub Issues](https://github.com/joegrabski/sprout/issues)
3. File a new issue with your OS, shell, `sprout version`, and `sprout doctor` output
