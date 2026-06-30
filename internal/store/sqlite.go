package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type SQLiteStore struct {
	db *sql.DB
}

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	TechStack     string    `json:"techStack,omitempty"`
	DefaultBranch string    `json:"defaultBranch,omitempty"`
	TestCommand   string    `json:"testCommand,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Task struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"projectId"`
	Prompt       string     `json:"prompt"`
	Status       string     `json:"status"`
	AgentType    string     `json:"agentType"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	Stdout       string     `json:"stdout,omitempty"`
	Stderr       string     `json:"stderr,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
}

type TaskEvent struct {
	ID          int64     `json:"id"`
	TaskID      string    `json:"taskId"`
	EventType   string    `json:"eventType"`
	Message     string    `json:"message"`
	PayloadJSON string    `json:"payloadJson,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ApprovalRequest struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"taskId"`
	ActionType  string     `json:"actionType"`
	Description string     `json:"description"`
	PayloadJSON string     `json:"payloadJson,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
}

type BackupResult struct {
	Path      string    `json:"path"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateProjectInput struct {
	Name          string
	Path          string
	TechStack     string
	DefaultBranch string
	TestCommand   string
}

type CreateTaskInput struct {
	ProjectID string
	AgentType string
	Prompt    string
}

type CreateApprovalRequestInput struct {
	TaskID      string
	ActionType  string
	Description string
	PayloadJSON string
}

func OpenSQLite(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) CreateProject(ctx context.Context, input CreateProjectInput) (Project, error) {
	if strings.TrimSpace(input.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	if strings.TrimSpace(input.Path) == "" {
		return Project{}, errors.New("project path is required")
	}

	absPath, err := filepath.Abs(input.Path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return Project{}, fmt.Errorf("project path must exist: %w", err)
	}
	if !info.IsDir() {
		return Project{}, errors.New("project path must be a directory")
	}

	now := time.Now().UTC()
	project := Project{
		ID:            "proj_" + randomID(),
		Name:          input.Name,
		Path:          absPath,
		TechStack:     input.TechStack,
		DefaultBranch: input.DefaultBranch,
		TestCommand:   input.TestCommand,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, path, tech_stack, default_branch, test_command, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, project.ID, project.Name, project.Path, project.TechStack, project.DefaultBranch, project.TestCommand, project.CreatedAt.Format(time.RFC3339Nano), project.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Project{}, err
	}

	return project, nil
}

func (s *SQLiteStore) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, path, tech_stack, default_branch, test_command, created_at, updated_at
		FROM projects
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var project Project
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&project.ID, &project.Name, &project.Path, &project.TechStack, &project.DefaultBranch, &project.TestCommand, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		project.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		project.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func (s *SQLiteStore) GetProject(ctx context.Context, id string) (Project, error) {
	var project Project
	var createdAt string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, path, tech_stack, default_branch, test_command, created_at, updated_at
		FROM projects
		WHERE id = ?
	`, id).Scan(&project.ID, &project.Name, &project.Path, &project.TechStack, &project.DefaultBranch, &project.TestCommand, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	project.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Project{}, err
	}
	project.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *SQLiteStore) UpdateProject(ctx context.Context, id string, input CreateProjectInput) (Project, error) {
	if strings.TrimSpace(input.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	if strings.TrimSpace(input.Path) == "" {
		return Project{}, errors.New("project path is required")
	}

	absPath, err := filepath.Abs(input.Path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return Project{}, fmt.Errorf("project path must exist: %w", err)
	}
	if !info.IsDir() {
		return Project{}, errors.New("project path must be a directory")
	}

	existing, err := s.GetProject(ctx, id)
	if err != nil {
		return Project{}, err
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		UPDATE projects
		SET name = ?, path = ?, tech_stack = ?, default_branch = ?, test_command = ?, updated_at = ?
		WHERE id = ?
	`, input.Name, absPath, input.TechStack, input.DefaultBranch, input.TestCommand, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return Project{}, err
	}

	existing.Name = input.Name
	existing.Path = absPath
	existing.TechStack = input.TechStack
	existing.DefaultBranch = input.DefaultBranch
	existing.TestCommand = input.TestCommand
	existing.UpdatedAt = now

	return existing, nil
}

func (s *SQLiteStore) DeleteProject(ctx context.Context, id string) error {
	_, err := s.GetProject(ctx, id)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE project_id = ?`, id)
	if err != nil {
		return err
	}
	defer rows.Close()

	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, taskID := range taskIDs {
		_, err = tx.ExecContext(ctx, `DELETE FROM task_events WHERE task_id = ?`, taskID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM approval_requests WHERE task_id = ?`, taskID)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM tasks WHERE project_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) CreateTask(ctx context.Context, input CreateTaskInput) (Task, error) {
	if input.ProjectID == "" {
		return Task{}, errors.New("project id is required")
	}
	if strings.TrimSpace(input.AgentType) == "" {
		return Task{}, errors.New("agent type is required")
	}
	if input.AgentType != "codex" && input.AgentType != "llm" {
		return Task{}, errors.New("agent type must be codex or llm")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return Task{}, errors.New("prompt is required")
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM projects WHERE id = ?", input.ProjectID).Scan(&exists); err != nil {
		return Task{}, err
	}
	if exists == 0 {
		return Task{}, errors.New("project is not registered")
	}

	now := time.Now().UTC()
	task := Task{
		ID:        "task_" + randomID(),
		ProjectID: input.ProjectID,
		Prompt:    input.Prompt,
		Status:    "queued",
		AgentType: input.AgentType,
		CreatedAt: now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tasks (id, project_id, prompt, status, agent_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, task.ID, task.ProjectID, task.Prompt, task.Status, task.AgentType, task.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events (task_id, event_type, message, created_at)
		VALUES (?, ?, ?, ?)
	`, task.ID, "task.created", "Task queued", task.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, err
	}

	if err := tx.Commit(); err != nil {
		return Task{}, err
	}

	return task, nil
}

func (s *SQLiteStore) UpdateTaskStatus(ctx context.Context, taskID string, status string) error {
	if taskID == "" {
		return errors.New("task id is required")
	}
	if !isValidTaskStatus(status) {
		return errors.New("task status is invalid")
	}

	var completedAt any
	if isTerminalTaskStatus(status) {
		completedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	var startedAt any
	if status == "running" {
		startedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, started_at = COALESCE(started_at, ?), completed_at = COALESCE(?, completed_at)
		WHERE id = ?
	`, status, startedAt, completedAt, taskID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) MarkRunningTasksInterrupted(ctx context.Context) (int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM tasks WHERE status = 'running'")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return 0, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(taskIDs) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, taskID := range taskIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'interrupted', completed_at = ?
			WHERE id = ? AND status = 'running'
		`, now.Format(time.RFC3339Nano), taskID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO task_events (task_id, event_type, message, created_at)
			VALUES (?, ?, ?, ?)
		`, taskID, "task.interrupted", "Task marked interrupted after gateway restart", now.Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(taskIDs)), nil
}

func (s *SQLiteStore) AddTaskEvent(ctx context.Context, taskID string, eventType string, message string, payloadJSON string) error {
	if taskID == "" {
		return errors.New("task id is required")
	}
	if eventType == "" {
		return errors.New("event type is required")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_events (task_id, event_type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, taskID, eventType, message, payloadJSON, now.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) GetTask(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, prompt, status, agent_type, started_at, completed_at, created_at,
		       COALESCE(stdout, ''), COALESCE(stderr, ''), COALESCE(error_message, '')
		FROM tasks
		WHERE id = ?
	`, id)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

// SaveTaskOutput persists the stdout, stderr and error_message of a completed task.
func (s *SQLiteStore) SaveTaskOutput(ctx context.Context, taskID string, stdout string, stderr string, errorMessage string) error {
	if taskID == "" {
		return errors.New("task id is required")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET stdout = ?, stderr = ?, error_message = ? WHERE id = ?
	`, stdout, stderr, errorMessage, taskID)
	return err
}

func (s *SQLiteStore) ListTasks(ctx context.Context, projectID string, status string) ([]Task, error) {
	query := `
		SELECT id, project_id, prompt, status, agent_type, started_at, completed_at, created_at,
		       COALESCE(stdout, ''), COALESCE(stderr, ''), COALESCE(error_message, '')
		FROM tasks
	`
	var conditions []string
	var args []any
	if projectID != "" {
		conditions = append(conditions, "project_id = ?")
		args = append(args, projectID)
	}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *SQLiteStore) ListTaskEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, event_type, message, payload_json, created_at
		FROM task_events
		WHERE task_id = ?
		ORDER BY id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []TaskEvent
	for rows.Next() {
		var event TaskEvent
		var createdAt string
		if err := rows.Scan(&event.ID, &event.TaskID, &event.EventType, &event.Message, &event.PayloadJSON, &createdAt); err != nil {
			return nil, err
		}
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) CreateApprovalRequest(ctx context.Context, input CreateApprovalRequestInput) (ApprovalRequest, error) {
	if input.TaskID == "" {
		return ApprovalRequest{}, errors.New("task id is required")
	}
	if strings.TrimSpace(input.ActionType) == "" {
		return ApprovalRequest{}, errors.New("action type is required")
	}
	if strings.TrimSpace(input.Description) == "" {
		return ApprovalRequest{}, errors.New("description is required")
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM tasks WHERE id = ?", input.TaskID).Scan(&exists); err != nil {
		return ApprovalRequest{}, err
	}
	if exists == 0 {
		return ApprovalRequest{}, errors.New("task is not registered")
	}

	now := time.Now().UTC()
	approval := ApprovalRequest{
		ID:          "appr_" + randomID(),
		TaskID:      input.TaskID,
		ActionType:  input.ActionType,
		Description: input.Description,
		PayloadJSON: input.PayloadJSON,
		Status:      "pending",
		CreatedAt:   now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRequest{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_requests (id, task_id, action_type, description, payload_json, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, approval.ID, approval.TaskID, approval.ActionType, approval.Description, approval.PayloadJSON, approval.Status, approval.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return ApprovalRequest{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events (task_id, event_type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, approval.TaskID, "approval.requested", approval.Description, approval.PayloadJSON, approval.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return ApprovalRequest{}, err
	}

	if err := tx.Commit(); err != nil {
		return ApprovalRequest{}, err
	}
	return approval, nil
}

func (s *SQLiteStore) ListApprovalRequests(ctx context.Context, status string) ([]ApprovalRequest, error) {
	query := `
		SELECT id, task_id, action_type, description, payload_json, status, created_at, resolved_at
		FROM approval_requests
	`
	var args []any
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []ApprovalRequest
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	return approvals, rows.Err()
}

func (s *SQLiteStore) ResolveApprovalRequest(ctx context.Context, id string, status string) (ApprovalRequest, error) {
	if status != "approved" && status != "rejected" {
		return ApprovalRequest{}, errors.New("approval status must be approved or rejected")
	}

	current, err := s.GetApprovalRequest(ctx, id)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if current.Status != "pending" {
		return ApprovalRequest{}, errors.New("approval request is already resolved")
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRequest{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = ?, resolved_at = ?
		WHERE id = ? AND status = 'pending'
	`, status, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return ApprovalRequest{}, err
	}

	eventType := "approval." + status
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events (task_id, event_type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, current.TaskID, eventType, current.Description, current.PayloadJSON, now.Format(time.RFC3339Nano))
	if err != nil {
		return ApprovalRequest{}, err
	}

	if err := tx.Commit(); err != nil {
		return ApprovalRequest{}, err
	}

	current.Status = status
	current.ResolvedAt = &now
	return current, nil
}

func (s *SQLiteStore) MarkApprovalExecuted(ctx context.Context, id string) (ApprovalRequest, error) {
	current, err := s.GetApprovalRequest(ctx, id)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if current.Status != "approved" {
		return ApprovalRequest{}, errors.New("approval request must be approved before execution")
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRequest{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = ?
		WHERE id = ? AND status = 'approved'
	`, "executed", id)
	if err != nil {
		return ApprovalRequest{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return ApprovalRequest{}, err
	}
	if rowsAffected != 1 {
		return ApprovalRequest{}, errors.New("approval request must be approved before execution")
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events (task_id, event_type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, current.TaskID, "approval.executed", current.Description, current.PayloadJSON, now.Format(time.RFC3339Nano))
	if err != nil {
		return ApprovalRequest{}, err
	}

	if err := tx.Commit(); err != nil {
		return ApprovalRequest{}, err
	}

	current.Status = "executed"
	return current, nil
}

func (s *SQLiteStore) GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, action_type, description, payload_json, status, created_at, resolved_at
		FROM approval_requests
		WHERE id = ?
	`, id)
	approval, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRequest{}, ErrNotFound
	}
	return approval, err
}

func (s *SQLiteStore) Backup(ctx context.Context, destinationPath string) (BackupResult, error) {
	if strings.TrimSpace(destinationPath) == "" {
		return BackupResult{}, errors.New("backup path is required")
	}

	absPath, err := filepath.Abs(destinationPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("resolve backup path: %w", err)
	}
	if _, err := os.Stat(absPath); err == nil {
		return BackupResult{}, errors.New("backup path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return BackupResult{}, fmt.Errorf("check backup path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return BackupResult{}, fmt.Errorf("create backup directory: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "VACUUM main INTO ?", absPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("backup database: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("stat backup: %w", err)
	}

	return BackupResult{
		Path:      absPath,
		SizeBytes: info.Size(),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		PRAGMA foreign_keys = ON;

		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			tech_stack TEXT NOT NULL DEFAULT '',
			default_branch TEXT NOT NULL DEFAULT '',
			test_command TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			status TEXT NOT NULL,
			agent_type TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id)
		);

		CREATE TABLE IF NOT EXISTS task_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);

		CREATE TABLE IF NOT EXISTS approval_requests (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			action_type TEXT NOT NULL,
			description TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			resolved_at TEXT,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);

		CREATE INDEX IF NOT EXISTS idx_tasks_project_created
		ON tasks (project_id, created_at DESC);

		CREATE INDEX IF NOT EXISTS idx_tasks_status_created
		ON tasks (status, created_at DESC);

		CREATE INDEX IF NOT EXISTS idx_task_events_task_id_id
		ON task_events (task_id, id);

		CREATE INDEX IF NOT EXISTS idx_approval_requests_status_created
		ON approval_requests (status, created_at ASC);
	`)
	if err != nil {
		return err
	}

	_, _ = s.db.ExecContext(ctx, "ALTER TABLE projects ADD COLUMN test_command TEXT NOT NULL DEFAULT '';")
	// Migration: add output columns to tasks table (safe to run multiple times - SQLite ignores duplicate ALTER TABLE)
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE tasks ADD COLUMN stdout TEXT NOT NULL DEFAULT '';")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE tasks ADD COLUMN stderr TEXT NOT NULL DEFAULT '';")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE tasks ADD COLUMN error_message TEXT NOT NULL DEFAULT '';")
	return nil
}

type approvalScanner interface {
	Scan(dest ...any) error
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (Task, error) {
	var task Task
	var createdAt string
	var startedAt sql.NullString
	var completedAt sql.NullString
	if err := scanner.Scan(
		&task.ID, &task.ProjectID, &task.Prompt, &task.Status, &task.AgentType,
		&startedAt, &completedAt, &createdAt,
		&task.Stdout, &task.Stderr, &task.ErrorMessage,
	); err != nil {
		return Task{}, err
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Task{}, err
	}
	task.CreatedAt = parsedCreatedAt

	if startedAt.Valid {
		parsedStartedAt, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return Task{}, err
		}
		task.StartedAt = &parsedStartedAt
	}
	if completedAt.Valid {
		parsedCompletedAt, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Task{}, err
		}
		task.CompletedAt = &parsedCompletedAt
	}
	return task, nil
}

func scanApproval(scanner approvalScanner) (ApprovalRequest, error) {
	var approval ApprovalRequest
	var createdAt string
	var resolvedAt sql.NullString
	if err := scanner.Scan(&approval.ID, &approval.TaskID, &approval.ActionType, &approval.Description, &approval.PayloadJSON, &approval.Status, &createdAt, &resolvedAt); err != nil {
		return ApprovalRequest{}, err
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ApprovalRequest{}, err
	}
	approval.CreatedAt = parsedCreatedAt

	if resolvedAt.Valid {
		parsedResolvedAt, err := time.Parse(time.RFC3339Nano, resolvedAt.String)
		if err != nil {
			return ApprovalRequest{}, err
		}
		approval.ResolvedAt = &parsedResolvedAt
	}
	return approval, nil
}

func (s *SQLiteStore) FindTaskPlanApproval(ctx context.Context, taskID string) (ApprovalRequest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, action_type, description, payload_json, status, created_at, resolved_at
		FROM approval_requests
		WHERE task_id = ? AND action_type = 'task_plan'
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID)
	approval, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRequest{}, ErrNotFound
	}
	return approval, err
}

func isTerminalTaskStatus(status string) bool {
	switch status {
	case "completed", "failed", "blocked", "canceled", "timed_out", "interrupted":
		return true
	default:
		return false
	}
}

func isValidTaskStatus(status string) bool {
	switch status {
	case "queued", "running", "waiting_for_approval", "completed", "failed", "blocked", "canceled", "timed_out", "interrupted":
		return true
	default:
		return false
	}
}

func randomID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}
