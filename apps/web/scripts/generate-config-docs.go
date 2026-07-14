package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type ConfigOption struct {
	Name        string
	Type        string
	Default     string
	EnvVar      string
	Description string
}

const configDocTemplate = `---
sidebar_position: 2
---

# Configuration Reference

Complete reference for all Sprout configuration options.

## Configuration Files

Sprout loads configuration in the following order, with each layer overriding the previous:

1. **Global config**: {{ backtick }}~/.config/sprout/config.toml{{ backtick }} (or {{ backtick }}$SPROUT_CONFIG{{ backtick }} if set)
2. **Repo config**: {{ backtick }}.sprout.toml{{ backtick }} at the root of the current git repository
3. **Environment variables**: highest priority, override everything

The repo config only needs to contain the keys you want to override. Everything else falls back to the global config.

### Example repo config

{{ backtick }}{{ backtick }}{{ backtick }}toml
# .sprout.toml (committed to the repo)
base_branch = "main"
default_agent_type = "claude"
auto_start_agent = false
{{ backtick }}{{ backtick }}{{ backtick }}

## Configuration Options

| Option | Type | Default | Environment Variable | Description |
|--------|------|---------|---------------------|-------------|
{{ range .Options }}| {{ backtick }}{{ .Name }}{{ backtick }} | {{ .Type }} | {{ backtick }}{{ .Default }}{{ backtick }} | {{ backtick }}{{ .EnvVar }}{{ backtick }} | {{ .Description }} |
{{ end }}

## Example Configuration

{{ backtick }}{{ backtick }}{{ backtick }}toml
# ~/.config/sprout/config.toml

# Base branch for new worktrees
base_branch = "main"

# Template for worktree root directory
# {{ .OpenBrace }}repo{{ .CloseBrace }} is replaced with repository name
worktree_root_template = "../{{ .OpenBrace }}repo{{ .CloseBrace }}.worktrees"

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
{{ backtick }}{{ backtick }}{{ backtick }}

## Environment Variable Overrides

All configuration options can be overridden with environment variables:

{{ backtick }}{{ backtick }}{{ backtick }}bash
{{ range .Options }}{{ if .EnvVar }}export {{ .EnvVar }}="{{ .Default }}"
{{ end }}{{ end }}{{ backtick }}{{ backtick }}{{ backtick }}

## Configuration Details

### base_branch

The default branch to use as the base when creating new worktrees. This is typically your main development branch (e.g., {{ backtick }}main{{ backtick }}, {{ backtick }}dev{{ backtick }}, {{ backtick }}develop{{ backtick }}).

### worktree_root_template

Template for the directory where worktrees will be created. The {{ backtick }}{{ .OpenBrace }}repo{{ .CloseBrace }}{{ backtick }} placeholder is replaced with the repository name.

For example, if your repo is {{ backtick }}/home/user/myproject{{ backtick }} and the template is {{ backtick }}../{{ .OpenBrace }}repo{{ .CloseBrace }}.worktrees{{ backtick }}, worktrees will be created in {{ backtick }}/home/user/myproject.worktrees/{{ backtick }}.

### auto_launch

When {{ backtick }}true{{ backtick }}, automatically creates and attaches to a tmux session when creating a new worktree with {{ backtick }}sprout new{{ backtick }}.

### auto_start_agent

When {{ backtick }}true{{ backtick }}, automatically starts an AI agent in a tmux window when creating a new worktree.

### update_check

When {{ backtick }}true{{ backtick }}, Sprout checks GitHub for updates once per day. Disable by setting {{ backtick }}SPROUT_UPDATE_CHECK=0{{ backtick }}.

### launch_nvim

When {{ backtick }}true{{ backtick }}, opens Neovim in a tmux pane when launching a session.

### launch_lazygit

When {{ backtick }}true{{ backtick }}, opens Lazygit in a tmux pane when launching a session.

### agent_command

**Deprecated:** Use {{ backtick }}default_agent_type{{ backtick }} instead.

The command to run for starting an AI agent.

### default_agent_type

The default AI agent to use. Must match one of the agent types defined in {{ backtick }}agent_command_*{{ backtick }} options.

Supported values: {{ backtick }}codex{{ backtick }}, {{ backtick }}aider{{ backtick }}, {{ backtick }}claude{{ backtick }}, {{ backtick }}gemini{{ backtick }}

### session_prefix

Prefix for tmux session names. Worktree sessions are named from a stable worktree-path token (for example, {{ backtick }}{prefix}-{repo}-{worktree}{{ backtick }}) so switching branches inside the same worktree does not change the tmux session name.

### agent_command_*

Custom commands for different AI agent types. Replace {{ backtick }}*{{ backtick }} with the agent type (e.g., {{ backtick }}agent_command_codex{{ backtick }}).

Examples:
- {{ backtick }}agent_command_codex = "codex"{{ backtick }}
- {{ backtick }}agent_command_aider = "aider --model gpt-4"{{ backtick }}
- {{ backtick }}agent_command_claude = "claude-code"{{ backtick }}

## Structured tmux windows

Use structured tmux window definitions when you want Sprout to launch specific windows and panes instead of the default tool-based layout.

- {{ backtick }}[[windows]]{{ backtick }} configures child worktree sessions.
- Set {{ backtick }}role = "agent"{{ backtick }} on a child window when that window should be treated as the agent window for attach.

{{ backtick }}{{ backtick }}{{ backtick }}toml
[[windows]]
name = "editor"
role = "agent"
layout = "main-vertical"

  [[windows.panes]]
  run = "nvim ."

  [[windows.panes]]
  run = "pnpm dev"
{{ backtick }}{{ backtick }}{{ backtick }}

## Preview worktree

Designate one worktree at a time as the **preview**: a dedicated tmux session ({{ backtick }}{prefix}-{repo}-{{ .OpenBrace }}preview_session_suffix{{ .CloseBrace }}{{ backtick }}) runs a configured set of long-running services from that worktree. Promoting another worktree tears the session down and rebuilds it pointed at the new worktree path, so you can validate an agent's branch by running your real stack against it.

The current preview is recorded in a state file in the repo's git common dir, so every worktree of the repo agrees on which one is the preview.

- {{ backtick }}[[preview_windows]]{{ backtick }} defines the services (same window/pane shape as {{ backtick }}[[windows]]{{ backtick }}, plus an optional {{ backtick }}url{{ backtick }} per service).
- {{ backtick }}{{ .OpenBrace }}worktree{{ .CloseBrace }}{{ backtick }} in a pane {{ backtick }}dir{{ backtick }} is replaced with the preview worktree path.
- {{ backtick }}preview_command_prefix{{ backtick }} optionally wraps each service command (e.g. a runner or profiler prefix). A pane whose {{ backtick }}dir{{ backtick }} resolves the same for every worktree (e.g. a fixed {{ backtick }}~{{ backtick }}) is left running across promotions instead of being restarted — use this for a shared tunnel agent like {{ backtick }}ngrok start --all{{ backtick }} so its URLs stay stable while you switch which worktree the code runs from.

Preview panes run under your login+interactive shell ({{ backtick }}zsh{{ backtick }}/{{ backtick }}bash{{ backtick }}), so {{ backtick }}~/.zshrc{{ backtick }} / {{ backtick }}~/.zprofile{{ backtick }} are sourced and tools installed via version managers ({{ backtick }}mise{{ backtick }}, {{ backtick }}asdf{{ backtick }}, {{ backtick }}nvm{{ backtick }}) or Homebrew are on {{ backtick }}PATH{{ backtick }}.

### Keeping app configs in sync ({{ backtick }}[[preview_sync]]{{ backtick }})

When a frontend (e.g. a mobile app) reaches your backend through a tunnel, its config file holds the tunnel's public URL — and that URL changes whenever the tunnel restarts. {{ backtick }}[[preview_sync]]{{ backtick }} rewrites those values automatically after each promote by reading the live URLs from the tunnel agent's local API ({{ backtick }}preview_tunnel_api{{ backtick }}, an [ngrok](https://ngrok.com)-compatible {{ backtick }}/api/tunnels{{ backtick }} endpoint by default).

- {{ backtick }}file{{ backtick }} is the JSON file to update (supports {{ backtick }}{{ .OpenBrace }}worktree{{ .CloseBrace }}{{ backtick }}).
- Each {{ backtick }}[[preview_sync.set]]{{ backtick }} writes one dotted {{ backtick }}path{{ backtick }} (e.g. {{ backtick }}api.baseUrl{{ backtick }}) from a named {{ backtick }}tunnel{{ backtick }}, optionally through a {{ backtick }}template{{ backtick }} where {{ backtick }}{url}{{ backtick }} is the tunnel URL.
- The file is only rewritten when a value actually changes; {{ backtick }}reload_windows{{ backtick }} names preview windows to restart in that case (e.g. a bundler that must reload to pick up the new URL). On a plain feature switch the URLs are unchanged, so nothing is rewritten and nothing reloads — no rebuild.

Commands:

{{ backtick }}{{ backtick }}{{ backtick }}bash
sprout preview              # show current preview + service URLs
sprout preview <target>     # promote a worktree to preview
sprout preview stop         # stop the preview session
sprout preview restart      # restart services from the current preview worktree
sprout preview sync         # rewrite [[preview_sync]] configs from live tunnel URLs
sprout preview logs <name>  # show recent output of a preview service
{{ backtick }}{{ backtick }}{{ backtick }}

In the TUI, press {{ backtick }}p{{ backtick }} to promote the selected worktree; the current preview is marked with {{ backtick }}▶{{ backtick }}.

{{ backtick }}{{ backtick }}{{ backtick }}toml
[[preview_windows]]
name = "tunnel"                     # shared tunnel agent, left running across promotions

  [[preview_windows.panes]]
  dir = "~"                         # fixed dir → not restarted when the preview worktree changes
  run = "ngrok start --all"

[[preview_windows]]
name = "api"
url = "http://localhost:8080"

  [[preview_windows.panes]]
  dir = "{{ .OpenBrace }}worktree{{ .CloseBrace }}"
  run = "make dev-api"

[[preview_windows]]
name = "web"

  [[preview_windows.panes]]
  dir = "{{ .OpenBrace }}worktree{{ .CloseBrace }}/web"
  run = "npm run dev"

# Rewrite the app's endpoint config from the live tunnel URLs on promote.
[[preview_sync]]
file = "{{ .OpenBrace }}worktree{{ .CloseBrace }}/web/config.json"
reload_windows = ["web"]           # restart the dev server only when a URL actually changed

  [[preview_sync.set]]
  path = "api.baseUrl"
  tunnel = "api"

  [[preview_sync.set]]
  path = "api.wsUrl"
  tunnel = "api"
  template = "{url}/socket"
{{ backtick }}{{ backtick }}{{ backtick }}

## Bootstrapping new worktrees

Fresh git worktrees do not contain gitignored local state — {{ backtick }}node_modules{{ backtick }}, local config files, dev certs, cached data — so apps fail to start until the worktree is prepared. Sprout no longer copies untracked files into new worktrees; instead it bootstraps them:

- {{ backtick }}bootstrap_links{{ backtick }} is a list of gitignored paths to **symlink** from the main worktree into the new one. Use this for small, identical-everywhere local config (e.g. {{ backtick }}.env{{ backtick }}, {{ backtick }}config.local.json{{ backtick }}, {{ backtick }}appsettings.Local.json{{ backtick }}) and for heavy shared local state (local DB/storage dirs). Do **not** link {{ backtick }}node_modules{{ backtick }} — it is per-branch build state and sharing it corrupts Yarn's state file.
- {{ backtick }}[[bootstrap]]{{ backtick }} steps are commands ({{ backtick }}dir{{ backtick }} + {{ backtick }}run{{ backtick }}, same shape as panes) run in order to install dependencies, e.g. {{ backtick }}yarn install{{ backtick }} or {{ backtick }}dotnet restore{{ backtick }}. {{ backtick }}{{ .OpenBrace }}worktree{{ .CloseBrace }}{{ backtick }} in {{ backtick }}dir{{ backtick }} resolves to the worktree being bootstrapped.

Bootstrap runs automatically on {{ backtick }}sprout new{{ backtick }} (skip with {{ backtick }}--no-bootstrap{{ backtick }}). Run {{ backtick }}sprout bootstrap [target]{{ backtick }} to (re)bootstrap an existing worktree — handy for worktrees created before bootstrap was configured.

{{ backtick }}{{ backtick }}{{ backtick }}toml
bootstrap_links = [
  "web/config.local.json",
  "api/appsettings.Local.json",
]

[[bootstrap]]
dir = "{{ .OpenBrace }}worktree{{ .CloseBrace }}/web"
run = "npm install"

[[bootstrap]]
dir = "{{ .OpenBrace }}worktree{{ .CloseBrace }}/api"
run = "dotnet restore"
{{ backtick }}{{ backtick }}{{ backtick }}
`

