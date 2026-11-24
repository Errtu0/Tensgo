$ErrorActionPreference = "Stop"

$TargetHost = "127.0.0.1:9090"
$TargetURL = "http://$TargetHost/json"
$HealthURL = "http://$TargetHost/health"
$Duration = "10s"
$CooldownSec = 5
$VUSets = @(50, 250)

function Invoke-MonitoredProcess {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string]$ArgumentList
    )
    $StdOutFile = Join-Path $env:TEMP ("bench-out-" + [guid]::NewGuid().ToString() + ".log")
    $StdErrFile = Join-Path $env:TEMP ("bench-err-" + [guid]::NewGuid().ToString() + ".log")

    $Proc = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList `
        -RedirectStandardOutput $StdOutFile -RedirectStandardError $StdErrFile `
        -PassThru -NoNewWindow

    $PeakRSS = 0L
    while (-not $Proc.HasExited) {
        try {
            $Proc.Refresh()
            if ($Proc.PeakWorkingSet64 -gt $PeakRSS) {
                $PeakRSS = $Proc.PeakWorkingSet64
            } elseif ($Proc.WorkingSet64 -gt $PeakRSS) {
                $PeakRSS = $Proc.WorkingSet64
            }
        } catch {}
        Start-Sleep -Milliseconds 50
    }
    $Proc.WaitForExit()

    $OutText = if (Test-Path $StdOutFile) { Get-Content $StdOutFile -Raw -ErrorAction SilentlyContinue } else { "" }
    $ErrText = if (Test-Path $StdErrFile) { Get-Content $StdErrFile -Raw -ErrorAction SilentlyContinue } else { "" }
    Remove-Item $StdOutFile, $StdErrFile -Force -ErrorAction SilentlyContinue

    return @{
        PeakMB = [math]::Round($PeakRSS / 1MB, 1)
        StdOut = [string]$OutText
        StdErr = [string]$ErrText
    }
}

function Get-K6MetricVal {
    param($MetricObj, [string]$Prop)
    if ($null -eq $MetricObj) { return $null }
    if ($null -ne $MetricObj.PSObject.Properties["values"] -and $null -ne $MetricObj.values) {
        return $MetricObj.values.$Prop
    }
    return $MetricObj.$Prop
}

Write-Host "==> Building tensgo.exe (release flags)..." -ForegroundColor Cyan
go build -trimpath -ldflags="-s -w" -o tensgo.exe .
$TensgoSizeMB = [math]::Round((Get-Item .\tensgo.exe).Length / 1MB, 2)

Write-Host "==> Starting benchmark target server on $TargetHost..." -ForegroundColor Cyan
$ServerProc = Start-Process -FilePath "go" -ArgumentList "run ./benchmarks/target_server.go -addr $TargetHost" -PassThru -NoNewWindow

