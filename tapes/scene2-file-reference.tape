# Scene 2 -- include a file
# Shows: @filename inline syntax -- reference files directly in the message

Output scene2-file-reference.gif

Require llm-cli

Set Shell "zsh"
Set FontSize 16
Set Width 854
Set Height 480
Set Padding 0
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms

Type "llm-cli 'what does the streamCompletion function do in @main.go, one sentence'"
Enter
Sleep 4s
