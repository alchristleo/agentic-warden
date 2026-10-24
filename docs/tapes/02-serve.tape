# Quickstart step 2. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/02-serve.tape
Output docs/assets/02-serve.gif
Set Shell bash
Set FontSize 16
Set Width 1000
Set Height 400
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 2"
Enter
Wait+Screen@240s /prep done/
Show

Type "export AWD_ADMIN_TOKEN=try-me"
Enter
Type "awd serve &"
Enter
Type "awd_pid=$!"
Enter
Type "sleep 1"
Enter
Sleep 3s
