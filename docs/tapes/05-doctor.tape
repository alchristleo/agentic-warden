# Quickstart step 5. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/05-doctor.tape
Output docs/assets/05-doctor.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20
Set MarginFill "#0a0a0a"
Set FontSize 14
Set Width 1100
Set Height 700
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 5"
Enter
Wait+Screen@240s /prep done/
Sleep 3s
Type "clear"
Enter
Sleep 300ms
Show

Type "cd .."
Enter
Sleep 600ms
Type `for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done`
Enter
Sleep 600ms
Type "echo FAKE=1 > payments-api/.env"
Enter
Sleep 600ms
Type "echo FAKE=1 > sandbox/.env"
Enter
Sleep 600ms
Type "cd payments-api"
Enter
Sleep 600ms
Type "clear"
Enter
Sleep 600ms
Type "aw doctor"
Enter
Sleep 4s
Type "clear"
Enter
Sleep 600ms
Type `settings=$(aw doctor | sed -n 's/^  file: *//p')`
Enter
Sleep 600ms
Type `cat "$settings"`
Enter
Sleep 4s
