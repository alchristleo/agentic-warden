# Quickstart step 5. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/05-doctor.tape
Output docs/assets/05-doctor.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20
Set MarginFill "#0a0a0a"
Set FontSize 16
Set Width 1100
Set Height 700
Set TypingSpeed 20ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 5"
Enter
Wait+Screen@240s /prep done/
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

Type "cd .."
Enter
Sleep 400ms
Type `for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done`
Enter
Sleep 400ms
Type "echo FAKE=1 > payments-api/.env"
Enter
Sleep 400ms
Type "echo FAKE=1 > sandbox/.env"
Enter
Sleep 400ms
Type "cd payments-api"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 400ms
Type "aw doctor"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 400ms
Type `settings=$(aw doctor | sed -n 's/^  file: *//p')`
Enter
Sleep 400ms
Type `cat "$settings"`
Enter
Sleep 2s
