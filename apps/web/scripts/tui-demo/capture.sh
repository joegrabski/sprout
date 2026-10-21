#!/bin/bash
# Captures real frames of the sprout TUI running against the demo repo and
# writes them to src/components/Landing/tuiFrames.generated.ts.
#
#   capture.sh            set up the demo, capture, tear down
#
# Needs: go, tmux, bun.
set -euo pipefail

unset TMUX
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB="$(cd "$HERE/../.." && pwd)"
OUT="$WEB/src/components/Landing/tuiFrames.generated.ts"
FRAMES="$(mktemp -d "${TMPDIR:-/tmp}/sprout-frames.XXXXXX")"
trap 'rm -rf "$FRAMES"; "$HERE/demo.sh" teardown >/dev/null 2>&1 || true' EXIT

"$HERE/demo.sh" setup
DEMO="$("$HERE/demo.sh" path)"
eval "$("$HERE/demo.sh" env)"

SESSION=frames
tmux new-session -d -s "$SESSION" -x 124 -y 30 -c "$DEMO/acme-api" "$DEMO/bin/sprout"
sleep 4

grab() { tmux capture-pane -p -e -t "$SESSION" >"$FRAMES/$1.ansi"; }
key() { tmux send-keys -t "$SESSION" "$@"; sleep "${WAIT:-0.8}"; }

# 1. Files: feat/billing, with a file in every state and its patch.
key j; key j; key 3; sleep 1
WAIT=0.3 key j; WAIT=0.3 key j; WAIT=0.3 key j; WAIT=0.3 key j; sleep 1
grab 1
key 2

# 2. Agent tab: fix/auth-timeout is waiting for input.
key j; key "]"; sleep 1.5
grab 2
key "]"

# 3. New worktree dialog with issues and pull requests.
key n; sleep 2
grab 3
key Escape

bun "$HERE/ansi-to-html.ts" "$FRAMES/1.ansi" "$FRAMES/2.ansi" "$FRAMES/3.ansi" >"$OUT"
echo "wrote $OUT"
