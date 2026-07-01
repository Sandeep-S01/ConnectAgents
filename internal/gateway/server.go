package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"personal-ai-assistant/internal/llm"
	"personal-ai-assistant/internal/policy"
	"personal-ai-assistant/internal/store"
)

const defaultTaskTimeout = 2 * time.Hour
const defaultLoginFailureLimit = 5
const defaultLoginFailureWindow = 5 * time.Minute
const defaultAPIWriteLimit = 120
const defaultAPIWriteRateWindow = time.Minute

type Store interface {
	Ping(context.Context) error
	CreateProject(context.Context, store.CreateProjectInput) (store.Project, error)
	ListProjects(context.Context) ([]store.Project, error)
	GetProject(context.Context, string) (store.Project, error)
	UpdateProject(context.Context, string, store.CreateProjectInput) (store.Project, error)
	DeleteProject(context.Context, string) error
	CreateTask(context.Context, store.CreateTaskInput) (store.Task, error)
	UpdateTaskStatus(context.Context, string, string) error
	SaveTaskOutput(context.Context, string, string, string, string) error
	AddTaskEvent(context.Context, string, string, string, string) error
	GetTask(context.Context, string) (store.Task, error)
	ListTasks(context.Context, string, string) ([]store.Task, error)
	ListTaskEvents(context.Context, string) ([]store.TaskEvent, error)
	CreateApprovalRequest(context.Context, store.CreateApprovalRequestInput) (store.ApprovalRequest, error)
	GetApprovalRequest(context.Context, string) (store.ApprovalRequest, error)
	ListApprovalRequests(context.Context, string) ([]store.ApprovalRequest, error)
	ResolveApprovalRequest(context.Context, string, string) (store.ApprovalRequest, error)
	MarkApprovalExecuted(context.Context, string) (store.ApprovalRequest, error)
	FindTaskPlanApproval(context.Context, string) (store.ApprovalRequest, error)
	Backup(context.Context, string) (store.BackupResult, error)
	MarkRunningTasksInterrupted(context.Context) (int64, error)
}

type CommandRunner interface {
	Run(context.Context, string, string) (CommandResult, error)
}

type AgentRunner interface {
	RunCodex(context.Context, string, string) (CommandResult, error)
}

