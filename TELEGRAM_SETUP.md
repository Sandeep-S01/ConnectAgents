# Telegram Setup

This guide sets up Telegram as the first phone interface for ConnectAgents.

The phone sends Telegram messages. The PC runs the gateway, Codex, Git, tests, and project file access.

## 1. Create Telegram Credentials

Create these values later when you are ready to run the real bot:

- `PAIA_TELEGRAM_BOT_TOKEN`: Bot token from BotFather.
- `PAIA_TELEGRAM_USER_ID`: Your numeric Telegram user ID.
- `PAIA_AUTH_TOKEN`: A private gateway API token, at least 32 characters for production mode.

## 2. Create Local Gateway Config

Copy the example config to a local secret file:

```powershell
cd D:\Personal_Project\ConnectAgents
Copy-Item .gateway.local.example.ps1 .gateway.local.ps1
notepad .gateway.local.ps1
```

Fill in:

- `PAIA_AUTH_TOKEN`
- `PAIA_TELEGRAM_BOT_TOKEN`
- `PAIA_TELEGRAM_USER_ID`

The real `.gateway.local.ps1` file is ignored by Git.

## 3. Start The Gateway Manually

Run this first so you can see startup errors directly:

```powershell
cd D:\Personal_Project\ConnectAgents
.\scripts\start-gateway.ps1
```

The script:

- Loads `.gateway.local.ps1`.
- Creates `data\` and `logs\` if missing.
- Starts `go run ./cmd/gateway`.
- Stops early if Telegram is enabled without `PAIA_AUTH_TOKEN`.

When the bot starts, it sends a Telegram message to the configured user:

```text
AI Gateway Bot started and listening for commands.
```

Startup logs also show:

- Whether the Telegram bot is enabled or disabled.
- The configured owner user ID.
- The gateway base URL used by Telegram commands.

The bot token is never written to logs.

If `PAIA_TELEGRAM_BOT_TOKEN` is set, `PAIA_TELEGRAM_USER_ID` must also be set to a positive numeric ID. The gateway exits with a clear config error when the user ID is missing.

If Telegram is enabled and `PAIA_AUTH_TOKEN` is empty, the gateway starts for local development but writes a warning because Telegram commands can reach an unauthenticated local gateway.

If the startup Telegram message cannot be sent, check the gateway logs for `telegram startup message failed`. That usually means the bot token is wrong, the owner user ID is wrong, or the bot has not been opened from your Telegram account yet.

## 4. Start The Gateway At Windows Login

After manual startup works, register a Windows Task Scheduler task:

```powershell
cd D:\Personal_Project\ConnectAgents
.\scripts\install-startup-task.ps1
```

Start it immediately without waiting for the next login:

```powershell
Start-ScheduledTask -TaskName "ConnectAgents Gateway"
```

To inspect or disable it later, open Task Scheduler and look for `ConnectAgents Gateway`.

## 5. Run Telegram Doctor

From Telegram:

```text
/doctor
```

Expected checks:

- SQLite is reachable.
- Git is available on the PC.
- Codex CLI is available on the PC.
- Registered project paths still exist.
- Gateway URL points to the local gateway address.

## 6. Register A Project From Telegram

Use pipe separators so Windows paths and project names can contain spaces:

```text
/addproject ConnectAgents | D:\Personal_Project\ConnectAgents | Go
```

Then list projects:

```text
/projects
```

Copy the project ID returned by `/projects`.

To update a registered project:

```text
/updateproject <project_id> | ConnectAgents | D:\Personal_Project\ConnectAgents | Go
```

To remove a registered project:

```text
/removeproject <project_id>
```

## 7. Smoke Test A Codex Task

Start with a harmless prompt:

```text
/run <project_id> Read README.md and summarize the current project in one paragraph.
```

Then check status and logs:

```text
/status <task_id>
/task <task_id>
/logs <task_id>
```

If a task is still running and should be stopped:

```text
/cancel <task_id>
```

## 8. Approval Flow

If the gateway creates an approval request, Telegram sends the approval ID.

Use:

```text
/approve <approval_id>
/reject <approval_id>
/execute <approval_id>
```

`/execute` is only for approved command execution requests.

## Current Telegram Commands

```text
/help
/doctor
/projects
/addproject <name> | <absolute_path> [| tech_stack]
/updateproject <project_id> | <name> | <absolute_path> [| tech_stack]
/removeproject <project_id>
/run <project_id> <prompt>
/tasks
/status <task_id>
/task <task_id>
/logs <task_id>
/cancel <task_id>
/runtest <project_id>
/git <project_id>
/approve <approval_id>
/reject <approval_id>
/execute <approval_id>
/commit <project_id> <message>
/push <project_id>
```
