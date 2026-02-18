---
sidebar_position: 2
---

# Installation

## Homebrew (macOS/Linux)

```bash
brew tap jgrabski/sprout
brew install sprout
```

## Pre-built binaries

Download from [GitHub Releases](https://github.com/jgrabski/sprout/releases).

```bash
# macOS (Apple Silicon)
curl -L https://github.com/jgrabski/sprout/releases/latest/download/sprout-darwin-arm64 -o sprout
chmod +x sprout && sudo mv sprout /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/jgrabski/sprout/releases/latest/download/sprout-darwin-amd64 -o sprout
chmod +x sprout && sudo mv sprout /usr/local/bin/

# Linux (amd64)
curl -L https://github.com/jgrabski/sprout/releases/latest/download/sprout-linux-amd64 -o sprout
chmod +x sprout && sudo mv sprout /usr/local/bin/
```

## Build from source

Requires Go 1.21+.

```bash
go install github.com/jgrabski/sprout/apps/sprout/cmd/sprout@latest
```

## Verify

```bash
sprout version
sprout doctor
```

## Shell integration

Enables automatic directory changing with `sprout go` and `sprout new`.

**Zsh** — add to `~/.zshrc`:
```bash
eval "$(sprout shell-hook zsh)"
```

**Bash** — add to `~/.bashrc`:
```bash
eval "$(sprout shell-hook bash)"
```

**Fish** — add to `~/.config/fish/config.fish`:
```fish
sprout shell-hook fish | source
```

Then reload your shell:
```bash
source ~/.zshrc
```

## Zsh completion

Homebrew installs completions automatically. For manual installs:

```bash
mkdir -p ~/.zsh/completions
cp completions/sprout.zsh ~/.zsh/completions/_sprout

# Add to ~/.zshrc if not already present
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit && compinit
```
