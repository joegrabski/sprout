#!/bin/bash
# Builds an isolated demo of the sprout TUI: a throwaway repo with worktrees in
# every git status shape, three fake agents (ready / busy / needs input), a
# fake `gh`, and a private tmux server. Nothing touches your real HOME, repos,
# tmux sessions or GitHub.
#
#   demo.sh setup     create the demo (idempotent)
#   demo.sh run       run the TUI inside the demo environment
#   demo.sh env       print the environment as `export` lines (eval it)
#   demo.sh path      print the demo directory
#   demo.sh teardown  stop its tmux server and delete it
set -euo pipefail

# Never let a surrounding tmux session leak into the demo's private server.
unset TMUX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../../.." && pwd)"
DEMO="${SPROUT_DEMO_DIR:-${TMPDIR:-/tmp}}"
DEMO="${DEMO%/}"
[ -n "${SPROUT_DEMO_DIR:-}" ] || DEMO="$DEMO/sprout-tui-demo"
DEMO="${DEMO%/}"
export TMUX_TMPDIR="/tmp/sprout-demo-tmux" # short: unix socket paths are length limited
BIN="$DEMO/bin"

case "$(basename "$DEMO")" in
  sprout-tui-demo*) ;;
  *) echo "refusing to use $DEMO: directory name must start with sprout-tui-demo" >&2; exit 1 ;;
esac

env_lines() {
  echo "export HOME='$DEMO/home'"
  echo "export TMUX_TMPDIR='$TMUX_TMPDIR'"
  echo "export PATH='$BIN':\"\$PATH\""
  echo "unset TMUX"
}

build() {
  mkdir -p "$BIN"
  local version
  version="$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null || echo dev)"
  (cd "$ROOT/apps/sprout" && go build -ldflags "-X sprout/internal/sprout.Version=${version#v}" -o "$BIN/sprout" ./cmd/sprout)
  cp "$HERE/fake-gh.sh" "$BIN/gh"
  chmod +x "$BIN/gh" "$HERE/fake-agent.sh"
}

setup() {
  teardown_quiet
  mkdir -p "$DEMO/home/.config/sprout" "$TMUX_TMPDIR"
  build # with the real HOME, so Go finds its module cache
  eval "$(env_lines)"
  printf 'agent_command = "%s/fake-agent.sh"\n' "$HERE" >"$DEMO/home/.config/sprout/config.toml"

  local repo="$DEMO/acme-api" wts="$DEMO/acme-api.worktrees"
  export GIT_AUTHOR_NAME=Demo GIT_AUTHOR_EMAIL=demo@acme.test GIT_COMMITTER_NAME=Demo GIT_COMMITTER_EMAIL=demo@acme.test
  git init -q -b main "$repo"
  git init -q --bare "$DEMO/origin.git"
  cd "$repo"
  mkdir -p src/handlers docs
  printf 'package main\n\nfunc main() {}\n' >src/main.go
  printf 'package handlers\n\nfunc Users() string { return "users" }\n' >src/handlers/users.go
  printf '# acme-api\n\nBilling and auth services.\n' >README.md
  printf 'notes\n' >docs/notes.md
  git add -A && git commit -qm "init"
  git remote add origin "$DEMO/origin.git"
  git push -q origin main

  git worktree add -q "$wts/feat-billing" -b feat/billing
  git worktree add -q "$wts/fix-auth-timeout" -b fix/auth-timeout
  git worktree add -q "$wts/chore-deps" -b chore/deps

  # feat/billing: one file in every status shape.
  cd "$wts/feat-billing"
  printf 'package handlers\n\nfunc Users() string { return "users" }\n\nfunc Invoices() []Invoice {\n\treturn load()\n}\n' >src/handlers/users.go
  git add src/handlers/users.go
  printf '\nfunc Total(items []Item) (sum int) {\n\tfor _, it := range items {\n\t\tsum += it.Cents\n\t}\n\treturn sum\n}\n' >>src/handlers/users.go
  printf 'package handlers\n\ntype Invoice struct{ ID string }\n' >src/handlers/billing.go
  git add src/handlers/billing.go
  printf 'notes\nwebhook retries: 5\n' >docs/notes.md
  git mv src/main.go src/app.go
  printf 'scratch\n' >scratch.txt

  # fix/auth-timeout: a small unstaged fix.
  cd "$wts/fix-auth-timeout"
  printf 'package handlers\n\nfunc Users() string { return "users" } // TODO: token ttl 15m\n' >src/handlers/users.go

  # Agents in three states.
  cd "$repo"
  for b in feat/billing fix/auth-timeout chore/deps; do "$BIN/sprout" agent start "$b" >/dev/null; done
  sleep 1
  tmux send-keys -t "sprout-acme-api-fix-auth-timeout:agent-fix-auth-timeout.0" "ask" Enter
  tmux send-keys -t "sprout-acme-api-chore-deps:agent-chore-deps.0" "busy" Enter
  echo "demo ready in $DEMO"
}

run() {
  eval "$(env_lines)"
  cd "$DEMO/acme-api"
  exec "$BIN/sprout"
}

teardown_quiet() {
  tmux kill-server >/dev/null 2>&1 || true
  rm -rf "$DEMO"
}

teardown() {
  teardown_quiet
  echo "removed $DEMO"
}

case "${1:-}" in
  setup) setup ;;
  run) run ;;
  env) env_lines ;;
  path) echo "$DEMO" ;;
  teardown) teardown ;;
  *) sed -n '2,12p' "${BASH_SOURCE[0]}"; exit 2 ;;
esac