type CommandResult struct {
	Status   string `json:"status"`
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type DoctorResponse struct {
	Status   string                `json:"status"`
	Checks   []DoctorCheck         `json:"checks"`
	Projects []DoctorProjectStatus `json:"projects"`
}

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type DoctorProjectStatus struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type Options struct {
	AuthToken          string
	MaxBodyBytes       int64
	Logger             *slog.Logger
	CommandRunner      CommandRunner
	AgentRunner        AgentRunner
	LLMProvider        llm.LLMProvider
	TaskTimeout        time.Duration
	LoginFailureLimit  int
	LoginFailureWindow time.Duration
	APIWriteLimit      int
	APIWriteRateWindow time.Duration
	OnTaskEvent        func(store.TaskEvent)
}

type Server struct {
	store              Store
	mux                *http.ServeMux
	authToken          string
	maxBodyBytes       int64
	logger             *slog.Logger
	metrics            *metricsCollector
	commandRunner      CommandRunner
	agentRunner        AgentRunner
	llmProvider        llm.LLMProvider
	taskTimeout        time.Duration
	loginFailureLimit  int
	loginFailureWindow time.Duration
	loginFailuresMu    sync.Mutex
	loginFailures      map[string]loginFailureState
	apiWriteLimit      int
	apiWriteRateWindow time.Duration
	apiWriteMu         sync.Mutex
	apiWriteWindows    map[string]rateLimitWindow
	runningMu          sync.Mutex
	runningTasks       map[string]context.CancelFunc
	runningProjects    map[string]string
	bridgeMu           sync.Mutex
	bridges            map[string]*bridgeRegistration
	bridgesByPath      map[string]string
	streamMu           sync.Mutex
	streams            map[string]map[chan store.TaskEvent]struct{}
	onTaskEvent        func(store.TaskEvent)
}

type bridgeRegistration struct {
	ID            string
	WorkspacePath string
	WorkspaceName string
	Queue         []bridgeTask
}

type bridgeTask struct {
	TaskID      string `json:"taskId"`
	ProjectID   string `json:"projectId"`
	ProjectPath string `json:"projectPath"`
	ProjectName string `json:"projectName"`
	TechStack   string `json:"techStack,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
	Prompt      string `json:"prompt"`
}

func NewServer(dataStore Store, options Options) http.Handler {
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = defaultMaxBodyBytes
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.CommandRunner == nil {
		options.CommandRunner = shellCommandRunner{}
	}
	if options.AgentRunner == nil {
		options.AgentRunner = codexAgentRunner{}
	}
	if options.TaskTimeout == 0 {
		options.TaskTimeout = defaultTaskTimeout
	}
	if options.LoginFailureLimit == 0 {
		options.LoginFailureLimit = defaultLoginFailureLimit
	}
	if options.LoginFailureWindow == 0 {
		options.LoginFailureWindow = defaultLoginFailureWindow
	}
	if options.APIWriteLimit == 0 {
		options.APIWriteLimit = defaultAPIWriteLimit
	}
	if options.APIWriteRateWindow == 0 {
		options.APIWriteRateWindow = defaultAPIWriteRateWindow
	}

	if options.LLMProvider == nil {
		options.LLMProvider = dummyLLMProvider{}
	}

	server := &Server{
		store:              dataStore,
		mux:                http.NewServeMux(),
		authToken:          options.AuthToken,
		maxBodyBytes:       options.MaxBodyBytes,
		logger:             options.Logger,
		metrics:            newMetricsCollector(),
		commandRunner:      options.CommandRunner,
		agentRunner:        options.AgentRunner,
		llmProvider:        options.LLMProvider,
		taskTimeout:        options.TaskTimeout,
		loginFailureLimit:  options.LoginFailureLimit,
		loginFailureWindow: options.LoginFailureWindow,
		loginFailures:      make(map[string]loginFailureState),
		apiWriteLimit:      options.APIWriteLimit,
		apiWriteRateWindow: options.APIWriteRateWindow,
		apiWriteWindows:    make(map[string]rateLimitWindow),
		runningTasks:       make(map[string]context.CancelFunc),
		runningProjects:    make(map[string]string),
		bridges:            make(map[string]*bridgeRegistration),
		bridgesByPath:      make(map[string]string),
		streams:            make(map[string]map[chan store.TaskEvent]struct{}),
		onTaskEvent:        options.OnTaskEvent,
	}
	server.routes()
	return server
}

type dummyLLMProvider struct{}

func (dummyLLMProvider) GenerateResponse(ctx context.Context, prompt string) (string, error) {
	return "Dummy LLM Response", nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requestID := requestIDFrom(r)
	w.Header().Set("X-Request-ID", requestID)
	setSecurityHeaders(w.Header(), r)
	setSensitiveResponseHeaders(w.Header(), r)
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	defer func() {
		duration := time.Since(startedAt)
		s.metrics.Record(recorder.status, duration)
		s.logger.InfoContext(r.Context(), "http_request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", duration.Milliseconds(),
		)
	}()

	if strings.HasPrefix(r.URL.Path, "/api/") {
		authMode := s.authMode(r)
		if authMode == "" {
			writeJSON(recorder, http.StatusUnauthorized, map[string]string{"error": "missing or invalid authentication"})
			return
		}
		if authMode == "session" && requiresCSRFCheck(r) && !sameOrigin(r) {
			writeJSON(recorder, http.StatusForbidden, map[string]string{"error": "cross-origin session request rejected"})
			return
		}
		if isMutatingAPIRequest(r) {
			retryAfter, limited := s.apiWriteRateLimited(remoteAddressKey(r), time.Now())
			if limited {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				writeJSON(recorder, http.StatusTooManyRequests, map[string]string{"error": "too many API write requests"})
				return
			}
		}
	}
	s.mux.ServeHTTP(recorder, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /app.js", s.handleDashboardScript)
	s.mux.HandleFunc("GET /styles.css", s.handleDashboardStyles)
	s.mux.HandleFunc("GET /manifest.webmanifest", s.handleDashboardManifest)
	s.mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("PUT /api/projects/{projectID}", s.handleUpdateProject)
	s.mux.HandleFunc("DELETE /api/projects/{projectID}", s.handleDeleteProject)
	s.mux.HandleFunc("GET /api/projects/{projectID}/git", s.handleProjectGitSummary)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/commit-message", s.handleGenerateCommitMessage)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/commit", s.handleGitCommit)
	s.mux.HandleFunc("POST /api/projects/{projectID}/git/push", s.handleGitPush)
	s.mux.HandleFunc("POST /api/projects/{projectID}/tests/run", s.handleRunProjectTests)
	s.mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	s.mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	s.mux.HandleFunc("GET /api/tasks/{taskID}", s.handleGetTask)
	s.mux.HandleFunc("GET /api/tasks/{taskID}/events", s.handleListTaskEvents)
	s.mux.HandleFunc("GET /api/tasks/{taskID}/events/stream", s.handleStreamTaskEvents)
	s.mux.HandleFunc("POST /api/tasks/{taskID}/run", s.handleRunTaskAgent)
	s.mux.HandleFunc("POST /api/tasks/{taskID}/cancel", s.handleCancelTask)
	s.mux.HandleFunc("POST /api/tasks/{taskID}/commands", s.handleRunTaskCommand)
	s.mux.HandleFunc("POST /api/bridge/register", s.handleRegisterBridge)
	s.mux.HandleFunc("GET /api/bridge", s.handleListBridges)
	s.mux.HandleFunc("GET /api/bridge/{bridgeID}/tasks/next", s.handleNextBridgeTask)
	s.mux.HandleFunc("POST /api/bridge/{bridgeID}/tasks/{taskID}/events", s.handleBridgeTaskEvent)
	s.mux.HandleFunc("POST /api/tasks/{taskID}/approvals", s.handleCreateApprovalRequest)
	s.mux.HandleFunc("GET /api/approvals", s.handleListApprovalRequests)
	s.mux.HandleFunc("POST /api/approvals/{approvalID}/approve", s.handleApproveApprovalRequest)
	s.mux.HandleFunc("POST /api/approvals/{approvalID}/reject", s.handleRejectApprovalRequest)
	s.mux.HandleFunc("POST /api/approvals/{approvalID}/execute", s.handleExecuteApprovedCommand)
	s.mux.HandleFunc("POST /api/command-policy/evaluate", s.handleEvaluateCommandPolicy)
	s.mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	s.mux.HandleFunc("POST /api/admin/backups", s.handleCreateBackup)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authToken == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	remoteAddr := remoteAddressKey(r)
	if s.loginThrottled(remoteAddr, time.Now()) {
		w.Header().Set("Retry-After", strconv.Itoa(int(s.loginFailureWindow.Seconds())))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many failed login attempts"})
		return
	}

	var request struct {
		Token string `json:"token"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Token), []byte(s.authToken)) != 1 {
		s.recordFailedLogin(remoteAddr, time.Now())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
		return
	}

	s.clearFailedLogins(remoteAddr)
	http.SetCookie(w, s.newSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, clearSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name          string `json:"name"`
		Path          string `json:"path"`
		TechStack     string `json:"techStack"`
		DefaultBranch string `json:"defaultBranch"`
		TestCommand   string `json:"testCommand"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	project, err := s.store.CreateProject(r.Context(), store.CreateProjectInput{
		Name:          request.Name,
		Path:          request.Path,
		TechStack:     request.TechStack,
		DefaultBranch: request.DefaultBranch,
		TestCommand:   request.TestCommand,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	var request struct {
		Name          string `json:"name"`
		Path          string `json:"path"`
		TechStack     string `json:"techStack"`
		DefaultBranch string `json:"defaultBranch"`
		TestCommand   string `json:"testCommand"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	project, err := s.store.UpdateProject(r.Context(), projectID, store.CreateProjectInput{
		Name:          request.Name,
		Path:          request.Path,
		TechStack:     request.TechStack,
		DefaultBranch: request.DefaultBranch,
		TestCommand:   request.TestCommand,
	})
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	err := s.store.DeleteProject(r.Context(), projectID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleProjectGitSummary(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	branch, err := s.runReadOnlyGitCommand(r.Context(), project.Path, "git branch --show-current")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	status, err := s.runReadOnlyGitCommand(r.Context(), project.Path, "git status --short")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	diffStat, err := s.runReadOnlyGitCommand(r.Context(), project.Path, "git diff --stat")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"projectId": project.ID,
		"branch":    strings.TrimSpace(branch),
		"status":    nonEmptyLines(status),
		"diffStat":  strings.TrimSpace(diffStat),
	})
}

