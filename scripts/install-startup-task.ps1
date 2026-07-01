param(
    [string]$TaskName = "ConnectAgents Gateway",
    [string]$ConfigPath = ".gateway.local.ps1"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$startScript = Join-Path $repoRoot "scripts\start-gateway.ps1"

if (-not (Test-Path -LiteralPath $startScript)) {
    throw "Missing startup script: $startScript"
}

$resolvedConfigPath = if ([System.IO.Path]::IsPathRooted($ConfigPath)) {
    $ConfigPath
} else {
    Join-Path $repoRoot $ConfigPath
}

if (-not (Test-Path -LiteralPath $resolvedConfigPath)) {
    throw "Gateway config file not found: $resolvedConfigPath. Copy .gateway.local.example.ps1 to .gateway.local.ps1 first."
}

$argument = "-NoProfile -ExecutionPolicy Bypass -File `"$startScript`" -ConfigPath `"$resolvedConfigPath`""
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $argument -WorkingDirectory $repoRoot
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Description "Starts the ConnectAgents gateway at Windows user login." -Force | Out-Null

Write-Host "Registered scheduled task: $TaskName"
Write-Host "Start it now with:"
Write-Host "Start-ScheduledTask -TaskName `"$TaskName`""
