Set Shell "powershell"
Set FontSize 15
Set Width 1100
Set Height 680
Set Padding 20
Set Theme { "name": "Larch", "background": "#0B0D0C", "foreground": "#ECEAE6", "cursor": "#DE8B4B" }

Type ".\tensgo.exe"
Enter
Sleep 1.5s

Down
Sleep 400ms
Down
Sleep 600ms
Enter
Sleep 1s

Type "http://localhost:8080/health"
Sleep 400ms
Tab
Sleep 300ms
Tab
Sleep 300ms
Ctrl+E
Ctrl+U
Type "100"
Sleep 400ms
Enter

Sleep 17s
Screenshot docs/assets/chaos-report.png
Sleep 1s