func (s *Server) handleGenerateCommitMessage(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	status, err := s.runReadOnlyGitCommand(r.Context(), project.Path, "git status --short")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	lines := nonEmptyLines(status)
	if len(lines) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "No changes to commit",
		})
		return
	}

	var sb strings.Builder
	sb.WriteString("chore: workspace updates\n\nChanges:\n")
	for _, line := range lines {
		sb.WriteString("- " + strings.TrimSpace(line) + "\n")
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": sb.String(),
	})
}

func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var request struct {
		Message string `json:"message"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	if strings.TrimSpace(request.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit message is required"})
		return
	}

	tempFile := filepath.Join(project.Path, ".git-commit-msg-temp")
	err = os.WriteFile(tempFile, []byte(request.Message), 0600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer func() {
		_ = os.Remove(tempFile)
	}()

	addResult, err := s.commandRunner.Run(r.Context(), project.Path, "git add -A")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if addResult.Status == "failed" {
		writeJSON(w, http.StatusBadGateway, addResult)
		return
	}

	commitResult, err := s.commandRunner.Run(r.Context(), project.Path, "git commit -F .git-commit-msg-temp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, commitResult)
}

func (s *Server) handleGitPush(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	branch, err := s.runReadOnlyGitCommand(r.Context(), project.Path, "git branch --show-current")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	branchName := strings.TrimSpace(branch)
	if branchName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not determine current branch"})
		return
	}

	for _, char := range branchName {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '/' || char == '.') {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid branch name"})
			return
		}
	}

	pushResult, err := s.commandRunner.Run(r.Context(), project.Path, "git push origin "+branchName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, pushResult)
}

func (s *Server) handleRunProjectTests(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	testCommand := strings.TrimSpace(project.TestCommand)
	if testCommand == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no test command configured for this project"})
		return
	}

	decision := policy.EvaluateCommand(testCommand)
	if decision.Action == policy.ActionBlock {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "test command blocked: " + decision.Reason})
		return
	}

	testResult, err := s.commandRunner.Run(r.Context(), project.Path, testCommand)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, testResult)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProjectID string `json:"projectId"`
		AgentType string `json:"agentType"`
		Prompt    string `json:"prompt"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	task, err := s.store.CreateTask(r.Context(), store.CreateTaskInput{
		ProjectID: request.ProjectID,
		AgentType: request.AgentType,
		Prompt:    request.Prompt,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context(), r.URL.Query().Get("projectId"), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleListTaskEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListTaskEvents(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleStreamTaskEvents(w http.ResponseWriter, r *http.Request) {
	lastEventID, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	events, err := s.store.ListTaskEvents(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	_, _ = io.WriteString(w, ": connected\n\n")
	for _, event := range events {
		if event.ID <= lastEventID {
			continue
		}
		if err := writeSSE(w, event); err != nil {
			return
		}
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	eventsChannel := s.subscribeTaskEvents(r.PathValue("taskID"))
	defer s.unsubscribeTaskEvents(r.PathValue("taskID"), eventsChannel)
	for {
		select {
		case event := <-eventsChannel:
			if event.ID <= lastEventID {
				continue
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleRunTaskAgent(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if task.AgentType != "codex" && task.AgentType != "llm" {
		writeError(w, http.StatusBadRequest, errors.New("unsupported agent type"))
		return
	}

	project, err := s.store.GetProject(r.Context(), task.ProjectID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	taskParentCtx := context.WithoutCancel(r.Context())
	runCtx, cancel := s.newTaskContext(taskParentCtx)
	if !s.registerRunningTask(task.ID, cancel) {
		cancel()
		writeError(w, http.StatusConflict, errors.New("task is already running"))
		return
	}
	if !s.registerRunningProject(project.ID, task.ID) {
		s.unregisterRunningTask(task.ID)
		cancel()
		writeError(w, http.StatusConflict, errors.New("project already has a running task"))
		return
	}
	defer s.unregisterRunningProject(project.ID)
	defer s.unregisterRunningTask(task.ID)
	defer cancel()

	s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "running"))

	// Extract OpenAI API Key and inject it into context for Codex runner
	openaiKey := r.Header.Get("X-OpenAI-API-Key")
	if openaiKey != "" {
		runCtx = context.WithValue(runCtx, "openai_api_key", openaiKey)
	}
	persistCtx := taskParentCtx

	var result CommandResult
	if task.AgentType == "llm" {
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "agent.started", "Calling LLM provider...", ""))

		apiKey := r.Header.Get("X-OpenRouter-API-Key")
		modelName := r.Header.Get("X-OpenRouter-Model")
		runCtxWithKey := runCtx
		if apiKey != "" {
			runCtxWithKey = context.WithValue(runCtxWithKey, "openrouter_api_key", apiKey)
		}
		if modelName != "" {
			runCtxWithKey = context.WithValue(runCtxWithKey, "openrouter_model", modelName)
		}

		respText, llmErr := s.llmProvider.GenerateResponse(runCtxWithKey, task.Prompt)
		if llmErr != nil {
			result = CommandResult{
				Status:   "failed",
				ExitCode: -1,
				Stderr:   llmErr.Error(),
			}
		} else {
			result = CommandResult{
				Status:   "completed",
				ExitCode: 0,
				Stdout:   respText,
			}
		}
	} else {
		planApproved := false
		planApproval, err := s.store.FindTaskPlanApproval(r.Context(), task.ID)
		if err == nil && planApproval.Status == "approved" {
			planApproved = true
		}

		if !planApproved {
			s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "agent.started", "Generating execution plan...", ""))
			plan := buildTaskPlan(task.Prompt, project.Path)
			_, approvalErr := s.store.CreateApprovalRequest(persistCtx, store.CreateApprovalRequestInput{
				TaskID:      task.ID,
				ActionType:  "task_plan",
				Description: "Approve execution plan for task: " + task.Prompt,
				PayloadJSON: mustJSON(map[string]string{
					"plan": plan,
				}),
			})
			if approvalErr != nil {
				s.logErr(r.Context(), "create_plan_approval", approvalErr)
			}
			s.logErr(persistCtx, "update_task_status", s.store.UpdateTaskStatus(persistCtx, task.ID, "waiting_for_approval"))
			s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.plan_generated", "Execution plan generated, waiting for approval", plan))
			s.logErr(persistCtx, "publish_task_event", s.publishLatestTaskEvent(persistCtx, task.ID))
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "waiting_for_approval",
				"plan":   plan,
			})
			return
		} else {
			if bridgeID, ok := s.bridgeForProject(project.Path); ok {
				queued := bridgeTask{
					TaskID:      task.ID,
					ProjectID:   project.ID,
					ProjectPath: project.Path,
					ProjectName: project.Name,
					TechStack:   project.TechStack,
					GitBranch:   project.DefaultBranch,
					Prompt:      task.Prompt,
				}
				if err := s.enqueueBridgeTask(bridgeID, queued); err != nil {
					writeError(w, http.StatusConflict, err)
					return
				}
				s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.dispatched", "Task dispatched to VS Code bridge", mustJSON(queued)))
				s.logErr(persistCtx, "publish_task_event", s.publishLatestTaskEvent(persistCtx, task.ID))
				writeJSON(w, http.StatusAccepted, map[string]any{
					"status":   "dispatched_to_bridge",
					"bridgeId": bridgeID,
					"taskId":   task.ID,
				})
				return
			}
			s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "agent.started", "Starting Codex task execution", ""))
			result, err = s.agentRunner.RunCodex(runCtx, project.Path, task.Prompt)
		}
	}

	if err != nil {
		result = CommandResult{
			Status:   "failed",
			ExitCode: -1,
			Stderr:   err.Error(),
		}
	}
	if runCtx.Err() != nil {
		result.ExitCode = -1
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.Status = "timed_out"
		} else {
			result.Status = "canceled"
		}
		if result.Stderr == "" {
			result.Stderr = runCtx.Err().Error()
		}
	}
	if result.Status == "" {
		if result.ExitCode == 0 {
			result.Status = "completed"
		} else {
			result.Status = "failed"
		}
	}
	// Persist the output so GET /api/tasks/:id returns stdout/stderr.
	errMsg := ""
	if result.Status != "completed" {
		errMsg = result.Stderr
		if errMsg == "" && result.ExitCode != 0 {
			errMsg = fmt.Sprintf("exit code %d", result.ExitCode)
		}
	}
	s.logErr(persistCtx, "save_task_output", s.store.SaveTaskOutput(persistCtx, task.ID, result.Stdout, result.Stderr, errMsg))
	if result.Status == "completed" {
		s.logErr(persistCtx, "update_task_status", s.store.UpdateTaskStatus(persistCtx, task.ID, "completed"))
		s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.completed", "Codex task completed", mustJSON(result)))
	} else if result.Status == "timed_out" {
		s.logErr(persistCtx, "update_task_status", s.store.UpdateTaskStatus(persistCtx, task.ID, "timed_out"))
		s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.timed_out", "Codex task timed out", mustJSON(result)))
	} else if result.Status == "canceled" {
		s.logErr(persistCtx, "update_task_status", s.store.UpdateTaskStatus(persistCtx, task.ID, "canceled"))
		s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.canceled", "Codex task canceled", mustJSON(result)))
	} else {
		s.logErr(persistCtx, "update_task_status", s.store.UpdateTaskStatus(persistCtx, task.ID, "failed"))
		s.logErr(persistCtx, "add_task_event", s.addTaskEvent(persistCtx, task.ID, "agent.failed", "Codex task failed", mustJSON(result)))
	}
	writeJSON(w, http.StatusOK, result)
}

