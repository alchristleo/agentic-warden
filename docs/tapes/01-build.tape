# Quickstart step 1. Run from the repository root: AW_REPO=$PWD vhs docs/tapes/01-build.tape
Output docs/assets/01-build.gif
Set Shell bash
Set FontSize 16
Set Width 1000
Set Height 500
Set TypingSpeed 40ms
Set Padding 16

Hide
Type ". docs/tapes/lib/prep.sh 1"
Enter
Wait+Screen@120s /prep done/
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
Type "ls bin"
Enter
Sleep 3s
