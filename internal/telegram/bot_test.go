package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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

func TestHelpIncludesStatusLogsAndCancelCommands(t *testing.T) {
	sent := &sentMessages{}
	bot := newTestBot("", sent)

	bot.handleMessage(context.Background(), ownerMessage("/help"))

	message := sent.joined()
	for _, command := range []string{"/status", "/logs", "/cancel", "/addproject", "/doctor"} {
		if !strings.Contains(message, command) {
			t.Fatalf("help message does not include %s:\n%s", command, message)
		}
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

func TestDoctorChecksHealthProjectsAndAuthConfiguration(t *testing.T) {
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/api/projects":
			io.WriteString(w, `[{"id":"proj_1","name":"Connect Agents","path":"D:\\Personal_Project\\ConnectAgents"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	sent := &sentMessages{}
	bot := newTestBot(gateway.URL, sent)

	bot.handleMessage(context.Background(), ownerMessage("/doctor"))

	if got, want := strings.Join(paths, ","), "GET /health,GET /api/projects"; got != want {
		t.Fatalf("unexpected gateway calls: got %q want %q", got, want)
	}
	message := sent.joined()
	for _, text := range []string{"Gateway health", "ok", "Projects API", "1 registered", "Auth token"} {
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
