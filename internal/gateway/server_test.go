package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-ai-assistant/internal/gateway"
	"personal-ai-assistant/internal/store"
)

func TestServerHealthEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestServerHealthEndpointReportsStoreFailure(t *testing.T) {
	t.Parallel()

	s, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	server := gateway.NewServer(s, gateway.Options{})
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	server.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("expected unhealthy status, got %q", body["status"])
	}
}

func TestServerDoctorReportsRuntimeReadiness(t *testing.T) {
	t.Parallel()

	runner := &sequencedCommandRunner{
		results: []gateway.CommandResult{
			{Status: "completed", ExitCode: 0, Stdout: "git version 2.50.0\n"},
			{Status: "completed", ExitCode: 0, Stdout: "codex 1.0.0\n"},
		},
	}
	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken:     "secret-token",
		CommandRunner: runner,
	})

	projectPath := t.TempDir()
	projectResponse := doAuthenticatedJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Missing Later",
		"path": projectPath,
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if err := os.RemoveAll(projectPath); err != nil {
		t.Fatalf("remove project path: %v", err)
	}

	unauthorized := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	server.ServeHTTP(unauthorized, unauthorizedRequest)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected doctor to require auth, got %d", unauthorized.Code)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected doctor status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
		Projects []struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode doctor response: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("expected degraded status for missing project path, got %q", body.Status)
	}
	if len(body.Projects) != 1 || body.Projects[0].Status != "missing" {
		t.Fatalf("expected missing project status, got %#v", body.Projects)
	}
	checks := map[string]string{}
	for _, check := range body.Checks {
		checks[check.Name] = check.Status
	}
	for name, want := range map[string]string{
		"sqlite": "ok",
		"git":    "ok",
		"codex":  "ok",
	} {
		if checks[name] != want {
			t.Fatalf("expected check %s=%s, got %q in %#v", name, want, checks[name], checks)
		}
	}
	if got, want := strings.Join(runner.commands, ","), "git --version,codex --version"; got != want {
		t.Fatalf("unexpected doctor commands: got %q want %q", got, want)
	}
}

func TestServerServesDashboardShell(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("expected html content type, got %q", contentType)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Personal AI Assistant") {
		t.Fatalf("expected dashboard title, got %q", body)
	}
	if !strings.Contains(body, "/app.js") {
		t.Fatalf("expected app script reference, got %q", body)
	}
}

func TestServerServesDashboardAssets(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/app.js", contentType: "text/javascript; charset=utf-8", contains: "/auth/login"},
		{path: "/styles.css", contentType: "text/css; charset=utf-8", contains: ".app-shell"},
		{path: "/manifest.webmanifest", contentType: "application/manifest+json", contains: "Personal AI Assistant"},
		{path: "/sw.js", contentType: "text/javascript; charset=utf-8", contains: "install"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			server.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); contentType != tt.contentType {
				t.Fatalf("expected content type %q, got %q", tt.contentType, contentType)
			}
			if !strings.Contains(response.Body.String(), tt.contains) {
				t.Fatalf("expected body to contain %q, got %q", tt.contains, response.Body.String())
			}
		})
	}
}

func TestServerDashboardIncludesTaskHistoryUI(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	htmlResponse := httptest.NewRecorder()
	htmlRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	server.ServeHTTP(htmlResponse, htmlRequest)
	if htmlResponse.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", htmlResponse.Code, htmlResponse.Body.String())
	}
	html := htmlResponse.Body.String()
	for _, expected := range []string{"Task history", `id="task-list"`, `id="refresh-tasks"`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected dashboard html to contain %q", expected)
		}
	}

	jsResponse := httptest.NewRecorder()
	jsRequest := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	server.ServeHTTP(jsResponse, jsRequest)
	if jsResponse.Code != http.StatusOK {
		t.Fatalf("expected dashboard script status 200, got %d: %s", jsResponse.Code, jsResponse.Body.String())
	}
	js := jsResponse.Body.String()
	for _, expected := range []string{`"/api/tasks?projectId="`, "loadTasks", "selectTask"} {
		if !strings.Contains(js, expected) {
			t.Fatalf("expected dashboard js to contain %q", expected)
		}
	}

	cssResponse := httptest.NewRecorder()
	cssRequest := httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	server.ServeHTTP(cssResponse, cssRequest)
	if cssResponse.Code != http.StatusOK {
		t.Fatalf("expected dashboard styles status 200, got %d: %s", cssResponse.Code, cssResponse.Body.String())
	}
	if !strings.Contains(cssResponse.Body.String(), ".task-list button.is-selected") {
		t.Fatal("expected dashboard css to style the selected task history item")
	}
}

func TestServerCreatesAndListsProjects(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	createBody := map[string]string{
		"name":      "Gateway MVP",
		"path":      t.TempDir(),
		"techStack": "Go",
	}
	createResponse := doJSON(t, server, http.MethodPost, "/api/projects", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	server.ServeHTTP(listResponse, listRequest)

	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listResponse.Code)
	}

	var projects []map[string]any
	if err := json.NewDecoder(listResponse.Body).Decode(&projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
}

func TestServerReturnsProjectGitSummary(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := &sequencedCommandRunner{
		results: []gateway.CommandResult{
			{ExitCode: 0, Stdout: "main\n"},
			{ExitCode: 0, Stdout: " M README.md\n?? internal/new.go\n"},
			{ExitCode: 0, Stdout: " README.md | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n"},
		},
	}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Git Project",
		"path": projectPath,
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/projects/"+project["id"].(string)+"/git", nil)
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected git summary status 200, got %d: %s", response.Code, response.Body.String())
	}
	var summary map[string]any
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatalf("decode git summary: %v", err)
	}
	if summary["branch"] != "main" {
		t.Fatalf("expected branch main, got %v", summary["branch"])
	}
	statusLines := summary["status"].([]any)
	if len(statusLines) != 2 || statusLines[0] != " M README.md" || statusLines[1] != "?? internal/new.go" {
		t.Fatalf("unexpected status lines: %#v", statusLines)
	}
	if summary["diffStat"] != "README.md | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)" {
		t.Fatalf("unexpected diff stat: %v", summary["diffStat"])
	}
	expectedCommands := []string{"git branch --show-current", "git status --short", "git diff --stat"}
	if strings.Join(runner.commands, "\n") != strings.Join(expectedCommands, "\n") {
		t.Fatalf("expected commands %#v, got %#v", expectedCommands, runner.commands)
	}
	for _, workdir := range runner.workdirs {
		if workdir != projectPath {
			t.Fatalf("expected git command workdir %q, got %q", projectPath, workdir)
		}
	}
}

