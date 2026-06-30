# Personal AI Assistant Gateway

Local-first MVP gateway for the Personal AI Software Engineer architecture.

## Current Scope

Implemented:

- Local Go HTTP gateway.
- SQLite-backed project registry.
- SQLite-backed task creation.
- Task history listing, task detail, and task event retrieval.
- Approval request creation, listing, approve, and reject flow.
- Command safety policy evaluator for future agent execution.
- Structured request logging with request IDs.
- Runtime metrics endpoint.
- Authenticated SQLite backup endpoint.
- Git branch, status, and diff summary endpoint for registered projects.
- Policy-enforced task command execution endpoint.
- Single-use approved command execution endpoint for command approvals.
- Built-in PWA dashboard for browser and mobile control.
- Telegram Bot for remote control via chat (commands: projects, tasks, git, run, approvals).
- Bearer-token API authentication and HttpOnly dashboard sessions when `PAIA_AUTH_TOKEN` is set.
- Project path allowlisting by registering only existing directories.
- Safe localhost default binding.
- Startup guard that rejects non-local bind addresses unless `PAIA_AUTH_TOKEN` is set.

## Run

```powershell
go run ./cmd/gateway
```

Defaults:

- Address: `127.0.0.1:8080`
- Database: `data/gateway.db`
- Log output: stdout unless `PAIA_LOG_PATH` is set.
- Log rotation size: 10 MiB unless `PAIA_LOG_MAX_BYTES` is set.
- Task execution timeout: 2 hours unless `PAIA_TASK_TIMEOUT_SECONDS` is set.
- Dashboard login throttle: 5 failed attempts per remote address within 5 minutes unless `PAIA_LOGIN_FAILURE_LIMIT` or `PAIA_LOGIN_FAILURE_WINDOW_SECONDS` is set.
- Mutating API request throttle: 120 writes per remote address per minute unless `PAIA_API_WRITE_LIMIT` or `PAIA_API_WRITE_RATE_WINDOW_SECONDS` is set.
- HTTP server timeouts: 5 seconds for headers, 30 seconds for request reads, and 2 minutes for idle keep-alive connections.

Override with environment variables:

```powershell
$env:PAIA_ADDR = "127.0.0.1:9090"
$env:PAIA_ENV = "production"
$env:PAIA_DB_PATH = "data/dev-gateway.db"
$env:PAIA_AUTH_TOKEN = "change-this-token-to-at-least-32-chars"
$env:PAIA_LOG_PATH = "logs/gateway.log"
$env:PAIA_LOG_MAX_BYTES = "10485760"
$env:PAIA_TASK_TIMEOUT_SECONDS = "7200"
$env:PAIA_LOGIN_FAILURE_LIMIT = "5"
$env:PAIA_LOGIN_FAILURE_WINDOW_SECONDS = "300"
$env:PAIA_API_WRITE_LIMIT = "120"
$env:PAIA_API_WRITE_RATE_WINDOW_SECONDS = "60"
$env:PAIA_TLS_CERT_PATH = "certs/gateway.crt"
$env:PAIA_TLS_KEY_PATH = "certs/gateway.key"
$env:PAIA_TELEGRAM_BOT_TOKEN = "your_bot_token_here"
$env:PAIA_TELEGRAM_USER_ID = "your_telegram_id_here"
go run ./cmd/gateway
```

`PAIA_ENV` may be empty, `development`, or `production`; any other value is rejected at startup. Numeric environment variables must be valid positive integers. If `PAIA_LOG_MAX_BYTES`, `PAIA_TASK_TIMEOUT_SECONDS`, `PAIA_LOGIN_FAILURE_LIMIT`, `PAIA_LOGIN_FAILURE_WINDOW_SECONDS`, `PAIA_API_WRITE_LIMIT`, or `PAIA_API_WRITE_RATE_WINDOW_SECONDS` is malformed or non-positive, the gateway rejects the configuration at startup.

If `PAIA_ADDR` is set to a non-local address such as `0.0.0.0:8080`, `PAIA_AUTH_TOKEN` is required. `PAIA_AUTH_TOKEN` is also required when `PAIA_ENV=production`, even for localhost binds, and must be at least 32 characters so production deployments cannot accidentally start with an unauthenticated or trivially weak API token. `/health` remains public; `/api/*` requires `Authorization: Bearer <token>` or a dashboard session cookie when a token is configured.

Set both `PAIA_TLS_CERT_PATH` and `PAIA_TLS_KEY_PATH` to serve HTTPS directly from the gateway. For local/private deployments, you can also terminate TLS at Tailscale or a local reverse proxy and keep the gateway bound to localhost.

