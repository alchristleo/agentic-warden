# Hero clip for docs/site/index.md: the step 6 curl command refused in payments-api, then run in sandbox.
# Needs a logged-in Claude Code, so CI does not render it. See docs/tapes/README.md.
# Run from the repository root: AW_MANUAL=1 AW_REPO=$PWD vhs docs/tapes/manual/hero.tape
Output docs/assets/hero.gif
Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 500
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 6"
Enter
Wait+Screen@240s /prep done/
Show

Type `aw claude -p "Run: curl -sI https://example.com and show the status line" --allowedTools "Bash(curl *)"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 3s
Type "clear"
Enter
Type "cd ../sandbox"
Enter
Type `aw claude -p "Run: curl -sI https://example.com and show the status line" --allowedTools "Bash(curl *)"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 4s

Hide
Type "kill $awd_pid"
Enter
Sleep 1s