func TestServerCreatesTaskForRegisteredProject(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Gateway MVP",
		"path": t.TempDir(),
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d", projectResponse.Code)
	}

	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": project["id"].(string),
		"agentType": "codex",
		"prompt":    "Run tests",
	})
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d: %s", taskResponse.Code, taskResponse.Body.String())
	}

	var task map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task["status"] != "queued" {
		t.Fatalf("expected queued task, got %v", task["status"])
	}
}

func TestServerListsTasksWithFilters(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	firstProject := createTestProject(t, server, t.TempDir())
	secondProject := createTestProject(t, server, t.TempDir())

	firstTask := createTaskForProject(t, server, firstProject["id"].(string))
	secondTask := createTaskForProject(t, server, firstProject["id"].(string))
	thirdTask := createTaskForProject(t, server, secondProject["id"].(string))

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected list task status 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var allTasks []map[string]any
	if err := json.NewDecoder(listResponse.Body).Decode(&allTasks); err != nil {
		t.Fatalf("decode all tasks: %v", err)
	}
	if len(allTasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(allTasks))
	}
	if allTasks[0]["id"] != thirdTask["id"] || allTasks[1]["id"] != secondTask["id"] || allTasks[2]["id"] != firstTask["id"] {
		t.Fatalf("expected newest-first tasks, got %#v", []any{allTasks[0]["id"], allTasks[1]["id"], allTasks[2]["id"]})
	}

	projectResponse := httptest.NewRecorder()
	projectRequest := httptest.NewRequest(http.MethodGet, "/api/tasks?projectId="+firstProject["id"].(string), nil)
	server.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("expected project task status 200, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var projectTasks []map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&projectTasks); err != nil {
		t.Fatalf("decode project tasks: %v", err)
	}
	if len(projectTasks) != 2 {
		t.Fatalf("expected 2 project tasks, got %d", len(projectTasks))
	}

	queuedResponse := httptest.NewRecorder()
	queuedRequest := httptest.NewRequest(http.MethodGet, "/api/tasks?status=queued", nil)
	server.ServeHTTP(queuedResponse, queuedRequest)
	if queuedResponse.Code != http.StatusOK {
		t.Fatalf("expected queued task status 200, got %d: %s", queuedResponse.Code, queuedResponse.Body.String())
	}
	var queuedTasks []map[string]any
	if err := json.NewDecoder(queuedResponse.Body).Decode(&queuedTasks); err != nil {
		t.Fatalf("decode queued tasks: %v", err)
	}
	if len(queuedTasks) != 3 {
		t.Fatalf("expected 3 queued tasks, got %d", len(queuedTasks))
	}
}

func TestServerReturnsTaskAndEvents(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Gateway MVP",
		"path": t.TempDir(),
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d", projectResponse.Code)
	}

	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": project["id"].(string),
		"agentType": "codex",
		"prompt":    "Run tests",
	})
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d", taskResponse.Code)
	}

	var createdTask map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode created task: %v", err)
	}

	getTaskResponse := httptest.NewRecorder()
	getTaskRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+createdTask["id"].(string), nil)
	server.ServeHTTP(getTaskResponse, getTaskRequest)
	if getTaskResponse.Code != http.StatusOK {
		t.Fatalf("expected get task status 200, got %d", getTaskResponse.Code)
	}

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+createdTask["id"].(string)+"/events", nil)
	server.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}

	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestServerStreamsTaskEventsAsSSE(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	task := createTestTask(t, server, t.TempDir())

	response := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ServeHTTP(response, request)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	if response.Code != http.StatusOK {
		t.Fatalf("expected stream status 200, got %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected text/event-stream content type, got %q", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("expected SSE Cache-Control to include no-store, got %q", cacheControl)
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: task.created\n") {
		t.Fatalf("expected task.created SSE event, got %q", body)
	}
	if !strings.Contains(body, "data: {") {
		t.Fatalf("expected JSON SSE data, got %q", body)
	}
}

func TestServerStreamsTaskEventsAfterLastEventID(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	task := createTestTask(t, server, t.TempDir())

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events", nil)
	server.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one existing event, got %d", len(events))
	}

	response := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events/stream", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", strconv.FormatInt(int64(events[0]["id"].(float64)), 10))
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ServeHTTP(response, request)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	if response.Code != http.StatusOK {
		t.Fatalf("expected stream status 200, got %d: %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); strings.Contains(body, "event: task.created\n") {
		t.Fatalf("expected no already-delivered events, got %q", body)
	}
}

func TestServerStreamsNewTaskEventsToOpenSSEConnection(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t)
	task := createTestTask(t, handler, t.TempDir())

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events", nil)
	handler.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	lastEventID := strconv.FormatInt(int64(events[len(events)-1]["id"].(float64)), 10)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	streamRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/tasks/"+task["id"].(string)+"/events/stream", nil)
	if err != nil {
		t.Fatalf("create stream request: %v", err)
	}
	streamRequest.Header.Set("Last-Event-ID", lastEventID)
	streamResponse, err := server.Client().Do(streamRequest)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected stream status 200, got %d", streamResponse.StatusCode)
	}

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(streamResponse.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	response := postJSONURL(t, server.Client(), server.URL+"/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "type C:\\Users\\Sandeep\\.ssh\\id_rsa",
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected blocked command status 403, got %d", response.StatusCode)
	}
	defer response.Body.Close()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-lines:
			if line == "event: command.blocked" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for command.blocked SSE event")
		}
	}
}

func TestServerStreamsApprovalRequestedToOpenSSEConnection(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t)
	task := createTestTask(t, handler, t.TempDir())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	lines, closeStream := openTaskEventStreamAfterExistingEvents(t, server, task["id"].(string))
	defer closeStream()

	response := postJSONURL(t, server.Client(), server.URL+"/api/tasks/"+task["id"].(string)+"/approvals", map[string]string{
		"actionType":  "install_package",
		"description": "Install github.com/example/package",
		"payloadJson": `{"package":"github.com/example/package"}`,
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected approval status 201, got %d", response.StatusCode)
	}
	defer response.Body.Close()

	waitForSSEEvent(t, lines, "approval.requested")
}

func TestServerStreamsApprovalResolvedToOpenSSEConnection(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t)
	task := createTestTask(t, handler, t.TempDir())
	approvalResponse := doJSON(t, handler, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/approvals", map[string]string{
		"actionType":  "install_package",
		"description": "Install github.com/example/package",
		"payloadJson": `{"package":"github.com/example/package"}`,
	})
	if approvalResponse.Code != http.StatusCreated {
		t.Fatalf("expected approval status 201, got %d: %s", approvalResponse.Code, approvalResponse.Body.String())
	}
	var approval map[string]any
	if err := json.NewDecoder(approvalResponse.Body).Decode(&approval); err != nil {
		t.Fatalf("decode approval: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	lines, closeStream := openTaskEventStreamAfterExistingEvents(t, server, task["id"].(string))
	defer closeStream()

	response := postJSONURL(t, server.Client(), server.URL+"/api/approvals/"+approval["id"].(string)+"/approve", map[string]string{})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected approve status 200, got %d", response.StatusCode)
	}
	defer response.Body.Close()

	waitForSSEEvent(t, lines, "approval.approved")
}

