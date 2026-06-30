# Personal AI Software Engineer - High Level Architecture

## 1. Problem Summary

Build a personal AI engineering assistant that lets a developer control local coding agents from a mobile device or browser while the actual work runs on the developer's own machine.

The system should support:

- Sending coding tasks remotely.
- Selecting a local project/workspace.
- Starting and monitoring AI agent sessions.
- Reviewing file, Git, log, and test activity.
- Approving sensitive actions.
- Managing multiple local projects.

Example flow:

```text
Mobile user:
  "Analyze the authentication issue and fix it."

System:
  1. Receives the task through a secure remote client.
  2. Selects the target local project.
  3. Starts Codex, Claude Code, or another local agent.
  4. Streams progress back to the user.
  5. Runs tests or verification commands.
  6. Reports completion and changed files.
```

## 2. Requirements and Constraints

### Functional Requirements

- Remote task submission from mobile or web.
- Project selection and project metadata management.
- Local AI tool execution using existing authenticated CLI tools.
- Real-time progress streaming.
- Task history, logs, and status tracking.
- User approval for sensitive actions.
- Basic Git awareness: branch, status, diff summary, and commit option.
- Notification support for task completion or required approval.

### Non-Functional Requirements

- Local-first by default; no cloud dependency for MVP.
- Secure remote access through private networking such as Tailscale.
- Clear permission boundaries for file access and command execution.
- Durable task and log storage.
- Recoverable long-running tasks.
- Minimal cost by reusing installed AI subscriptions and local tooling.

### Assumptions

- Single primary user in MVP.
- Agent runs on a trusted developer PC.
- Developer already has Codex, Claude Code, or similar CLI tools installed and authenticated locally.
- Remote access initially happens over Tailscale or another private VPN, not a public internet endpoint.

## 3. Capacity Estimates

MVP scale is modest:

- Users: 1 developer.
- Projects: 5-50 local workspaces.
- Concurrent active tasks: 1-3.
- Task duration: seconds to hours.
- Event volume: roughly 1-10 progress events per second during active agent work.
- Storage: low; SQLite is sufficient for task metadata, logs, and settings.

SQLite and a single local gateway process are enough until multiple users, distributed agents, or high-volume team usage are required.

## 4. Proposed Architecture

```text
Mobile Client / PWA / Telegram Bot
        |
        | HTTPS or Bot API over Tailscale/private network
        v
Personal AI Gateway
        |
        +-- Auth and Permission Manager
        +-- Project Context Manager
        +-- Task Execution Engine
        +-- Real-Time Event Stream
        +-- SQLite Store
        |
        +-- AI Tool Adapter Layer
        |       +-- Codex CLI
        |       +-- Claude Code CLI
        |       +-- Other local agents
        |
        +-- Local System Integrations
                +-- Git
                +-- File system
                +-- Test commands
                +-- VS Code extension
```

### Main Components

#### Mobile Client

Remote control surface for sending prompts, selecting projects, viewing progress, approving actions, and receiving completion notifications.

Recommended path:

- Phase 1: Telegram bot for the fastest MVP.
- Phase 2: Next.js PWA for richer mobile and desktop control.
- Phase 3: React Native app only if native notifications, sharing, or deeper mobile integration become important.

#### Personal AI Gateway

Main local controller running on the developer PC.

Recommended technology:

- Go for a small, reliable long-running local service.
- Python FastAPI is acceptable if faster iteration and ecosystem convenience matter more than a compact binary.

Responsibilities:

- Authenticate remote requests.
- Validate project and command permissions.
- Create and manage task sessions.
- Start local AI tools as child processes or managed sessions.
- Stream logs, status, and approval requests.
- Persist tasks, project settings, and audit records.

#### AI Tool Adapter Layer

Normalizes interaction with local agent tools.

Each adapter should define:

- How to start a session.
- How to pass a prompt.
- How to stream stdout/stderr/events.
- How to detect completion/failure.
- How to request approval for sensitive operations if supported.

Initial adapters:

- Codex CLI.
- Claude Code CLI.

#### Project Context Manager

Stores and resolves project metadata:

- Project name.
- Absolute path.
- Default branch.
- Tech stack.
- Common commands.
- Allowed command patterns.
- Documentation paths.
- Last task history.

#### Task Execution Engine

Owns the task lifecycle:

```text
Queued -> Starting -> Running -> WaitingForApproval -> Verifying -> Completed
                                      |                    |
                                      v                    v
                                  Rejected              Failed
```

Responsibilities:

- Queue tasks.
- Prevent unsafe concurrent work in the same project.
- Capture output and structured progress events.
- Handle cancellation and timeout.
- Retry only safe, idempotent operations.
- Preserve logs after crashes.

#### VS Code Extension

Optional but useful once the gateway is functional.

Responsibilities:

- Detect active workspace.
- Register current project with the gateway.
- Show active task state inside VS Code.
- Display changed files, logs, and approval requests.
- Provide local controls for pause, cancel, and resume.

## 5. Data and API Design

### SQLite Schema Sketch

```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  tech_stack TEXT,
  default_branch TEXT,
  allowed_commands_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  prompt TEXT NOT NULL,
  status TEXT NOT NULL,
  agent_type TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE TABLE task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  message TEXT NOT NULL,
  payload_json TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE approval_requests (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  action_type TEXT NOT NULL,
  description TEXT NOT NULL,
  payload_json TEXT,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);
```

### API Sketch

```http
POST /api/tasks
Content-Type: application/json

{
  "projectId": "trading-platform",
  "agentType": "codex",
  "prompt": "Fix the authentication issue and run tests."
}
```

