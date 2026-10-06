param(
    [Parameter(Position=0)]
    [ValidatePattern('^(stable|latest|\d+\.\d+\.\d+(-[^\s]+)?)$')]
    [string]$Target = "latest"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = 'SilentlyContinue'

# Check for 32-bit Windows
if (-not [Environment]::Is64BitProcess) {
    Write-Error "Claude Code does not support 32-bit Windows. Please use a 64-bit version of Windows."
    exit 1
}

$DOWNLOAD_BASE_URL = "@REDAPP_BASE_URL@"
$DOWNLOAD_DIR = "$env:USERPROFILE\.claude\downloads"

# Use native ARM64 binary on ARM64 Windows, x64 otherwise
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
    $platform = "win32-arm64"
} else {
    $platform = "win32-x64"
}
New-Item -ItemType Directory -Force -Path $DOWNLOAD_DIR | Out-Null
$DOWNLOAD_DIR = Join-Path $DOWNLOAD_DIR ("redapp-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $DOWNLOAD_DIR | Out-Null
try {

try {
    $version = $Target
    if ($Target -eq 'latest' -or $Target -eq 'stable') {
        $version = (Invoke-RestMethod -Uri "$DOWNLOAD_BASE_URL/$Target" -ErrorAction Stop).Trim()
    }
}
catch {
    Write-Error "Failed to get requested version: $_"
    exit 1
}

# Reject non-version content (e.g. an HTML error page) before it reaches the manifest URL
if ($version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$') {
    Write-Error "Failed to get a valid version from the download service (got unexpected content). This can happen if the download service is unreachable or not available in your region - see https://www.anthropic.com/supported-countries"
    exit 1
}

try {
    $manifest = Invoke-RestMethod -Uri "$DOWNLOAD_BASE_URL/$version/manifest.json" -ErrorAction Stop
    $checksum = $manifest.platforms.$platform.checksum

    if ($checksum -notmatch '^[0-9a-f]{64}$') {
        Write-Error "Platform $platform not found in manifest"
        exit 1
    }
}
catch {
    Write-Error "Failed to get manifest: $_"
    exit 1
}

# Download and verify
$binaryPath = "$DOWNLOAD_DIR\claude-$version-$platform.exe"
try {
    Invoke-WebRequest -Uri "$DOWNLOAD_BASE_URL/$version/$platform/claude.exe" -OutFile $binaryPath -ErrorAction Stop
}
catch {
    Write-Error "Failed to download binary: $_"
    if (Test-Path $binaryPath) {
        Remove-Item -Force $binaryPath
    }
    exit 1
}

# Calculate checksum
$actualChecksum = (Get-FileHash -Path $binaryPath -Algorithm SHA256).Hash.ToLower()

if ($actualChecksum -ne $checksum) {
    Write-Error "Checksum verification failed"
    Remove-Item -Force $binaryPath
    exit 1
}

Write-Output "Setting up Claude Code..."
$installExitCode = 0
try {
    $bin = "$env:USERPROFILE\.local\bin"
    $versions = "$env:USERPROFILE\.local\share\claude\versions"
    New-Item -ItemType Directory -Force -Path $versions, $bin | Out-Null
    $installed = Join-Path $versions "$version.exe"
    $stage = Join-Path $versions ([guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::Copy($binaryPath, $stage)
        Move-Item -LiteralPath $stage -Destination $installed -Force
    } finally { if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Force } }
    $psLauncher = @'
# RedApp managed Claude launcher
$previous = $env:DISABLE_UPDATES
try {
    $env:DISABLE_UPDATES = '1'
    & "$PSScriptRoot\..\share\claude\versions\@VERSION@.exe" @args
    $code = $LASTEXITCODE
} finally { $env:DISABLE_UPDATES = $previous }
exit $code
'@
    $cmdLauncher = "@echo off`r`nrem RedApp managed Claude launcher`r`nsetlocal`r`nset `"DISABLE_UPDATES=1`"`r`n`"%~dp0..\share\claude\versions\@VERSION@.exe`" %*`r`nexit /b %errorlevel%`r`n"
    foreach ($entry in @(@('claude.ps1', $psLauncher), @('claude.cmd', $cmdLauncher))) {
        $launcher = Join-Path $bin $entry[0]
        $stage = Join-Path $bin ([guid]::NewGuid().ToString('N') + '.tmp')
        try {
            [IO.File]::WriteAllText($stage, $entry[1].Replace('@VERSION@', $version), [Text.Encoding]::ASCII)
            if (Test-Path -LiteralPath $launcher) { [IO.File]::Replace($stage, $launcher, [NullString]::Value) }
            else { [IO.File]::Move($stage, $launcher) }
        } finally { if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Force } }
    }
    # The official executable takes precedence over .cmd in cmd.exe.
    if (Test-Path -LiteralPath "$bin\claude.exe") { Remove-Item -LiteralPath "$bin\claude.exe" -Force }
    Write-Output "Installed Claude Code $version at $bin"
    if (($env:PATH -split ';') -notcontains $bin) { Write-Output "Add $bin to your user PATH, then open a new terminal." }
}
catch { $installExitCode = 1; Write-Warning "Installation failed: $_" }
if ($installExitCode -ne 0) { exit $installExitCode }

Write-Output ""
Write-Output "$([char]0x2705) Installation complete!"
Write-Output ""
} finally {
    if (Test-Path -LiteralPath $DOWNLOAD_DIR) { Remove-Item -LiteralPath $DOWNLOAD_DIR -Recurse -Force }
}