func TestServerCreatesListsAndResolvesApprovals(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Gateway MVP",
		"path": t.TempDir(),
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d", projectResponse.Code)
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": project["id"].(string),
		"agentType": "codex",
		"prompt":    "Install package",
	})
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d", taskResponse.Code)
	}
	var task map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}

	approvalResponse := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/approvals", map[string]string{
		"actionType":  "install_package",
		"description": "Install github.com/example/package",
		"payloadJson": `{"package":"github.com/example/package"}`,
	})
	if approvalResponse.Code != http.StatusCreated {
		t.Fatalf("expected approval status 201, got %d: %s", approvalResponse.Code, approvalResponse.Body.String())
	}
	var approval map[string]any
	if err := json.NewDecoder(approvalResponse.Body).Decode(&approval); err != nil {
		t.Fatalf("decode approval: %v", err)
	}
	if approval["status"] != "pending" {
		t.Fatalf("expected pending approval, got %v", approval["status"])
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/approvals?status=pending", nil)
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResponse.Code)
	}

	resolveResponse := httptest.NewRecorder()
	resolveRequest := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/approve", nil)
	server.ServeHTTP(resolveResponse, resolveRequest)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("expected approve status 200, got %d: %s", resolveResponse.Code, resolveResponse.Body.String())
	}

	var resolved map[string]any
	if err := json.NewDecoder(resolveResponse.Body).Decode(&resolved); err != nil {
		t.Fatalf("decode resolved approval: %v", err)
	}
	if resolved["status"] != "approved" {
		t.Fatalf("expected approved status, got %v", resolved["status"])
	}
}

func TestServerRequiresBearerTokenForAPIWhenConfigured(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken: "secret-token",
	})

	healthResponse := httptest.NewRecorder()
	healthRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	server.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("expected public health status 200, got %d", healthResponse.Code)
	}

	unauthorized := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	server.ServeHTTP(unauthorized, unauthorizedRequest)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 without token, got %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer secret-token")
	server.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected status 200 with token, got %d", authorized.Code)
	}
}

func TestServerLoginSetsSessionCookie(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	response := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "secret-token",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", response.Code, response.Body.String())
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "paia_session" {
		t.Fatalf("expected paia_session cookie, got %q", cookie.Name)
	}
	if cookie.Value == "" {
		t.Fatal("expected non-empty session cookie value")
	}
	if !cookie.HttpOnly {
		t.Fatal("expected session cookie to be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", cookie.SameSite)
	}
}

func TestServerLoginSetsSecureSessionCookieBehindForwardedHTTPS(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	body, err := json.Marshal(map[string]string{"token": "secret-token"})
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}
	if !cookies[0].Secure {
		t.Fatal("expected forwarded HTTPS session cookie to be Secure")
	}
}

func TestServerRejectsInvalidLogin(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	response := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "wrong-token",
	})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected login status 401, got %d: %s", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("expected invalid login not to set cookies")
	}
}

func TestServerThrottlesRepeatedInvalidLogins(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken:          "secret-token",
		LoginFailureLimit:  2,
		LoginFailureWindow: time.Hour,
	})

	for i := 0; i < 2; i++ {
		response := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
			"token": "wrong-token",
		})
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected invalid login status 401, got %d: %s", response.Code, response.Body.String())
		}
	}

	response := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "wrong-token",
	})
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected throttled login status 429, got %d: %s", response.Code, response.Body.String())
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter != "3600" {
		t.Fatalf("expected Retry-After 3600, got %q", retryAfter)
	}
}

