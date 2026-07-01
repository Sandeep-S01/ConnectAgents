# ConnectAgents VS Code Bridge

This extension is the Phase 1 bridge between an open VS Code workspace and the local ConnectAgents gateway.

## What It Does

- Registers the active workspace with the gateway.
- Polls the gateway for queued tasks for that workspace.
- Reports task progress and completion back to the gateway.
- Optionally invokes a configured VS Code command with the task prompt.

## Local Development

1. Open this folder in VS Code.
2. Press `F5` to launch an Extension Development Host.
3. Open the target project workspace in that host.
4. Configure:

```json
{
  "connectAgents.gatewayUrl": "http://127.0.0.1:8080",
  "connectAgents.authToken": "your-gateway-token",
  "connectAgents.codexCommand": ""
}
```

If `connectAgents.codexCommand` is empty, the bridge copies the prompt to the clipboard and writes it to the `ConnectAgents Bridge` output channel. Once the Codex extension exposes a stable command ID, set that command ID here so the bridge can invoke it.
