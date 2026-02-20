---
sidebar_position: 1
---

# Introduction

Sprout is a TUI for managing git worktrees with tmux sessions and AI coding agents. Work on multiple branches simultaneously without stashing or losing context.

```bash
# Create a worktree + branch
sprout new feat checkout-redesign

# Switch to it
sprout go feat/checkout-redesign

# Or use the interactive TUI
sprout
```

Each worktree gets its own tmux session with your editor, lazygit, and an AI agent, all isolated per branch.

## Quick start

**1. Create a worktree**

```bash
sprout new feat checkout-redesign
```

Creates `feat/checkout-redesign`, sets up the worktree, launches tmux, and starts your agent.

**2. Switch worktrees**

```bash
sprout go main
sprout go feat/checkout-redesign
```

**3. View worktrees**

```bash
sprout list
```

**4. Manage sessions**

```bash
sprout launch feat/checkout-redesign
sprout detach feat/checkout-redesign
```

**5. Manage agents**

```bash
sprout agent start feat/checkout-redesign
sprout agent attach feat/checkout-redesign
sprout agent stop feat/checkout-redesign
```

**6. Remove a worktree**

```bash
sprout rm feat/checkout-redesign
sprout rm feat/checkout-redesign --delete-branch
```

## Recommended config

```toml
base_branch = "main"
auto_launch = true
auto_start_agent = true
session_tools = ["agent", "lazygit", "nvim"]
```
