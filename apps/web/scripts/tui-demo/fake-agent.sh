#!/bin/bash
# A pretend coding agent for the TUI demo. Type "busy" or "ask" to change state.
echo "demo-agent v0 — $(basename "$PWD")"
echo
echo "Reading src/handlers/users.go ..."
echo "Ready for your next instruction"
while true; do
  printf '> '
  read -r line || exit 0
  case "$line" in
    busy) for i in $(seq 14); do echo "tool call $i ..."; done; sleep 3600 ;;
    ask) echo "I need your input before I continue."; echo "Can you confirm which path to take?" ;;
    *) echo "ok: $line"; echo "Ready for your next instruction" ;;
  esac
done
