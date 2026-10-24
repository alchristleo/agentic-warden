# Quickstart step 3. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/03-apply.tape
Output docs/assets/03-apply.gif
Set Shell bash
Set FontSize 16
Set Width 1000
Set Height 500
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 3"
Enter
Wait+Screen@240s /prep done/
Show

Type "cat examples/quickstart.yaml"
Enter
Sleep 3s
Type "awd apply examples/quickstart.yaml"
Enter
Sleep 3s
