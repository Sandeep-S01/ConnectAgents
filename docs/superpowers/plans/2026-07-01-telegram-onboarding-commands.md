# Telegram Onboarding Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add phone-first Telegram setup commands so the owner can validate the gateway from Telegram without reading docs first.

**Architecture:** Extend the existing Telegram command dispatcher in `internal/telegram/bot.go`. Reuse the existing `/api/doctor` request flow for `/testsetup`, and keep `/setup` local to the bot so it works even before doctor succeeds.

**Tech Stack:** Go, existing Telegram bot package tests, existing gateway doctor endpoint.

---

### Task 1: Add Tests

**Files:**
- Modify: `internal/telegram/bot_test.go`

- [ ] **Step 1: Add `/setup` test**

Add a test that sends `/setup` through `handleMessage` and asserts the reply contains `First Time Setup`, `/doctor`, `/projects`, `/addproject`, and `/testsetup`.

- [ ] **Step 2: Add `/testsetup` test**

Add a test server for `/api/doctor`, send `/testsetup`, and assert the reply includes `Setup Test`, `Gateway reachable`, check statuses, and registered project status.

- [ ] **Step 3: Run tests and confirm failure**

Run: `go test -count=1 ./internal/telegram`

Expected: FAIL because `/setup` and `/testsetup` are unknown commands.

### Task 2: Implement Commands

**Files:**
- Modify: `internal/telegram/bot.go`

- [ ] **Step 1: Dispatch commands**

Add `/setup` and `/testsetup` cases in `handleMessage`.

- [ ] **Step 2: Implement `/setup`**

Add `handleSetup(chatID int64)` that sends a short first-time checklist with gateway URL and next commands.

- [ ] **Step 3: Implement `/testsetup`**

Add `handleTestSetup(chatID int64)` that calls the doctor endpoint and formats a compact pass/fail checklist.

- [ ] **Step 4: Improve `/help`**

Move first-time commands near the top of help output.

- [ ] **Step 5: Run focused tests**

Run: `go test -count=1 ./internal/telegram`

Expected: PASS.

### Task 3: Docs And Verification

**Files:**
- Modify: `TELEGRAM_SETUP.md`
- Modify: `README.md` if needed

- [ ] **Step 1: Update command list**

Add `/setup` and `/testsetup` to setup docs.

- [ ] **Step 2: Run full verification**

Run:

```powershell
go test -count=1 ./...
git status --short
```

Expected: all tests pass and only intended files are modified.
