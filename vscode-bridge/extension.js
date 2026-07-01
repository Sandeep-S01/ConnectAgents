const vscode = require("vscode");

let bridgeId = "";
let workspacePath = "";
let pollTimer;
let polling = false;
let output;

function activate(context) {
  output = vscode.window.createOutputChannel("ConnectAgents Bridge");
  context.subscriptions.push(output);
  context.subscriptions.push(vscode.commands.registerCommand("connectAgents.registerBridge", registerBridge));
  context.subscriptions.push(vscode.commands.registerCommand("connectAgents.pollOnce", pollOnce));
  context.subscriptions.push(vscode.commands.registerCommand("connectAgents.showStatus", showStatus));

  registerBridge().then(startPolling).catch((error) => {
    log(`Bridge registration failed: ${error.message}`);
  });
}

function deactivate() {
  if (pollTimer) {
    clearInterval(pollTimer);
  }
}

function config() {
  const cfg = vscode.workspace.getConfiguration("connectAgents");
  const envGatewayUrl = process.env.PAIA_ADDR ? `http://${process.env.PAIA_ADDR}` : "";
  return {
    gatewayUrl: String(cfg.get("gatewayUrl") || envGatewayUrl || "http://127.0.0.1:8080").replace(/\/+$/, ""),
    authToken: String(cfg.get("authToken") || process.env.PAIA_AUTH_TOKEN || ""),
    pollIntervalMs: Number(cfg.get("pollIntervalMs") || 2000),
    codexCommand: String(cfg.get("codexCommand") || "")
  };
}

async function registerBridge() {
  const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
  if (!folder) {
    throw new Error("Open a workspace folder before registering the bridge.");
  }

  workspacePath = folder.uri.fsPath;
  const body = {
    workspacePath,
    workspaceName: folder.name
  };
  const response = await gatewayFetch("/api/bridge/register", {
    method: "POST",
    body: JSON.stringify(body)
  });
  if (!response.ok) {
    throw new Error(`gateway returned ${response.status}: ${await response.text()}`);
  }
  const payload = await response.json();
  bridgeId = payload.bridgeId || "";
  if (!bridgeId) {
    throw new Error("gateway did not return a bridgeId");
  }
  log(`Registered bridge ${bridgeId} for ${workspacePath}`);
  return bridgeId;
}

function startPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
  }
  const interval = Math.max(config().pollIntervalMs, 500);
  pollTimer = setInterval(() => {
    pollOnce().catch((error) => log(`Poll failed: ${error.message}`));
  }, interval);
  pollOnce().catch((error) => log(`Initial poll failed: ${error.message}`));
}

async function pollOnce() {
  if (!bridgeId || polling) {
    return;
  }
  polling = true;
  try {
    const response = await gatewayFetch(`/api/bridge/${encodeURIComponent(bridgeId)}/tasks/next`);
    if (response.status === 204) {
      return;
    }
    if (!response.ok) {
      throw new Error(`gateway returned ${response.status}: ${await response.text()}`);
    }
    const task = await response.json();
    await executeTask(task);
  } finally {
    polling = false;
  }
}

async function executeTask(task) {
  const prompt = String(task.prompt || "");
  const taskId = String(task.taskId || "");
  const projectName = String(task.projectName || "");
  const projectPath = String(task.projectPath || "");
  const techStack = String(task.techStack || "");
  const gitBranch = String(task.gitBranch || "");
  const bridgeText = [
    `Task: ${taskId}`,
    projectName ? `Project: ${projectName}` : "",
    projectPath ? `Path: ${projectPath}` : "",
    techStack ? `Tech stack: ${techStack}` : "",
    gitBranch ? `Git branch: ${gitBranch}` : "",
    "",
    prompt
  ].filter(Boolean).join("\n");
  log(`Received task ${taskId}: ${prompt}`);
  await reportTaskEvent(taskId, {
    status: "running",
    message: "VS Code bridge received task"
  });

  await vscode.env.clipboard.writeText(prompt);
  output.show(true);
  output.appendLine("");
  output.appendLine("========== Mobile Task ==========");
  output.appendLine(bridgeText);
  output.appendLine("=================================");

  const commandId = config().codexCommand;
  if (commandId) {
    try {
      try {
        await vscode.commands.executeCommand(commandId, prompt);
      } catch (firstError) {
        await vscode.commands.executeCommand(commandId);
      }
      await reportTaskEvent(taskId, {
        status: "completed",
        message: `Task delivered to VS Code command ${commandId}`,
        stdout: `Prompt copied to clipboard and Codex command ${commandId} was invoked. Paste the prompt into Codex if the command did not accept arguments.`
      });
      return;
    } catch (error) {
      await reportTaskEvent(taskId, {
        status: "failed",
        message: `VS Code command ${commandId} failed`,
        stderr: error.message
      });
      return;
    }
  }

  await reportTaskEvent(taskId, {
    status: "completed",
    message: "Task delivered to VS Code bridge output and clipboard",
    stdout: "No Codex command is configured yet. The prompt was copied to the clipboard and written to the ConnectAgents Bridge output channel."
  });
}

async function reportTaskEvent(taskId, event) {
  if (!bridgeId || !taskId) {
    return;
  }
  const response = await gatewayFetch(`/api/bridge/${encodeURIComponent(bridgeId)}/tasks/${encodeURIComponent(taskId)}/events`, {
    method: "POST",
    body: JSON.stringify(event)
  });
  if (!response.ok) {
    throw new Error(`failed to report task event: ${response.status}: ${await response.text()}`);
  }
}

async function gatewayFetch(path, options = {}) {
  const cfg = config();
  const headers = Object.assign({ "Content-Type": "application/json" }, options.headers || {});
  if (cfg.authToken) {
    headers.Authorization = `Bearer ${cfg.authToken}`;
  }
  return fetch(`${cfg.gatewayUrl}${path}`, Object.assign({}, options, { headers }));
}

function showStatus() {
  const status = bridgeId ? `connected as ${bridgeId}` : "not connected";
  vscode.window.showInformationMessage(`ConnectAgents bridge is ${status}. Workspace: ${workspacePath || "-"}`);
}

function log(message) {
  const line = `[${new Date().toISOString()}] ${message}`;
  output.appendLine(line);
}

module.exports = {
  activate,
  deactivate
};
