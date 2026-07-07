# Scene 5 -- resume the session
# Shows: -c picks up exactly where the previous session left off

Output scene5-resume.gif

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
Type `llm-cli -includeFile main.go "what does this program do? one sentence"`
Enter
Sleep 3s
Show
Type "llm-cli -c 'what language is it written in, one word'"
Enter
Sleep 3s
