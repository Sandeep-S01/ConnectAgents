package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"personal-ai-assistant/internal/store"
)

type Bot struct {
	token        string
	userID       int64
	baseURL      string
	authToken    string
	httpClient   *http.Client
	logger       *slog.Logger
	lastUpdateID int64
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID int64 `json:"id"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type TelegramResponse struct {
	Ok     bool     `json:"ok"`
	Result []Update `json:"result"`
}

func NewBot(token string, userID int64, gatewayAddr string, authToken string) *Bot {
	// Parse the loopback URL
	host, port, err := net.SplitHostPort(gatewayAddr)
	if err != nil {
		port = strings.TrimPrefix(gatewayAddr, ":")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	baseURL := fmt.Sprintf("http://%s", net.JoinHostPort(host, port))

	return &Bot{
		token:      token,
		userID:     userID,
		baseURL:    baseURL,
		authToken:  authToken,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     slog.Default().With("component", "telegram_bot"),
	}
}

func (b *Bot) GatewayBaseURL() string {
	return b.baseURL
}

func (b *Bot) Start(ctx context.Context) {
	b.logger.Info("starting telegram bot long-polling loop", "user_id", b.userID, "gateway_base_url", b.baseURL)
	if err := b.sendMessage(b.userID, "AI Gateway Bot started and listening for commands."); err != nil {
		b.logger.Error("telegram startup message failed", "error", err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("telegram bot stopping")
			return
		case <-ticker.C:
			updates, err := b.getUpdates(ctx)
			if err != nil {
				b.logger.Error("failed to get updates", "error", err)
				time.Sleep(5 * time.Second) // backoff on failure
				continue
			}

			for _, update := range updates {
				b.lastUpdateID = update.UpdateID
				if update.Message != nil {
					b.handleMessage(ctx, *update.Message)
				}
			}
		}
	}
}

func (b *Bot) getUpdates(ctx context.Context) ([]Update, error) {
	u := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", b.token)
	payload := map[string]any{
		"offset":  b.lastUpdateID + 1,
		"timeout": 20,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, string(body))
	}

	var tr TelegramResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}

	if !tr.Ok {
		return nil, errors.New("telegram API response ok=false")
	}

	return tr.Result, nil
}

func (b *Bot) sendMessage(chatID int64, text string) error {
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		b.logger.Error("failed to marshal message payload", "error", err)
		return err
	}

	resp, err := b.httpClient.Post(u, "application/json", bytes.NewReader(data))
	if err != nil {
		b.logger.Error("failed to send message", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("telegram API send failed: status %d: %s", resp.StatusCode, string(body))
		b.logger.Error("telegram API send failed", "status", resp.StatusCode, "body", string(body))
		return err
	}
	return nil
}

func (b *Bot) HandleEvent(event store.TaskEvent) {
	var emoji string
	var notify bool

	switch event.EventType {
	case "approval.requested":
		emoji = "⚠️"
		notify = true
		// Query pending approvals to find the one for this task to include its ID
		var approvals []map[string]any
		time.Sleep(100 * time.Millisecond) // Give the database transaction a split second to commit fully
		if err := b.doGatewayRequest("GET", "/api/approvals?status=pending", nil, &approvals); err == nil {
			for _, app := range approvals {
				if app["taskId"] == event.TaskID {
					msg := fmt.Sprintf("⚠️ <b>Approval Required</b>\n\n<b>Task ID:</b> <code>%s</code>\n<b>Approval ID:</b> <code>%v</code>\n<b>Request:</b> %s\n\nRun:\n/approve %v\n/reject %v\n/execute %v",
						event.TaskID, app["id"], app["description"], app["id"], app["id"], app["id"])
					b.sendMessage(b.userID, msg)
					return
				}
			}
		}
	case "agent.completed", "command.completed":
		emoji = "✅"
		notify = true
	case "agent.failed", "command.failed", "agent.timed_out":
		emoji = "❌"
		notify = true
	case "command.blocked":
		emoji = "🚫"
		notify = true
	case "task.created":
		emoji = "🆕"
		notify = true
	case "agent.started", "command.started":
		emoji = "🔄"
		notify = true
	}

	if !notify {
		return
	}

	msg := fmt.Sprintf("%s <b>Event: %s</b>\n<b>Task ID:</b> <code>%s</code>\n<b>Message:</b> %s",
		emoji, event.EventType, event.TaskID, event.Message)
	if summary := b.taskOutputSummary(event.TaskID); summary != "" {
		msg += "\n\n" + summary
	}
	b.sendMessage(b.userID, msg)
}

func (b *Bot) handleMessage(ctx context.Context, msg Message) {
	if msg.From == nil || msg.From.ID != b.userID {
		b.logger.Warn("unauthorized message received", "from_id", msg.From.ID)
		b.sendMessage(msg.Chat.ID, "🚫 <b>Unauthorized</b>: Only the gateway owner can control this assistant.")
		return
	}

	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, "/") {
		return
	}

	parts := strings.Fields(text)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/start", "/help":
		b.handleHelp(msg.Chat.ID)
	case "/setup":
		b.handleSetup(msg.Chat.ID)
	case "/testsetup":
		b.handleTestSetup(msg.Chat.ID)
	case "/projects":
		b.handleProjects(msg.Chat.ID)
	case "/addproject":
		b.handleAddProject(msg.Chat.ID, text)
	case "/updateproject":
		b.handleUpdateProject(msg.Chat.ID, text)
	case "/removeproject":
		b.handleRemoveProject(msg.Chat.ID, args)
	case "/doctor":
		b.handleDoctor(msg.Chat.ID)
	case "/git":
		b.handleGit(msg.Chat.ID, args)
	case "/commit":
		b.handleGitCommitCmd(msg.Chat.ID, args)
	case "/push":
		b.handleGitPushCmd(msg.Chat.ID, args)
	case "/tasks":
		b.handleTasks(msg.Chat.ID)
	case "/run":
		b.handleRun(msg.Chat.ID, args)
	case "/status", "/task":
		b.handleStatus(msg.Chat.ID, args)
	case "/runtest":
		b.handleRunTest(msg.Chat.ID, args)
	case "/logs":
		b.handleLogs(msg.Chat.ID, args)
	case "/cancel":
		b.handleCancel(msg.Chat.ID, args)
	case "/approve":
		b.handleResolveApproval(msg.Chat.ID, args, "approve")
	case "/reject":
		b.handleResolveApproval(msg.Chat.ID, args, "reject")
	case "/execute":
		b.handleExecute(msg.Chat.ID, args)
	default:
		b.sendMessage(msg.Chat.ID, fmt.Sprintf("❓ Unknown command: %s. Use /help for a list of commands.", cmd))
	}
}

func (b *Bot) handleHelp(chatID int64) {
	help := `<b>Personal AI Assistant Gateway Bot</b>

<b>First Time Setup:</b>
/setup - Show phone setup checklist
/testsetup - Run setup validation from the phone
/doctor - Check gateway health, project access, and bot configuration
/projects - List registered workspace projects
/addproject &lt;name&gt; | &lt;absolute_path&gt; [| tech_stack] - Register a local project

<b>Commands:</b>
/projects - List registered workspace projects
/addproject &lt;name&gt; | &lt;absolute_path&gt; [| tech_stack] - Register a local project
/updateproject &lt;project_id&gt; | &lt;name&gt; | &lt;absolute_path&gt; [| tech_stack] - Update a project
/removeproject &lt;project_id&gt; - Remove a registered project
/doctor - Check gateway health, project access, and bot configuration
/setup - Show phone setup checklist
/testsetup - Run setup validation from the phone
/git &lt;project_id&gt; - Get short Git status and diff stats
/commit &lt;project_id&gt; &lt;message&gt; - Stage all changes and commit
/push &lt;project_id&gt; - Push current branch to remote origin
/tasks - List the last 5 tasks and their status
/run &lt;project_id&gt; &lt;prompt&gt; - Create and run a Codex agent task
/status &lt;task_id&gt; - Show task status and latest event
/task &lt;task_id&gt; - Alias for /status
/runtest &lt;project_id&gt; - Run the configured project test command
/logs &lt;task_id&gt; - Show recent task events
/cancel &lt;task_id&gt; - Cancel a running Codex task
/approve &lt;approval_id&gt; - Approve a pending request
/reject &lt;approval_id&gt; - Reject a pending request
/execute &lt;approval_id&gt; - Execute an approved command request`

	b.sendMessage(chatID, help)
}

func (b *Bot) handleSetup(chatID int64) {
	msg := fmt.Sprintf(`<b>First Time Setup</b>

<b>Gateway URL:</b> <code>%s</code>

1. Run <code>/testsetup</code> to validate the PC gateway.
2. Run <code>/projects</code> to see registered projects.
3. If no project is registered, run:
<code>/addproject ConnectAgents | D:\Personal_Project\ConnectAgents | Go</code>
4. Run <code>/doctor</code> any time something looks wrong.

After a project exists, use:
<code>/run &lt;project_id&gt; &lt;prompt&gt;</code>`,
		escapeTelegram(b.baseURL),
	)

	b.sendMessage(chatID, msg)
}

func (b *Bot) handleTestSetup(chatID int64) {
	var doctor struct {
		Status string `json:"status"`
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
		Projects []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"projects"`
	}
	if err := b.doGatewayRequest("GET", "/api/doctor", nil, &doctor); err != nil {
		b.sendMessage(chatID, "<b>Setup Test</b>\n\nGateway reachable: failed\nError: "+escapeTelegram(err.Error()))
		return
	}

	var sb strings.Builder
	sb.WriteString("<b>Setup Test</b>\n\n")
	sb.WriteString("Gateway reachable: ok\n")
	sb.WriteString(fmt.Sprintf("Overall status: %s\n\n", escapeTelegram(doctor.Status)))
	sb.WriteString("<b>Checks:</b>\n")
	for _, check := range doctor.Checks {
		sb.WriteString(fmt.Sprintf("- %s: %s", escapeTelegram(check.Name), escapeTelegram(check.Status)))
		if check.Message != "" {
			sb.WriteString(" - " + escapeTelegram(truncateText(check.Message, 140)))
		}
		sb.WriteString("\n")
	}
	if len(doctor.Projects) == 0 {
		sb.WriteString("\nProjects: none registered\n")
		sb.WriteString("Next: use /addproject to register a workspace.\n")
	} else {
		sb.WriteString("\n<b>Projects:</b>\n")
		for _, project := range doctor.Projects {
			sb.WriteString(fmt.Sprintf("- %s (<code>%s</code>): %s\n",
				escapeTelegram(project.Name),
				escapeTelegram(project.ID),
				escapeTelegram(project.Status),
			))
		}
	}

	b.sendMessage(chatID, truncateText(sb.String(), 3900))
}