```http
GET /api/tasks/{taskId}
```

```http
GET /api/tasks/{taskId}/events
```

```http
POST /api/approvals/{approvalId}/approve
```

```http
POST /api/approvals/{approvalId}/reject
```

```http
GET /api/projects
```

```http
POST /api/projects
Content-Type: application/json

{
  "name": "Trading Platform",
  "path": "D:\\Personal_Project\\TradingPlatform",
  "techStack": "Go, Next.js, PostgreSQL"
}
```

### Real-Time Events

Use WebSocket or Server-Sent Events for the PWA.

Example event:

```json
{
  "taskId": "task_001",
  "type": "progress",
  "message": "Running backend tests",
  "createdAt": "2026-06-28T01:45:00+05:30"
}
```

For Telegram MVP, convert important events into bot messages instead of maintaining a live socket.

## 6. Reliability, Security, and Operations

### Security Model

The gateway can modify local source code, so it must be treated as a privileged local service.

Required controls:

- Bind only to localhost or Tailscale/private IP by default.
- Require authentication for every remote command.
- Maintain a project allowlist.
- Reject paths outside registered project roots.
- Use command allowlists for unattended execution.
- Require approval for package installs, destructive file operations, Git pushes, credential access, and external network exposure.
- Store secrets outside SQLite using OS secret storage when possible.
- Record an audit trail for tasks, commands, approvals, and changed files.

### Permission Levels

Allowed without approval:

- Read files inside registered projects.
- Edit files inside registered projects.
- Run configured test and build commands.
- Read Git status and diffs.

Require approval:

- Installing packages.
- Running arbitrary shell commands.
- Deleting many files.
- Accessing files outside project roots.
- Git push, release, deploy, or publish operations.
- Changing gateway settings.

Always blocked by default:

- Reading browser password stores.
- Reading SSH keys or private tokens.
- Deleting system directories.
- Running commands as administrator.

### Failure Handling

- Persist task state before starting child processes.
- Stream logs incrementally so output survives crashes.
- Mark orphaned running tasks as `interrupted` on gateway restart.
- Support manual retry by creating a new task linked to the failed task.
- Prefer cancellation through process groups so child processes are cleaned up.

### Observability

MVP logs:

- Gateway request logs.
- Task lifecycle events.
- Agent stdout/stderr.
- Approval decisions.
- Command execution records.

Later:

- Local metrics endpoint.
- Structured JSON logs.
- Health check endpoint.
- Task duration and failure dashboards.

## 7. Alternatives and Trade-Offs

### Telegram Bot vs PWA

Telegram bot:

- Fastest MVP.
- Good notifications.
- Limited UI for diffs, logs, and project management.

PWA:

- Better developer experience.
- Easier to show task history, file changes, and approvals.
- Requires frontend build and authentication.

Recommendation: start with Telegram if the immediate goal is remote task execution; move to PWA once task orchestration works.

### Go vs Python FastAPI

Go:

- Strong fit for long-running local service and process management.
- Easy single-binary distribution.
- Good concurrency primitives.

Python FastAPI:

- Faster prototyping.
- Rich AI and automation ecosystem.
- More packaging/runtime complexity on end-user machines.

Recommendation: use Go for the gateway unless the first version needs heavy Python-specific automation.

### Tailscale vs Cloud Relay

Tailscale:

- Simple and secure for personal use.
- No public endpoint.
- Requires devices to be joined to the private network.

Cloud relay:

- Easier access from anywhere.
- Enables push notifications and offline routing patterns.
- Introduces hosting, authentication, and privacy risk.

Recommendation: use Tailscale for MVP. Add cloud relay only after the local product is proven.

## 8. Development Roadmap

### Phase 1 - MVP: Remote Prompt Execution

Goal: send a prompt from mobile and run a local AI agent against a selected project.

Build:

- Local gateway.
- Project registry.
- Telegram bot or minimal PWA.
- Codex CLI adapter.
- Task table and event log.
- Basic status messages.

Success criteria:

- A mobile command can start a Codex task in a registered project.
- Progress and completion are visible remotely.
- Logs are saved locally.
- The system blocks unknown project paths.

### Phase 2 - Developer Experience

Add:

- Web dashboard.
- Project selection UI.
- Task history.
- Git status and diff summary.
- Test command configuration.
- Approval workflow.

### Phase 3 - AI Engineering Workflow

Add:

- Planning mode.
- Test runner integration.
- Code review agent.
- Documentation agent.
- Commit message generation.
- Reusable project playbooks.

### Phase 4 - Multi-Agent Orchestration

Add:

- Manager agent.
- Coder agent.
- Tester agent.
- Reviewer agent.
- Parallel task execution across different projects.

## 9. Recommended First Implementation Stack

- Gateway: Go.
- Storage: SQLite.
- Real-time updates: Server-Sent Events for PWA; bot messages for Telegram.
- Mobile MVP: Telegram bot first, then Next.js PWA.
- Desktop integration: VS Code extension after gateway MVP.
- Network: Tailscale.
- AI tools: existing local Codex and Claude Code installations.

## 10. Immediate Next Steps

1. Create a minimal Go gateway with `/health`, `/projects`, and `/tasks`.
2. Add SQLite persistence for projects, tasks, and task events.
3. Implement project allowlisting and path validation.
4. Add a Codex CLI adapter that can run a prompt inside a project directory.
5. Add Telegram bot commands for project list, task start, task status, and task logs.
6. Add explicit approval handling before dangerous commands or project-external file access.
