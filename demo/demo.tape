# Record from the repo root: vhs demo/demo.tape
# Needs perch on PATH. Everything runs in the fake world of demo/setup.sh.
Output demo/out.gif

Set Shell zsh
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 16

Hide
Type "source demo/setup.sh 2>/dev/null && clear" Enter
Sleep 4s
Type "perch" Enter
Sleep 1s
Show

# Preview of each session
Sleep 3s
Type "j"
Sleep 2s
Type "j"
Sleep 2s
Type "k"
Sleep 1s

# Diff tab (uncommitted change + untracked file of the folder)
Tab
Sleep 4s
Type "j"
Sleep 3s
Tab
Sleep 1.5s

# Attach, then leave with the tmux prefix (default ctrl-b d on the private server)
Enter
Sleep 3s
Ctrl+b
Type "d"
Sleep 2s

# New session: the folder is prefilled with the last one used
Type "n"
Sleep 2s
Type "add-tests"
Sleep 2s
Enter
Sleep 4s

Type "q"
Sleep 1s

Hide
Type "perch_demo_cleanup" Enter