func (b *Bot) handleProjects(chatID int64) {
	var projects []map[string]any
	if err := b.doGatewayRequest("GET", "/api/projects", nil, &projects); err != nil {
		b.sendMessage(chatID, "❌ Failed to list projects: "+err.Error())
		return
	}

	if len(projects) == 0 {
		b.sendMessage(chatID, "📁 No projects registered yet.")
		return
	}

	var sb strings.Builder
	sb.WriteString("📁 <b>Registered Projects:</b>\n\n")
	for _, p := range projects {
		sb.WriteString(fmt.Sprintf("- <b>%s</b>\n  ID: <code>%s</code>\n  Path: <code>%s</code>\n\n", p["name"], p["id"], p["path"]))
	}

	b.sendMessage(chatID, sb.String())
}

func (b *Bot) handleAddProject(chatID int64, text string) {
	body := strings.TrimSpace(strings.TrimPrefix(text, "/addproject"))
	parts := strings.Split(body, "|")
	if len(parts) < 2 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/addproject &lt;name&gt; | &lt;absolute_path&gt; [| tech_stack]</code>")
		return
	}

	name := strings.TrimSpace(parts[0])
	path := strings.TrimSpace(parts[1])
	techStack := ""
	if len(parts) > 2 {
		techStack = strings.TrimSpace(parts[2])
	}
	if name == "" || path == "" {
		b.sendMessage(chatID, "⚠️ Project name and absolute path are required.")
		return
	}

	payload := map[string]string{
		"name":      name,
		"path":      path,
		"techStack": techStack,
	}

	var project map[string]any
	if err := b.doGatewayRequest("POST", "/api/projects", payload, &project); err != nil {
		b.sendMessage(chatID, "❌ Failed to register project: "+err.Error())
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("✅ <b>Project registered</b>\n\n<b>Name:</b> %s\n<b>ID:</b> <code>%s</code>\n<b>Path:</b> <code>%s</code>",
		escapeTelegram(formatAny(project["name"])),
		escapeTelegram(formatAny(project["id"])),
		escapeTelegram(formatAny(project["path"])),
	))
}

