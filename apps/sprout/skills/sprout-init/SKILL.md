---
name: sprout-init
description: Analyze the current repository and generate (or improve) an optimal sprout `.sprout.toml` — preview-worktree services, bootstrap links/commands, and optional tunnel (ngrok) URL syncing. Use when the user wants to set up sprout for a repo, configure the preview worktree, fix "stuff breaks when I switch worktrees", wire up ngrok/tunnels, or asks to auto-detect a sprout config. Works on any stack (.NET, Node/Yarn/pnpm workspaces, Django, Next.js, Expo/React Native, Docker Compose).
version: 1.0.0
user-invocable: true
argument-hint: "[path-to-repo]"
---

Generate an **optimal `.sprout.toml`** for a repository by inspecting how it actually runs, then writing a config the user can use immediately. sprout is a git-worktree manager; the config you produce drives two features:

- **Preview worktree** — `[[preview_windows]]` defines the long-running services (APIs, web apps, mobile bundler) that run from whichever worktree is "preview". `sprout preview <branch>` swaps which worktree they run from. `preview_command_prefix` optionally wraps each command.
- **Tunnel URL sync** — `[[preview_sync]]` rewrites config files with the live ngrok tunnel URLs on each promote (read from ngrok's local API, `preview_tunnel_api`), so a frontend that reaches the backend through a tunnel (typically a mobile app) never needs its config hand-edited.
- **Bootstrap** — `bootstrap_links` symlinks gitignored local config from the main worktree into new worktrees; `[[bootstrap]]` runs setup commands (installs). This replaces the old copy-untracked behaviour, so fresh worktrees actually run.

You are an agent: inspect, reason, ask the one question that matters (tunnels), then write the file. Do not guess commands — read the repo's real run setup.

## Hard rules

- **Never clobber an existing `.sprout.toml`.** Read it first. Preserve existing `[[windows]]`, agent settings, etc. Only add/merge the preview + bootstrap sections. Show a diff-style summary of what you changed.
- **`bootstrap_links` must be a single-line array** (`["a", "b"]`). The flat config parser does not support multi-line arrays.
- Pane/bootstrap `dir` values use the `{worktree}` placeholder (replaced with the target worktree path), e.g. `dir = "{worktree}/src/apps"`.
- **Never symlink `node_modules`, `bin/`, `obj/`, build output, or anything per-branch.** Those are installed by a `[[bootstrap]]` step, not linked. Only link small/identical-everywhere config and heavy shared *data* (local DB/object-store dirs) the user wants shared across worktrees.
- Only link files that (a) exist in the main checkout and (b) are gitignored. Verify with `git check-ignore`.
- Keep service `name`s short and unique (they become tmux window names).

## Phase 1 — Detect repo facts

Resolve the target repo (argument or cwd). Then gather, running commands rather than assuming:

1. **Repo shape**: `git rev-parse --show-toplevel`; top-level dirs; presence of `*.sln`, `package.json` (+ `workspaces`/`pnpm-workspace.yaml`), `docker-compose*.yml`, `manage.py`, `app.config.*`/`app.json` (Expo), `Makefile`/`Taskfile.yml`/`justfile`.
2. **Runnable services + ports** — read the real launch config:
   - .NET: each `**/Properties/launchSettings.json` (applicationUrl ports), the `.sln` projects, Azure Functions (`func start --port`, `local.settings.json`).
   - Node web: `vite.config.*` / `next.config.*` ports, `package.json` scripts (`dev`/`start`), workspace member names.
   - Expo/RN: Metro (8081), `yarn start`.
   - Docker: `docker-compose*.yml` services, published `ports:`, bind-mounted source (`volumes: ./x:/...`), and `env_file:` entries. Prefer a Makefile/Taskfile target if one runs the local stack.
3. **How each frontend points at its backend** — find the config that holds the API base URL (e.g. `public/config.json`, `src/config/config*.json`, `appsettings*.json` `EndpointConfig`, `.env`/`VITE_*`/`NEXT_PUBLIC_*`). Note the file path and key for each.
4. **Gitignored local state** — for candidate local files/dirs (`*.local.json`, `appsettings.*.Local.json`, `*.env`/`secrets.env`, local DB/storage dirs like `local/**/data`, dev certs), check existence in the main checkout and `git check-ignore -q <path>`. These are bootstrap-link candidates.
5. **Dependency installs** — package managers in play (`yarn`/`pnpm`/`npm` — read `packageManager`/lockfiles), `dotnet restore`, `poetry`, etc., and the dir each runs in.

Summarize the detected services, ports, frontend-config files, gitignored local files, and install commands back to the user before writing.

## Phase 2 — Decide the config

- **preview_windows**: one entry per long-running component (each `dir`+`run`, with a `url`). Use the real ports you found. Put the mobile bundler in too. If the stack needs tunnels (e.g. a mobile app reaching the backend), add an `ngrok` window with a **fixed** pane `dir` (e.g. `~`) running `ngrok start --all` — a fixed dir makes sprout leave it running across promotions so the URLs stay stable. If a single command runs the whole stack (e.g. `docker compose ... up`), make that one window; pin a fixed compose project name and run it foreground so promotion cleanly replaces it.
- **bootstrap_links**: the gitignored local config + heavy shared data dirs you confirmed.
- **[[bootstrap]]**: the install commands (`yarn install --immutable`, `dotnet restore`, …), each with the right `{worktree}/...` dir.

## Phase 3 — Ask about tunnel URL syncing (REQUIRED if a frontend reaches the backend via a tunnel)

If Phase 1.3 found a frontend config that holds tunnel URLs (typically a mobile app pointing at ngrok URLs), those go stale whenever the tunnels restart. `[[preview_sync]]` fixes this: sprout reads the live URLs from ngrok's local API after each promote and rewrites the config. **Use AskUserQuestion** to ask which the user wants:

- **ngrok + `[[preview_sync]]`** — add an `ngrok` preview window (`ngrok start --all`, fixed `dir`), give each backend service a fixed-port `url`, and add a `[[preview_sync]]` block mapping each config key (dotted `path`) to a named `tunnel` (with an optional `template` when the key needs a URL suffix). Add `reload_windows = ["<bundler>"]` so the bundler restarts only when a URL actually changed. Confirm the ngrok tunnel names/ports (`~/.config/ngrok/ngrok.yml`) match the service ports. Reserved ngrok domains (a `domain:` per tunnel) make URLs permanent — mention it as an optional upgrade.
- **`.sprout.toml` only** — fixed-port `url`s, no tunnel window, no config rewriting; don't touch app files.

(If the user already named a preference, skip the question. If no frontend reads tunnel URLs, skip this phase and use fixed-port URLs.)

## Phase 4 — Write

Merge into `.sprout.toml` at the repo root (create if absent; preserve existing content). Add brief comments explaining the preview, preview_sync, and bootstrap blocks. Keep `bootstrap_links` single-line. `[[preview_sync]]` does the config rewriting at runtime, so you don't hand-edit the frontend config yourself.

## Phase 5 — Validate

If the `sprout` binary is available, run `sprout preview status` in the repo (it loads the full config) to confirm it parses, and show the detected services. If `sprout` isn't installed, say so and skip. Finish with: what services were configured, what will be symlinked vs installed on bootstrap, whether tunnel syncing was wired, and the exact commands to use it (`sprout preview <branch>`, `sprout preview sync`, `sprout bootstrap`).
