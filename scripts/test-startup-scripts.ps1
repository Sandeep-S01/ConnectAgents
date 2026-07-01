$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$requiredFiles = @(
    "scripts/start-gateway.ps1",
    "scripts/install-startup-task.ps1",
    ".gateway.local.example.ps1"
)

foreach ($relativePath in $requiredFiles) {
    $path = Join-Path $repoRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        throw "Missing required file: $relativePath"
    }
}

$startScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot "scripts/start-gateway.ps1")
foreach ($requiredText in @(
    "param(",
    ".gateway.local.ps1",
    "PAIA_AUTH_TOKEN",
    "go run ./cmd/gateway"
)) {
    if ($startScript -notlike "*$requiredText*") {
        throw "start-gateway.ps1 is missing required text: $requiredText"
    }
}

$installScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot "scripts/install-startup-task.ps1")
foreach ($requiredText in @(
    "Register-ScheduledTask",
    "ConnectAgents Gateway",
    "start-gateway.ps1",
    "AtLogOn"
)) {
    if ($installScript -notlike "*$requiredText*") {
        throw "install-startup-task.ps1 is missing required text: $requiredText"
    }
}

$exampleConfig = Get-Content -Raw -LiteralPath (Join-Path $repoRoot ".gateway.local.example.ps1")
foreach ($requiredText in @(
    "PAIA_TELEGRAM_BOT_TOKEN",
    "PAIA_TELEGRAM_USER_ID",
    "PAIA_AUTH_TOKEN"
)) {
    if ($exampleConfig -notlike "*$requiredText*") {
        throw ".gateway.local.example.ps1 is missing required text: $requiredText"
    }
}

Write-Host "Startup script validation passed."
