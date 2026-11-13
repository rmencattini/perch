# perch

One screen to manage several Claude Code sessions.
Each session is a window in the tmux session `perch`, started as
`nono run --name <session> --profile <p> -- claude`.

## Install

1. `go build -o ~/.local/bin/perch ./cmd/perch`
2. Run `perch` in a plain terminal, or inside tmux. Inside tmux, `enter` opens the session in a popup, and your prefix + `d` closes only the popup.

Needs `tmux` and `nono` on your PATH, and `fzf` for the folder picker. Works on macOS and Linux.
Optional: `secret-tool` (libsecret) on Linux, only for the DeepSeek balance.

## Config

Optional file: `~/.config/perch/config.json` (`$XDG_CONFIG_HOME/perch/config.json` when set). Keys you leave out keep the defaults in [`internal/config/config.default.json`](internal/config/config.default.json), which is built into the binary. Copy that file as a template.

| Key | Default | What |
|---|---|---|
| `default_profile` | `""` | Profile preselected in the new-session dialog. Empty: the first nono profile with `claude` in its name |
| `git_root` | `~/git` | Repos listed by the fzf folder picker |
| `worktree_root` | `<git_root>/worktrees` | Where the Worktree option creates `<repo>/<branch>` |
| `deepseek.enabled` | `false` | Show the DeepSeek balance in the header |
| `deepseek.keychain_service` | `perch` | Keyring item service of the DeepSeek key |
| `deepseek.keychain_account` | `deepseek_api_key` | Keyring item account |

`config.json` is in `.gitignore`: keep your own copy out of the repo.

## Keys

| Key | Does |
|---|---|
| `j` `k` / arrows | move |
| `enter` | attach (your tmux prefix + `d` returns to perch; in tmux it opens a popup). |
| `n` | new session dialog |
| `K` | kill: asks, then `nono stop <name>` and drops the window. Only this stops a session |
| `tab` | switch the panel between Preview and Diff |
| `b` | show / hide the panel |
| `q` | quit (sessions keep running) |

Mouse: click a row to select it.

## Panel: Preview and Diff

The right panel has two tabs. **Preview** is the live pane. **Diff** is `git diff HEAD` of the session folder plus its untracked files (first 400 lines).
The panel opens on **Preview**. `tab` switches to **Diff** and back.
A folder that is not a git repo shows "no changes".

Status symbols (readable without color):
`●` running · `!` needs input · `○` idle · `◌` detached · `◐` attached in another terminal tab

## Existing and detached sessions

perch lists every running nono session (`nono ps --json`), not only its own tabs.
A `◌ detached` row has no tab yet: `enter` runs `nono attach <name>` in a new tab and attaches.
A `◐ other tab` row is held by another terminal: `enter` runs `nono detach <name>` there first (that terminal gets its shell back), then attaches here.
`K` on it runs `nono stop`. If a session does not show up, run `perch ps` and look at what it reads.

The key to come back is **your own tmux prefix** (shown in the key bar and in the popup title, e.g. `ctrl-a d`).

## New session dialog

- **Name**: letters, digits, `. _ -`. Must be unique.
- **Folder**: `ctrl+f` opens **fzf** over your recent folders and the git repos under `git_root`. It also opens by itself when you tab onto an empty Folder. You can still type a path (`~` works, `tab` completes). Needs `fzf` on PATH.
- **Also allow**: more folders for the same session. Type a path and press `enter` to add it (or `ctrl+f` for fzf). `backspace` on empty text removes the last one. Each folder gets `--allow <dir>` on nono and `--add-dir <dir>` on claude. Leave the text empty and press `enter` to create.
- **Profile**: `←` `→`. The list comes from `nono profile list`. Default: see `default_profile` in Config.
- **Worktree**: `space` to tick. Makes `<worktree_root>/<repo>/<branch>` (branch created if missing).
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

## DeepSeek balance (header)

Off by default. Set `"deepseek": { "enabled": true }` in the config file and the header shows `DeepSeek 110.00 CNY` before the usage bars. perch looks for the key in this order:

1. the `DEEPSEEK_API_KEY` environment variable of the shell that starts perch;
2. the system keyring item with the service and account from the config (`deepseek.keychain_service`, `deepseek.keychain_account`):
   - macOS: `security find-generic-password -s <service> -a <account> -w` (macOS may ask once to allow access);
   - Linux: `secret-tool lookup service <service> account <account>`. Store it with `secret-tool store --label=perch service <service> account <account>`.
   Env overrides for the item: `PERCH_DEEPSEEK_KEYCHAIN` (service), `PERCH_DEEPSEEK_KEYCHAIN_ACCOUNT` (account).

- perch asks `GET https://api.deepseek.com/user/balance` (free, uses no credit) at most every 5 minutes, in the background.
- The key is sent only as that request's `Authorization: Bearer` header. It is never printed, logged or saved.
- `DeepSeek: no key` none found · `DeepSeek …` loading · `DeepSeek ?` request failed or 401 · `DeepSeek empty ✗` 402 or `is_available: false`.
- `perch balance` prints where the key was found (never the key) and the balance in words. Run it when the header looks wrong.
- This runs in perch itself, not in the nono sessions, so it needs no `--allow` or network rule.

## Files

| Path | What |
|---|---|
| `~/.config/perch/config.json` | your settings (optional, see Config) |
| `~/.config/perch/state.json` | recent folders, session info. No secrets |
| `~/.cache/perch/usage/` | rate-limit snapshots |
| `<worktree_root>/<repo>/<branch>` | worktrees |

## Develop

```
go vet ./... && go test ./...
```

Tests never touch your tmux (private `tmux -L` sockets) and never run real `nono` or `claude`.
