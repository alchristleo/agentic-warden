# Quickstart step 1. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/01-build.tape
Output docs/assets/01-build.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20
Set MarginFill "#0a0a0a"
Set FontSize 16
Set Width 1000
Set Height 500
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 1"
Enter
Wait+Screen@300s /prep done/
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

Type "git clone https://github.com/alchristleo/agentic-warden.git"
Enter
Sleep 1s
Type "cd agentic-warden"
Enter
Sleep 400ms
Type "REL=https://github.com/alchristleo/agentic-warden/releases"
Enter
Sleep 400ms
Type 'curl -fsSL $REL/latest/download/install.sh \'
Alt+Enter
Type "  | sh"
Enter
Wait+Line@60s /\$\s*$/
Type 'export PATH="$HOME/.local/bin:$PATH"'
Enter
Sleep 400ms
Type 'git checkout -q "$(aw version)"'
Enter
Sleep 600ms
Type "ls ~/.local/bin/aw*"
Enter
Sleep 2s

Hide
Type "kill $aw_srv"
Enter
Sleep 300ms
