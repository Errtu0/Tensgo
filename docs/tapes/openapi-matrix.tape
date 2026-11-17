Set Shell "powershell"
Set FontSize 15
Set Width 1380
Set Height 720
Set Padding 20
Set Theme { "name": "Larch", "background": "#0B0D0C", "foreground": "#ECEAE6", "cursor": "#DE8B4B" }

Type '.\tensgo.exe --openapi ./examples/03-openapi-demo.json --token "Bearer demo-token-123" -d 2s'
Enter
Sleep 2.5s

Type "a"
Sleep 800ms
Enter

Sleep 16s
Screenshot docs/assets/openapi-matrix.png
Sleep 1s