If TLS is terminated by a reverse proxy, forward `X-Forwarded-Proto: https` to the gateway. The dashboard uses that header to mark session cookies as `Secure` and to compare same-origin browser writes against the public HTTPS scheme.

HTTPS responses, including proxied HTTPS requests that send `X-Forwarded-Proto: https`, include `Strict-Transport-Security`. Plain local HTTP responses omit HSTS so local development is not pinned to HTTPS.

When dashboard sessions are used, mutating `/api/*` requests authenticated by the session cookie require a same-origin `Origin` header when browsers send one. Bearer-token API clients are not subject to this browser CSRF check.

Dashboard login rejects repeated invalid token attempts from the same remote address for a short in-memory window. Restarting the gateway clears this local throttle state.
Throttled login responses return `429 Too Many Requests` with a `Retry-After` header in seconds.

Mutating `/api/*` requests are rate-limited per remote address. Throttled write responses return `429 Too Many Requests` with a `Retry-After` header in seconds. Read-only API requests, task event streams, health checks, and dashboard assets are not counted against this write limit.

## Dashboard

Open the built-in browser dashboard:

```powershell
Start-Process http://127.0.0.1:8080/
```

The dashboard is served by the Go gateway and provides project registration, task creation, Codex run/cancel controls, pending approval actions, and live SSE task events. When `PAIA_AUTH_TOKEN` is set, enter the same token in the dashboard sign-in field. The token is exchanged for an HttpOnly same-origin session cookie and is not stored in browser local storage.

## Telegram Setup

Telegram is the first phone control surface. See `TELEGRAM_SETUP.md` for BotFather setup, gateway environment variables, `/doctor`, `/addproject`, and the phone-to-PC smoke test flow.

## API

Health:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/health
```

The health endpoint verifies that the SQLite store is reachable. It returns `200` with `{"status":"ok"}` when healthy and `503` with `{"status":"unhealthy"}` when storage is unavailable.

For authenticated API calls:

```powershell
$headers = @{ Authorization = "Bearer change-this-token" }
```

Create a project:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/projects `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"name":"Personal AI Assistant","path":"D:\\Personal_Project\\PersonalAIAssitent","techStack":"Go"}'
```

List projects:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/projects
```

Get Git summary for a registered project:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/projects/proj_id_from_create_project/git
```

The Git summary endpoint runs read-only Git commands inside the registered project path and returns the current branch, `git status --short` lines, and `git diff --stat`.

Create a task:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/tasks `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"projectId":"proj_id_from_create_project","agentType":"codex","prompt":"Run tests"}'
```

List tasks:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/tasks
Invoke-RestMethod -Headers $headers "http://127.0.0.1:8080/api/tasks?projectId=proj_id_from_create_project"
Invoke-RestMethod -Headers $headers "http://127.0.0.1:8080/api/tasks?status=queued"
```

The task list returns newest tasks first. Use `projectId` and `status` query parameters to filter task history.

Get task details:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/tasks/task_id_from_create_task
```

Get task events:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/tasks/task_id_from_create_task/events
```

Stream task events with Server-Sent Events:

```powershell
Invoke-WebRequest `
  -Headers $headers `
  -Uri http://127.0.0.1:8080/api/tasks/task_id_from_create_task/events/stream
```

The stream uses `text/event-stream`, emits stored task events as SSE frames, and keeps the connection open for new gateway-originated task events, including approval requests and approval decisions. Task events include stored `payloadJson` when an event has structured audit details such as command decisions or runner results. Clients can reconnect with `Last-Event-ID` to receive only events after the last delivered event id.

Run a Codex task:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/tasks/task_id_from_create_task/run `
  -Headers $headers
```

The gateway runs `codex exec` in the registered project directory using the task prompt saved at creation time. The Codex process is launched with argument-safe process execution, `--sandbox workspace-write`, `--skip-git-repo-check`, and `--json`.

Cancel a running task:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/tasks/task_id_from_create_task/cancel `
  -Headers $headers
```

Cancellation is best-effort for active in-memory Codex runs. The gateway cancels the run context, records `task.cancel_requested`, and the run handler records `agent.canceled` when the Codex process exits through that cancellation path.

Codex runs and task command executions also have a deadline controlled by `PAIA_TASK_TIMEOUT_SECONDS`. When the deadline expires, the gateway cancels the runner context, marks the task `timed_out`, and records `agent.timed_out` or `command.timed_out`.

The gateway allows only one active local execution per registered project. A second Codex run or task command for the same project receives `409 Conflict` until the current execution completes, fails, is canceled, or times out.

