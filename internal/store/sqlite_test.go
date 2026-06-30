package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"personal-ai-assistant/internal/store"
)

func TestSQLiteStoreCreatesAndListsProjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	projectPath := t.TempDir()

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name:      "Personal AI Assistant",
		Path:      projectPath,
		TechStack: "Go",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].ID != project.ID {
		t.Fatalf("expected project id %q, got %q", project.ID, projects[0].ID)
	}
	if !filepath.IsAbs(projects[0].Path) {
		t.Fatalf("expected absolute project path, got %q", projects[0].Path)
	}
}

func TestSQLiteStoreRejectsProjectPathThatDoesNotExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Missing Project",
		Path: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil {
		t.Fatal("expected missing project path to be rejected")
	}
}

func TestSQLiteStoreRejectsBlankProjectName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, name := range []string{"", "   "} {
		_, err = s.CreateProject(ctx, store.CreateProjectInput{
			Name: name,
			Path: t.TempDir(),
		})
		if err == nil {
			t.Fatalf("expected blank project name %q to be rejected", name)
		}
	}
}

func TestSQLiteStoreRejectsBlankProjectPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, path := range []string{"", "   "} {
		_, err = s.CreateProject(ctx, store.CreateProjectInput{
			Name: "Blank Path Project",
			Path: path,
		})
		if err == nil {
			t.Fatalf("expected blank project path %q to be rejected", path)
		}
	}
}

func TestSQLiteStoreCreatesTaskOnlyForRegisteredProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: "unknown-project",
		AgentType: "codex",
		Prompt:    "Run tests",
	})
	if err == nil {
		t.Fatal("expected task creation for unknown project to fail")
	}

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run tests",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Status != "queued" {
		t.Fatalf("expected queued task, got %q", task.Status)
	}
}

func TestSQLiteStoreRejectsBlankTaskPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	for _, prompt := range []string{"", "   "} {
		_, err = s.CreateTask(ctx, store.CreateTaskInput{
			ProjectID: project.ID,
			AgentType: "codex",
			Prompt:    prompt,
		})
		if err == nil {
			t.Fatalf("expected blank task prompt %q to be rejected", prompt)
		}
	}
}

func TestSQLiteStoreRejectsUnsupportedTaskAgentType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	for _, agentType := range []string{"", "   ", "claude"} {
		_, err = s.CreateTask(ctx, store.CreateTaskInput{
			ProjectID: project.ID,
			AgentType: agentType,
			Prompt:    "Run tests",
		})
		if err == nil {
			t.Fatalf("expected unsupported task agent type %q to be rejected", agentType)
		}
	}
}

func TestSQLiteStoreGetsTaskAndCreationEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	created, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run tests",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	fetched, err := s.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected task id %q, got %q", created.ID, fetched.ID)
	}

	events, err := s.ListTaskEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("list task events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 task event, got %d", len(events))
	}
	if events[0].EventType != "task.created" {
		t.Fatalf("expected task.created event, got %q", events[0].EventType)
	}
}

func TestSQLiteStoreListsTaskEventPayloadJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run tests",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := s.AddTaskEvent(ctx, task.ID, "command.completed", "go test ./...", `{"status":"completed"}`); err != nil {
		t.Fatalf("add task event: %v", err)
	}

	events, err := s.ListTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("list task events: %v", err)
	}
	last := events[len(events)-1]
	if last.PayloadJSON != `{"status":"completed"}` {
		t.Fatalf("expected payload JSON to round trip, got %q", last.PayloadJSON)
	}
}

func TestSQLiteStoreListsTasksWithFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	firstProject, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "First Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create first project: %v", err)
	}
	secondProject, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Second Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}

	firstTask, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: firstProject.ID,
		AgentType: "codex",
		Prompt:    "First task",
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	time.Sleep(time.Millisecond)
	secondTask, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: firstProject.ID,
		AgentType: "codex",
		Prompt:    "Second task",
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	time.Sleep(time.Millisecond)
	thirdTask, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: secondProject.ID,
		AgentType: "codex",
		Prompt:    "Third task",
	})
	if err != nil {
		t.Fatalf("create third task: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, secondTask.ID, "completed"); err != nil {
		t.Fatalf("mark task completed: %v", err)
	}

	allTasks, err := s.ListTasks(ctx, "", "")
	if err != nil {
		t.Fatalf("list all tasks: %v", err)
	}
	if len(allTasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(allTasks))
	}
	if allTasks[0].ID != thirdTask.ID || allTasks[1].ID != secondTask.ID || allTasks[2].ID != firstTask.ID {
		t.Fatalf("expected newest-first task order, got %#v", []string{allTasks[0].ID, allTasks[1].ID, allTasks[2].ID})
	}

	projectTasks, err := s.ListTasks(ctx, firstProject.ID, "")
	if err != nil {
		t.Fatalf("list project tasks: %v", err)
	}
	if len(projectTasks) != 2 {
		t.Fatalf("expected 2 project tasks, got %d", len(projectTasks))
	}

	completedTasks, err := s.ListTasks(ctx, "", "completed")
	if err != nil {
		t.Fatalf("list completed tasks: %v", err)
	}
	if len(completedTasks) != 1 || completedTasks[0].ID != secondTask.ID {
		t.Fatalf("expected completed task %q, got %#v", secondTask.ID, completedTasks)
	}
	if completedTasks[0].CompletedAt == nil {
		t.Fatal("expected completed task to include completed timestamp")
	}
}