func TestServerLoginThrottleIsPerRemoteAddress(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken:          "secret-token",
		LoginFailureLimit:  1,
		LoginFailureWindow: time.Hour,
	})

	first := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "wrong-token",
	})
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid login status 401, got %d: %s", first.Code, first.Body.String())
	}

	limited := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "wrong-token",
	})
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected throttled login status 429, got %d: %s", limited.Code, limited.Body.String())
	}

	body, err := json.Marshal(map[string]string{"token": "secret-token"})
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	request.RemoteAddr = "198.51.100.20:54321"
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected different remote login status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerAcceptsSessionCookieForAPIWhenConfigured(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})
	loginResponse := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "secret-token",
	})
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	request.AddCookie(cookies[0])
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200 with session cookie, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerRejectsCrossOriginSessionCookieWrites(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})
	loginResponse := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "secret-token",
	})
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}

	body, err := json.Marshal(map[string]string{
		"name": "Cross Origin Project",
		"path": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.AddCookie(cookies[0])
	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected cross-origin session write status 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerAcceptsSameOriginSessionCookieWrites(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})
	loginResponse := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "secret-token",
	})
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}

	body, err := json.Marshal(map[string]string{
		"name": "Same Origin Project",
		"path": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1")
	request.AddCookie(cookies[0])
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected same-origin session write status 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerAcceptsCrossOriginBearerWrites(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	body, err := json.Marshal(map[string]string{
		"name": "Bearer Project",
		"path": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://api-client.example")
	request.Header.Set("Authorization", "Bearer secret-token")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected cross-origin bearer write status 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerLogoutClearsSessionCookie(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected logout status 200, got %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cleared session cookie, got %d", len(cookies))
	}
	if cookies[0].Name != "paia_session" {
		t.Fatalf("expected paia_session cookie, got %q", cookies[0].Name)
	}
	if cookies[0].MaxAge >= 0 {
		t.Fatalf("expected clearing cookie max age, got %d", cookies[0].MaxAge)
	}
}

func TestServerLogoutClearsSecureSessionCookieBehindForwardedHTTPS(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected logout status 200, got %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cleared session cookie, got %d", len(cookies))
	}
	if !cookies[0].Secure {
		t.Fatal("expected forwarded HTTPS clearing cookie to be Secure")
	}
}

func TestServerRejectsOversizedJSONBodies(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		MaxBodyBytes: 16,
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"large","path":"`+t.TempDir()+`"}`))
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerRejectsTrailingJSONBodies(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	body, err := json.Marshal(map[string]string{
		"name": "Trailing JSON",
		"path": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	response := doRaw(t, server, http.MethodPost, "/api/projects", bytes.NewReader(append(body, []byte(" {}")...)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for trailing JSON, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerRejectsNonJSONContentTypeForJSONBodies(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	body, err := json.Marshal(map[string]string{
		"name": "Wrong Content Type",
		"path": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	request.Header.Set("Content-Type", "text/plain")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status 415 for non-JSON content type, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerAddsRequestIDHeader(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	server.ServeHTTP(response, request)

	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected generated request id header")
	}
}

func TestServerAddsSecurityHeaders(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	server.ServeHTTP(response, request)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'; base-uri 'self'; frame-ancestors 'none'",
	}
	for name, expected := range expectedHeaders {
		if actual := response.Header().Get(name); actual != expected {
			t.Fatalf("expected %s header %q, got %q", name, expected, actual)
		}
	}
}

func TestServerMarksSensitiveResponsesNoStore(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})

	apiResponse := httptest.NewRecorder()
	apiRequest := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	apiRequest.Header.Set("Authorization", "Bearer secret-token")
	server.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected API Cache-Control no-store, got %q", apiResponse.Header().Get("Cache-Control"))
	}

	authResponse := doJSON(t, server, http.MethodPost, "/auth/login", map[string]string{
		"token": "secret-token",
	})
	if authResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected auth Cache-Control no-store, got %q", authResponse.Header().Get("Cache-Control"))
	}
}

func TestServerDoesNotMarkDashboardAssetsNoStore(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	server.ServeHTTP(response, request)

	if response.Header().Get("Cache-Control") == "no-store" {
		t.Fatal("expected dashboard asset to omit no-store cache control")
	}
}

func TestServerAddsHSTSHeaderForForwardedHTTPS(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Forwarded-Proto", "https")

	server.ServeHTTP(response, request)

	if actual := response.Header().Get("Strict-Transport-Security"); actual != "max-age=31536000; includeSubDomains" {
		t.Fatalf("expected HSTS header for forwarded HTTPS, got %q", actual)
	}
}

func TestServerOmitsHSTSHeaderForPlainHTTP(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	server.ServeHTTP(response, request)

	if actual := response.Header().Get("Strict-Transport-Security"); actual != "" {
		t.Fatalf("expected no HSTS header for plain HTTP, got %q", actual)
	}
}

func TestServerPreservesCallerRequestID(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Request-ID", "caller-request-id")

	server.ServeHTTP(response, request)

	if response.Header().Get("X-Request-ID") != "caller-request-id" {
		t.Fatalf("expected caller request id, got %q", response.Header().Get("X-Request-ID"))
	}
}

func TestServerWritesStructuredRequestLog(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{}))
	server := newTestServerWithOptions(t, gateway.Options{
		Logger: logger,
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Request-ID", "log-test-request")
	server.ServeHTTP(response, request)

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v; log=%s", err, logs.String())
	}
	if entry["msg"] != "http_request" {
		t.Fatalf("expected http_request log message, got %v", entry["msg"])
	}
	if entry["request_id"] != "log-test-request" {
		t.Fatalf("expected request id in log, got %v", entry["request_id"])
	}
	if entry["method"] != http.MethodGet {
		t.Fatalf("expected method in log, got %v", entry["method"])
	}
	if entry["path"] != "/health" {
		t.Fatalf("expected path in log, got %v", entry["path"])
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("expected status 200 in log, got %v", entry["status"])
	}
}

func TestServerExposesRuntimeMetrics(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	for range 3 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected health status 200, got %d", response.Code)
		}
	}

	notFoundResponse := httptest.NewRecorder()
	notFoundRequest := httptest.NewRequest(http.MethodGet, "/missing", nil)
	server.ServeHTTP(notFoundResponse, notFoundRequest)
	if notFoundResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing route status 404, got %d", notFoundResponse.Code)
	}

	metricsResponse := httptest.NewRecorder()
	metricsRequest := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	server.ServeHTTP(metricsResponse, metricsRequest)

	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("expected metrics status 200, got %d", metricsResponse.Code)
	}

	var metrics map[string]any
	if err := json.NewDecoder(metricsResponse.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}

	if metrics["requestsTotal"] != float64(5) {
		t.Fatalf("expected 5 total requests including metrics request, got %v", metrics["requestsTotal"])
	}
	statusCounts := metrics["statusCounts"].(map[string]any)
	if statusCounts["2xx"] != float64(4) {
		t.Fatalf("expected 4 2xx requests including metrics request, got %v", statusCounts["2xx"])
	}
	if statusCounts["4xx"] != float64(1) {
		t.Fatalf("expected 1 4xx request, got %v", statusCounts["4xx"])
	}
	if metrics["averageDurationMs"] == nil {
		t.Fatal("expected average duration metric")
	}
}

func TestMetricsEndpointRequiresBearerTokenWhenConfigured(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken: "secret-token",
	})

	unauthorized := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	server.ServeHTTP(unauthorized, unauthorizedRequest)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 without token, got %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer secret-token")
	server.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected status 200 with token, got %d", authorized.Code)
	}
}

func TestMutatingAPIRequestsAreRateLimited(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken:          "secret-token",
		APIWriteLimit:      2,
		APIWriteRateWindow: time.Minute,
	})

	first := doAuthenticatedJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "First Project",
		"path": t.TempDir(),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first create status 201, got %d: %s", first.Code, first.Body.String())
	}

	second := doAuthenticatedJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Second Project",
		"path": t.TempDir(),
	})
	if second.Code != http.StatusCreated {
		t.Fatalf("expected second create status 201, got %d: %s", second.Code, second.Body.String())
	}

	limited := doAuthenticatedJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Third Project",
		"path": t.TempDir(),
	})
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected rate limited status 429, got %d: %s", limited.Code, limited.Body.String())
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestTaskEventStreamAcceptsTokenQueryWhenConfigured(t *testing.T) {
	t.Parallel()

	server := newTestServerWithOptions(t, gateway.Options{AuthToken: "secret-token"})
	task := createAuthorizedTestTask(t, server, t.TempDir(), "secret-token")

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events/stream?token=secret-token", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ServeHTTP(response, request)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	if response.Code != http.StatusOK {
		t.Fatalf("expected stream status 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerCreatesDatabaseBackup(t *testing.T) {
	t.Parallel()

	backupPath := filepath.Join(t.TempDir(), "gateway-backup.db")
	server := newTestServer(t)

	response := doJSON(t, server, http.MethodPost, "/api/admin/backups", map[string]string{
		"path": backupPath,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("expected backup status 201, got %d: %s", response.Code, response.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode backup response: %v", err)
	}
	if result["path"] != backupPath {
		t.Fatalf("expected backup path %q, got %v", backupPath, result["path"])
	}
	if result["sizeBytes"].(float64) == 0 {
		t.Fatal("expected non-empty backup size")
	}
}

func TestBackupEndpointRequiresBearerTokenWhenConfigured(t *testing.T) {
	t.Parallel()

	backupPath := filepath.Join(t.TempDir(), "gateway-backup.db")
	server := newTestServerWithOptions(t, gateway.Options{
		AuthToken: "secret-token",
	})

	unauthorized := doJSON(t, server, http.MethodPost, "/api/admin/backups", map[string]string{
		"path": backupPath,
	})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 without token, got %d", unauthorized.Code)
	}

	authorizedBody, err := json.Marshal(map[string]string{"path": backupPath})
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodPost, "/api/admin/backups", bytes.NewReader(authorizedBody))
	authorizedRequest.Header.Set("Authorization", "Bearer secret-token")
	authorizedRequest.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusCreated {
		t.Fatalf("expected status 201 with token, got %d: %s", authorized.Code, authorized.Body.String())
	}
}

func TestServerRunsAllowedCommandForTask(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		result: gateway.CommandResult{
			ExitCode: 0,
			Stdout:   "ok",
		},
	}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	response := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "go test ./...",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected command status 200, got %d: %s", response.Code, response.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode command result: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("expected completed status, got %v", result["status"])
	}
	if runner.calls != 1 {
		t.Fatalf("expected runner to be called once, got %d", runner.calls)
	}

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events", nil)
	server.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	completedEvent := findEvent(events, "command.completed")
	if completedEvent == nil {
		t.Fatalf("expected command.completed event, got %#v", events)
	}
	if completedEvent["payloadJson"] == "" {
		t.Fatal("expected command.completed event payloadJson")
	}
}

func TestServerTimesOutAllowedCommandForTask(t *testing.T) {
	t.Parallel()

	runner := &blockingCommandRunner{done: make(chan struct{})}
	server := newTestServerWithOptions(t, gateway.Options{
		CommandRunner: runner,
		TaskTimeout:   10 * time.Millisecond,
	})
	task := createTestTask(t, server, t.TempDir())

	response := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "go test ./...",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected command status 200, got %d: %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode command result: %v", err)
	}
	if result["status"] != "timed_out" {
		t.Fatalf("expected timed_out result, got %v", result["status"])
	}

	getTaskResponse := httptest.NewRecorder()
	getTaskRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string), nil)
	server.ServeHTTP(getTaskResponse, getTaskRequest)
	if getTaskResponse.Code != http.StatusOK {
		t.Fatalf("expected get task status 200, got %d", getTaskResponse.Code)
	}
	var updatedTask map[string]any
	if err := json.NewDecoder(getTaskResponse.Body).Decode(&updatedTask); err != nil {
		t.Fatalf("decode updated task: %v", err)
	}
	if updatedTask["status"] != "timed_out" {
		t.Fatalf("expected timed_out task status, got %v", updatedTask["status"])
	}

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events", nil)
	server.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if !hasEventType(events, "command.timed_out") {
		t.Fatalf("expected command.timed_out event, got %#v", events)
	}
}

func TestServerRejectsConcurrentCommandsForSameProject(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := &blockingCommandRunner{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	server := newTestServerWithOptions(t, gateway.Options{
		CommandRunner: runner,
		TaskTimeout:   50 * time.Millisecond,
	})
	project := createTestProject(t, server, projectPath)
	firstTask := createTaskForProject(t, server, project["id"].(string))
	secondTask := createTaskForProject(t, server, project["id"].(string))

	firstResponse := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/"+firstTask["id"].(string)+"/commands", strings.NewReader(`{"command":"go test ./..."}`))
	firstRequest.Header.Set("Content-Type", "application/json")
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		server.ServeHTTP(firstResponse, firstRequest)
	}()
	<-runner.started

	secondResponse := doJSON(t, server, http.MethodPost, "/api/tasks/"+secondTask["id"].(string)+"/commands", map[string]string{
		"command": "go test ./...",
	})
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected concurrent command status 409, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	<-firstDone
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("expected first command status 200, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
}

func TestServerRunsCodexTask(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := &fakeAgentRunner{
		result: gateway.CommandResult{
			ExitCode: 0,
			Stdout:   `{"msg":"done"}`,
		},
	}
	server := newTestServerWithOptions(t, gateway.Options{AgentRunner: runner})
	task := createTestTask(t, server, projectPath)

	// First run: starts plan generation, pauses at waiting_for_approval
	response1 := httptest.NewRecorder()
	request1 := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task["id"].(string)+"/run", nil)
	server.ServeHTTP(response1, request1)
	if response1.Code != http.StatusOK {
		t.Fatalf("expected run status 200, got %d: %s", response1.Code, response1.Body.String())
	}
	var res1 map[string]any
	if err := json.NewDecoder(response1.Body).Decode(&res1); err != nil {
		t.Fatalf("decode response 1: %v", err)
	}
	if res1["status"] != "waiting_for_approval" {
		t.Fatalf("expected waiting_for_approval status, got %v", res1["status"])
	}

	// Fetch pending approvals to find the generated plan approval
	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/approvals?status=pending", nil)
	server.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected approvals status 200, got %d", listRec.Code)
	}
	var approvals []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&approvals); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	if len(approvals) == 0 {
		t.Fatalf("expected at least one pending approval")
	}
	approval := approvals[0]

	// Approve it!
	approveRec := httptest.NewRecorder()
	approveReq := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/approve", nil)
	server.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected approve status 200, got %d", approveRec.Code)
	}

	// Second run: executes the task now that it's approved
	response2 := httptest.NewRecorder()
	request2 := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task["id"].(string)+"/run", nil)
	server.ServeHTTP(response2, request2)
	if response2.Code != http.StatusOK {
		t.Fatalf("expected run status 200, got %d: %s", response2.Code, response2.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(response2.Body).Decode(&result); err != nil {
		t.Fatalf("decode run result: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("expected completed status, got %v", result["status"])
	}
	if runner.calls != 2 {
		t.Fatalf("expected runner to be called twice (planning + execution), got %d", runner.calls)
	}
	if runner.workdir != projectPath {
		t.Fatalf("expected runner workdir %q, got %q", projectPath, runner.workdir)
	}
	if runner.prompt != "Run safe command" {
		t.Fatalf("expected task prompt to be passed, got %q", runner.prompt)
	}
}

func TestServerTimesOutCodexTask(t *testing.T) {
	t.Parallel()

	runner := newBlockingAgentRunner()
	server := newTestServerWithOptions(t, gateway.Options{
		AgentRunner: runner,
		TaskTimeout: 10 * time.Millisecond,
	})
	task := createTestTask(t, server, t.TempDir())

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task["id"].(string)+"/run", nil)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected run status 200, got %d: %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode run result: %v", err)
	}
	if result["status"] != "timed_out" {
		t.Fatalf("expected timed_out result, got %v", result["status"])
	}

	getTaskResponse := httptest.NewRecorder()
	getTaskRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string), nil)
	server.ServeHTTP(getTaskResponse, getTaskRequest)
	if getTaskResponse.Code != http.StatusOK {
		t.Fatalf("expected get task status 200, got %d", getTaskResponse.Code)
	}
	var updatedTask map[string]any
	if err := json.NewDecoder(getTaskResponse.Body).Decode(&updatedTask); err != nil {
		t.Fatalf("decode updated task: %v", err)
	}
	if updatedTask["status"] != "timed_out" {
		t.Fatalf("expected timed_out task status, got %v", updatedTask["status"])
	}

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string)+"/events", nil)
	server.ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.Code)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if !hasEventType(events, "agent.timed_out") {
		t.Fatalf("expected agent.timed_out event, got %#v", events)
	}
}

func TestServerRejectsConcurrentCodexRunsForSameProject(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := newBlockingAgentRunner()
	server := newTestServerWithOptions(t, gateway.Options{
		AgentRunner: runner,
		TaskTimeout: 50 * time.Millisecond,
	})
	project := createTestProject(t, server, projectPath)
	firstTask := createTaskForProject(t, server, project["id"].(string))
	secondTask := createTaskForProject(t, server, project["id"].(string))

	firstResponse := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/"+firstTask["id"].(string)+"/run", nil)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		server.ServeHTTP(firstResponse, firstRequest)
	}()
	<-runner.started

	secondResponse := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/"+secondTask["id"].(string)+"/run", nil)
	server.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected concurrent run status 409, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	<-firstDone
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("expected first run status 200, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
}

func TestServerRejectsUnsupportedAgentTaskCreation(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Unsupported Agent Project",
		"path": t.TempDir(),
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": project["id"].(string),
		"agentType": "claude",
		"prompt":    "Run safe command",
	})
	if taskResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected task status 400, got %d: %s", taskResponse.Code, taskResponse.Body.String())
	}
}

func TestServerCancelsRunningCodexTask(t *testing.T) {
	t.Parallel()

	runner := newBlockingAgentRunner()
	server := newTestServerWithOptions(t, gateway.Options{AgentRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	runResponse := httptest.NewRecorder()
	runRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task["id"].(string)+"/run", nil)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		server.ServeHTTP(runResponse, runRequest)
	}()

	<-runner.started

	cancelResponse := httptest.NewRecorder()
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task["id"].(string)+"/cancel", nil)
	server.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("expected cancel status 200, got %d: %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	<-runDone
	if runResponse.Code != http.StatusOK {
		t.Fatalf("expected run status 200, got %d: %s", runResponse.Code, runResponse.Body.String())
	}
	if !runner.canceled() {
		t.Fatal("expected runner context to be canceled")
	}

	getTaskResponse := httptest.NewRecorder()
	getTaskRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task["id"].(string), nil)
	server.ServeHTTP(getTaskResponse, getTaskRequest)
	if getTaskResponse.Code != http.StatusOK {
		t.Fatalf("expected get task status 200, got %d", getTaskResponse.Code)
	}
	var updatedTask map[string]any
	if err := json.NewDecoder(getTaskResponse.Body).Decode(&updatedTask); err != nil {
		t.Fatalf("decode updated task: %v", err)
	}
	if updatedTask["status"] != "canceled" {
		t.Fatalf("expected canceled task status, got %v", updatedTask["status"])
	}
}

func TestServerCreatesApprovalInsteadOfRunningSensitiveCommand(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	response := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "npm install left-pad",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected command status 202, got %d: %s", response.Code, response.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode approval result: %v", err)
	}
	if result["status"] != "waiting_for_approval" {
		t.Fatalf("expected waiting_for_approval status, got %v", result["status"])
	}
	if runner.calls != 0 {
		t.Fatalf("expected runner not to be called, got %d", runner.calls)
	}
}

func TestServerRunsCommandAfterApproval(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		result: gateway.CommandResult{
			ExitCode: 0,
			Stdout:   "go version go1.23.0 windows/amd64",
		},
	}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	requestApproval := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "go version",
	})
	if requestApproval.Code != http.StatusAccepted {
		t.Fatalf("expected approval status 202, got %d: %s", requestApproval.Code, requestApproval.Body.String())
	}
	var approvalResult map[string]any
	if err := json.NewDecoder(requestApproval.Body).Decode(&approvalResult); err != nil {
		t.Fatalf("decode approval result: %v", err)
	}
	approval := approvalResult["approval"].(map[string]any)

	approveResponse := httptest.NewRecorder()
	approveRequest := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/approve", nil)
	server.ServeHTTP(approveResponse, approveRequest)
	if approveResponse.Code != http.StatusOK {
		t.Fatalf("expected approve status 200, got %d: %s", approveResponse.Code, approveResponse.Body.String())
	}

	executeResponse := httptest.NewRecorder()
	executeRequest := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/execute", nil)
	server.ServeHTTP(executeResponse, executeRequest)
	if executeResponse.Code != http.StatusOK {
		t.Fatalf("expected execute status 200, got %d: %s", executeResponse.Code, executeResponse.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(executeResponse.Body).Decode(&result); err != nil {
		t.Fatalf("decode command result: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("expected completed status, got %v", result["status"])
	}
	if result["command"] != "go version" {
		t.Fatalf("expected command go version, got %v", result["command"])
	}
	if runner.calls != 1 {
		t.Fatalf("expected runner to be called once, got %d", runner.calls)
	}

	replayResponse := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/execute", nil)
	server.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusConflict {
		t.Fatalf("expected replay execute status 409, got %d: %s", replayResponse.Code, replayResponse.Body.String())
	}
	if runner.calls != 1 {
		t.Fatalf("expected replay not to call runner again, got %d calls", runner.calls)
	}
}

func TestServerRejectsPendingCommandApprovalExecution(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	requestApproval := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "go version",
	})
	if requestApproval.Code != http.StatusAccepted {
		t.Fatalf("expected approval status 202, got %d: %s", requestApproval.Code, requestApproval.Body.String())
	}
	var approvalResult map[string]any
	if err := json.NewDecoder(requestApproval.Body).Decode(&approvalResult); err != nil {
		t.Fatalf("decode approval result: %v", err)
	}
	approval := approvalResult["approval"].(map[string]any)

	executeResponse := httptest.NewRecorder()
	executeRequest := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approval["id"].(string)+"/execute", nil)
	server.ServeHTTP(executeResponse, executeRequest)
	if executeResponse.Code != http.StatusConflict {
		t.Fatalf("expected execute status 409, got %d: %s", executeResponse.Code, executeResponse.Body.String())
	}
	if runner.calls != 0 {
		t.Fatalf("expected runner not to be called, got %d", runner.calls)
	}
}

func TestServerBlocksDangerousCommand(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{}
	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})
	task := createTestTask(t, server, t.TempDir())

	response := doJSON(t, server, http.MethodPost, "/api/tasks/"+task["id"].(string)+"/commands", map[string]string{
		"command": "type C:\\Users\\Sandeep\\.ssh\\id_rsa",
	})
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected command status 403, got %d: %s", response.Code, response.Body.String())
	}
	if runner.calls != 0 {
		t.Fatalf("expected runner not to be called, got %d", runner.calls)
	}
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()

	return newTestServerWithOptions(t, gateway.Options{})
}

func newTestServerWithOptions(t *testing.T, options gateway.Options) http.Handler {
	t.Helper()

	s, err := store.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	return gateway.NewServer(s, options)
}

type fakeCommandRunner struct {
	calls  int
	result gateway.CommandResult
	err    error
}

func (r *fakeCommandRunner) Run(_ context.Context, _ string, _ string) (gateway.CommandResult, error) {
	r.calls++
	if r.err != nil {
		return gateway.CommandResult{}, r.err
	}
	if r.result.Status == "" {
		r.result.Status = "completed"
	}
	return r.result, nil
}

type blockingCommandRunner struct {
	started     chan struct{}
	done        chan struct{}
	startedOnce sync.Once
	doneOnce    sync.Once
}

func (r *blockingCommandRunner) Run(ctx context.Context, _ string, command string) (gateway.CommandResult, error) {
	if r.started != nil {
		r.startedOnce.Do(func() { close(r.started) })
	}
	<-ctx.Done()
	if r.done != nil {
		r.doneOnce.Do(func() { close(r.done) })
	}
	return gateway.CommandResult{
		Status:   "failed",
		Command:  command,
		ExitCode: -1,
		Stderr:   ctx.Err().Error(),
	}, nil
}

type sequencedCommandRunner struct {
	results  []gateway.CommandResult
	commands []string
	workdirs []string
}

func (r *sequencedCommandRunner) Run(_ context.Context, workdir string, command string) (gateway.CommandResult, error) {
	r.commands = append(r.commands, command)
	r.workdirs = append(r.workdirs, workdir)
	if len(r.results) == 0 {
		return gateway.CommandResult{Status: "failed", ExitCode: -1, Stderr: "unexpected command"}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	if result.Status == "" {
		result.Status = "completed"
	}
	return result, nil
}

type fakeAgentRunner struct {
	calls   int
	workdir string
	prompt  string
	result  gateway.CommandResult
	err     error
}

func (r *fakeAgentRunner) RunCodex(_ context.Context, workdir string, prompt string) (gateway.CommandResult, error) {
	r.calls++
	r.workdir = workdir
	r.prompt = prompt
	if r.err != nil {
		return gateway.CommandResult{}, r.err
	}
	if r.result.Status == "" {
		r.result.Status = "completed"
	}
	return r.result, nil
}

type blockingAgentRunner struct {
	started      chan struct{}
	cancelResult chan struct{}
	once         sync.Once
	cancelOnce   sync.Once
	mu           sync.Mutex
	wasCanceled  bool
}

func newBlockingAgentRunner() *blockingAgentRunner {
	return &blockingAgentRunner{
		started:      make(chan struct{}),
		cancelResult: make(chan struct{}),
	}
}

func (r *blockingAgentRunner) RunCodex(ctx context.Context, _ string, _ string) (gateway.CommandResult, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	r.mu.Lock()
	r.wasCanceled = true
	r.mu.Unlock()
	r.cancelOnce.Do(func() { close(r.cancelResult) })
	return gateway.CommandResult{}, ctx.Err()
}

func (r *blockingAgentRunner) canceled() bool {
	<-r.cancelResult
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.wasCanceled
}

func createTestTask(t *testing.T, server http.Handler, projectPath string) map[string]any {
	t.Helper()

	project := createTestProject(t, server, projectPath)
	return createTaskForProject(t, server, project["id"].(string))
}

func createTestProject(t *testing.T, server http.Handler, projectPath string) map[string]any {
	t.Helper()

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Command Project",
		"path": projectPath,
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return project
}

func createTaskForProject(t *testing.T, server http.Handler, projectID string) map[string]any {
	t.Helper()

	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": projectID,
		"agentType": "codex",
		"prompt":    "Run safe command",
	})
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d: %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task
}

func openTaskEventStreamAfterExistingEvents(t *testing.T, server *httptest.Server, taskID string) (<-chan string, func()) {
	t.Helper()

	eventsResponse, err := server.Client().Get(server.URL + "/api/tasks/" + taskID + "/events")
	if err != nil {
		t.Fatalf("list existing events: %v", err)
	}
	defer eventsResponse.Body.Close()
	if eventsResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected events status 200, got %d", eventsResponse.StatusCode)
	}
	var events []map[string]any
	if err := json.NewDecoder(eventsResponse.Body).Decode(&events); err != nil {
		t.Fatalf("decode existing events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one existing event")
	}
	lastEventID := strconv.FormatInt(int64(events[len(events)-1]["id"].(float64)), 10)

	streamRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/tasks/"+taskID+"/events/stream", nil)
	if err != nil {
		t.Fatalf("create stream request: %v", err)
	}
	streamRequest.Header.Set("Last-Event-ID", lastEventID)
	streamResponse, err := server.Client().Do(streamRequest)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	if streamResponse.StatusCode != http.StatusOK {
		_ = streamResponse.Body.Close()
		t.Fatalf("expected stream status 200, got %d", streamResponse.StatusCode)
	}

	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(streamResponse.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	return lines, func() { _ = streamResponse.Body.Close() }
}

func waitForSSEEvent(t *testing.T, lines <-chan string, eventType string) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	expectedLine := "event: " + eventType
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("event stream closed before %s", eventType)
			}
			if line == expectedLine {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s SSE event", eventType)
		}
	}
}

func hasEventType(events []map[string]any, eventType string) bool {
	return findEvent(events, eventType) != nil
}

func findEvent(events []map[string]any, eventType string) map[string]any {
	for _, event := range events {
		if event["eventType"] == eventType {
			return event
		}
	}
	return nil
}

func createAuthorizedTestTask(t *testing.T, server http.Handler, projectPath string, token string) map[string]any {
	t.Helper()

	projectBody, err := json.Marshal(map[string]string{
		"name": "Command Project",
		"path": projectPath,
	})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	projectResponse := httptest.NewRecorder()
	projectRequest := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(projectBody))
	projectRequest.Header.Set("Authorization", "Bearer "+token)
	projectRequest.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}

	taskBody, err := json.Marshal(map[string]string{
		"projectId": project["id"].(string),
		"agentType": "codex",
		"prompt":    "Run safe command",
	})
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	taskResponse := httptest.NewRecorder()
	taskRequest := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(taskBody))
	taskRequest.Header.Set("Authorization", "Bearer "+token)
	taskRequest.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(taskResponse, taskRequest)
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d: %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task
}

func doJSON(t *testing.T, handler http.Handler, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}

func doAuthenticatedJSON(t *testing.T, handler http.Handler, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer secret-token")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}

func postJSONURL(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	response, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post json: %v", err)
	}
	return response
}

func doRaw(t *testing.T, handler http.Handler, method string, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}

func TestServerUpdatesAndDeletesProjects(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	createBody := map[string]string{
		"name":      "Gateway MVP",
		"path":      t.TempDir(),
		"techStack": "Go",
	}
	createResponse := doJSON(t, server, http.MethodPost, "/api/projects", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode created project: %v", err)
	}
	projectID := created["id"].(string)

	updateBody := map[string]string{
		"name":      "Updated MVP Name",
		"path":      t.TempDir(),
		"techStack": "Rust",
	}
	updateResponse := doJSON(t, server, http.MethodPut, "/api/projects/"+projectID, updateBody)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated map[string]any
	if err := json.NewDecoder(updateResponse.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated project: %v", err)
	}
	if updated["name"] != "Updated MVP Name" {
		t.Fatalf("expected updated name to be %q, got %q", "Updated MVP Name", updated["name"])
	}

	deleteResponse := doJSON(t, server, http.MethodDelete, "/api/projects/"+projectID, nil)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected list projects status 200, got %d", listResponse.Code)
	}
	var projects []map[string]any
	if err := json.NewDecoder(listResponse.Body).Decode(&projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(projects))
	}
}

func TestServerGitCommitAndPush(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := &sequencedCommandRunner{
		results: []gateway.CommandResult{
			{ExitCode: 0, Stdout: " M main.go\n"},
			{ExitCode: 0, Status: "completed"},
			{ExitCode: 0, Status: "completed", Stdout: "[main a1b2c3d] chore: workspace updates"},
			{ExitCode: 0, Stdout: "main\n"},
			{ExitCode: 0, Status: "completed", Stdout: "Everything up-to-date"},
		},
	}

	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "Git Project",
		"path": projectPath,
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	projectID := project["id"].(string)

	msgResponse := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/git/commit-message", nil)
	if msgResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", msgResponse.Code, msgResponse.Body.String())
	}
	var msgResult map[string]string
	if err := json.NewDecoder(msgResponse.Body).Decode(&msgResult); err != nil {
		t.Fatalf("decode commit message: %v", err)
	}
	if !strings.Contains(msgResult["message"], "chore: workspace updates") {
		t.Fatalf("expected commit message to contain updates, got %q", msgResult["message"])
	}

	commitResponse := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/git/commit", map[string]string{
		"message": "chore: workspace updates",
	})
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", commitResponse.Code, commitResponse.Body.String())
	}
	var commitResult map[string]any
	if err := json.NewDecoder(commitResponse.Body).Decode(&commitResult); err != nil {
		t.Fatalf("decode commit result: %v", err)
	}
	if commitResult["status"] != "completed" {
		t.Fatalf("expected completed commit status, got %v", commitResult["status"])
	}

	pushResponse := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/git/push", nil)
	if pushResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", pushResponse.Code, pushResponse.Body.String())
	}
	var pushResult map[string]any
	if err := json.NewDecoder(pushResponse.Body).Decode(&pushResult); err != nil {
		t.Fatalf("decode push result: %v", err)
	}
	if pushResult["status"] != "completed" {
		t.Fatalf("expected completed push status, got %v", pushResult["status"])
	}
}

func TestServerRunProjectTests(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	runner := &sequencedCommandRunner{
		results: []gateway.CommandResult{
			{ExitCode: 0, Status: "completed", Stdout: "PASS: TestConfig\n"},
		},
	}

	server := newTestServerWithOptions(t, gateway.Options{CommandRunner: runner})

	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name":        "Test Project",
		"path":        projectPath,
		"testCommand": "go test ./...",
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	projectID := project["id"].(string)

	runResponse := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/tests/run", nil)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", runResponse.Code, runResponse.Body.String())
	}
	var testResult map[string]any
	if err := json.NewDecoder(runResponse.Body).Decode(&testResult); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if testResult["status"] != "completed" {
		t.Fatalf("expected completed status, got %v", testResult["status"])
	}

	updateResponse := doJSON(t, server, http.MethodPut, "/api/projects/"+projectID, map[string]string{
		"name":        "Test Project",
		"path":        projectPath,
		"testCommand": "env",
	})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}

	blockedResponse := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/tests/run", nil)
	if blockedResponse.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", blockedResponse.Code, blockedResponse.Body.String())
	}
}

func TestServerRunsLLMTask(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	server := newTestServer(t)

	// Create project
	projectResponse := doJSON(t, server, http.MethodPost, "/api/projects", map[string]string{
		"name": "LLM Project",
		"path": projectPath,
	})
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	projectID := project["id"].(string)

	// Create LLM task
	taskResponse := doJSON(t, server, http.MethodPost, "/api/tasks", map[string]string{
		"projectId": projectID,
		"agentType": "llm",
		"prompt":    "Test Prompt",
	})
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("expected task status 201, got %d: %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task map[string]any
	if err := json.NewDecoder(taskResponse.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	taskID := task["id"].(string)

	// Run task
	runResponse := doJSON(t, server, http.MethodPost, "/api/tasks/"+taskID+"/run", nil)
	if runResponse.Code != http.StatusOK {
		t.Fatalf("expected run status 200, got %d: %s", runResponse.Code, runResponse.Body.String())
	}

	var runResult map[string]any
	if err := json.NewDecoder(runResponse.Body).Decode(&runResult); err != nil {
		t.Fatalf("decode run result: %v", err)
	}
	if runResult["status"] != "completed" {
		t.Fatalf("expected completed status, got %v", runResult["status"])
	}
	if runResult["stdout"] != "Dummy LLM Response" {
		t.Fatalf("expected dummy response, got %v", runResult["stdout"])
	}
}