Create an approval request for a sensitive task action:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/tasks/task_id_from_create_task/approvals `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"actionType":"install_package","description":"Install github.com/example/package","payloadJson":"{\"package\":\"github.com/example/package\"}"}'
```

List pending approvals:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/approvals?status=pending
```

Approve or reject:

```powershell
Invoke-RestMethod -Method Post -Headers $headers http://127.0.0.1:8080/api/approvals/approval_id/approve
Invoke-RestMethod -Method Post -Headers $headers http://127.0.0.1:8080/api/approvals/approval_id/reject
```

Execute an approved command approval:

```powershell
Invoke-RestMethod -Method Post -Headers $headers http://127.0.0.1:8080/api/approvals/approval_id/execute
```

Evaluate command safety before execution:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/command-policy/evaluate `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"command":"go test ./..."}'
```

Policy decisions:

- `allow`: known safe read or verification command, such as `go test ./...`, `go vet ./...`, `go build ./cmd/gateway`, `git status --short`, or `git diff`.
- `require_approval`: dependency changes, process execution, destructive file commands, Git writes, deploy/publish commands, and unknown commands.
- `block`: credential and system path access, including environment-variable dumps, direct shell env-token reads, browser credential stores, SSH private keys, AWS credentials, GitHub CLI hosts, Docker auth config, package-manager auth files, netrc files, Kubernetes config, dotenv files, Windows credential stores, Windows system directories, `/etc/passwd`, and `/etc/shadow`.

Run a command for a task:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/tasks/task_id_from_create_task/commands `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"command":"go test ./..."}'
```

Command execution behavior:

- `allow`: the command runs in the registered project directory, task status moves through `running` to `completed` or `failed`, and task events record `command.started` plus `command.completed` or `command.failed`.
- `require_approval`: the command does not run. The gateway creates a pending `command_execution` approval request and moves the task to `waiting_for_approval`. After the approval is approved, `POST /api/approvals/{approvalID}/execute` runs the command once in the registered project directory. Replayed execute calls return `409 Conflict`.
- `block`: the command does not run. The gateway records `command.blocked` and marks the task as `blocked`.

Codex task execution uses the saved task prompt and registered project path. The separate command endpoint remains the safety-controlled path for explicit shell commands.

## Production Readiness Status

Current hardening:

- API routes can be protected with a bearer token.
- Browser dashboard authentication uses an HttpOnly, SameSite=Lax, expiring session cookie instead of storing the bearer token in local storage.
- Dashboard session cookies are marked `Secure` for direct HTTPS requests and proxied HTTPS requests that send `X-Forwarded-Proto: https`.
- Dashboard logout clears the session cookie with matching `Secure` handling for direct or proxied HTTPS requests.
- HTTPS responses include `Strict-Transport-Security`; plain HTTP responses do not.
- Dashboard login throttles repeated invalid token attempts per remote address, with configurable limit and window settings.
- Throttled dashboard login responses include `Retry-After` so clients can back off consistently.
- Mutating API requests are throttled per remote address, with configurable limit and window settings.
- Cookie-authenticated browser writes reject cross-origin `Origin` headers; bearer-token API requests remain available for non-browser clients.
- Responses include baseline browser security headers: `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and a same-origin Content Security Policy.
- API, task event stream, and auth responses include `Cache-Control: no-store` so prompts, task logs, approval payloads, and session responses are not cached by browsers or proxies.
- Public binds require an auth token at startup.
- Production mode requires an auth token at startup, including localhost deployments, and rejects tokens shorter than 32 characters.
- Malformed or non-positive numeric environment settings are rejected at startup.
- HTTPS can be served directly when `PAIA_TLS_CERT_PATH` and `PAIA_TLS_KEY_PATH` are configured.
- Request bodies are capped to reduce accidental or abusive large payloads.
- JSON request bodies require `Content-Type: application/json`, reject unknown fields, and reject trailing extra JSON values.
- HTTP server read, header, idle, and header-size limits are configured to reduce slow-client and oversized-header resource exhaustion risk.
- Local command and Codex runner stdout/stderr capture is capped to 64 KiB per stream before responses or task events are written.
- SQLite foreign keys are enabled.
- SQLite indexes cover task history, task event streaming, and approval queue query paths.
- SQLite access is serialized through one gateway database connection to avoid local single-writer `SQLITE_BUSY` failures.
- Task creation writes an auditable `task.created` event.
- Task creation rejects blank prompts and unsupported agent types; the current executable task agent is `codex`.
- Task status updates are constrained to known lifecycle states so invalid status strings are not persisted.
- Approval requests and decisions are persisted and mirrored into task events.
- Approval request creation rejects blank action types and descriptions before writing audit records.
- Task event APIs and streams expose stored `payloadJson` audit details for command decisions, runner results, and approval payloads.
- Project registration rejects blank names and paths so the allowlist cannot accidentally include unnamed projects or the gateway process working directory.
- Command policy can classify safe, approval-required, and blocked commands before execution.
- Task commands are routed through command policy before any local process is started.
- Registered projects expose read-only Git branch, status, and diff summary data without accepting arbitrary Git command input.
- Approval-required commands can only execute after the stored approval is explicitly approved, the command is rechecked for blocked policy before execution, and the approval is consumed so replayed execute calls return `409 Conflict`.
- Codex task runs execute through `codex exec` with a workspace-write sandbox and without shell-string prompt interpolation.
- Active Codex task runs can be canceled through an authenticated API call.
- Codex task runs and task command executions have a configurable deadline and are marked `timed_out` when they exceed it.
- Running tasks expose `startedAt`; terminal task states expose `completedAt`, including failed, canceled, blocked, timed-out, and interrupted tasks.
- Codex task runs and task command executions are serialized per registered project to prevent overlapping local edits or test runs in the same workspace.
- Gateway startup marks orphaned `running` tasks as `interrupted` and records `task.interrupted` events.
- Task events can be streamed as SSE frames with live fan-out, including approval request/decision events, and `Last-Event-ID` resume support.
- Built-in PWA dashboard can create projects and tasks, run/cancel Codex tasks, review approvals, and stream task events.
- Every HTTP response includes `X-Request-ID`; caller-provided request IDs are preserved.
- `/health` checks SQLite reachability and returns `503` when the store is unavailable.
- Every HTTP request writes one structured log with method, path, status, request ID, and duration.
- Structured logs can be written to a rotating file with `PAIA_LOG_PATH` and `PAIA_LOG_MAX_BYTES`.
- Authenticated metrics expose request totals, status-class counts, and average request duration.
- Authenticated backups use SQLite `VACUUM INTO` to create a consistent database copy.
- Backup creation rejects blank destination paths before filesystem resolution.
- GitHub Actions CI runs tests, vet, and gateway build.
- Runtime state is ignored through `.gitignore`.

