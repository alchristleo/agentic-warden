# Quickstart step 7, CLI part only (the Claude command is in docs/tapes/manual).
# Run from the repository root: AW_REPO=$PWD vhs docs/tapes/07-reapply.tape
Output docs/assets/07-reapply.gif
Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 700
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 7"
Enter
Wait+Screen@240s /prep done/
Show

Type "cd ../agentic-warden"
Enter
Type `sed 's/quickstart.1/quickstart.2/' examples/quickstart.yaml > quickstart.2.yaml`
Enter
Type `echo '              - Bash(wget *)' >> quickstart.2.yaml`
Enter
Type "awd apply quickstart.2.yaml"
Enter
Sleep 1s
Type "aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude"
Enter
Sleep 2s
Type "cd ../payments-api"
Enter
Type `settings=$(aw doctor | sed -n 's/^  file: *//p')`
Enter
Type `cat "$settings"`
Enter
Sleep 3s
Type "clear"
Enter
Type "kill $awd_pid"
Enter
Type "wait $awd_pid || true"
Enter
Type `aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude || echo "exit status $?"`
Enter
Sleep 2s
Type `settings=$(aw doctor | sed -n 's/^  file: *//p')`
Enter
Type `cat "$settings"`
Enter
Type "aw-sync status --state-dir ~/.aw-try"
Enter
Sleep 4s
