Output docs/tapes/spike/a-ble.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1000
Set Height 500
Set TypingSpeed 40ms
Set Padding 16
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20

Hide
Type ". docs/tapes/lib/prep.sh 1 && . $AW_REPO/docs/tapes/spike/ble.sh"
Enter
Wait+Screen@120s /prep done/
Sleep 3s
Type "clear"
Enter
Sleep 500ms
Show

Type "git clone https://github.com/alchristleo/agentic-warden.git"
Enter
Sleep 1s
Type "cd agentic-warden"
Enter
Type "go build -o bin/ ./cmd/..."
Enter
Wait+Line@180s /\$\s*$/
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "export X=$(echo hi) ; sleep 1 &"
Sleep 800ms
Screenshot docs/tapes/spike/a-mid.png
Enter
Type "ls bin"
Enter
Sleep 2s
