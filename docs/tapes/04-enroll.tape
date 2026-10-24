# Quickstart step 4. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/04-enroll.tape
Output docs/assets/04-enroll.gif
Set Shell bash
Set FontSize 16
Set Width 1000
Set Height 500
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 4"
Enter
Wait+Screen@240s /prep done/
Show

Type "export AW_SYNC_STATE_DIR=~/.aw-try"
Enter
Type "token=$(awd enroll-token you@acme.com)"
Enter
Type "AW_SYNC_TOKEN=$token aw-sync enroll --server http://localhost:8080 --agents claude --state-dir ~/.aw-try"
Enter
Sleep 2s
Type "aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude"
Enter
Sleep 3s
