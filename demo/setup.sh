#!/bin/sh
# Fake world for recording the demo. Use `source`, not `sh`, so the exports stay
# in your shell:
#
#   source demo/setup.sh && perch        # try it by hand
#   perch_demo_cleanup                   # when done
#
# Nothing here touches your real config, tmux server, nono or claude:
#   - XDG_* point to a temp dir (no real config.json, state.json, usage files)
#   - TMUX_TMPDIR gives perch a private tmux server
#   - fake `nono` and `claude` come first on PATH
# No `set -e` on purpose: this file is sourced, a failure must not close your shell.

# /tmp, not $TMPDIR: tmux socket paths are limited to about 100 characters.
PERCH_DEMO=$(mktemp -d /tmp/perch-demo.XXXXXX)
export PERCH_DEMO
export XDG_CONFIG_HOME=$PERCH_DEMO/config
export XDG_CACHE_HOME=$PERCH_DEMO/cache
export TMUX_TMPDIR=$PERCH_DEMO/tmux
export PATH=$PERCH_DEMO/bin:$PATH
unset TMUX DEEPSEEK_API_KEY
mkdir -p "$PERCH_DEMO/bin" "$PERCH_DEMO/code" "$PERCH_DEMO/panes" "$TMUX_TMPDIR" "$XDG_CONFIG_HOME/perch"

# --- fake repos, each with an uncommitted change for the Diff tab -------------
_demo_repo() { # name file
  r=$PERCH_DEMO/code/$1
  mkdir -p "$r"
  git init -q "$r"
  printf 'package %s\n\nfunc Run() error {\n\treturn nil\n}\n' "$1" > "$r/$2"
  git -C "$r" add . >/dev/null
  git -C "$r" -c commit.gpgsign=false -c user.name=demo -c user.email=demo@example.com commit -qm "initial commit"
  printf 'package %s\n\nfunc Run() error {\n\tif err := validate(); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n' "$1" > "$r/$2"
  echo "# notes" > "$r/NOTES.md"
}
_demo_repo webapp login.go
_demo_repo billing invoice.go
_demo_repo docs readme.go

# --- perch config and state ---------------------------------------------------
cat > "$XDG_CONFIG_HOME/perch/config.json" <<EOF
{ "default_profile": "demo/claude", "git_root": "$PERCH_DEMO/code" }
EOF

NOW=$(date +%s)
cat > "$XDG_CONFIG_HOME/perch/state.json" <<EOF
{
  "recent": ["$PERCH_DEMO/code/webapp", "$PERCH_DEMO/code/billing", "$PERCH_DEMO/code/docs"],
  "sessions": {
    "fix-login":       { "dir": "$PERCH_DEMO/code/webapp",  "profile": "demo/claude", "command": "nono run --name fix-login --profile demo/claude -- claude",       "created": $NOW },
    "refactor-billing": { "dir": "$PERCH_DEMO/code/billing", "profile": "demo/claude", "command": "nono run --name refactor-billing --profile demo/claude -- claude", "created": $NOW },
    "docs":            { "dir": "$PERCH_DEMO/code/docs",    "profile": "demo/claude", "command": "nono run --name docs --profile demo/claude -- claude",            "created": $NOW }
  }
}
EOF

# --- pane text: each one trips a different status regex -----------------------
cat > "$PERCH_DEMO/panes/fix-login.txt" <<'EOF'
> Reading internal/auth/session.go
> Editing internal/auth/session.go
> Running go test ./...
  ok  internal/auth  0.31s

✢ Sublimating… (13s · ↓ 820 tokens · esc to interrupt)
EOF
cat > "$PERCH_DEMO/panes/refactor-billing.txt" <<'EOF'
> Reading invoice.go

 Do you want to make this edit to invoice.go?
 ❯ 1. Yes
   2. Yes, allow all edits during this session
   3. No, and tell Claude what to do differently
EOF
cat > "$PERCH_DEMO/panes/docs.txt" <<'EOF'
> Updated README.md with the new install steps.

Anything else you want me to change?

>
EOF

# --- fake claude: shows a banner, then waits ----------------------------------
cat > "$PERCH_DEMO/bin/claude" <<'EOF'
#!/bin/sh
echo "> Hello! What should we work on?"
exec sleep 100000
EOF

# --- fake nono ----------------------------------------------------------------
# ps: no sessions beyond the tmux windows (no "detached" row). profile list: one profile.
# run: skip nono's own flags, exec what follows `--`. Anything else: do nothing.
cat > "$PERCH_DEMO/bin/nono" <<'EOF'
#!/bin/sh
case "$1" in
  ps)
    echo '[]' ;;
  profile)
    echo '[{"name":"demo/claude","source":"user","description":"","extends":[]}]' ;;
  run)
    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
    shift
    exec "$@" ;;
esac
EOF
chmod +x "$PERCH_DEMO/bin/claude" "$PERCH_DEMO/bin/nono"

# --- the tmux windows perch lists (private server, default config) ------------
for n in fix-login refactor-billing docs; do
  d=$PERCH_DEMO/code/webapp; [ "$n" = refactor-billing ] && d=$PERCH_DEMO/code/billing; [ "$n" = docs ] && d=$PERCH_DEMO/code/docs
  if tmux has-session -t =perch 2>/dev/null; then
    tmux new-window -d -t =perch: -n "$n" -c "$d" "cat '$PERCH_DEMO/panes/$n.txt'; exec sleep 100000"
  else
    tmux -f /dev/null new-session -d -x 200 -y 50 -s perch -n "$n" -c "$d" "cat '$PERCH_DEMO/panes/$n.txt'; exec sleep 100000"
  fi
done

perch_demo_cleanup() {
  tmux kill-server 2>/dev/null   # only the private server: TMUX_TMPDIR is still set
  rm -rf "$PERCH_DEMO"
  unset PERCH_DEMO XDG_CONFIG_HOME XDG_CACHE_HOME TMUX_TMPDIR
}
