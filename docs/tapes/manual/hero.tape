# Hero clip for docs/site/index.md: the step 6 curl command refused in payments-api, then run in sandbox.
# Needs a logged-in Claude Code, so CI does not render it. See docs/tapes/README.md.
# Run from the repository root: AW_MANUAL=1 AW_REPO=$PWD vhs docs/tapes/manual/hero.tape
Output docs/assets/hero.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20
Set MarginFill "#0a0a0a"
Set FontSize 18
Set Width 1000
Set Height 460
Set TypingSpeed 20ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 6"
Enter
Wait+Screen@240s /prep done/
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

Type "# policy: deny Bash(curl *) in repos matching github.com/acme/payments*"
Enter
Sleep 1500ms
Type `aw claude -p "Run: curl -sI https://example.com and show the status line. If a tool is denied, say so in one line and stop." --allowedTools "Bash(curl *)"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 2s
Type "cd ../sandbox"
Enter
Sleep 400ms
Type `aw claude -p "Run: curl -sI https://example.com and show the status line. If a tool is denied, say so in one line and stop." --allowedTools "Bash(curl *)"`
Enter
Wait+Line@180s /\$\s*$/
Sleep 2s

Hide
Type "kill $awd_pid"
Enter
Sleep 1s
