# Scene 6 -- pipe output (~10s)
# Shows: no TTY means no markdown wrapper, output is clean plain text

Output scene6-pipe-output.gif

Require llm-cli

Set Shell "zsh"
Set FontSize 16
Set Width 854
Set Height 480
Set Padding 0
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms

Type "llm-cli 'write a .gitignore for a Go project, no explanation' > .gitignore"
Enter
Sleep 2s

Type "cat .gitignore"
Enter
Sleep 2s
