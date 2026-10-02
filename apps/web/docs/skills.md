---
sidebar_position: 6
---

# Agent skills

Sprout ships two [Claude Code](https://claude.ai/code) skills that drive sprout for you. They are bundled in the sprout binary, and the source is in the [`apps/sprout/skills`](https://github.com/joegrabski/sprout/tree/main/apps/sprout/skills) directory of the repo.

## Install

```bash
sprout skills install
```

This copies both skills into `~/.claude/skills`. Pass skill names to install just those, `--dir` to install somewhere else (for example a project's `.claude/skills`), and `--force` to overwrite skills that are already installed.

The skills ship inside the sprout binary, so refresh them after you upgrade sprout:

```bash
brew upgrade sprout
sprout skills install --force
```

A skill that is a symlink (for example one linked to a working checkout) is never overwritten.

### With the skills CLI

The same skills can be installed with the [skills CLI](https://www.npmjs.com/package/skills), which is handy if you do not have sprout on that machine or want a project-level install:

```bash
npx skills add joegrabski/sprout
```

Useful flags:

```bash
# Install one skill, for Claude Code, for every project (user level)
npx skills add joegrabski/sprout \
  --skill sprout-issue --agent claude-code --global

# See what is available without installing
npx skills add joegrabski/sprout --list
```

Keep them current with `npx skills update` and remove one with `npx skills remove sprout-issue`. Both skills are written for Claude Code, so installing them for other agents will not give you a working skill.

## sprout-issue

Turns a GitHub issue into a worktree with an agent already working on it.

```text
/sprout-issue 123
/sprout-issue https://github.com/you/repo/issues/123
```

It fetches the issue, picks a branch name that follows your repo's naming rules, creates the branch off the latest base branch, runs `sprout new` (including your bootstrap steps), and starts a new Claude Code agent in the worktree's tmux session. The agent opens in plan mode with the issue and its comments as its first message, so you review the plan before anything is edited. It never pushes or opens a PR.

Your current terminal view does not move. Jump in with `sprout go <branch> --attach`.

Requires the `gh` CLI (authenticated), `tmux` and `sprout` on your `PATH`.

## sprout-init

Generates a `.sprout.toml` for the current repository by reading how it actually runs: launch settings, package scripts, Docker Compose files, and gitignored local config.

```text
/sprout-init
```

It sets up preview services, bootstrap symlinks and install steps, and (if your frontend reaches the backend through a tunnel) ngrok URL syncing. It merges into an existing `.sprout.toml` rather than overwriting it, and validates the result with `sprout preview status`.
