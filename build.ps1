$ErrorActionPreference = "Stop"

$DistDir = "dist"
if (!(Test-Path $DistDir)) {
    New-Item -ItemType Directory -Path $DistDir | Out-Null
}

$Targets = @(
    @{ OS = "windows"; ARCH = "amd64"; Out = "$DistDir/tensgo-windows-amd64.exe" },
    @{ OS = "linux";   ARCH = "amd64"; Out = "$DistDir/tensgo-linux-amd64" },
    @{ OS = "linux";   ARCH = "arm64"; Out = "$DistDir/tensgo-linux-arm64" },
    @{ OS = "darwin";  ARCH = "amd64"; Out = "$DistDir/tensgo-darwin-amd64" },
    @{ OS = "darwin";  ARCH = "arm64"; Out = "$DistDir/tensgo-darwin-arm64" }
)

foreach ($Target in $Targets) {
    Write-Host "Building $($Target.Out) ($($Target.OS)/$($Target.ARCH))..."
    $env:GOOS = $Target.OS
    $env:GOARCH = $Target.ARCH
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags="-s -w" -o $Target.Out .
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed for $($Target.OS)/$($Target.ARCH)"
    }
}

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "All binaries built successfully in '$DistDir/'."
