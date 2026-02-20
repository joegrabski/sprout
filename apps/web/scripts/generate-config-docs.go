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

## Configuration File

Sprout reads configuration from {{ backtick }}~/.config/sprout/config.toml{{ backtick }} by default. You can override this location with the {{ backtick }}SPROUT_CONFIG{{ backtick }} environment variable.

## Configuration Options

| Option | Type | Default | Environment Variable | Description |
|--------|------|---------|---------------------|-------------|
{{ range .Options }}| {{ backtick }}{{ .Name }}{{ backtick }} | {{ .Type }} | {{ backtick }}{{ .Default }}{{ backtick }} | {{ backtick }}{{ .EnvVar }}{{ backtick }} | {{ .Description }} |
{{ end }}

## Example Configuration

{{ backtick }}{{ backtick }}{{ backtick }}toml
# ~/.config/sprout/config.toml

# Base branch for new worktrees
base_branch = "dev"

# Template for worktree root directory
# {{ .OpenBrace }}repo{{ .CloseBrace }} is replaced with repository name
worktree_root_template = "../{{ .OpenBrace }}repo{{ .CloseBrace }}.worktrees"

# Automatically launch tmux when creating new worktrees
auto_launch = true

# Automatically start AI agent when creating worktrees
auto_start_agent = true

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

Prefix for tmux session names. Sessions will be named {{ backtick }}{prefix}-{branch}{{ backtick }}.

### agent_command_*

Custom commands for different AI agent types. Replace {{ backtick }}*{{ backtick }} with the agent type (e.g., {{ backtick }}agent_command_codex{{ backtick }}).

Examples:
- {{ backtick }}agent_command_codex = "codex"{{ backtick }}
- {{ backtick }}agent_command_aider = "aider --model gpt-4"{{ backtick }}
- {{ backtick }}agent_command_claude = "claude-code"{{ backtick }}
`

func main() {
	options := []ConfigOption{
		{
			Name:        "base_branch",
			Type:        "string",
			Default:     "dev",
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
