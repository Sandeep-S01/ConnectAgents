param(
    [string]$ConfigPath = ".gateway.local.ps1"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location -LiteralPath $repoRoot

$resolvedConfigPath = if ([System.IO.Path]::IsPathRooted($ConfigPath)) {
    $ConfigPath
} else {
    Join-Path $repoRoot $ConfigPath
}

if (-not (Test-Path -LiteralPath $resolvedConfigPath)) {
    throw "Gateway config file not found: $resolvedConfigPath. Copy .gateway.local.example.ps1 to .gateway.local.ps1 and fill in your private values."
}

. $resolvedConfigPath

New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot "data") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot "logs") | Out-Null

if ([string]::IsNullOrWhiteSpace($env:PAIA_ADDR)) {
    $env:PAIA_ADDR = "127.0.0.1:8080"
}
if ([string]::IsNullOrWhiteSpace($env:PAIA_DB_PATH)) {
    $env:PAIA_DB_PATH = "data/gateway.db"
}
if ([string]::IsNullOrWhiteSpace($env:PAIA_LOG_PATH)) {
    $env:PAIA_LOG_PATH = "logs/gateway.log"
}

$telegramEnabled = -not [string]::IsNullOrWhiteSpace($env:PAIA_TELEGRAM_BOT_TOKEN)
if ($telegramEnabled -and [string]::IsNullOrWhiteSpace($env:PAIA_AUTH_TOKEN)) {
    throw "PAIA_AUTH_TOKEN is required by start-gateway.ps1 when Telegram is enabled."
}

Write-Host "Starting ConnectAgents gateway at $env:PAIA_ADDR"
Write-Host "Using config: $resolvedConfigPath"

go run ./cmd/gateway