func buildTaskPlan(prompt string, projectPath string) string {
	return strings.Join([]string{
		"1. Run Codex in the registered project workspace.",
		"2. Use this exact user prompt: " + prompt,
		"3. Let Codex inspect or modify files as needed for the prompt.",
		"4. Save the final stdout, stderr, status, and task events for Telegram status/log commands.",
		"",
		"Project path: " + projectPath,
	}, "\n")
}

func (s *Server) handleRegisterBridge(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WorkspacePath string `json:"workspacePath"`
		WorkspaceName string `json:"workspaceName"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	workspacePath := strings.TrimSpace(request.WorkspacePath)
	if workspacePath == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspacePath is required"))
		return
	}
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("resolve workspace path: %w", err))
		return
	}
	if _, err := os.Stat(absPath); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("workspace path must exist: %w", err))
		return
	}
	bridgeID := "bridge_" + randomHexID()
	bridge := &bridgeRegistration{
		ID:            bridgeID,
		WorkspacePath: absPath,
		WorkspaceName: strings.TrimSpace(request.WorkspaceName),
	}
	s.bridgeMu.Lock()
	s.bridges[bridgeID] = bridge
	s.bridgesByPath[absPath] = bridgeID
	s.bridgeMu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"bridgeId":      bridgeID,
		"workspacePath": absPath,
		"workspaceName": bridge.WorkspaceName,
	})
}

func (s *Server) handleListBridges(w http.ResponseWriter, r *http.Request) {
	type bridgeSummary struct {
		BridgeID      string `json:"bridgeId"`
		WorkspacePath string `json:"workspacePath"`
		WorkspaceName string `json:"workspaceName"`
		QueuedTasks   int    `json:"queuedTasks"`
	}
	s.bridgeMu.Lock()
	bridges := make([]bridgeSummary, 0, len(s.bridges))
	for _, bridge := range s.bridges {
		bridges = append(bridges, bridgeSummary{
			BridgeID:      bridge.ID,
			WorkspacePath: bridge.WorkspacePath,
			WorkspaceName: bridge.WorkspaceName,
			QueuedTasks:   len(bridge.Queue),
		})
	}
	s.bridgeMu.Unlock()
	writeJSON(w, http.StatusOK, bridges)
}

func (s *Server) handleNextBridgeTask(w http.ResponseWriter, r *http.Request) {
	bridgeID := r.PathValue("bridgeID")
	s.bridgeMu.Lock()
	bridge, ok := s.bridges[bridgeID]
	if !ok {
		s.bridgeMu.Unlock()
		writeError(w, http.StatusNotFound, errors.New("bridge not found"))
		return
	}
	if len(bridge.Queue) == 0 {
		s.bridgeMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	task := bridge.Queue[0]
	bridge.Queue = bridge.Queue[1:]
	s.bridgeMu.Unlock()

	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleBridgeTaskEvent(w http.ResponseWriter, r *http.Request) {
	bridgeID := r.PathValue("bridgeID")
	taskID := r.PathValue("taskID")
	s.bridgeMu.Lock()
	_, ok := s.bridges[bridgeID]
	s.bridgeMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bridge not found"))
		return
	}

	var request struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "running"
	}
	message := strings.TrimSpace(request.Message)
	if message == "" {
		message = "VS Code bridge task update"
	}

	eventType := "agent.bridge_update"
	switch status {
	case "running":
		eventType = "agent.started"
	case "completed":
		eventType = "agent.completed"
	case "failed":
		eventType = "agent.failed"
	case "canceled", "cancelled":
		status = "canceled"
		eventType = "agent.canceled"
	default:
		writeError(w, http.StatusBadRequest, errors.New("unsupported bridge task status"))
		return
	}

	if status == "completed" || status == "failed" || status == "canceled" {
		errMsg := ""
		if status != "completed" {
			errMsg = request.Stderr
		}
		s.logErr(r.Context(), "save_task_output", s.store.SaveTaskOutput(r.Context(), taskID, request.Stdout, request.Stderr, errMsg))
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), taskID, status))
	}
	s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), taskID, eventType, message, mustJSON(request)))
	s.logErr(r.Context(), "publish_task_event", s.publishLatestTaskEvent(r.Context(), taskID))
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) bridgeForProject(projectPath string) (string, bool) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return "", false
	}
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	bridgeID, ok := s.bridgesByPath[absPath]
	return bridgeID, ok
}

func (s *Server) enqueueBridgeTask(bridgeID string, task bridgeTask) error {
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	bridge, ok := s.bridges[bridgeID]
	if !ok {
		return errors.New("bridge not found")
	}
	bridge.Queue = append(bridge.Queue, task)
	return nil
}

func randomHexID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(bytes[:])
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	cancel, ok := s.cancelRunningTask(taskID)
	if !ok {
		writeError(w, http.StatusConflict, errors.New("task is not running"))
		return
	}
	s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), taskID, "task.cancel_requested", "Task cancellation requested", ""))
	cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancel_requested"})
}

func (s *Server) handleRunTaskCommand(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Command string `json:"command"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	taskID := r.PathValue("taskID")
	task, err := s.store.GetTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	decision := policy.EvaluateCommand(request.Command)
	switch decision.Action {
	case policy.ActionBlock:
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "blocked"))
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "command.blocked", decision.Reason, mustJSON(decision)))
		writeJSON(w, http.StatusForbidden, map[string]any{
			"status":   "blocked",
			"decision": decision,
		})
	case policy.ActionRequireApproval:
		approval, err := s.store.CreateApprovalRequest(r.Context(), store.CreateApprovalRequestInput{
			TaskID:      task.ID,
			ActionType:  "command_execution",
			Description: "Approve command execution: " + request.Command,
			PayloadJSON: mustJSON(decision),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "waiting_for_approval"))
		s.logErr(r.Context(), "publish_task_event", s.publishLatestTaskEvent(r.Context(), task.ID))
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":   "waiting_for_approval",
			"decision": decision,
			"approval": approval,
		})
	case policy.ActionAllow:
		s.executeTaskCommand(w, r, task, request.Command, decision)
	}
}

