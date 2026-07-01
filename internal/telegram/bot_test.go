package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-ai-assistant/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type sentMessages struct {
	mu       sync.Mutex
	messages []string
}

func (s *sentMessages) addFromRequest(r *http.Request) error {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, payload.Text)
	return nil
}

func (s *sentMessages) joined() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.messages, "\n---\n")
}

func newTestBot(gatewayURL string, sent *sentMessages) *Bot {
	bot := &Bot{
		token:     "test-token",
		userID:    42,
		baseURL:   gatewayURL,
		authToken: "auth-token",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasPrefix(r.URL.String(), "https://api.telegram.org/") {
					if err := sent.addFromRequest(r); err != nil {
						return nil, err
					}
					return jsonResponse(http.StatusOK, `{"ok":true,"result":[]}`), nil
				}
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return bot
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func ownerMessage(text string) Message {
	return Message{
		From: &User{ID: 42},
		Chat: Chat{ID: 42},
		Text: text,
	}
}

func TestNewBotExposesLoopbackGatewayBaseURLForWildcardBind(t *testing.T) {
	bot := NewBot("test-token", 42, "0.0.0.0:8080", "auth-token")

	if got, want := bot.GatewayBaseURL(), "http://127.0.0.1:8080"; got != want {
		t.Fatalf("unexpected gateway base URL: got %q want %q", got, want)
	}
}

func TestSendMessageReturnsTelegramAPIError(t *testing.T) {
	bot := newTestBot("", &sentMessages{})
	bot.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{"ok":false,"description":"Unauthorized"}`), nil
		}),
	}

	err := bot.sendMessage(42, "hello")
	if err == nil {
		t.Fatal("expected sendMessage to return Telegram API error")
	}
	if !strings.Contains(err.Error(), "telegram API send failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBotUsesSeparateGatewayClientWithoutShortTelegramTimeout(t *testing.T) {
	bot := NewBot("test-token", 42, "127.0.0.1:8080", "auth-token")

	if bot.httpClient.Timeout <= 20*time.Second {
		t.Fatalf("telegram client timeout must exceed long-poll timeout, got %s", bot.httpClient.Timeout)
	}
	if bot.gatewayClient == nil {
		t.Fatal("expected separate gateway client")
	}
	if bot.gatewayClient.Timeout != 0 {
		t.Fatalf("gateway client should not use short Telegram timeout, got %s", bot.gatewayClient.Timeout)
	}
}

func TestGatewayRequestsUseGatewayClient(t *testing.T) {
	bot := NewBot("test-token", 42, "127.0.0.1:8080", "auth-token")
	bot.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("gateway request unexpectedly used telegram client for %s", r.URL.String())
			return nil, nil
		}),
	}
	bot.gatewayClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/doctor" {
				t.Fatalf("unexpected gateway path: %s", r.URL.Path)
			}
			return jsonResponse(http.StatusOK, `{"status":"ok"}`), nil
		}),
	}

	var result map[string]any
	if err := bot.doGatewayRequest(http.MethodGet, "/api/doctor", nil, &result); err != nil {
		t.Fatalf("gateway request failed: %v", err)
	}
}

func TestHelpIncludesStatusLogsAndCancelCommands(t *testing.T) {
	sent := &sentMessages{}
	bot := newTestBot("", sent)

	bot.handleMessage(context.Background(), ownerMessage("/help"))

	message := sent.joined()
	for _, command := range []string{"/status", "/task", "/logs", "/cancel", "/runtest", "/addproject", "/updateproject", "/removeproject", "/doctor"} {
		if !strings.Contains(message, command) {
			t.Fatalf("help message does not include %s:\n%s", command, message)
		}
	}
}

func TestSetupShowsFirstTimePhoneWorkflow(t *testing.T) {
	sent := &sentMessages{}
	bot := newTestBot("http://127.0.0.1:8080", sent)

	bot.handleMessage(context.Background(), ownerMessage("/setup"))

	message := sent.joined()
	for _, text := range []string{"First Time Setup", "127.0.0.1:8080", "/doctor", "/projects", "/addproject", "/testsetup"} {
		if !strings.Contains(message, text) {
			t.Fatalf("setup response missing %q:\n%s", text, message)
		}
	}
}

func TestTestSetupUsesGatewayDoctorEndpoint(t *testing.T) {
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/doctor" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"status":"ok","checks":[{"name":"sqlite","status":"ok"},{"name":"git","status":"ok","message":"git version 2.50.0"},{"name":"codex","status":"ok","message":"codex version 0.1.0"}],"projects":[{"id":"proj_1","name":"Connect Agents","path":"D:\\Personal_Project\\ConnectAgents","status":"ok"}]}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/testsetup"))

	if got, want := strings.Join(paths, ","), "GET /api/doctor"; got != want {
		t.Fatalf("unexpected gateway calls: got %q want %q", got, want)
	}
	message := sent.joined()
	for _, text := range []string{"Setup Test", "Gateway reachable", "sqlite: ok", "git: ok", "codex: ok", "Connect Agents", "proj_1"} {
		if !strings.Contains(message, text) {
			t.Fatalf("testsetup response missing %q:\n%s", text, message)
		}
	}
}

func TestTaskAliasFetchesTaskStatus(t *testing.T) {
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/task_1":
			io.WriteString(w, `{"id":"task_1","projectId":"proj_1","agentType":"codex","prompt":"Run tests","status":"completed"}`)
		case "/api/tasks/task_1/events":
			io.WriteString(w, `[{"id":1,"taskId":"task_1","eventType":"agent.completed","message":"Codex task completed","createdAt":"2026-06-30T10:00:01+05:30"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/task task_1"))

	if got, want := strings.Join(paths, ","), "GET /api/tasks/task_1,GET /api/tasks/task_1/events"; got != want {
		t.Fatalf("unexpected gateway calls: got %q want %q", got, want)
	}
	if message := sent.joined(); !strings.Contains(message, "Task Status") || !strings.Contains(message, "completed") {
		t.Fatalf("task alias response missing status details:\n%s", message)
	}
}

func TestBridgesListsConnectedVSCodeBridges(t *testing.T) {
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/bridge" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `[{"bridgeId":"bridge_1","workspaceName":"ConnectAgents","workspacePath":"D:\\Personal_Project\\ConnectAgents","queuedTasks":2}]`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/bridges"))

	if methodPath != "GET /api/bridge" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	message := sent.joined()
	for _, text := range []string{"Connected VS Code Bridges", "bridge_1", "ConnectAgents", "Queue"} {
		if !strings.Contains(message, text) {
			t.Fatalf("bridges response missing %q:\n%s", text, message)
		}
	}
}

func TestRunTestPostsToProjectTestEndpoint(t *testing.T) {
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"completed","command":"go test ./...","exitCode":0,"stdout":"ok personal-ai-assistant/internal/telegram","stderr":""}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/runtest proj_1"))

	if methodPath != "POST /api/projects/proj_1/tests/run" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	message := sent.joined()
	for _, text := range []string{"Test Run Complete", "go test ./...", "completed", "ok personal-ai-assistant"} {
		if !strings.Contains(message, text) {
			t.Fatalf("runtest response missing %q:\n%s", text, message)
		}
	}
}

func TestCompletedEventIncludesTaskOutputSummary(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/tasks/task_1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"task_1","projectId":"proj_1","agentType":"codex","prompt":"Run tests","status":"completed","stdout":"tests passed successfully","stderr":""}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.HandleEvent(store.TaskEvent{
		TaskID:    "task_1",
		EventType: "agent.completed",
		Message:   "Codex task completed",
	})

	message := sent.joined()
	for _, text := range []string{"agent.completed", "task_1", "tests passed successfully"} {
		if !strings.Contains(message, text) {
			t.Fatalf("completion notification missing %q:\n%s", text, message)
		}
	}
}

