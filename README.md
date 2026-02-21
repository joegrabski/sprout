# Sprout

<div align="center">

**Git worktree operations, without the context switching**

A modern TUI for managing git worktrees with integrated tmux sessions and AI coding agents.

[Documentation](https://sprout.dev) • [Installation](#installation) • [Quick Start](#quick-start)

</div>

## What is Sprout?

Sprout simplifies git worktree management by providing:

- **Interactive TUI** - Visual interface for creating, switching, and managing worktrees
- **Tmux Integration** - Automatic session management with Neovim and Lazygit
- **AI Agent Support** - Built-in integration with popular AI coding assistants (Codex, Aider, Claude, Gemini)
- **Shell Integration** - Seamless directory switching via shell hooks
- **Zero Context Switching** - Work on multiple branches simultaneously without losing your flow

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap joegrabski/sprout
brew install sprout
```

### Pre-built Binaries

Download from [GitHub Releases](https://github.com/joegrabski/sprout/releases)

### From Source

```bash
git clone https://github.com/joegrabski/sprout.git
cd sprout
make sprout-build
sudo make sprout-install
```

## Quick Start

```bash
# Navigate to a git repository
cd ~/projects/myapp

# Create a new worktree
sprout new feat checkout-redesign

# Launch the interactive TUI
sprout

# List all worktrees
sprout list

# Switch to another worktree
sprout go main
```

See the [Quick Start Guide](https://sprout.dev/docs/quick-start) for more details.

## Key Features

### Interactive TUI
Beautiful terminal interface showing all worktrees with their status, active tmux sessions, and running AI agents at a glance.

### Automatic Tmux Sessions
Each worktree gets its own isolated tmux session with Neovim, Lazygit, and your preferred AI agent.

### Smart Organization
Worktrees are organized in a dedicated directory structure, keeping your repository root clean.

### AI Integration
Built-in support for:
- Codex
- Aider
- Claude Code
- Gemini

Each worktree can have its own AI coding assistant with independent context.

## Documentation

Full documentation is available at [sprout.dev](https://sprout.dev)

- [Installation Guide](https://sprout.dev/docs/installation)
- [Quick Start](https://sprout.dev/docs/quick-start)
- [Configuration](https://sprout.dev/docs/configuration/overview)
- [CLI Reference](https://sprout.dev/docs/cli/commands)
- [Integrations](https://sprout.dev/docs/integrations/tmux)

## Development

This is a monorepo containing:

- `apps/sprout` - Go CLI/TUI worktree manager
- `apps/web` - Documentation website (Docusaurus)

### Working on Sprout CLI

```bash
# Build sprout binary
make sprout-build

# Run tests
cd apps/sprout
go test ./...

# Install locally
make sprout-install
```

### Working on Documentation

```bash
# Generate docs and start dev server
make docs-dev

# Build production docs
make docs-build

# Just generate auto-generated docs
make docs-generate
```

See `apps/web/README.md` for more documentation development details.

## Available Make Commands

```bash
make help                 # Show all available commands
make docs-dev            # Start documentation dev server
make docs-build          # Build production documentation
make docs-generate       # Generate auto-generated docs
make sprout-build        # Build sprout binary
make sprout-install      # Build and install sprout
```

## Configuration

Create `~/.config/sprout/config.toml`:

```toml
base_branch = "main"
worktree_root_template = "../{repo}.worktrees"
auto_launch = true
auto_start_agent = true
session_tools = ["agent", "lazygit", "nvim"]
default_agent_type = "aider"
```

See [Configuration Reference](https://sprout.dev/docs/configuration/reference) for all options.

## Shell Integration

Add to your shell config:

```bash
# Zsh (~/.zshrc)
eval "$(sprout shell-hook zsh)"

# Bash (~/.bashrc)
eval "$(sprout shell-hook bash)"

# Fish (~/.config/fish/config.fish)
sprout shell-hook fish | source
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Support

- [Documentation](https://sprout.dev)
- [GitHub Issues](https://github.com/joegrabski/sprout/issues)
- [Discussions](https://github.com/joegrabski/sprout/discussions)