func (s *Server) handleCreateApprovalRequest(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ActionType  string `json:"actionType"`
		Description string `json:"description"`
		PayloadJSON string `json:"payloadJson"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	approval, err := s.store.CreateApprovalRequest(r.Context(), store.CreateApprovalRequestInput{
		TaskID:      r.PathValue("taskID"),
		ActionType:  request.ActionType,
		Description: request.Description,
		PayloadJSON: request.PayloadJSON,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logErr(r.Context(), "publish_task_event", s.publishLatestTaskEvent(r.Context(), approval.TaskID))
	writeJSON(w, http.StatusCreated, approval)
}

func (s *Server) handleListApprovalRequests(w http.ResponseWriter, r *http.Request) {
	approvals, err := s.store.ListApprovalRequests(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, approvals)
}

func (s *Server) handleApproveApprovalRequest(w http.ResponseWriter, r *http.Request) {
	s.handleResolveApprovalRequest(w, r, "approved")
}

func (s *Server) handleRejectApprovalRequest(w http.ResponseWriter, r *http.Request) {
	s.handleResolveApprovalRequest(w, r, "rejected")
}

func (s *Server) handleResolveApprovalRequest(w http.ResponseWriter, r *http.Request, status string) {
	approval, err := s.store.ResolveApprovalRequest(r.Context(), r.PathValue("approvalID"), status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logErr(r.Context(), "publish_task_event", s.publishLatestTaskEvent(r.Context(), approval.TaskID))
	writeJSON(w, http.StatusOK, approval)
}

func (s *Server) handleExecuteApprovedCommand(w http.ResponseWriter, r *http.Request) {
	approval, err := s.store.GetApprovalRequest(r.Context(), r.PathValue("approvalID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if approval.Status != "approved" {
		writeError(w, http.StatusConflict, errors.New("approval request must be approved before execution"))
		return
	}
	if approval.ActionType != "command_execution" {
		writeError(w, http.StatusBadRequest, errors.New("approval request is not for command execution"))
		return
	}

	var approvedDecision policy.Decision
	if err := json.Unmarshal([]byte(approval.PayloadJSON), &approvedDecision); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("approval payload is not a command decision"))
		return
	}
	if approvedDecision.Command == "" {
		writeError(w, http.StatusBadRequest, errors.New("approval payload is missing command"))
		return
	}

	currentDecision := policy.EvaluateCommand(approvedDecision.Command)
	if currentDecision.Action == policy.ActionBlock {
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), approval.TaskID, "blocked"))
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), approval.TaskID, "command.blocked", currentDecision.Reason, mustJSON(currentDecision)))
		writeJSON(w, http.StatusForbidden, map[string]any{
			"status":   "blocked",
			"decision": currentDecision,
		})
		return
	}

	task, err := s.store.GetTask(r.Context(), approval.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.executeTaskCommandWithCallback(w, r, task, approvedDecision.Command, currentDecision, func() error {
		executed, err := s.store.MarkApprovalExecuted(r.Context(), approval.ID)
		if err != nil {
			return err
		}
		return s.publishLatestTaskEvent(r.Context(), executed.TaskID)
	})
}

func (s *Server) executeTaskCommand(w http.ResponseWriter, r *http.Request, task store.Task, command string, decision policy.Decision) {
	s.executeTaskCommandWithCallback(w, r, task, command, decision, nil)
}

func (s *Server) executeTaskCommandWithCallback(w http.ResponseWriter, r *http.Request, task store.Task, command string, decision policy.Decision, beforeRun func() error) {
	project, err := s.store.GetProject(r.Context(), task.ProjectID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !s.registerRunningProject(project.ID, task.ID) {
		writeError(w, http.StatusConflict, errors.New("project already has a running task"))
		return
	}
	defer s.unregisterRunningProject(project.ID)
	if beforeRun != nil {
		if err := beforeRun(); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
	}
	s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "running"))
	s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "command.started", command, mustJSON(decision)))
	runCtx, cancel := s.newTaskContext(r.Context())
	defer cancel()
	result, err := s.commandRunner.Run(runCtx, project.Path, command)
	if err != nil {
		result = CommandResult{
			Status:   "failed",
			Command:  command,
			ExitCode: -1,
			Stderr:   err.Error(),
		}
	}
	result.Command = command
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.Status = "timed_out"
		result.ExitCode = -1
		if result.Stderr == "" {
			result.Stderr = runCtx.Err().Error()
		}
	}
	if result.Status == "" {
		if result.ExitCode == 0 {
			result.Status = "completed"
		} else {
			result.Status = "failed"
		}
	}
	if result.Status == "completed" {
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "completed"))
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "command.completed", command, mustJSON(result)))
	} else if result.Status == "timed_out" {
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "timed_out"))
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "command.timed_out", command, mustJSON(result)))
	} else {
		s.logErr(r.Context(), "update_task_status", s.store.UpdateTaskStatus(r.Context(), task.ID, "failed"))
		s.logErr(r.Context(), "add_task_event", s.addTaskEvent(r.Context(), task.ID, "command.failed", command, mustJSON(result)))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleEvaluateCommandPolicy(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Command string `json:"command"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy.EvaluateCommand(request.Command))
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.SnapshotWithPending(http.StatusOK))
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	response := DoctorResponse{
		Status: "ok",
	}

	addCheck := func(check DoctorCheck) {
		if check.Status != "ok" {
			response.Status = "degraded"
		}
		response.Checks = append(response.Checks, check)
	}

	if err := s.store.Ping(r.Context()); err != nil {
		addCheck(DoctorCheck{Name: "sqlite", Status: "failed", Message: err.Error()})
	} else {
		addCheck(DoctorCheck{Name: "sqlite", Status: "ok"})
	}

	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		addCheck(DoctorCheck{Name: "projects", Status: "failed", Message: err.Error()})
	} else {
		addCheck(DoctorCheck{Name: "projects", Status: "ok", Message: fmt.Sprintf("%d registered", len(projects))})
		for _, project := range projects {
			projectStatus := DoctorProjectStatus{
				ID:     project.ID,
				Name:   project.Name,
				Path:   project.Path,
				Status: "ok",
			}
			if _, statErr := os.Stat(project.Path); statErr != nil {
				projectStatus.Status = "missing"
				projectStatus.Message = statErr.Error()
				response.Status = "degraded"
			}
			response.Projects = append(response.Projects, projectStatus)
		}
	}

	addCheck(s.commandDoctorCheck(r.Context(), "git", "git --version"))
	addCheck(s.commandDoctorCheck(r.Context(), "codex", "codex --version"))

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) commandDoctorCheck(ctx context.Context, name string, command string) DoctorCheck {
	result, err := s.commandRunner.Run(ctx, "", command)
	if err != nil {
		return DoctorCheck{Name: name, Status: "failed", Message: err.Error()}
	}
	if result.Status == "failed" || result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = strings.TrimSpace(result.Stdout)
		}
		if message == "" {
			message = fmt.Sprintf("%s check failed", name)
		}
		return DoctorCheck{Name: name, Status: "failed", Message: message}
	}
	return DoctorCheck{Name: name, Status: "ok", Message: strings.TrimSpace(result.Stdout)}
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if err := s.decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}

	result, err := s.store.Backup(r.Context(), request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) runReadOnlyGitCommand(ctx context.Context, workdir string, command string) (string, error) {
	result, err := s.commandRunner.Run(ctx, workdir, command)
	if err != nil {
		return "", err
	}
	if result.Status == "failed" || result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = "git command failed: " + command
		}
		return "", errors.New(message)
	}
	return result.Stdout, nil
}

