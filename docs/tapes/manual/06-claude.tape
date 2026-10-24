# Quickstart step 6. Needs a logged-in Claude Code, so CI does not render it. See docs/tapes/README.md.
# Run from the repository root: AW_MANUAL=1 AW_REPO=$PWD vhs docs/tapes/manual/06-claude.tape
Output docs/assets/06-claude.gif
Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 700
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
Type "cd ../sandbox"
Enter
Type `aw claude -p "Run: curl -sI https://example.com and show the status line" --allowedTools "Bash(curl *)"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 3s
Type `aw claude -p "Print the contents of .env"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 3s

Hide
Type "kill $awd_pid"
Enter
Sleep 1s
