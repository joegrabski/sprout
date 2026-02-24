---
sidebar_position: 5
---

# AI Agents

Sprout runs AI coding agents in dedicated tmux windows within each worktree's session. Each worktree gets its own agent with independent context. The TUI reads agent output directly from the tmux pane running your agent command.

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

## API keys

Set the relevant key for your agent:

```bash
export OPENAI_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

Add to `~/.zshrc` (or equivalent) to persist.