func TestApproveTaskPlanStartsCodexRun(t *testing.T) {
	runCalled := make(chan string, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/approvals/appr_1/approve":
			io.WriteString(w, `{"id":"appr_1","taskId":"task_1","actionType":"task_plan","status":"approved"}`)
		case "POST /api/tasks/task_1/run":
			runCalled <- r.Method + " " + r.URL.Path
			io.WriteString(w, `{"status":"completed","stdout":"done"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleResolveApproval(42, []string{"appr_1"}, "approve")

	select {
	case got := <-runCalled:
		if got != "POST /api/tasks/task_1/run" {
			t.Fatalf("unexpected run request: %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected approved task plan to start codex run")
	}
}

func TestAddProjectPostsProjectToGateway(t *testing.T) {
	var payload map[string]string
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode project payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"proj_1","name":"Connect Agents","path":"D:\\Personal Project\\ConnectAgents","techStack":"Go"}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage(`/addproject Connect Agents | D:\Personal Project\ConnectAgents | Go`))

	if methodPath != "POST /api/projects" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	if payload["name"] != "Connect Agents" || payload["path"] != `D:\Personal Project\ConnectAgents` || payload["techStack"] != "Go" {
		t.Fatalf("unexpected project payload: %#v", payload)
	}
	message := sent.joined()
	for _, text := range []string{"Project registered", "proj_1", "Connect Agents"} {
		if !strings.Contains(message, text) {
			t.Fatalf("addproject response missing %q:\n%s", text, message)
		}
	}
}

func TestUpdateProjectPutsProjectToGateway(t *testing.T) {
	var payload map[string]string
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode project payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"proj_1","name":"Connect Agents","path":"D:\\Personal_Project\\ConnectAgents","techStack":"Go"}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage(`/updateproject proj_1 | Connect Agents | D:\Personal_Project\ConnectAgents | Go`))

	if methodPath != "PUT /api/projects/proj_1" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	if payload["name"] != "Connect Agents" || payload["path"] != `D:\Personal_Project\ConnectAgents` || payload["techStack"] != "Go" {
		t.Fatalf("unexpected project payload: %#v", payload)
	}
	message := sent.joined()
	for _, text := range []string{"Project updated", "proj_1", "Connect Agents"} {
		if !strings.Contains(message, text) {
			t.Fatalf("updateproject response missing %q:\n%s", text, message)
		}
	}
}

func TestRemoveProjectDeletesProjectFromGateway(t *testing.T) {
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/removeproject proj_1"))

	if methodPath != "DELETE /api/projects/proj_1" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	message := sent.joined()
	for _, text := range []string{"Project removed", "proj_1"} {
		if !strings.Contains(message, text) {
			t.Fatalf("removeproject response missing %q:\n%s", text, message)
		}
	}
}

func TestDoctorUsesGatewayDoctorEndpoint(t *testing.T) {
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/doctor" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"status":"degraded","checks":[{"name":"sqlite","status":"ok"},{"name":"git","status":"ok","message":"git version 2.50.0"},{"name":"codex","status":"failed","message":"codex not found"}],"projects":[{"id":"proj_1","name":"Connect Agents","path":"D:\\Personal_Project\\ConnectAgents","status":"ok"}]}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/doctor"))

	if got, want := strings.Join(paths, ","), "GET /api/doctor"; got != want {
		t.Fatalf("unexpected gateway calls: got %q want %q", got, want)
	}
	message := sent.joined()
	for _, text := range []string{"Gateway Doctor", "degraded", "sqlite: ok", "codex: failed", "Connect Agents"} {
		if !strings.Contains(message, text) {
			t.Fatalf("doctor response missing %q:\n%s", text, message)
		}
	}
}

func TestStatusFetchesTaskAndLatestEvent(t *testing.T) {
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/task_1":
			io.WriteString(w, `{"id":"task_1","projectId":"proj_1","agentType":"codex","prompt":"Run tests","status":"running","startedAt":"2026-06-30T10:00:00+05:30"}`)
		case "/api/tasks/task_1/events":
			io.WriteString(w, `[{"id":1,"taskId":"task_1","eventType":"agent.started","message":"Starting Codex","createdAt":"2026-06-30T10:00:01+05:30"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/status task_1"))

	if got, want := strings.Join(paths, ","), "GET /api/tasks/task_1,GET /api/tasks/task_1/events"; got != want {
		t.Fatalf("unexpected gateway calls: got %q want %q", got, want)
	}
	message := sent.joined()
	for _, text := range []string{"task_1", "running", "Run tests", "agent.started", "Starting Codex"} {
		if !strings.Contains(message, text) {
			t.Fatalf("status response missing %q:\n%s", text, message)
		}
	}
}

func TestLogsFetchesEventsAndTruncatesLongPayloads(t *testing.T) {
	longPayload := strings.Repeat("x", 600)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/tasks/task_1/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":          1,
				"taskId":      "task_1",
				"eventType":   "agent.completed",
				"message":     "Codex task completed",
				"payloadJson": longPayload,
				"createdAt":   "2026-06-30T10:00:02+05:30",
			},
		})
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/logs task_1"))

	message := sent.joined()
	if !strings.Contains(message, "agent.completed") {
		t.Fatalf("logs response missing event type:\n%s", message)
	}
	if strings.Contains(message, longPayload) {
		t.Fatalf("logs response included untruncated payload")
	}
	if !strings.Contains(message, "...") {
		t.Fatalf("logs response does not show truncation:\n%s", message)
	}
}

func TestCancelPostsToCancelEndpoint(t *testing.T) {
	var methodPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodPath = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"task_1","status":"cancel_requested"}`)
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/cancel task_1"))

	if methodPath != "POST /api/tasks/task_1/cancel" {
		t.Fatalf("unexpected gateway call: %s", methodPath)
	}
	if message := sent.joined(); !strings.Contains(message, "task_1") || !strings.Contains(message, "cancel") {
		t.Fatalf("cancel response missing task/cancel text:\n%s", message)
	}
}
