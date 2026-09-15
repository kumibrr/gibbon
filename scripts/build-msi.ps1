# Build the Windows MSI installer for gibbon into dist/.
# Only runs on Windows: WiX (https://wixtoolset.org) does not support other
# platforms. Requires `wix` on PATH (`dotnet tool install --global wix`).
#
# Usage: scripts/build-msi.ps1 -Version v1.2.0
param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = "Stop"

$msiVersion = "0.0.0"
if ($Version -match '(\d+\.\d+\.\d+)') {
    $msiVersion = $Matches[1]
}

$targets = @(
    @{ GoArch = "amd64"; WixArch = "x64" },
    @{ GoArch = "arm64"; WixArch = "arm64" }
)

New-Item -ItemType Directory -Force -Path dist | Out-Null
$checksumLines = @()

foreach ($target in $targets) {
    $goArch = $target.GoArch
    $wixArch = $target.WixArch
    $name = "gibbon_${Version}_windows_${goArch}"
    $stage = "dist/msi-stage-$goArch"
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    $exePath = Join-Path $stage "gibbon.exe"

    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = $goArch
    $ldflags = "-s -w -X github.com/kumibrr/gibbon/internal/cli.version=$Version"
    go build -trimpath -ldflags $ldflags -o $exePath ./cmd/gibbon
    if ($LASTEXITCODE -ne 0) { throw "go build failed for $goArch" }

    $msiPath = "dist/$name.msi"
    wix build packaging/windows/gibbon.wxs `
        -arch $wixArch `
        -d "Version=$msiVersion" `
        -d "ExePath=$exePath" `
        -o $msiPath
    if ($LASTEXITCODE -ne 0) { throw "wix build failed for $goArch" }

    Remove-Item -Recurse -Force $stage

    $hash = (Get-FileHash -Algorithm SHA256 $msiPath).Hash.ToLower()
    $checksumLines += "$hash  $name.msi"
    Write-Host "built $name.msi"
}

$checksumLines -join "`n" | Out-File -FilePath dist/checksums-msi.txt -Encoding ascii
