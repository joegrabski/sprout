# AGENTS.md

Guidance for AI coding agents and automation working in this repository.

## Scope

- This file applies to the whole repo unless a nested `AGENTS.md` overrides it for a subdirectory.

## Repository Map

- `apps/sprout`: Go CLI/TUI application (primary product code).
- `apps/web`: Documentation site (Docusaurus).
- `Makefile`: Common dev/build/docs commands.
- `go.work`: Go workspace config for monorepo development.

## Expected Workflow

1. Read the relevant README/docs before editing.
2. Keep changes focused and minimal for the task.
3. Run the smallest useful validation (tests/build/lint) before finishing.
4. Report what changed, what was validated, and any gaps.

## Build and Test Commands

- `make sprout-build`: Build the CLI binary.
- `cd apps/sprout && go test ./...`: Run Go tests.
- `make docs-generate`: Regenerate auto-generated docs.
- `make docs-dev`: Run docs dev server.
- `make docs-build`: Build docs for production.
- `make help`: List available make targets.

## Coding Standards

- Prefer idiomatic, readable Go in `apps/sprout`.
- Avoid unrelated refactors unless requested.
- Preserve existing patterns and naming in touched files.
- Keep docs changes in sync with behavior changes.
- Do not add license headers unless requested.

## Safety Rules

- Never run destructive git commands (e.g. `git reset --hard`) unless explicitly requested.
- Do not revert user-authored unrelated changes.
- If unexpected modifications appear, stop and ask before proceeding.

## Completion Checklist

- Code compiles or changed docs build cleanly.
- Relevant tests/checks were run (or clearly explain why not).
- Any follow-up work is called out explicitly.