func (b *Bot) handleUpdateProject(chatID int64, text string) {
	body := strings.TrimSpace(strings.TrimPrefix(text, "/updateproject"))
	parts := strings.Split(body, "|")
	if len(parts) < 3 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/updateproject &lt;project_id&gt; | &lt;name&gt; | &lt;absolute_path&gt; [| tech_stack]</code>")
		return
	}

	projectID := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	path := strings.TrimSpace(parts[2])
	techStack := ""
	if len(parts) > 3 {
		techStack = strings.TrimSpace(parts[3])
	}
	if projectID == "" || name == "" || path == "" {
		b.sendMessage(chatID, "⚠️ Project ID, name, and absolute path are required.")
		return
	}

	payload := map[string]string{
		"name":      name,
		"path":      path,
		"techStack": techStack,
	}

	var project map[string]any
	updatePath := "/api/projects/" + url.PathEscape(projectID)
	if err := b.doGatewayRequest("PUT", updatePath, payload, &project); err != nil {
		b.sendMessage(chatID, "❌ Failed to update project: "+err.Error())
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("✅ <b>Project updated</b>\n\n<b>Name:</b> %s\n<b>ID:</b> <code>%s</code>\n<b>Path:</b> <code>%s</code>",
		escapeTelegram(formatAny(project["name"])),
		escapeTelegram(formatAny(project["id"])),
		escapeTelegram(formatAny(project["path"])),
	))
}

