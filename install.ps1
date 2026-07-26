#!/usr/bin/env pwsh
param(
    [switch]$ci,
    [string]$version,
    [string]$os,
    [string]$arch,
    [string]$dest,
    [switch]$noPath,
    [switch]$dryRun,
    [switch]$list,
    [switch]$listVersions,
    [switch]$listArtifacts,
    [switch]$help
)

$REPO = "yaso09/tengiz"
$RAW = "https://raw.githubusercontent.com/$REPO/main"

function Detect-OSArch {
    $archStr = [Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
    $archMap = @{ "AMD64" = "amd64"; "ARM64" = "arm64" }
    $mapped = $archMap[$archStr]
    if (-not $mapped) { $mapped = "amd64" }
    return "windows", $mapped
}

function Use-GitHubCLI {
    if (-not (Get-Command "gh" -ErrorAction SilentlyContinue)) { return $null }

    Write-Host ":: Looking for latest CI run via gh..."
    $runId = gh run list --repo $REPO --limit 1 --json databaseId --jq ".[0].databaseId" 2>$null
    if (-not $runId) { return $null }

    $tmpDir = Join-Path $env:TEMP "tengiz-installer-$runId"
    if (Test-Path $tmpDir) { Remove-Item -Recurse -Force $tmpDir }
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    Write-Host ":: Downloading tengiz-installer-windows (run $runId)..."
    $result = gh run download $runId --repo $REPO --name tengiz-installer-windows --dir $tmpDir 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "gh download failed, falling back..."
        return $null
    }

    $binary = Join-Path $tmpDir "tengiz-installer.exe"
    if (-not (Test-Path $binary)) { return $null }

    return $binary
}

function Use-LocalSource {
    $scriptPath = Join-Path $PSScriptRoot "installer" "install.py"
    if (Test-Path $scriptPath) { return $scriptPath }
    return $null
}

function Download-Source {
    $tmpDir = Join-Path $env:TEMP "tengiz-installer-$(Get-Random)"
    $srcDir = Join-Path $tmpDir "installer"
    $pkgDir = Join-Path $srcDir "installer"

    New-Item -ItemType Directory -Path $pkgDir -Force | Out-Null

    $files = @(
        @("installer/install.py", "install.py"),
        @("installer/installer/__init__.py", "installer/__init__.py"),
        @("installer/installer/__main__.py", "installer/__main__.py"),
        @("installer/installer/cli.py", "installer/cli.py"),
        @("installer/installer/core.py", "installer/core.py"),
        @("installer/installer/github.py", "installer/github.py"),
        @("installer/installer/platform.py", "installer/platform.py")
    )

    $wc = New-Object System.Net.WebClient
    foreach ($f in $files) {
        $url = "$RAW/$($f[0])"
        $out = Join-Path $srcDir $f[1]
        Write-Host "  downloading $($f[0])"
        $wc.DownloadFile($url, $out)
    }

    return Join-Path $srcDir "install.py"
}

function Build-Args {
    $argList = @()
    if ($ci) { $argList += "--ci" }
    if ($version) { $argList += "--version"; $argList += $version }
    if ($os) { $argList += "--os"; $argList += $os }
    if ($arch) { $argList += "--arch"; $argList += $arch }
    if ($dest) { $argList += "--dest"; $argList += $dest }
    if ($noPath) { $argList += "--no-path" }
    if ($dryRun) { $argList += "--dry-run" }
    if ($listVersions -or $list) { $argList += "--list" }
    if ($listArtifacts -or ($list -and $ci)) { $argList += "--list-artifacts" }
    return $argList
}

$python = if (Get-Command "python3" -ErrorAction SilentlyContinue) { "python3" }
           elseif (Get-Command "python" -ErrorAction SilentlyContinue) { "python" }
           else { $null }

# Try gh binary first
$binary = Use-GitHubCLI
if ($binary) {
    $installerArgs = Build-Args
    if ($installerArgs.Count -gt 0) {
        & $binary @installerArgs
    } else {
        & $binary
    }
    exit $LASTEXITCODE
}

# Try local Python source
$source = Use-LocalSource
if (-not $source) {
    if (-not $python) {
        Write-Host "No gh, Python, or local source found."
        Write-Host "Install Python or gh CLI (https://cli.github.com/)"
        exit 1
    }
    Write-Host ":: Downloading installer source from GitHub..."
    $source = Download-Source
}

if (-not $python) {
    Write-Host "Python not found."
    exit 1
}

$installerArgs = Build-Args
& $python $source @installerArgs
exit $LASTEXITCODE