func TestSQLiteStoreMigrationCreatesQueryIndexes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite metadata connection: %v", err)
	}
	defer db.Close()

	expectedIndexes := []string{
		"idx_tasks_project_created",
		"idx_tasks_status_created",
		"idx_task_events_task_id_id",
		"idx_approval_requests_status_created",
	}
	for _, indexName := range expectedIndexes {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(1)
			FROM sqlite_master
			WHERE type = 'index' AND name = ?
		`, indexName).Scan(&count)
		if err != nil {
			t.Fatalf("query index %s: %v", indexName, err)
		}
		if count != 1 {
			t.Fatalf("expected index %s to exist", indexName)
		}
	}
}

func TestSQLiteStoreSetsCompletedAtForTimedOutTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run long task",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, "timed_out"); err != nil {
		t.Fatalf("mark task timed out: %v", err)
	}

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updated.Status != "timed_out" {
		t.Fatalf("expected timed_out task, got %q", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Fatal("expected completed timestamp for timed_out task")
	}
}

func TestSQLiteStoreSetsStartedAtForRunningTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run task",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.StartedAt != nil {
		t.Fatal("expected queued task not to have started timestamp")
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("mark task running: %v", err)
	}

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("expected running task, got %q", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Fatal("expected started timestamp for running task")
	}
}

func TestSQLiteStoreRejectsInvalidTaskStatusUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run task",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	for _, status := range []string{"", "   ", "done"} {
		if err := s.UpdateTaskStatus(ctx, task.ID, status); err == nil {
			t.Fatalf("expected invalid task status %q to be rejected", status)
		}
	}

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if updated.Status != "queued" {
		t.Fatalf("expected invalid status updates to leave task queued, got %q", updated.Status)
	}
}

func TestSQLiteStoreCreatesListsAndResolvesApprovalRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Install package",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	approval, err := s.CreateApprovalRequest(ctx, store.CreateApprovalRequestInput{
		TaskID:      task.ID,
		ActionType:  "install_package",
		Description: "Install github.com/example/package",
		PayloadJSON: `{"package":"github.com/example/package"}`,
	})
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval, got %q", approval.Status)
	}

	pending, err := s.ListApprovalRequests(ctx, "pending")
	if err != nil {
		t.Fatalf("list approval requests: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}

	resolved, err := s.ResolveApprovalRequest(ctx, approval.ID, "approved")
	if err != nil {
		t.Fatalf("resolve approval request: %v", err)
	}
	if resolved.Status != "approved" {
		t.Fatalf("expected approved status, got %q", resolved.Status)
	}
	if resolved.ResolvedAt == nil {
		t.Fatal("expected resolved timestamp")
	}

	events, err := s.ListTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("list task events: %v", err)
	}
	if events[len(events)-1].EventType != "approval.approved" {
		t.Fatalf("expected approval.approved event, got %q", events[len(events)-1].EventType)
	}
}

func TestSQLiteStoreRejectsBlankApprovalFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run command",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	tests := []struct {
		name  string
		input store.CreateApprovalRequestInput
	}{
		{
			name: "blank action type",
			input: store.CreateApprovalRequestInput{
				TaskID:      task.ID,
				ActionType:  "   ",
				Description: "Run command",
			},
		},
		{
			name: "blank description",
			input: store.CreateApprovalRequestInput{
				TaskID:      task.ID,
				ActionType:  "command_execution",
				Description: "   ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.CreateApprovalRequest(ctx, tt.input); err == nil {
				t.Fatal("expected blank approval field to be rejected")
			}
		})
	}
}

func TestSQLiteStoreMarksApprovedCommandApprovalExecutedOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Registered Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run command",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	approval, err := s.CreateApprovalRequest(ctx, store.CreateApprovalRequestInput{
		TaskID:      task.ID,
		ActionType:  "command_execution",
		Description: "Run go version",
		PayloadJSON: `{"command":"go version"}`,
	})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	if _, err := s.ResolveApprovalRequest(ctx, approval.ID, "approved"); err != nil {
		t.Fatalf("approve request: %v", err)
	}

	executed, err := s.MarkApprovalExecuted(ctx, approval.ID)
	if err != nil {
		t.Fatalf("mark approval executed: %v", err)
	}
	if executed.Status != "executed" {
		t.Fatalf("expected executed status, got %q", executed.Status)
	}
	if _, err := s.MarkApprovalExecuted(ctx, approval.ID); err == nil {
		t.Fatal("expected second execute mark to be rejected")
	}

	events, err := s.ListTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("list task events: %v", err)
	}
	if events[len(events)-1].EventType != "approval.executed" {
		t.Fatalf("expected approval.executed event, got %q", events[len(events)-1].EventType)
	}
}

func TestSQLiteStoreRejectsApprovalForUnknownTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateApprovalRequest(ctx, store.CreateApprovalRequestInput{
		TaskID:      "missing-task",
		ActionType:  "install_package",
		Description: "Install package",
	})
	if err == nil {
		t.Fatal("expected approval request for unknown task to fail")
	}
}

func TestSQLiteStoreMarksRunningTasksInterrupted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Interrupted Project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	runningTask, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Run long task",
	})
	if err != nil {
		t.Fatalf("create running task: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, runningTask.ID, "running"); err != nil {
		t.Fatalf("mark task running: %v", err)
	}

	queuedTask, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Queued task",
	})
	if err != nil {
		t.Fatalf("create queued task: %v", err)
	}

	interrupted, err := s.MarkRunningTasksInterrupted(ctx)
	if err != nil {
		t.Fatalf("mark running tasks interrupted: %v", err)
	}
	if interrupted != 1 {
		t.Fatalf("expected 1 interrupted task, got %d", interrupted)
	}

	updatedRunningTask, err := s.GetTask(ctx, runningTask.ID)
	if err != nil {
		t.Fatalf("get running task: %v", err)
	}
	if updatedRunningTask.Status != "interrupted" {
		t.Fatalf("expected interrupted running task, got %q", updatedRunningTask.Status)
	}
	if updatedRunningTask.CompletedAt == nil {
		t.Fatal("expected completed timestamp for interrupted task")
	}

	updatedQueuedTask, err := s.GetTask(ctx, queuedTask.ID)
	if err != nil {
		t.Fatalf("get queued task: %v", err)
	}
	if updatedQueuedTask.Status != "queued" {
		t.Fatalf("expected queued task to remain queued, got %q", updatedQueuedTask.Status)
	}

	events, err := s.ListTaskEvents(ctx, runningTask.ID)
	if err != nil {
		t.Fatalf("list running task events: %v", err)
	}
	if events[len(events)-1].EventType != "task.interrupted" {
		t.Fatalf("expected task.interrupted event, got %q", events[len(events)-1].EventType)
	}
}

func TestSQLiteStoreBacksUpDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	_, err = s.CreateProject(ctx, store.CreateProjectInput{
		Name: "Project To Backup",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "gateway-backup.db")
	result, err := s.Backup(ctx, backupPath)
	if err != nil {
		t.Fatalf("backup database: %v", err)
	}
	if result.Path != backupPath {
		t.Fatalf("expected backup path %q, got %q", backupPath, result.Path)
	}
	if result.SizeBytes == 0 {
		t.Fatal("expected non-empty backup file")
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close original store: %v", err)
	}

	backup, err := store.OpenSQLite(ctx, backupPath)
	if err != nil {
		t.Fatalf("open backup store: %v", err)
	}
	defer backup.Close()

	projects, err := backup.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list backup projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 backed up project, got %d", len(projects))
	}
}

func TestSQLiteStoreRejectsBlankBackupPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, path := range []string{"", "   "} {
		_, err = s.Backup(ctx, path)
		if err == nil {
			t.Fatalf("expected blank backup path %q to be rejected", path)
		}
	}
}

func TestSQLiteStoreUpdatesAndDeletesProjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "gateway.db")
	projectPath := t.TempDir()

	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject(ctx, store.CreateProjectInput{
		Name:      "Original Name",
		Path:      projectPath,
		TechStack: "Go",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	updatedPath := t.TempDir()
	updated, err := s.UpdateProject(ctx, project.ID, store.CreateProjectInput{
		Name:      "Updated Name",
		Path:      updatedPath,
		TechStack: "Rust",
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Fatalf("expected updated name %q, got %q", "Updated Name", updated.Name)
	}
	if updated.TechStack != "Rust" {
		t.Fatalf("expected updated tech stack %q, got %q", "Rust", updated.TechStack)
	}

	task, err := s.CreateTask(ctx, store.CreateTaskInput{
		ProjectID: project.ID,
		AgentType: "codex",
		Prompt:    "Test prompt",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	err = s.AddTaskEvent(ctx, task.ID, "task.created", "Task created message", "")
	if err != nil {
		t.Fatalf("add task event: %v", err)
	}
	_, err = s.CreateApprovalRequest(ctx, store.CreateApprovalRequestInput{
		TaskID:      task.ID,
		ActionType:  "command_execution",
		Description: "Approve command",
		PayloadJSON: "{}",
	})
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}

	err = s.DeleteProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("delete project: %v", err)
	}

	_, err = s.GetProject(ctx, project.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleted project, got: %v", err)
	}

	_, err = s.GetTask(ctx, task.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleted project's task, got: %v", err)
	}
}

