---
sidebar_position: 5
---

# AI Agents

Sprout runs AI coding agents in dedicated tmux windows within each worktree's session. The [TUI](/docs/tui) keeps the worktree list, agent status, and git diff review together while tmux remains the full interactive workspace.

Use `sprout agent attach` or `sprout go` when you want the full live tmux workspace. Use the TUI when you want to review several agent sessions on one repo from a single board.

## Supported agents

- **Codex**: `npm install -g @openai/codex`
- **Aider**: `pip install aider-chat`
- **Claude Code**: see [claude.ai/code](https://claude.ai/code)
- **Gemini**: see Google's Gemini CLI docs
- **Custom**: any CLI tool you configure

## Configuration

```toml
default_agent_type = "claude"
auto_start_agent = true

agent_command_codex = "codex"
agent_command_aider = "aider --model gpt-4"
agent_command_claude = "claude"
agent_command_gemini = "gemini"
```

## Usage

Agents start automatically when `auto_start_agent = true`. To control them manually:

```bash
sprout agent start feat/my-feature
sprout agent attach feat/my-feature
sprout agent stop feat/my-feature
```

Agents stop automatically when you remove a worktree.

## Claude Code skills

Sprout ships skills that create a worktree from a GitHub issue and generate your `.sprout.toml`. Install them with `sprout skills install`. See [Agent skills](/docs/skills).

## TUI agent status

Each running agent shows its state next to its worktree in the [TUI](/docs/tui): `ready`, `busy` or `needs input`. Sprout works this out by reading the agent's tmux pane, for every running agent at once, so you can see which ones are waiting for you without opening them.

- `enter` or `g`: attach to the worktree tmux session
- `p`: promote the selected worktree to preview
- `]`: switch the fourth pane to the **Agent** tab, a live read-only view of the selected agent's pane

Agent interaction itself still happens in tmux. The TUI never types into the pane or resizes it.

## API keys

Set the relevant key for your agent:

```bash
export OPENAI_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

Add to `~/.zshrc` (or equivalent) to persist.
