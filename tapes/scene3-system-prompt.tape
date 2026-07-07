# Scene 3 -- system prompt as a tool 
# Shows: turning the CLI into a specialized one-shot tool with a system prompt

Output scene3-system-prompt.gif

Require llm-cli

Set Shell "zsh"
Set FontSize 16
Set Width 854
Set Height 480
Set Padding 0
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

Hide
Type "cd ..;clear"
Enter
Show
Type `alias s="llm-cli -systemMsg 'Reply only with a shell command for zsh on macos, no explanation.' -pretty=false"``
Enter
Type "s list all go files modified in the last week"
Enter
Sleep 2s
Type "function run(){. <(s $@)}"
Enter
Type "run list all go files"
Enter
Sleep 3s
