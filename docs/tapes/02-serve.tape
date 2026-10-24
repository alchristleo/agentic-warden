# Quickstart step 2. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/02-serve.tape
Output docs/assets/02-serve.gif
Set Shell bash
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 8
Set Margin 20
Set MarginFill "#0a0a0a"
Set FontSize 16
Set Width 1000
Set Height 400
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 2"
Enter
Wait+Screen@240s /prep done/
Sleep 3s
Type "clear"
Enter
Sleep 300ms
Show

Type "export AWD_ADDR=127.0.0.1:8080"
Enter
Sleep 600ms
Type "export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)"
Enter
Sleep 600ms
Type "awd serve &"
Enter
Sleep 1s
Type "awd_pid=$!"
Enter
Sleep 600ms
Type "sleep 1"
Enter
Sleep 3s
