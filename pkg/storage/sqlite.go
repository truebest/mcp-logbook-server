package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/truebest/mcp-logbook-server/pkg/models"
)

// SQLiteStorage implements WorkStorage using SQLite.
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLiteStorage creates a new SQLite storage instance.
func NewSQLiteStorage(connectionString string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", connectionString)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	storage := &SQLiteStorage{db: db}
	if err := storage.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return storage, nil
}

// NewSQLiteStorageWithSearch is kept for old internal callers; search is unused for work entries.
func NewSQLiteStorageWithSearch(connectionString, searchIndexPath string) (*SQLiteStorage, error) {
	return NewSQLiteStorage(connectionString)
}

func (s *SQLiteStorage) migrate() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{
			version: 1,
			sql: `
				CREATE TABLE IF NOT EXISTS work_entries (
					id TEXT PRIMARY KEY,
					parent_task_id TEXT NOT NULL,
					sub_task_id TEXT NOT NULL,
					text TEXT NOT NULL,
					agent_id TEXT NOT NULL,
					timestamp DATETIME NOT NULL,
					metadata TEXT,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				);

				CREATE INDEX IF NOT EXISTS idx_work_entries_parent_task_id ON work_entries(parent_task_id);
				CREATE INDEX IF NOT EXISTS idx_work_entries_sub_task_id ON work_entries(sub_task_id);
				CREATE INDEX IF NOT EXISTS idx_work_entries_timestamp ON work_entries(timestamp);
				CREATE INDEX IF NOT EXISTS idx_work_entries_agent_id ON work_entries(agent_id);
				CREATE INDEX IF NOT EXISTS idx_work_entries_parent_sub_timestamp ON work_entries(parent_task_id, sub_task_id, timestamp);
			`,
		},
		{
			version: 2,
			sql: `
				ALTER TABLE work_entries ADD COLUMN git_branch TEXT NOT NULL DEFAULT 'NA';
				CREATE INDEX IF NOT EXISTS idx_work_entries_git_branch ON work_entries(git_branch);
			`,
		},
		{
			version: 3,
			sql: `
				ALTER TABLE work_entries ADD COLUMN content TEXT;
			`,
		},
		{
			version: 4,
			sql: `
				ALTER TABLE work_entries RENAME COLUMN parent_task_id TO orchestrator_task_id;
				ALTER TABLE work_entries RENAME COLUMN sub_task_id TO agent_task_id;
				DROP INDEX IF EXISTS idx_work_entries_parent_task_id;
				DROP INDEX IF EXISTS idx_work_entries_sub_task_id;
				DROP INDEX IF EXISTS idx_work_entries_parent_sub_timestamp;
				DROP INDEX IF EXISTS idx_work_entries_orchestrator_task_id;
				DROP INDEX IF EXISTS idx_work_entries_agent_task_id;
				DROP INDEX IF EXISTS idx_work_entries_orchestrator_agent_timestamp;
				CREATE INDEX IF NOT EXISTS idx_work_entries_orchestrator_task_id ON work_entries(orchestrator_task_id);
				CREATE INDEX IF NOT EXISTS idx_work_entries_agent_task_id ON work_entries(agent_task_id);
				CREATE INDEX IF NOT EXISTS idx_work_entries_orchestrator_agent_timestamp ON work_entries(orchestrator_task_id, agent_task_id, timestamp);
			`,
		},
	}

	for _, migration := range migrations {
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM migrations WHERE version = ?", migration.version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration version %d: %w", migration.version, err)
		}
		if count > 0 {
			continue
		}
		if _, err := s.db.Exec(migration.sql); err != nil {
			return fmt.Errorf("failed to apply migration version %d: %w", migration.version, err)
		}
		if _, err := s.db.Exec("INSERT INTO migrations (version) VALUES (?)", migration.version); err != nil {
			return fmt.Errorf("failed to record migration version %d: %w", migration.version, err)
		}
	}

	return nil
}

