# perch

One screen to manage several Claude Code sessions.
Each session is a window in the tmux session `perch`, started as
`nono run --name <session> --profile <p> -- claude`.

## Install

1. `go build -o ~/.local/bin/perch ./cmd/perch`
2. Run `perch` in a plain terminal, or inside tmux. Inside tmux, `enter` opens the session in a popup, and your prefix + `d` closes only the popup.

Needs `tmux` and `nono` on your PATH.

## Keys

| Key | Does |
|---|---|
| `j` `k` / arrows | move |
| `enter` | attach (your tmux prefix + `d` returns to perch; in tmux it opens a popup). On a closed row: reopen + attach |
| `n` | new session dialog |
| `x` | close the tab. The session **keeps running** |
| `o` | reopen the last closed tab |
| `K` | kill: asks, then `nono stop <name>` and drops the window. Only this stops a session |
| `tab` | switch the panel between Preview and Diff |
| `b` | show / hide the panel |
| `q` | quit (sessions keep running) |

Mouse: click a row to select it.

## Panel: Preview and Diff

The right panel has two tabs. **Preview** is the live pane. **Diff** is `git diff HEAD` of the session folder plus its untracked files (first 400 lines).
A session that is idle or whose tab is closed opens on **Diff**, since its pane has nothing new. A running one opens on Preview. `tab` overrides this.
A folder that is not a git repo shows "no changes".

Status symbols (readable without color):
`●` running · `!` needs input · `○` idle · `⊘` tab closed · `◌` detached · `◐` attached in another terminal tab

## Existing and detached sessions

perch lists every running nono session (`nono ps --json`), not only its own tabs.
A `◌ detached` row has no tab yet: `enter` runs `nono attach <name>` in a new tab and attaches.
A `◐ other tab` row is held by another terminal: `enter` runs `nono detach <name>` there first (that terminal gets its shell back), then attaches here.
`K` on it runs `nono stop`. If a session does not show up, run `perch ps` and look at what it reads.

The key to come back is **your own tmux prefix** (shown in the key bar and in the popup title, e.g. `ctrl-a d`).

## New session dialog

- **Name**: letters, digits, `. _ -`. Must be unique.
- **Folder**: `ctrl+f` opens **fzf** over your recent folders and the git repos under `~/git`. It also opens by itself when you tab onto an empty Folder. You can still type a path (`~` works, `tab` completes). Needs `fzf` on PATH.
- **Also allow**: more folders for the same session. Type a path and press `enter` to add it (or `ctrl+f` for fzf). `backspace` on empty text removes the last one. Each folder gets `--allow <dir>` on nono and `--add-dir <dir>` on claude. Leave the text empty and press `enter` to create.
- **Profile**: `←` `→`. The list comes from `nono profile list`. Default `nolabs-ai/claude`.
- **Worktree**: `space` to tick. Makes `~/git/worktrees/<repo>/<branch>` (branch created if missing).
- The exact command is shown before you press `enter`. That is what runs.

## Usage bars (statusLine)

The header shows the 5h and 7d rate limits. Claude Code has to feed them to perch.
Add this to `~/.claude/settings.json` **yourself** (perch never edits it):

```json
"statusLine": { "type": "command", "command": "perch statusline" }
```

Already have a statusLine? Keep it and let perch listen:

```json
"statusLine": { "type": "command", "command": "perch statusline --wrap '<your command>'" }
```

- `perch statusline` writes `~/.cache/perch/usage/<session_id>.json` and prints `5h 42% · 7d 10%`.
- No data, or a window that has already reset, shows `unknown` with an empty gray bar. Never `0%`.
- Sessions run inside nono. perch adds `--allow ~/.cache/perch/usage` and `--read <folder of the perch binary>`
  to the command, so the script can run and write. A profile that denies them breaks the bars.

## Files

| Path | What |
|---|---|
| `~/.config/perch/state.json` | closed tabs, recent folders, session info. No secrets |
| `~/.cache/perch/usage/` | rate-limit snapshots |
| `~/git/worktrees/<repo>/<branch>` | worktrees |

## Develop

```
go vet ./... && go test ./...
```

Tests never touch your tmux (private `tmux -L` sockets) and never run real `nono` or `claude`.