func (b *Bot) handleRemoveProject(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/removeproject &lt;project_id&gt;</code>")
		return
	}
	projectID := args[0]

	removePath := "/api/projects/" + url.PathEscape(projectID)
	if err := b.doGatewayRequest("DELETE", removePath, nil, nil); err != nil {
		b.sendMessage(chatID, "❌ Failed to remove project: "+err.Error())
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("✅ Project removed: <code>%s</code>", escapeTelegram(projectID)))
}

func (b *Bot) handleDoctor(chatID int64) {
	var doctor struct {
		Status string `json:"status"`
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
		Projects []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"projects"`
	}
	if err := b.doGatewayRequest("GET", "/api/doctor", nil, &doctor); err != nil {
		b.sendMessage(chatID, "❌ Failed to run gateway doctor: "+err.Error())
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Gateway Doctor</b>\n\n<b>Status:</b> <code>%s</code>\n<b>Gateway URL:</b> <code>%s</code>\n\n",
		escapeTelegram(doctor.Status),
		escapeTelegram(b.baseURL),
	))
	sb.WriteString("<b>Checks:</b>\n")
	for _, check := range doctor.Checks {
		sb.WriteString(fmt.Sprintf("- %s: %s", escapeTelegram(check.Name), escapeTelegram(check.Status)))
		if check.Message != "" {
			sb.WriteString(" - " + escapeTelegram(truncateText(check.Message, 180)))
		}
		sb.WriteString("\n")
	}
	if len(doctor.Projects) > 0 {
		sb.WriteString("\n<b>Projects:</b>\n")
		for _, project := range doctor.Projects {
			sb.WriteString(fmt.Sprintf("- %s (<code>%s</code>): %s\n",
				escapeTelegram(project.Name),
				escapeTelegram(project.ID),
				escapeTelegram(project.Status),
			))
		}
	}

	b.sendMessage(chatID, truncateText(sb.String(), 3900))
}

func (b *Bot) handleGit(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/git &lt;project_id&gt;</code>")
		return
	}
	projectID := args[0]

	var git map[string]any
	path := "/api/projects/" + url.PathEscape(projectID) + "/git"
	if err := b.doGatewayRequest("GET", path, nil, &git); err != nil {
		b.sendMessage(chatID, "❌ Failed to get Git summary: "+err.Error())
		return
	}

	statusLines, _ := git["status"].([]any)
	var statusSummary string
	if len(statusLines) == 0 {
		statusSummary = "Clean working directory."
	} else {
		statusSummary = fmt.Sprintf("%d modified/untracked files.", len(statusLines))
	}

	var diff string
	if d, ok := git["diffStat"].(string); ok && d != "" {
		diff = d
	} else {
		diff = "No differences."
	}

	msg := fmt.Sprintf("🌿 <b>Git Summary</b> (Project ID: <code>%s</code>)\n\n<b>Branch:</b> <code>%v</code>\n<b>Status:</b> %s\n\n<b>Diff Stats:</b>\n<pre>%s</pre>",
		projectID, git["branch"], statusSummary, diff)

	b.sendMessage(chatID, msg)
}

func (b *Bot) handleTasks(chatID int64) {
	var tasks []map[string]any
	// Fetch last 5 tasks
	if err := b.doGatewayRequest("GET", "/api/tasks", nil, &tasks); err != nil {
		b.sendMessage(chatID, "❌ Failed to list tasks: "+err.Error())
		return
	}

	if len(tasks) == 0 {
		b.sendMessage(chatID, "📋 No tasks found.")
		return
	}

	// cap at 5
	limit := len(tasks)
	if limit > 5 {
		limit = 5
	}

	var sb strings.Builder
	sb.WriteString("📋 <b>Last 5 Tasks:</b>\n\n")
	for i := 0; i < limit; i++ {
		t := tasks[i]
		sb.WriteString(fmt.Sprintf("- <b>%s</b> [ID: <code>%s</code>]\n  Prompt: <i>%s</i>\n\n", t["status"], t["id"], t["prompt"]))
	}

	b.sendMessage(chatID, sb.String())
}

func (b *Bot) handleRun(chatID int64, args []string) {
	if len(args) < 2 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/run &lt;project_id&gt; &lt;prompt&gt;</code>")
		return
	}
	projectID := args[0]
	prompt := strings.Join(args[1:], " ")

	payload := map[string]string{
		"projectId": projectID,
		"agentType": "codex",
		"prompt":    prompt,
	}

	var task map[string]any
	if err := b.doGatewayRequest("POST", "/api/tasks", payload, &task); err != nil {
		b.sendMessage(chatID, "❌ Failed to create task: "+err.Error())
		return
	}

	taskID, ok := task["id"].(string)
	if !ok {
		b.sendMessage(chatID, "❌ Failed to parse task ID from response")
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("🆕 Created Task ID: <code>%s</code>. Starting Codex execution...", taskID))

	// Run the task asynchronously via loopback HTTP POST
	go func() {
		var result map[string]any
		runPath := "/api/tasks/" + url.PathEscape(taskID) + "/run"
		if err := b.doGatewayRequest("POST", runPath, nil, &result); err != nil {
			b.sendMessage(b.userID, fmt.Sprintf("❌ Codex run failed for Task <code>%s</code>: %s", taskID, err.Error()))
		}
	}()
}

func (b *Bot) handleStatus(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "âš ï¸ Usage: <code>/status &lt;task_id&gt;</code>")
		return
	}
	taskID := args[0]

	var task store.Task
	taskPath := "/api/tasks/" + url.PathEscape(taskID)
	if err := b.doGatewayRequest("GET", taskPath, nil, &task); err != nil {
		b.sendMessage(chatID, "âŒ Failed to get task status: "+err.Error())
		return
	}

	var events []store.TaskEvent
	eventsPath := "/api/tasks/" + url.PathEscape(taskID) + "/events"
	if err := b.doGatewayRequest("GET", eventsPath, nil, &events); err != nil {
		b.sendMessage(chatID, "âŒ Failed to get task events: "+err.Error())
		return
	}

	latest := "No events recorded."
	if len(events) > 0 {
		event := events[len(events)-1]
		latest = fmt.Sprintf("%s - %s", event.EventType, event.Message)
	}

	msg := fmt.Sprintf(`<b>Task Status</b>

<b>ID:</b> <code>%s</code>
<b>Project:</b> <code>%s</code>
<b>Agent:</b> <code>%s</code>
<b>Status:</b> <code>%s</code>
<b>Started:</b> %s
<b>Completed:</b> %s
<b>Prompt:</b> <i>%s</i>
<b>Latest Event:</b> %s`,
		escapeTelegram(task.ID),
		escapeTelegram(task.ProjectID),
		escapeTelegram(task.AgentType),
		escapeTelegram(task.Status),
		escapeTelegram(formatOptionalTime(task.StartedAt)),
		escapeTelegram(formatOptionalTime(task.CompletedAt)),
		escapeTelegram(truncateText(task.Prompt, 600)),
		escapeTelegram(truncateText(latest, 700)),
	)
	if task.ErrorMessage != "" {
		msg += "\n<b>Error:</b> " + escapeTelegram(truncateText(task.ErrorMessage, 700))
	}

	b.sendMessage(chatID, truncateText(msg, 3900))
}

func (b *Bot) handleRunTest(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/runtest &lt;project_id&gt;</code>")
		return
	}
	projectID := args[0]

	var result map[string]any
	testPath := "/api/projects/" + url.PathEscape(projectID) + "/tests/run"
	if err := b.doGatewayRequest("POST", testPath, nil, &result); err != nil {
		b.sendMessage(chatID, "❌ Failed to run project tests: "+err.Error())
		return
	}

	msg := fmt.Sprintf(`<b>Test Run Complete</b>

<b>Project:</b> <code>%s</code>
<b>Status:</b> <code>%s</code>
<b>Command:</b> <code>%s</code>
<b>Exit Code:</b> <code>%s</code>`,
		escapeTelegram(projectID),
		escapeTelegram(formatAny(result["status"])),
		escapeTelegram(formatAny(result["command"])),
		escapeTelegram(formatAny(result["exitCode"])),
	)
	if stdout := strings.TrimSpace(formatAny(result["stdout"])); stdout != "" {
		msg += "\n<b>Stdout:</b>\n<pre>" + escapeTelegram(truncateText(stdout, 700)) + "</pre>"
	}
	if stderr := strings.TrimSpace(formatAny(result["stderr"])); stderr != "" {
		msg += "\n<b>Stderr:</b>\n<pre>" + escapeTelegram(truncateText(stderr, 700)) + "</pre>"
	}

	b.sendMessage(chatID, truncateText(msg, 3900))
}

func (b *Bot) handleLogs(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "âš ï¸ Usage: <code>/logs &lt;task_id&gt;</code>")
		return
	}
	taskID := args[0]

	var events []store.TaskEvent
	eventsPath := "/api/tasks/" + url.PathEscape(taskID) + "/events"
	if err := b.doGatewayRequest("GET", eventsPath, nil, &events); err != nil {
		b.sendMessage(chatID, "âŒ Failed to get task logs: "+err.Error())
		return
	}
	if len(events) == 0 {
		b.sendMessage(chatID, fmt.Sprintf("ðŸ“‹ No events found for Task <code>%s</code>.", escapeTelegram(taskID)))
		return
	}

	start := 0
	if len(events) > 5 {
		start = len(events) - 5
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ðŸ“‹ <b>Recent Events</b> for Task <code>%s</code>\n\n", escapeTelegram(taskID)))
	for _, event := range events[start:] {
		sb.WriteString(fmt.Sprintf("<b>#%d %s</b>\n%s\n",
			event.ID,
			escapeTelegram(event.EventType),
			escapeTelegram(truncateText(event.Message, 500)),
		))
		if event.PayloadJSON != "" {
			sb.WriteString(fmt.Sprintf("<pre>%s</pre>\n", escapeTelegram(truncateText(event.PayloadJSON, 240))))
		}
		sb.WriteString("\n")
	}

	b.sendMessage(chatID, truncateText(sb.String(), 3900))
}

func (b *Bot) handleCancel(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "âš ï¸ Usage: <code>/cancel &lt;task_id&gt;</code>")
		return
	}
	taskID := args[0]

	var result map[string]any
	cancelPath := "/api/tasks/" + url.PathEscape(taskID) + "/cancel"
	if err := b.doGatewayRequest("POST", cancelPath, nil, &result); err != nil {
		b.sendMessage(chatID, "âŒ Failed to cancel task: "+err.Error())
		return
	}

	status, _ := result["status"].(string)
	if status == "" {
		status = "cancel_requested"
	}
	b.sendMessage(chatID, fmt.Sprintf("âœ… Cancel requested for Task <code>%s</code>. Status: <code>%s</code>",
		escapeTelegram(taskID),
		escapeTelegram(status),
	))
}

func (b *Bot) handleResolveApproval(chatID int64, args []string, action string) {
	if len(args) == 0 {
		b.sendMessage(chatID, fmt.Sprintf("⚠️ Usage: <code>/%s &lt;approval_id&gt;</code>", action))
		return
	}
	approvalID := args[0]

	path := fmt.Sprintf("/api/approvals/%s/%s", url.PathEscape(approvalID), action)
	var approval map[string]any
	if err := b.doGatewayRequest("POST", path, nil, &approval); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ Failed to %s request: %s", action, err.Error()))
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("✅ Request <code>%s</code> has been successfully %sd.", approvalID, action))
}

func (b *Bot) handleExecute(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/execute &lt;approval_id&gt;</code>")
		return
	}
	approvalID := args[0]

	b.sendMessage(chatID, fmt.Sprintf("⚙️ Launching execution for Approval <code>%s</code>...", approvalID))

	go func() {
		path := fmt.Sprintf("/api/approvals/%s/execute", url.PathEscape(approvalID))
		var result map[string]any
		if err := b.doGatewayRequest("POST", path, nil, &result); err != nil {
			b.sendMessage(b.userID, fmt.Sprintf("❌ Execution failed for Approval <code>%s</code>: %s", approvalID, err.Error()))
		}
	}()
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func truncateText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func escapeTelegram(value string) string {
	return html.EscapeString(value)
}

func formatAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func (b *Bot) taskOutputSummary(taskID string) string {
	var task store.Task
	taskPath := "/api/tasks/" + url.PathEscape(taskID)
	if err := b.doGatewayRequest("GET", taskPath, nil, &task); err != nil {
		return ""
	}

	var sections []string
	if stdout := strings.TrimSpace(task.Stdout); stdout != "" {
		sections = append(sections, "<b>Stdout:</b>\n<pre>"+escapeTelegram(truncateText(stdout, 700))+"</pre>")
	}
	if stderr := strings.TrimSpace(task.Stderr); stderr != "" {
		sections = append(sections, "<b>Stderr:</b>\n<pre>"+escapeTelegram(truncateText(stderr, 700))+"</pre>")
	}
	if task.ErrorMessage != "" {
		sections = append(sections, "<b>Error:</b> "+escapeTelegram(truncateText(task.ErrorMessage, 700)))
	}
	return strings.Join(sections, "\n")
}

func (b *Bot) doGatewayRequest(method, path string, reqBody any, respOut any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, b.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.authToken)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if msg, ok := errResp["error"]; ok {
			return errors.New(msg)
		}
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return errors.New(string(body))
		}
		return fmt.Errorf("gateway returned status %d", resp.StatusCode)
	}

	if respOut != nil {
		return json.NewDecoder(resp.Body).Decode(respOut)
	}
	return nil
}

func (b *Bot) handleGitCommitCmd(chatID int64, args []string) {
	if len(args) < 2 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/commit &lt;project_id&gt; &lt;message&gt;</code>")
		return
	}
	projectID := args[0]
	message := strings.Join(args[1:], " ")

	payload := map[string]string{
		"message": message,
	}

	b.sendMessage(chatID, fmt.Sprintf("⚙️ Committing changes for Project <code>%s</code>...", projectID))

	var result map[string]any
	path := "/api/projects/" + url.PathEscape(projectID) + "/git/commit"
	if err := b.doGatewayRequest("POST", path, payload, &result); err != nil {
		b.sendMessage(chatID, "❌ Commit failed: "+err.Error())
		return
	}

	stdout, _ := result["stdout"].(string)
	stderr, _ := result["stderr"].(string)
	msg := fmt.Sprintf("✅ <b>Commit Complete!</b>\n\n<b>Stdout:</b>\n<pre>%s</pre>", stdout)
	if stderr != "" {
		msg += fmt.Sprintf("\n<b>Stderr:</b>\n<pre>%s</pre>", stderr)
	}
	b.sendMessage(chatID, msg)
}

func (b *Bot) handleGitPushCmd(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "⚠️ Usage: <code>/push &lt;project_id&gt;</code>")
		return
	}
	projectID := args[0]

	b.sendMessage(chatID, fmt.Sprintf("⚙️ Pushing branch for Project <code>%s</code>...", projectID))

	var result map[string]any
	path := "/api/projects/" + url.PathEscape(projectID) + "/git/push"
	if err := b.doGatewayRequest("POST", path, nil, &result); err != nil {
		b.sendMessage(chatID, "❌ Push failed: "+err.Error())
		return
	}

	stdout, _ := result["stdout"].(string)
	stderr, _ := result["stderr"].(string)
	msg := fmt.Sprintf("✅ <b>Push Complete!</b>\n\n<b>Stdout:</b>\n<pre>%s</pre>", stdout)
	if stderr != "" {
		msg += fmt.Sprintf("\n<b>Stderr:</b>\n<pre>%s</pre>", stderr)
	}
	b.sendMessage(chatID, msg)
}