// StoreWorkEntries stores work-log entries in one transaction.
func (s *SQLiteStorage) StoreWorkEntries(ctx context.Context, entries []models.WorkEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO work_entries (
			id, orchestrator_task_id, agent_task_id, text, content, agent_id, git_branch, timestamp, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		if strings.TrimSpace(entry.GitBranch) == "" {
			entry.GitBranch = "NA"
		}

		if err := entry.Validate(); err != nil {
			return fmt.Errorf("invalid work entry %s: %w", entry.ID, err)
		}

		var metadataJSON *string
		if entry.Metadata != nil {
			data, err := json.Marshal(entry.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata for work entry %s: %w", entry.ID, err)
			}
			metadataStr := string(data)
			metadataJSON = &metadataStr
		}

		var contentJSON *string
		if len(entry.Content) > 0 {
			data, err := json.Marshal(entry.Content)
			if err != nil {
				return fmt.Errorf("failed to marshal content for work entry %s: %w", entry.ID, err)
			}
			contentStr := string(data)
			contentJSON = &contentStr
		}

		if _, err := stmt.ExecContext(ctx,
			entry.ID,
			entry.OrchestratorTaskID,
			entry.AgentTaskID,
			entry.Text,
			contentJSON,
			entry.AgentID,
			entry.GitBranch,
			entry.Timestamp,
			metadataJSON,
		); err != nil {
			return fmt.Errorf("failed to insert work entry %s: %w", entry.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// QueryWorkEntries retrieves work-log entries with filtering and pagination.
func (s *SQLiteStorage) QueryWorkEntries(ctx context.Context, filter models.WorkEntryFilter) (*models.WorkEntryResult, error) {
	whereClause, args := workEntryWhereClause(filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM work_entries %s", whereClause)
	var totalCount int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, orchestrator_task_id, agent_task_id, text, content, agent_id, git_branch, timestamp, metadata
		FROM work_entries %s
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query work entries: %w", err)
	}
	defer rows.Close()

	entries := make([]models.WorkEntry, 0)
	for rows.Next() {
		entry, err := scanWorkEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating work entries: %w", err)
	}

	return &models.WorkEntryResult{
		Entries:    entries,
		TotalCount: totalCount,
		HasMore:    offset+len(entries) < totalCount,
	}, nil
}

// ListTasks returns task-level work-log summaries.
func (s *SQLiteStorage) ListTasks(ctx context.Context, filter models.WorkEntryFilter) ([]models.TaskInfo, error) {
	whereClause, args := workEntryWhereClause(filter)
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT orchestrator_task_id, agent_task_id, COUNT(*) as entry_count, MAX(timestamp) as last_entry_at
		FROM work_entries %s
		GROUP BY orchestrator_task_id, agent_task_id
		ORDER BY last_entry_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]models.TaskInfo, 0)
	for rows.Next() {
		var task models.TaskInfo
		var lastEntryAt string
		if err := rows.Scan(&task.OrchestratorTaskID, &task.AgentTaskID, &task.EntryCount, &lastEntryAt); err != nil {
			return nil, fmt.Errorf("failed to scan task info: %w", err)
		}
		parsedLastEntryAt, err := parseSQLiteTime(lastEntryAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_entry_at for task %s/%s: %w", task.OrchestratorTaskID, task.AgentTaskID, err)
		}
		task.LastEntryAt = parsedLastEntryAt

		taskFilter := filter
		taskFilter.OrchestratorTaskID = task.OrchestratorTaskID
		taskFilter.AgentTaskID = task.AgentTaskID
		agents, err := s.listTaskAgents(ctx, taskFilter)
		if err != nil {
			return nil, err
		}
		task.Agents = agents

		branches, err := s.listTaskBranches(ctx, taskFilter)
		if err != nil {
			return nil, err
		}
		task.GitBranches = branches

		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tasks: %w", err)
	}

	return tasks, nil
}

// DeleteTaskEntries deletes all work-log entries for one parent/sub-task pair.
func (s *SQLiteStorage) DeleteTaskEntries(ctx context.Context, orchestratorTaskID, agentTaskID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM work_entries
		WHERE orchestrator_task_id = ? AND agent_task_id = ?
	`, orchestratorTaskID, agentTaskID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete task entries for %s/%s: %w", orchestratorTaskID, agentTaskID, err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted entry count for %s/%s: %w", orchestratorTaskID, agentTaskID, err)
	}

	return deleted, nil
}

// DeleteParentTaskEntries deletes all work-log entries for one parent task.
func (s *SQLiteStorage) DeleteParentTaskEntries(ctx context.Context, orchestratorTaskID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM work_entries
		WHERE orchestrator_task_id = ?
	`, orchestratorTaskID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete parent task entries for %s: %w", orchestratorTaskID, err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted entry count for %s: %w", orchestratorTaskID, err)
	}

	return deleted, nil
}

