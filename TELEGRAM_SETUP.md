# Telegram Setup

This guide sets up Telegram as the first phone interface for ConnectAgents.

The phone sends Telegram messages. The PC runs the gateway, Codex, Git, tests, and project file access.

## 1. Create Telegram Credentials

Create these values later when you are ready to run the real bot:

- `PAIA_TELEGRAM_BOT_TOKEN`: Bot token from BotFather.
- `PAIA_TELEGRAM_USER_ID`: Your numeric Telegram user ID.
- `PAIA_AUTH_TOKEN`: A private gateway API token, at least 32 characters for production mode.

## 2. Start The Gateway With Telegram

PowerShell example:

```powershell
cd D:\Personal_Project\ConnectAgents

$env:PAIA_ADDR = "127.0.0.1:8080"
$env:PAIA_AUTH_TOKEN = "change-this-token-to-at-least-32-chars"
$env:PAIA_TELEGRAM_BOT_TOKEN = "your_bot_token_here"
$env:PAIA_TELEGRAM_USER_ID = "your_numeric_telegram_user_id"

go run ./cmd/gateway
```

When the bot starts, it sends a Telegram message to the configured user:

```text
AI Gateway Bot started and listening for commands.
```

## 3. Run Telegram Doctor

From Telegram:

```text
/doctor
```

Expected checks:

- Gateway health is `ok`.
- Projects API is reachable.
- Auth token is configured.
- Gateway URL points to the local gateway address.

## 4. Register A Project From Telegram

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

## 5. Smoke Test A Codex Task

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

## 6. Approval Flow

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
