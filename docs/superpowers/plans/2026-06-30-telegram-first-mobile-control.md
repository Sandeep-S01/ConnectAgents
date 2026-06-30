# Telegram First Mobile Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Telegram `/status`, `/logs`, and `/cancel` commands, improve `/help`, and verify the bot calls the existing gateway APIs correctly.

**Architecture:** Keep the current Telegram long-polling bot and gateway loopback API. Add command handlers in `internal/telegram/bot.go` and test them with an in-process HTTP test server replacing the bot `baseURL`.

**Tech Stack:** Go, standard `net/http/httptest`, existing gateway JSON API contracts.

---

### Task 1: Test Telegram Status, Logs, Cancel, And Help

**Files:**
- Create: `internal/telegram/bot_test.go`

- [ ] **Step 1: Write failing tests**

Add tests that construct a `Bot` with a fake gateway server and a fake Telegram send server. Verify:

- `/help` includes `/status`, `/logs`, and `/cancel`.
- `/status task_1` calls `GET /api/tasks/task_1` and `GET /api/tasks/task_1/events`.
- `/logs task_1` calls `GET /api/tasks/task_1/events` and truncates long output.
- `/cancel task_1` calls `POST /api/tasks/task_1/cancel`.

- [ ] **Step 2: Run tests and watch them fail**

Run: `go test ./internal/telegram`

Expected: fail because the new commands are not implemented.

### Task 2: Implement Telegram Commands

**Files:**
- Modify: `internal/telegram/bot.go`

- [ ] **Step 1: Add command dispatch cases**

Add `/status`, `/logs`, and `/cancel` cases in `handleMessage`.

- [ ] **Step 2: Add handlers**

Add handlers that call existing gateway endpoints:

- `/api/tasks/{taskID}`
- `/api/tasks/{taskID}/events`
- `/api/tasks/{taskID}/cancel`

- [ ] **Step 3: Add small formatting helpers**

Add helpers to format values safely and truncate Telegram messages/payloads.

- [ ] **Step 4: Run targeted tests**

Run: `go test ./internal/telegram`

Expected: pass.

### Task 3: Full Verification

**Files:**
- Existing Go files only.

- [ ] **Step 1: Run package tests**

Run: `go test ./internal/telegram ./internal/gateway ./internal/store ./internal/policy ./internal/config ./cmd/gateway`

Expected: pass.

- [ ] **Step 2: Run full suite if practical**

Run: `go test ./...`

Expected: pass. If it exceeds the command timeout, report the packages that completed and the timeout.