func main() {
	options := []ConfigOption{
		{
			Name:        "base_branch",
			Type:        "string",
			Default:     "main",
			EnvVar:      "SPROUT_BASE_BRANCH",
			Description: "Default base branch for new worktrees",
		},
		{
			Name:        "worktree_root_template",
			Type:        "string",
			Default:     "../\\{repo\\}.worktrees",
			EnvVar:      "SPROUT_WORKTREE_ROOT_TEMPLATE",
			Description: "Template for worktree root directory (\\{repo\\} is replaced with repo name)",
		},
		{
			Name:        "auto_launch",
			Type:        "bool",
			Default:     "true",
			EnvVar:      "SPROUT_AUTO_LAUNCH",
			Description: "Automatically launch tmux session when creating worktrees",
		},
		{
			Name:        "auto_start_agent",
			Type:        "bool",
			Default:     "true",
			EnvVar:      "SPROUT_AUTO_START_AGENT",
			Description: "Automatically start AI agent when creating worktrees",
		},
		{
			Name:        "update_check",
			Type:        "bool",
			Default:     "true",
			EnvVar:      "SPROUT_UPDATE_CHECK",
			Description: "Check GitHub for updates once per day",
		},
		{
			Name:        "launch_nvim",
			Type:        "bool",
			Default:     "true",
			EnvVar:      "SPROUT_LAUNCH_NVIM",
			Description: "Launch Neovim in tmux session",
		},
		{
			Name:        "launch_lazygit",
			Type:        "bool",
			Default:     "true",
			EnvVar:      "SPROUT_LAUNCH_LAZYGIT",
			Description: "Launch Lazygit in tmux session",
		},
		{
			Name:        "agent_command",
			Type:        "string",
			Default:     "codex",
			EnvVar:      "SPROUT_AGENT_COMMAND",
			Description: "Default agent command (deprecated: use default_agent_type)",
		},
		{
			Name:        "default_agent_type",
			Type:        "string",
			Default:     "codex",
			EnvVar:      "SPROUT_DEFAULT_AGENT_TYPE",
			Description: "Default AI agent type (codex, aider, claude, gemini)",
		},
		{
			Name:        "session_prefix",
			Type:        "string",
			Default:     "sprout",
			EnvVar:      "SPROUT_SESSION_PREFIX",
			Description: "Prefix for tmux session names",
		},
		{
			Name:        "agent_command_*",
			Type:        "string",
			Default:     "varies",
			EnvVar:      "SPROUT_AGENT_COMMAND_*",
			Description: "Custom command for specific agent type (* = agent type)",
		},
		{
			Name:        "layout_<repo>_win_<name>_pane_<idx>",
			Type:        "string",
			Default:     "-",
			EnvVar:      "",
			Description: "Custom multi-pane tmux window configuration",
		},
		{
			Name:        "[[windows]]",
			Type:        "table array",
			Default:     "-",
			EnvVar:      "",
			Description: "Structured tmux window layout for child worktree sessions",
		},
		{
			Name:        "preview_session_suffix",
			Type:        "string",
			Default:     "preview",
			EnvVar:      "SPROUT_PREVIEW_SESSION_SUFFIX",
			Description: "Suffix for the dedicated preview tmux session name",
		},
		{
			Name:        "preview_command_prefix",
			Type:        "string",
			Default:     "",
			EnvVar:      "SPROUT_PREVIEW_COMMAND_PREFIX",
			Description: "Optional prefix wrapped around each preview service command (e.g. \"portless run\")",
		},
		{
			Name:        "preview_auto_attach",
			Type:        "bool",
			Default:     "false",
			EnvVar:      "SPROUT_PREVIEW_AUTO_ATTACH",
			Description: "Attach to the preview session after promoting (default: run detached)",
		},
		{
			Name:        "[[preview_windows]]",
			Type:        "table array",
			Default:     "-",
			EnvVar:      "",
			Description: "Long-running services run from the preview worktree",
		},
		{
			Name:        "preview_tunnel_api",
			Type:        "string",
			Default:     "http://127.0.0.1:4040/api/tunnels",
			EnvVar:      "SPROUT_PREVIEW_TUNNEL_API",
			Description: "ngrok agent local API used by [[preview_sync]] to resolve tunnel URLs",
		},
		{
			Name:        "[[preview_sync]]",
			Type:        "table array",
			Default:     "-",
			EnvVar:      "",
			Description: "Config files kept in sync with live tunnel URLs on each promote",
		},
		{
			Name:        "bootstrap_links",
			Type:        "array",
			Default:     "[]",
			EnvVar:      "SPROUT_BOOTSTRAP_LINKS",
			Description: "Gitignored paths to symlink from the main worktree into each new worktree",
		},
		{
			Name:        "[[bootstrap]]",
			Type:        "table array",
			Default:     "-",
			EnvVar:      "",
			Description: "Setup commands (dir + run) to run when bootstrapping a worktree",
		},
	}

	tmpl := template.Must(template.New("doc").Funcs(template.FuncMap{
		"backtick": func() string { return "`" },
	}).Parse(configDocTemplate))

	data := map[string]interface{}{
		"Options":    options,
		"OpenBrace":  "{",
		"CloseBrace": "}",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Fprintf(os.Stderr, "error generating docs: %v\n", err)
		os.Exit(1)
	}

	outputPath := filepath.Join("..", "docs", "configuration", "reference.md")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating docs directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing docs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated configuration documentation: %s\n", outputPath)
}
