# Scene 4 -- interactive session with @file references 
# Shows: interactive REPL, @filename loads file context, multi-turn session

Output scene4-interactive.gif

Require llm-cli

Set Shell "zsh"
Set FontSize 16
Set Width 854
Set Height 480
Set Padding 0
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms

Hide
Type "cd ..;clear"
Enter
Show

Type "llm-cli"
Enter
Sleep 1s

Type "what does this program do? @main.go"
Enter
Sleep 5s

Type "what would you name the main data type?"
Enter
Sleep 5s
