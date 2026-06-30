# Telegram-First Mobile Control Design

## Goal

Make Telegram the first practical phone interface for the Personal AI Assistant Gateway.

The developer should be able to send a prompt from a phone, choose a registered local project, start a Codex task on the PC, monitor status, inspect recent logs, cancel active work, and approve or reject sensitive actions. Codex, Git, files, and tests still run on the PC.

Telegram credentials are not part of this implementation pass. `PAIA_TELEGRAM_BOT_TOKEN`, `PAIA_TELEGRAM_USER_ID`, and `PAIA_AUTH_TOKEN` remain setup inputs to provide later.

## Approved Approach

Build in this order:

1. Improve the Telegram bot daily-use workflow.
2. Then add the Telegram setup and smoke-test path.

The existing bot already supports project listing, task creation, task running, recent tasks, Git summary, approval actions, commit, and push. The next work should make the Telegram interface complete enough for normal phone-driven development.

## User Workflow

Primary flow:

```text
/projects
/run <project> <prompt>
/status <task_id>
/logs <task_id>
/approve <approval_id> or /reject <approval_id>
/execute <approval_id>
```

The bot should send important task events back to the owner:

- Task created.
- Agent started.
- Approval requested.
- Agent completed.
- Agent failed.
- Agent timed out.
- Task canceled.
- Command blocked.

## Telegram Commands

Existing commands stay available:

- `/help`
- `/projects`
- `/tasks`
- `/run <project_id> <prompt>`
- `/git <project_id>`
- `/approve <approval_id>`
- `/reject <approval_id>`
- `/execute <approval_id>`
- `/commit <project_id> <message>`
- `/push <project_id>`

Add or improve:

- `/status <task_id>`: show task status, project ID, agent type, prompt, start/completion timestamps, error message when present, and the latest event.
- `/logs <task_id>`: show recent task events. Keep the response short enough for Telegram by sending only the latest events and truncating long payloads.
- `/cancel <task_id>`: cancel an active Codex task through the existing gateway cancel endpoint.

Improve help text so the normal phone workflow is obvious.

## Project Selection

The first implementation should continue using project IDs because the gateway already exposes them.

Project aliases or project-name matching can be added later if typing IDs from a phone becomes too awkward. This avoids changing the project storage model during the first Telegram UX pass.

## Data Flow

```text
Phone Telegram app
  -> Telegram Bot API
  -> Gateway Telegram long-polling bot running on PC
  -> Local gateway HTTP API over loopback
  -> SQLite task/project/event store
  -> Codex CLI running inside selected local project
```

The phone does not run Codex and does not need direct filesystem access. The PC remains the execution environment.

## Security

The bot must continue accepting commands only from `PAIA_TELEGRAM_USER_ID`.

The bot should continue calling the gateway API with `PAIA_AUTH_TOKEN` when configured.

Sensitive local actions remain guarded by the gateway approval flow. Telegram may approve, reject, or execute an already-approved command, but it should not bypass command policy.

## Error Handling

For command errors, Telegram should return concise messages that include the failed operation and the gateway error.

For long logs or payloads, Telegram responses should be truncated instead of failing due to message size.

For missing arguments, each command should return a usage example.

For unauthorized Telegram users, the bot should reject the command and avoid exposing project or task details.

## Testing

Unit tests should cover command handlers where practical by using a fake gateway client or test server.

At minimum, implementation verification should include:

- `go test ./...`
- `/help` includes the new commands.
- `/status <task_id>` calls the task detail and event endpoints.
- `/logs <task_id>` calls the task events endpoint and truncates output.
- `/cancel <task_id>` calls the cancel endpoint.
- Existing `/run`, `/projects`, `/tasks`, approvals, and Git commands still work.

## Follow-Up Setup Path

After the Telegram UX pass, document and verify the setup flow:

1. Create a Telegram bot with BotFather.
2. Get the numeric Telegram user ID.
3. Set `PAIA_TELEGRAM_BOT_TOKEN`.
4. Set `PAIA_TELEGRAM_USER_ID`.
5. Set `PAIA_AUTH_TOKEN`.
6. Start the gateway.
7. Register at least one project.
8. Send `/projects`.
9. Send `/run <project_id> <prompt>`.
10. Confirm task events return to Telegram.

## Out Of Scope

- Native mobile app.
- PWA improvements.
- Cloud relay.
- Claude Code adapter.
- Multi-agent orchestration.
- Project aliases unless needed after first real Telegram use.
