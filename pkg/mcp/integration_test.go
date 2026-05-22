package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/truebest/mcp-logbook-server/pkg/models"
)

func TestMCPWorkLogbookWorkflow(t *testing.T) {
	store := &mockWorkStorage{}
	server := NewServer(8081, store)
	ctx := context.Background()

	initResponse := server.handleMessage(ctx, &MCPMessage{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
	})
	if initResponse.Error != nil {
		t.Fatalf("initialize failed: %+v", initResponse.Error)
	}

	toolsResponse := server.handleMessage(ctx, &MCPMessage{
		JSONRPC: "2.0",
		ID:      "tools-1",
		Method:  "tools/list",
	})
	if toolsResponse.Error != nil {
		t.Fatalf("tools/list failed: %+v", toolsResponse.Error)
	}

	now := time.Now().UTC()
	addResponse := server.handleMessage(ctx, &MCPMessage{
		JSONRPC: "2.0",
		ID:      "add-1",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "add_work_entry",
			"arguments": map[string]interface{}{
				"orchestrator_task_id": "KOD-494",
				"agent_task_id":        "KOD-569",
				"agent_id":             "codex",
				"git_branch":           "feature/work-logbook-content",
				"timestamp":            now.Format(time.RFC3339),
				"text":                 "Added structured content support",
				"content": []interface{}{
					map[string]interface{}{"type": "console", "command": "go test ./pkg/mcp", "exit_code": 0, "text": "ok pkg/mcp"},
					map[string]interface{}{"type": "code", "language": "go", "text": "type WorkEntryContentBlock struct {}"},
				},
			},
		},
	})
	if addResponse.Error != nil {
		t.Fatalf("add_work_entry failed: %+v", addResponse.Error)
	}
	addResult := addResponse.Result.(*ToolResult)
	if addResult.IsError {
		t.Fatalf("add_work_entry returned tool error: %+v", addResult)
	}

	getResponse := server.handleMessage(ctx, &MCPMessage{
		JSONRPC: "2.0",
		ID:      "get-1",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "get_task_entries",
			"arguments": map[string]interface{}{
				"orchestrator_task_id": "KOD-494",
				"agent_task_id":        "KOD-569",
				"start_time":           now.Add(-time.Minute).Format(time.RFC3339),
			},
		},
	})
	if getResponse.Error != nil {
		t.Fatalf("get_task_entries failed: %+v", getResponse.Error)
	}
	getResult := getResponse.Result.(*ToolResult)
	var entries models.WorkEntryResult
	if err := json.Unmarshal([]byte(getResult.Content[0].Text), &entries); err != nil {
		t.Fatalf("failed to decode get_task_entries response: %v", err)
	}
	if entries.TotalCount != 1 {
		t.Fatalf("expected 1 entry, got %+v", entries)
	}
	if len(entries.Entries[0].Content) != 2 {
		t.Fatalf("expected structured content to round trip, got %+v", entries.Entries[0].Content)
	}

	listResponse := server.handleMessage(ctx, &MCPMessage{
		JSONRPC: "2.0",
		ID:      "list-1",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "list_tasks",
			"arguments": map[string]interface{}{
				"start_time": now.Add(-time.Minute).Format(time.RFC3339),
			},
		},
	})
	if listResponse.Error != nil {
		t.Fatalf("list_tasks failed: %+v", listResponse.Error)
	}
	listResult := listResponse.Result.(*ToolResult)
	var tasksResponse struct {
		Tasks []models.TaskInfo `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(listResult.Content[0].Text), &tasksResponse); err != nil {
		t.Fatalf("failed to decode list_tasks response: %v", err)
	}
	if len(tasksResponse.Tasks) != 1 || tasksResponse.Tasks[0].AgentTaskID != "KOD-569" {
		t.Fatalf("unexpected task list: %+v", tasksResponse.Tasks)
	}
}
