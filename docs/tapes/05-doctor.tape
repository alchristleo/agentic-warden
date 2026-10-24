# Quickstart step 5. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/05-doctor.tape
Output docs/assets/05-doctor.gif
Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 700
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 5"
Enter
Wait+Screen@240s /prep done/
Show

Type "cd .."
Enter
Type `for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done`
Enter
Type "echo FAKE=1 > payments-api/.env"
Enter
Type "echo FAKE=1 > sandbox/.env"
Enter
Type "cd payments-api"
Enter
Type "clear"
Enter
Type "aw doctor"
Enter
Sleep 4s
Type "clear"
Enter
Type `settings=$(aw doctor | sed -n 's/^  file: *//p')`
Enter
Type `cat "$settings"`
Enter
Sleep 4s