Remaining before real production use:

- Put the gateway behind SSO, a private overlay network, or a hardened reverse proxy before exposing it to the public internet.

## Backup and Restore

Create a database backup:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/admin/backups `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"path":"D:\\Personal_Project\\PersonalAIAssitent\\backups\\gateway-2026-06-28.db"}'
```

The backup endpoint refuses to overwrite an existing file. Store backups outside the runtime `data/` directory if you rotate or delete local runtime files.

Restore a backup:

1. Stop the gateway process.
2. Copy the backup file over the configured `PAIA_DB_PATH`.
3. Start the gateway.
4. Call `/health`, then verify `/api/projects`, `/api/tasks/{taskID}`, or `/api/approvals` as needed.

Example:

```powershell
Stop-Process -Name gateway -ErrorAction SilentlyContinue
Copy-Item `
  -LiteralPath "D:\\Personal_Project\\PersonalAIAssitent\\backups\\gateway-2026-06-28.db" `
  -Destination "D:\\Personal_Project\\PersonalAIAssitent\\data\\gateway.db" `
  -Force
go run ./cmd/gateway
```

## Logging

The gateway writes structured request logs through Go `slog`. By default logs are written to stdout. Set `PAIA_LOG_PATH` to write logs to a file, and set `PAIA_LOG_MAX_BYTES` to rotate the active log file to `.1` when it exceeds the configured size.

Example:

```powershell
$env:PAIA_LOG_PATH = "logs/gateway.log"
$env:PAIA_LOG_MAX_BYTES = "10485760"
go run ./cmd/gateway
```

Each request log includes:

- `request_id`
- `method`
- `path`
- `status`
- `duration_ms`

Request and response bodies are not logged, so prompts, project paths, and approval payloads are not copied into logs by the HTTP middleware. Send `X-Request-ID` from clients to correlate mobile/PWA requests with gateway logs; otherwise the gateway generates one.

## Metrics

Query runtime metrics:

```powershell
Invoke-RestMethod -Headers $headers http://127.0.0.1:8080/api/metrics
```

Response fields:

- `requestsTotal`: total HTTP requests observed by the gateway, including the current metrics request.
- `statusCounts`: request counts grouped by `1xx`, `2xx`, `3xx`, `4xx`, and `5xx`.
- `averageDurationMs`: average request duration in milliseconds.

## Test

```powershell
go test ./...
```
