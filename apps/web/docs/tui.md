---
sidebar_position: 5
---

# The TUI

Run `sprout` (or `sprout ui`) inside a git repository to open the TUI. It shows every worktree of the repo at once, the changes in the selected one, and what its agent is doing, then lets you act on them without leaving.

```
╭─ 1 sprout ───────────────╮╭─ 4 Patch │ Agent ─────────────────────────╮
│ acme-api  ›  main        ││ diff --git a/src/handlers/users.go …     │
╰──────────────────────────╯│ @@ -1,3 +1,7 @@                          │
╭─ 2 Worktrees ────────────╮│  package handlers                        │
│  ●  main           clean ││ +func Invoices() []Invoice {             │
│▌    feat/billing  #12 ✓  ││ +    return load()                       │
│     fix/auth  ◆ needs in ││ +}                                       │
╰──────────────────────────╯│                                          │
╭─ 3 Files ────────────────╮│                                          │
│  ▾ src/handlers/         ││                                          │
│▌   MM users.go           ││                                          │
╰──────────────────────────╯╰──────────────────────────────────────────╯
 ⏎ attach  p preview  n new  x remove  d detach  o PR  / filter  ? help
```

The panes are numbered. Press `1`–`4` to jump to one, or `tab` / `shift+tab` to cycle. The key bar at the bottom always shows the keys for the pane you are in, and `?` opens the full list. On terminals narrower than 100 columns the patch moves out of the way and takes the whole screen when you focus it.

## Worktrees

Each row shows the branch and, on the right, what is going on with it:

| Chip | Meaning |
| --- | --- |
| `●` before the name | the worktree you launched sprout from |
| `▶` before the name | the worktree currently running your [preview services](/docs/configuration) |
| `clean` / `dirty` | uncommitted changes in the work tree |
| `tmux` | the worktree has a tmux session |
| `#12 ✓` `#12 ✗` `#12 …` | its pull request, with CI passing, failing or still running |
| `#12 draft`, `merged #12`, `closed #12` | the pull request's state |
| `● ready` `● busy` `◆ needs input` | what the agent in that worktree is doing |

Sprout reads the agent's tmux pane to tell these apart, for every running agent, so you can see which ones are waiting for you at a glance. Pull request and CI status come from the [GitHub CLI](https://cli.github.com) (`gh`); without it, or outside a GitHub repository, those chips are simply left out.

| Key | Action |
| --- | --- |
| `j` `k` / arrows | move |
| `enter` or `g` | attach to the worktree's tmux session (starts it if needed) |
| `p` | promote this worktree to the preview |
| `o` | open its pull request in the browser |
| `n` | create a worktree |
| `x` | remove the worktree |
| `d` | detach: stop its tmux session but keep the worktree |
| `/` | filter by branch or path (`esc` clears it) |

### Creating a worktree

`n` opens a list of your branches. Type to filter, or type a new name to create a branch. If `gh` is available the list also offers your open **issues** and **pull requests**:

- an issue creates a branch named `<type>/<number>-<slug>` (for example `fix/31-crash-when-an-invoice-has-no`, with the type taken from the issue's labels) and a worktree for it. It is the same convention the [`sprout-issue` skill](/docs/skills) falls back to;
- a pull request creates a worktree on the PR's branch, fetching it first. Pull requests from forks are fetched into a local `pr/<number>-<slug>` branch.

Type `#31` to jump straight to an issue or pull request by number.

## Files and patch

The Files pane lists the changed files of the selected worktree as a tree, and the Patch pane shows the diff of the selected file, staged changes first and then unstaged ones. The two characters before each file are its git status, like lazygit: the first is the index (green), the second the work tree (amber, red for deletions), `·` meaning unchanged and `??` untracked.

| Key | Action |
| --- | --- |
| `space` | stage or unstage the file, or everything under a directory |
| `a` | stage everything, or unstage everything when it is all staged |
| `c` | commit what is staged |
| `d` | discard changes (always asks first; untracked files are deleted) |
| `enter` | fold or unfold a directory, or jump into the patch |
| `` ` `` | switch between tree and flat list |
| `-` `=` | fold or unfold every directory |
| `J` `K` | scroll the patch without leaving the list |

Committing runs `git commit` in the background so your hooks run as usual and a failing hook shows its message. If you sign commits (`commit.gpgsign`), sprout hands the terminal over for the commit so your passphrase prompt works, then comes back.

In the Patch pane, `j` `k` scroll, `ctrl+d` / `ctrl+u` page, `g` / `G` jump to the top or bottom, and `h` / `l` pan long lines.

## Agent tab

`[` and `]` switch the fourth pane between **Patch** and **Agent**. The Agent tab is a live view of the selected worktree's agent pane in tmux, so you can watch it work without attaching. Press `enter` there to attach when it needs you. The tab only reads the pane; it never types into it or resizes it.

## Mouse

Click a pane to focus it, click a row to select it, and scroll with the wheel.

## Other keys

| Key | Action |
| --- | --- |
| `R` | refresh everything |
| `?` | keys for the current pane |
| `q` or `ctrl+c` | quit |

Press `enter` in the top pane to switch to another repository next to this one.
