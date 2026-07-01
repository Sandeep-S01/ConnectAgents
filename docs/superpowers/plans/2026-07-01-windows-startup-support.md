# Windows Startup Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Windows scripts so the gateway can be started manually or at login without keeping VS Code open.

**Architecture:** Keep secrets out of Git by loading a local PowerShell config file. Use one script to start the gateway and one script to register a Windows Task Scheduler task that calls the starter script.

**Tech Stack:** PowerShell 5+, Windows Task Scheduler, Go gateway.

---

### Task 1: Startup Script Validation

**Files:**
- Create: `scripts/test-startup-scripts.ps1`

- [ ] **Step 1: Write the failing validation script**

Create `scripts/test-startup-scripts.ps1` with checks that require:

```powershell
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
```

- [ ] **Step 2: Run validation to verify it fails**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-startup-scripts.ps1`

Expected: FAIL with `Missing required file: scripts/start-gateway.ps1`.

### Task 2: Startup Scripts

**Files:**
- Create: `scripts/start-gateway.ps1`
- Create: `scripts/install-startup-task.ps1`
- Create: `.gateway.local.example.ps1`
- Modify: `.gitignore` if needed

- [ ] **Step 1: Create local config template**

Create `.gateway.local.example.ps1` with local-only values and comments. The real `.gateway.local.ps1` must remain untracked.

- [ ] **Step 2: Create manual start script**

Create `scripts/start-gateway.ps1` that:
- Resolves the repo root.
- Loads `.gateway.local.ps1` by default.
- Creates `data` and `logs` directories.
- Validates `PAIA_AUTH_TOKEN` when Telegram is enabled.
- Starts `go run ./cmd/gateway`.

- [ ] **Step 3: Create startup task installer**

Create `scripts/install-startup-task.ps1` that:
- Resolves the repo root.
- Registers a task named `ConnectAgents Gateway`.
- Runs `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/start-gateway.ps1`.
- Starts at user login.

- [ ] **Step 4: Keep local secrets untracked**

Ensure `.gateway.local.ps1` is ignored by Git.

- [ ] **Step 5: Run validation**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-startup-scripts.ps1`

Expected: PASS with `Startup script validation passed.`

### Task 3: Documentation And Final Verification

**Files:**
- Modify: `TELEGRAM_SETUP.md`
- Modify: `README.md` if a short link is useful

- [ ] **Step 1: Document manual startup**

Add instructions for copying `.gateway.local.example.ps1` to `.gateway.local.ps1`, filling secrets, and running `scripts/start-gateway.ps1`.

- [ ] **Step 2: Document login startup**

Add instructions for running `scripts/install-startup-task.ps1` and checking the task in Task Scheduler.

- [ ] **Step 3: Run all checks**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-startup-scripts.ps1
go test -count=1 ./...
git status --short
```

Expected: validation passes, Go tests pass, and only intended files are modified before commit.