func (s *Server) logErr(ctx context.Context, operation string, err error) {
	if err != nil {
		s.logger.ErrorContext(ctx, "best_effort_failed", "operation", operation, "error", err)
	}
}

func (s *Server) newTaskContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, s.taskTimeout)
}

func (s *Server) addTaskEvent(ctx context.Context, taskID string, eventType string, message string, payloadJSON string) error {
	if err := s.store.AddTaskEvent(ctx, taskID, eventType, message, payloadJSON); err != nil {
		return err
	}
	events, err := s.store.ListTaskEvents(ctx, taskID)
	if err != nil || len(events) == 0 {
		return err
	}
	s.publishTaskEvent(events[len(events)-1])
	return nil
}

func (s *Server) publishTaskEvent(event store.TaskEvent) {
	s.streamMu.Lock()
	for subscriber := range s.streams[event.TaskID] {
		select {
		case subscriber <- event:
		default:
		}
	}
	s.streamMu.Unlock()

	if s.onTaskEvent != nil {
		s.onTaskEvent(event)
	}
}

func (s *Server) publishLatestTaskEvent(ctx context.Context, taskID string) error {
	events, err := s.store.ListTaskEvents(ctx, taskID)
	if err != nil || len(events) == 0 {
		return err
	}
	s.publishTaskEvent(events[len(events)-1])
	return nil
}

