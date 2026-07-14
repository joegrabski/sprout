# AGENTS.md

Guidance for AI coding agents and automation working in this repository.

## Scope

- Applies to the entire repository unless a nested `AGENTS.md` overrides it.

## Repository Map

- `apps/sprout`: Go CLI/TUI application.
- `apps/sprout/cmd/sprout`: CLI entrypoint.
- `apps/sprout/internal/sprout`: core logic (manager, UI, config, daemon, tests).
- `apps/sprout/completions/sprout.zsh`: shell completion script.
- `apps/web`: Docusaurus documentation site.
- `apps/web/docs`: authored docs plus generated command/config reference pages.
- `apps/web/scripts`: docs generation scripts.
- `Makefile`: top-level build/docs helpers.
- `go.work`: Go workspace, currently includes `./apps/sprout`.

## Expected Workflow

1. Read relevant docs/code before editing (`README.md`, app README, touched package files).
2. Keep edits focused to the request; avoid opportunistic refactors.
3. Prefer minimal, targeted validation first; expand only as needed.
4. If behavior changes, update docs and regenerate generated docs when applicable.
5. Report: what changed, what was validated, and any remaining gaps.

## Build, Test, and Dev Commands

- `make help`: list available make targets.
- `make sprout-build`: build CLI binary to repo root as `./sprout`.
- `make sprout-install`: install built binary to `/usr/local/bin/sprout`.
- `cd apps/sprout && go test ./...`: run full Go test suite.
- `cd apps/sprout && go test ./internal/sprout`: run focused core tests.
- `make docs-generate`: regenerate docs from CLI/config sources.
- `make docs-dev`: generate docs and start Docusaurus dev server.
- `make docs-build`: generate docs and build production site.
- `cd apps/web && bun run lint`: run docs/frontend lint checks.
- `cd apps/web && bun run typecheck`: run TypeScript checks.

## Generated Artifacts and Docs Sync

- Do not hand-edit generated docs when source-of-truth is code/scripts:
  - `apps/web/docs/cli/commands.md`
  - `apps/web/docs/configuration/reference.md`
- Regenerate them with `make docs-generate` after CLI/config behavior changes.
- Prefer editing generators in `apps/web/scripts/` when changing generated wording.
- Avoid manual edits in transient build output directories unless explicitly requested:
  - `apps/web/build/`
  - `apps/web/.docusaurus/`

## Coding Standards

- Prefer idiomatic, readable Go in `apps/sprout`.
- Preserve existing command/config patterns (Cobra commands in `cli.go`, behavior in manager/config).
- Keep docs and completions in sync with user-facing CLI changes.
- Do not add license headers unless requested.
- Tmux session identity invariant: do not key worktree tmux session names by current git branch. Use a stable worktree-derived token so branch switches inside an active tmux session do not break CLI/session mapping.

## Safety Rules

- Never run destructive git commands (e.g., `git reset --hard`) unless explicitly requested.
- Do not revert user-authored unrelated changes.
- If unexpected modifications appear, stop and ask before proceeding.

## Completion Checklist

- Changed code compiles, or docs build/generation succeeds for docs-only changes.
- Relevant tests/checks were run (or clearly explain why they were not).
- Generated docs were refreshed when required by behavior/config/CLI changes.
- Follow-up work and risks are called out explicitly.