func (s *SQLiteStorage) listTaskAgents(ctx context.Context, filter models.WorkEntryFilter) ([]string, error) {
	whereClause, args := workEntryWhereClause(filter)
	query := fmt.Sprintf(`
		SELECT DISTINCT agent_id
		FROM work_entries %s
		ORDER BY agent_id ASC
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
	}
	defer rows.Close()

	agents := make([]string, 0)
	for rows.Next() {
		var agent string
		if err := rows.Scan(&agent); err != nil {
			return nil, fmt.Errorf("failed to scan agent for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
	}

	return agents, nil
}

func (s *SQLiteStorage) listTaskBranches(ctx context.Context, filter models.WorkEntryFilter) ([]string, error) {
	whereClause, args := workEntryWhereClause(filter)
	query := fmt.Sprintf(`
		SELECT DISTINCT git_branch
		FROM work_entries %s
		ORDER BY git_branch ASC
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query git branches for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
	}
	defer rows.Close()

	branches := make([]string, 0)
	for rows.Next() {
		var branch string
		if err := rows.Scan(&branch); err != nil {
			return nil, fmt.Errorf("failed to scan git branch for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
		}
		branches = append(branches, branch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating git branches for task %s/%s: %w", filter.OrchestratorTaskID, filter.AgentTaskID, err)
	}

	return branches, nil
}

func scanWorkEntry(rows interface {
	Scan(dest ...interface{}) error
}) (models.WorkEntry, error) {
	var entry models.WorkEntry
	var contentJSON sql.NullString
	var metadataJSON sql.NullString

	if err := rows.Scan(
		&entry.ID,
		&entry.OrchestratorTaskID,
		&entry.AgentTaskID,
		&entry.Text,
		&contentJSON,
		&entry.AgentID,
		&entry.GitBranch,
		&entry.Timestamp,
		&metadataJSON,
	); err != nil {
		return entry, fmt.Errorf("failed to scan work entry: %w", err)
	}

	if contentJSON.Valid && strings.TrimSpace(contentJSON.String) != "" {
		if err := json.Unmarshal([]byte(contentJSON.String), &entry.Content); err != nil {
			return entry, fmt.Errorf("failed to unmarshal content for work entry %s: %w", entry.ID, err)
		}
	}

	if metadataJSON.Valid {
		if err := json.Unmarshal([]byte(metadataJSON.String), &entry.Metadata); err != nil {
			return entry, fmt.Errorf("failed to unmarshal metadata for work entry %s: %w", entry.ID, err)
		}
	}

	return entry, nil
}

func workEntryWhereClause(filter models.WorkEntryFilter) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if filter.OrchestratorTaskID != "" {
		conditions = append(conditions, "orchestrator_task_id = ?")
		args = append(args, filter.OrchestratorTaskID)
	}
	if filter.AgentTaskID != "" {
		conditions = append(conditions, "agent_task_id = ?")
		args = append(args, filter.AgentTaskID)
	}
	if filter.AgentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if !filter.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.EndTime)
	}

	if len(conditions) == 0 {
		return "", args
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}

func parseSQLiteTime(value string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
	}

	var lastErr error
	for _, format := range formats {
		parsed, err := time.Parse(format, value)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}

	return time.Time{}, lastErr
}

// HealthCheck returns the health status of the SQLite database.
func (s *SQLiteStorage) HealthCheck(ctx context.Context) models.HealthStatus {
	status := models.HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"database": "connected"},
	}

	if err := s.db.PingContext(ctx); err != nil {
		status.Status = "unhealthy"
		status.Details["database"] = "disconnected"
		status.Details["error"] = err.Error()
		return status
	}

	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_entries").Scan(&count); err != nil {
		status.Status = "unhealthy"
		status.Details["database"] = "query_failed"
		status.Details["error"] = err.Error()
		return status
	}
	status.Details["entry_count"] = fmt.Sprintf("%d", count)

	return status
}

// Close closes the storage connection.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}