try {
    $Ready = $false
    for ($i = 0; $i -lt 40; $i++) {
        try {
            $resp = Invoke-WebRequest -Uri $HealthURL -UseBasicParsing -TimeoutSec 1
            if ($resp.StatusCode -eq 200) {
                $Ready = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 200
    }
    if (-not $Ready) {
        throw "Target server did not become ready at $HealthURL"
    }

    $Results = @()
    $FirstRun = $true

    foreach ($VUs in $VUSets) {
        # 1. tensgo
        if (-not $FirstRun) {
            Write-Host "    [Cooldown ${CooldownSec}s for TCP TIME_WAIT sockets...]" -ForegroundColor DarkGray
            Start-Sleep -Seconds $CooldownSec
        }
        $FirstRun = $false

        Write-Host "==> Running tensgo ($VUs VUs, $Duration)..." -ForegroundColor Yellow
        $JsonOut = Join-Path $env:TEMP "tensgo-bench-$VUs.json"
        if (Test-Path $JsonOut) { Remove-Item $JsonOut -Force }

        $RunRes = Invoke-MonitoredProcess -FilePath ".\tensgo.exe" `
            -ArgumentList "-u $TargetURL -c $VUs -d $Duration --no-tui --output-format json --output-file `"$JsonOut`""

        if (Test-Path $JsonOut) {
            $Report = Get-Content $JsonOut -Raw | ConvertFrom-Json
            $TotalReqs = [int]$Report.metrics.total_requests
            $ErrCnt = [int]$Report.metrics.error_count
            $ErrPct = [double]$Report.metrics.error_rate_pct
            $Results += [PSCustomObject]@{
                Tool       = "tensgo"
                VUs        = $VUs
                Requests   = $TotalReqs
                Errors     = ("{0} ({1:N2}%)" -f $ErrCnt, $ErrPct)
                RPS        = [math]::Round([double]$Report.metrics.rps, 1)
                P50_ms     = [math]::Round([double]$Report.metrics.latency.p50_ms, 2)
                P95_ms     = [math]::Round([double]$Report.metrics.latency.p95_ms, 2)
                P99_ms     = [math]::Round([double]$Report.metrics.latency.p99_ms, 2)
                PeakRSS_MB = $RunRes.PeakMB
                Binary_MB  = $TensgoSizeMB
            }
            Remove-Item $JsonOut -Force -ErrorAction SilentlyContinue
        }

        # 2. hey
        $HeyCmd = Get-Command hey -ErrorAction SilentlyContinue
        if ($HeyCmd) {
            Write-Host "    [Cooldown ${CooldownSec}s for TCP TIME_WAIT sockets...]" -ForegroundColor DarkGray
            Start-Sleep -Seconds $CooldownSec

            Write-Host "==> Running hey ($VUs VUs, $Duration)..." -ForegroundColor Yellow
            $HeySizeMB = [math]::Round((Get-Item $HeyCmd.Source).Length / 1MB, 2)
            $HeyRun = Invoke-MonitoredProcess -FilePath $HeyCmd.Source -ArgumentList "-z $Duration -c $VUs $TargetURL"
            $HeyOut = $HeyRun.StdOut + "`n" + $HeyRun.StdErr

            $HeyRPS = if ($HeyOut -match "Requests/sec:\s+([0-9.]+)") { [math]::Round([double]$Matches[1], 1) } else { 0 }
            $HeyP50 = if ($HeyOut -match "50%\s+in\s+([0-9.]+)\s+secs") { [math]::Round([double]$Matches[1] * 1000.0, 2) } else { "-" }
            $HeyP95 = if ($HeyOut -match "95%\s+in\s+([0-9.]+)\s+secs") { [math]::Round([double]$Matches[1] * 1000.0, 2) } else { "-" }
            $HeyP99 = if ($HeyOut -match "99%\s+in\s+([0-9.]+)\s+secs") { [math]::Round([double]$Matches[1] * 1000.0, 2) } else { "-" }

            $Ok200 = if ($HeyOut -match "\[200\]\s+([0-9]+)\s+responses") { [int]$Matches[1] } else { 0 }
            $TotalStatus = 0
            foreach ($m in [regex]::Matches($HeyOut, "\[[0-9]{3}\]\s+([0-9]+)\s+responses")) {
                $TotalStatus += [int]$m.Groups[1].Value
            }
            $NetErrs = 0
            if ($HeyOut -match "(?s)Error distribution:(.*)$") {
                foreach ($em in [regex]::Matches($Matches[1], "\[[0-9]+\]\s+")) {
                    $NetErrs += [int]($em.Value -replace "[^0-9]", "")
                }
            }
            $HeyTotal = $TotalStatus + $NetErrs
            $HeyErrs = ($TotalStatus - $Ok200) + $NetErrs
            $HeyErrPct = if ($HeyTotal -gt 0) { ($HeyErrs / $HeyTotal) * 100.0 } else { 0.0 }

            $Results += [PSCustomObject]@{
                Tool       = "hey"
                VUs        = $VUs
                Requests   = $HeyTotal
                Errors     = ("{0} ({1:N2}%)" -f $HeyErrs, $HeyErrPct)
                RPS        = $HeyRPS
                P50_ms     = $HeyP50
                P95_ms     = $HeyP95
                P99_ms     = $HeyP99
                PeakRSS_MB = $HeyRun.PeakMB
                Binary_MB  = $HeySizeMB
            }
        }

        # 3. k6
        $K6Cmd = Get-Command k6 -ErrorAction SilentlyContinue
        if ($K6Cmd) {
            Write-Host "    [Cooldown ${CooldownSec}s for TCP TIME_WAIT sockets...]" -ForegroundColor DarkGray
            Start-Sleep -Seconds $CooldownSec

            Write-Host "==> Running k6 ($VUs VUs, $Duration)..." -ForegroundColor Yellow
            $K6SizeMB = [math]::Round((Get-Item $K6Cmd.Source).Length / 1MB, 2)
            $K6Script = Join-Path $env:TEMP "tensgo-k6-bench.js"
            "import http from 'k6/http'; export default function () { http.get('$TargetURL'); }" | Set-Content -Path $K6Script -Encoding UTF8
            $K6Summary = Join-Path $env:TEMP "tensgo-k6-summary-$VUs.json"
            if (Test-Path $K6Summary) { Remove-Item $K6Summary -Force }

            $K6Args = "run --quiet --vus $VUs --duration $Duration --summary-trend-stats=`"min,avg,med,p(90),p(95),p(99),max`" --summary-export=`"$K6Summary`" `"$K6Script`""
            $K6Run = Invoke-MonitoredProcess -FilePath $K6Cmd.Source -ArgumentList $K6Args

            if (Test-Path $K6Summary) {
                $K6Data = Get-Content $K6Summary -Raw | ConvertFrom-Json
                $ReqsObj = $K6Data.metrics.http_reqs
                $DurObj = $K6Data.metrics.http_req_duration
                $FailObj = $K6Data.metrics.http_req_failed

                $K6Total = [int](Get-K6MetricVal $ReqsObj "count")
                $K6RPS = [math]::Round([double](Get-K6MetricVal $ReqsObj "rate"), 1)
                $K6P50 = [math]::Round([double](Get-K6MetricVal $DurObj "med"), 2)
                $K6P95 = [math]::Round([double](Get-K6MetricVal $DurObj "p(95)"), 2)
                $K6P99 = [math]::Round([double](Get-K6MetricVal $DurObj "p(99)"), 2)

                # In k6, http_req_failed has 'passes' = number of failed requests (where condition failed==true) or 'value' = failure rate [0..1]
                $K6FailRate = [double](Get-K6MetricVal $FailObj "value") * 100.0
                $K6FailCount = [int][math]::Round(($K6FailRate / 100.0) * $K6Total)

                $Results += [PSCustomObject]@{
                    Tool       = "k6"
                    VUs        = $VUs
                    Requests   = $K6Total
                    Errors     = ("{0} ({1:N2}%)" -f $K6FailCount, $K6FailRate)
                    RPS        = $K6RPS
                    P50_ms     = $K6P50
                    P95_ms     = $K6P95
                    P99_ms     = $K6P99
                    PeakRSS_MB = $K6Run.PeakMB
                    Binary_MB  = $K6SizeMB
                }
                Remove-Item $K6Summary -Force -ErrorAction SilentlyContinue
            }
            Remove-Item $K6Script -Force -ErrorAction SilentlyContinue
        }
    }

    Write-Host "`n================ Fair Benchmark Comparison (HTTP Keep-Alive Enabled) ================" -ForegroundColor Green
    $Results | Format-Table -AutoSize
} finally {
    if ($ServerProc -and -not $ServerProc.HasExited) {
        Stop-Process -Id $ServerProc.Id -Force -ErrorAction SilentlyContinue
    }
}