func (s *Server) subscribeTaskEvents(taskID string) chan store.TaskEvent {
	events := make(chan store.TaskEvent, 16)
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.streams[taskID] == nil {
		s.streams[taskID] = make(map[chan store.TaskEvent]struct{})
	}
	s.streams[taskID][events] = struct{}{}
	return events
}

func (s *Server) unsubscribeTaskEvents(taskID string, events chan store.TaskEvent) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	delete(s.streams[taskID], events)
	if len(s.streams[taskID]) == 0 {
		delete(s.streams, taskID)
	}
	close(events)
}

func (s *Server) registerRunningTask(taskID string, cancel context.CancelFunc) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if _, exists := s.runningTasks[taskID]; exists {
		return false
	}
	s.runningTasks[taskID] = cancel
	return true
}

func (s *Server) cancelRunningTask(taskID string) (context.CancelFunc, bool) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	cancel, ok := s.runningTasks[taskID]
	return cancel, ok
}

func (s *Server) unregisterRunningTask(taskID string) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	delete(s.runningTasks, taskID)
}

func (s *Server) registerRunningProject(projectID string, taskID string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if _, exists := s.runningProjects[projectID]; exists {
		return false
	}
	s.runningProjects[projectID] = taskID
	return true
}

func (s *Server) unregisterRunningProject(projectID string) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	delete(s.runningProjects, projectID)
}
