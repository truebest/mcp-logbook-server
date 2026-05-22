package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/truebest/mcp-logbook-server/pkg/models"
)

type mockWorkStorage struct {
	entries []models.WorkEntry
}

func (m *mockWorkStorage) StoreWorkEntries(ctx context.Context, entries []models.WorkEntry) error {
	m.entries = append(m.entries, entries...)
	return nil
}

func (m *mockWorkStorage) QueryWorkEntries(ctx context.Context, filter models.WorkEntryFilter) (*models.WorkEntryResult, error) {
	var entries []models.WorkEntry
	for _, entry := range m.entries {
		if filter.OrchestratorTaskID != "" && entry.OrchestratorTaskID != filter.OrchestratorTaskID {
			continue
		}
		if filter.AgentTaskID != "" && entry.AgentTaskID != filter.AgentTaskID {
			continue
		}
		if filter.AgentID != "" && entry.AgentID != filter.AgentID {
			continue
		}
		if !filter.StartTime.IsZero() && entry.Timestamp.Before(filter.StartTime) {
			continue
		}
		if !filter.EndTime.IsZero() && entry.Timestamp.After(filter.EndTime) {
			continue
		}
		entries = append(entries, entry)
	}
	return &models.WorkEntryResult{Entries: entries, TotalCount: len(entries)}, nil
}

func (m *mockWorkStorage) ListTasks(ctx context.Context, filter models.WorkEntryFilter) ([]models.TaskInfo, error) {
	counts := map[string]*models.TaskInfo{}
	for _, entry := range m.entries {
		if !filter.StartTime.IsZero() && entry.Timestamp.Before(filter.StartTime) {
			continue
		}
		if !filter.EndTime.IsZero() && entry.Timestamp.After(filter.EndTime) {
			continue
		}
		key := entry.OrchestratorTaskID + "/" + entry.AgentTaskID
		task := counts[key]
		if task == nil {
			task = &models.TaskInfo{
				OrchestratorTaskID: entry.OrchestratorTaskID,
				AgentTaskID:        entry.AgentTaskID,
				Agents:             []string{entry.AgentID},
				GitBranches:        []string{entry.GitBranch},
			}
			counts[key] = task
		}
		task.EntryCount++
		if entry.Timestamp.After(task.LastEntryAt) {
			task.LastEntryAt = entry.Timestamp
		}
	}
	tasks := make([]models.TaskInfo, 0, len(counts))
	for _, task := range counts {
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

func (m *mockWorkStorage) HealthCheck(ctx context.Context) models.HealthStatus {
	return models.HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"storage": "ok"},
	}
}

func (m *mockWorkStorage) Close() error {
	return nil
}

func TestNewServerRegistersWorkLogbookTools(t *testing.T) {
	server := NewServer(8081, &mockWorkStorage{})
	expectedTools := []string{"add_work_entry", "get_task_entries", "list_tasks", "get_service_status"}
	for _, toolName := range expectedTools {
		if _, ok := server.tools[toolName]; !ok {
			t.Fatalf("expected tool %s to be registered", toolName)
		}
	}
}

func TestToolsListIncludesStructuredContentAndTimeFilters(t *testing.T) {
	server := NewServer(8081, &mockWorkStorage{})
	response := server.handleToolsList(&MCPMessage{JSONRPC: "2.0", ID: "tools-1", Method: "tools/list"})
	if response.Error != nil {
		t.Fatalf("expected no error, got %+v", response.Error)
	}

	result := response.Result.(map[string]interface{})
	tools := result["tools"].([]Tool)
	var addWorkEntry, getTaskEntries Tool
	for _, tool := range tools {
		if tool.Name == "add_work_entry" {
			addWorkEntry = tool
		}
		if tool.Name == "get_task_entries" {
			getTaskEntries = tool
		}
	}

	addSchemaJSON, _ := json.Marshal(addWorkEntry.InputSchema)
	if !json.Valid(addSchemaJSON) || !containsString(string(addSchemaJSON), "content") {
		t.Fatalf("expected add_work_entry schema to include content, got %s", addSchemaJSON)
	}
	getSchemaJSON, _ := json.Marshal(getTaskEntries.InputSchema)
	if !containsString(string(getSchemaJSON), "start_time") || !containsString(string(getSchemaJSON), "end_time") {
		t.Fatalf("expected get_task_entries schema to include time filters, got %s", getSchemaJSON)
	}
}

func TestHandleAddWorkEntryAcceptsStructuredContent(t *testing.T) {
	store := &mockWorkStorage{}
	server := NewServer(8081, store)

	result, err := server.handleAddWorkEntry(context.Background(), map[string]interface{}{
		"orchestrator_task_id": "KOD-123",
		"agent_task_id":        "KOD-124",
		"agent_id":             "codex",
		"git_branch":           "feature/structured-content",
		"text":                 "Implemented structured content",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "Readable details"},
			map[string]interface{}{"type": "console", "title": "Verification", "command": "go test ./pkg/mcp", "exit_code": 0, "text": "ok pkg/mcp"},
			map[string]interface{}{"type": "code", "language": "go", "text": "type WorkEntryContentBlock struct {}"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Content[0].Text != "OK" {
		t.Fatalf("expected OK result, got %+v", result)
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.entries))
	}
	if len(store.entries[0].Content) != 3 || store.entries[0].Content[1].Command != "go test ./pkg/mcp" {
		t.Fatalf("unexpected structured content: %+v", store.entries[0].Content)
	}
}

func TestHandleGetTaskEntriesUsesTimeFilters(t *testing.T) {
	now := time.Now().UTC()
	store := &mockWorkStorage{entries: []models.WorkEntry{
		{
			ID:                 "550e8400-e29b-41d4-a716-446655440000",
			OrchestratorTaskID: "KOD-123",
			AgentTaskID:        "KOD-124",
			Text:               "old",
			AgentID:            "codex",
			GitBranch:          "old-branch",
			Timestamp:          now.Add(-48 * time.Hour),
		},
		{
			ID:                 "550e8400-e29b-41d4-a716-446655440001",
			OrchestratorTaskID: "KOD-123",
			AgentTaskID:        "KOD-124",
			Text:               "recent",
			AgentID:            "codex",
			GitBranch:          "recent-branch",
			Timestamp:          now,
		},
	}}
	server := NewServer(8081, store)

	result, err := server.handleGetTaskEntries(context.Background(), map[string]interface{}{
		"orchestrator_task_id": "KOD-123",
		"agent_task_id":        "KOD-124",
		"start_time":           now.Add(-24 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var response models.WorkEntryResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.TotalCount != 1 || response.Entries[0].Text != "recent" {
		t.Fatalf("unexpected filtered response: %+v", response)
	}
}

func TestHandleMessageProtocolErrors(t *testing.T) {
	server := NewServer(8081, &mockWorkStorage{})

	unknownMethod := server.handleMessage(context.Background(), &MCPMessage{
		JSONRPC: "2.0",
		ID:      "unknown-method",
		Method:  "unknown_method",
	})
	if unknownMethod.Error == nil || unknownMethod.Error.Code != -32601 {
		t.Fatalf("expected method error, got %+v", unknownMethod)
	}

	unknownTool := server.handleMessage(context.Background(), &MCPMessage{
		JSONRPC: "2.0",
		ID:      "unknown-tool",
		Method:  "tools/call",
		Params:  map[string]interface{}{"name": "unknown_tool"},
	})
	if unknownTool.Error == nil || unknownTool.Error.Message != "Tool not found" {
		t.Fatalf("expected tool error, got %+v", unknownTool)
	}
}

func containsString(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
