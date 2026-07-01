param(
    [string]$BridgePath = "vscode-bridge",
    [string]$WorkspacePath = ".",
    [string]$ConfigPath = ".gateway.local.ps1"
)

$repoRoot = Split-Path -Parent $PSScriptRoot
$resolvedConfigPath = Resolve-Path -LiteralPath (Join-Path $repoRoot $ConfigPath) -ErrorAction SilentlyContinue
if ($resolvedConfigPath) {
    . $resolvedConfigPath
}
$resolvedBridgePath = Resolve-Path -LiteralPath (Join-Path $repoRoot $BridgePath)
$resolvedWorkspacePath = Resolve-Path -LiteralPath (Join-Path $repoRoot $WorkspacePath)

code --extensionDevelopmentPath="$resolvedBridgePath" "$resolvedWorkspacePath"
