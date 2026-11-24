Output docs/assets/demo-v03.gif

Set Shell "powershell"
Set FontSize 15
Set Width 1300
Set Height 720
Set Padding 20
Set Theme { "name": "Larch", "background": "#0B0D0C", "foreground": "#ECEAE6", "cursor": "#DE8B4B" }

Hide
Type '(Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue).OwningProcess | ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }'
Enter
Sleep 500ms
Type '$srv = Start-Process -FilePath "go" -ArgumentList "run ./examples/mock_server.go -addr :8080" -RedirectStandardOutput "$env:TEMP\tensgo-mock-out.log" -RedirectStandardError "$env:TEMP\tensgo-mock-err.log" -WindowStyle Hidden -PassThru; for ($i=0; $i -lt 30; $i++) { try { if ((Invoke-WebRequest -Uri "http://127.0.0.1:8080/health" -UseBasicParsing -TimeoutSec 1).StatusCode -eq 200) { break } } catch {}; Start-Sleep -Milliseconds 200 }'
Enter
Sleep 3s
Type '.\tensgo.exe -u http://localhost:8080/health -c 5 -d 1s --no-tui --output-format json --output-file tensgo-reports/baseline.json *> $null; Clear-Host'
Enter
Sleep 2.5s
Show

Type '.\tensgo.exe import curl --out - curl -X POST https://api.example.com/v1/orders -H "Authorization: Bearer token" -d "{\"item\":\"pro\"}"'
Enter
Sleep 2.5s

Type '.\tensgo.exe --scenario ./examples/stateful-crud-assertions.yaml --baseline tensgo-reports/baseline.json --max-p95-regression 50% --output-format json,junit,markdown --no-tui'
Enter
Sleep 11.5s

Type 'Get-Content tensgo-reports/tensgo-report.md'
Enter
Sleep 3.5s

Hide
Type '(Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue).OwningProcess | ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }; Remove-Item -Recurse -Force tensgo-reports -ErrorAction SilentlyContinue'
Enter
Sleep 500ms
Show
