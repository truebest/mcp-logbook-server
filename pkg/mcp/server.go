package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/truebest/mcp-logbook-server/pkg/models"
	"github.com/truebest/mcp-logbook-server/pkg/storage"
)

// MCPMessage represents a JSON-RPC MCP message.
type MCPMessage struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC error response.
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ContentBlock represents a content block in MCP responses.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult represents an MCP tool result.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// Server represents the MCP server.
type Server struct {
	port    int
	storage storage.WorkStorage
	tools   map[string]Tool
	server  *http.Server
}

// NewServer creates a new MCP server.
func NewServer(port int, store storage.WorkStorage) *Server {
	s := &Server{
		port:    port,
		storage: store,
		tools:   make(map[string]Tool),
	}
	s.registerTools()
	return s
}

func (s *Server) registerTools() {
	s.tools["add_work_entry"] = Tool{
		Name:        "add_work_entry",
		Description: "Add one agent work-log entry for an orchestrator task and agent task",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"orchestrator_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Orchestrator task ID, for example KOD-123",
				},
				"agent_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent task ID, for example KOD-124",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Plain summary/fallback text. Either text or content must be non-empty.",
				},
				"content": map[string]interface{}{
					"type":        "array",
					"description": "Optional structured content blocks for readable text, console output, and code snippets.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"type": map[string]interface{}{
								"type":        "string",
								"enum":        []string{"text", "console", "code"},
								"description": "Block type.",
							},
							"text": map[string]interface{}{
								"type":        "string",
								"description": "Block body.",
							},
							"title": map[string]interface{}{
								"type":        "string",
								"description": "Optional block title.",
							},
							"language": map[string]interface{}{
								"type":        "string",
								"description": "Optional code language for code blocks.",
							},
							"command": map[string]interface{}{
								"type":        "string",
								"description": "Optional command for console blocks.",
							},
							"exit_code": map[string]interface{}{
								"type":        "integer",
								"description": "Optional command exit code for console blocks.",
							},
						},
						"required": []string{"type", "text"},
					},
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID",
				},
				"git_branch": map[string]interface{}{
					"type":        "string",
					"description": "Git branch where the agent is working. Use NA if unavailable.",
				},
				"timestamp": map[string]interface{}{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 timestamp. Current UTC time is used when omitted.",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Optional structured metadata",
				},
			},
			"required": []string{"orchestrator_task_id", "agent_task_id", "agent_id", "git_branch"},
		},
	}

	s.tools["get_task_entries"] = Tool{
		Name:        "get_task_entries",
		Description: "Get work-log entries for an orchestrator task and agent task",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"orchestrator_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Orchestrator task ID, for example KOD-123",
				},
				"agent_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent task ID, for example KOD-124",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional agent ID filter",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"default":     100,
					"minimum":     1,
					"maximum":     1000,
					"description": "Maximum number of entries to return",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"default":     0,
					"minimum":     0,
					"description": "Number of entries to skip",
				},
				"start_time": map[string]interface{}{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 lower timestamp bound.",
				},
				"end_time": map[string]interface{}{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 upper timestamp bound.",
				},
			},
			"required": []string{"orchestrator_task_id", "agent_task_id"},
		},
	}

	s.tools["list_tasks"] = Tool{
		Name:        "list_tasks",
		Description: "List orchestrator task and agent task pairs that have work-log entries",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"default":     100,
					"minimum":     1,
					"maximum":     1000,
					"description": "Maximum number of tasks to return",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"default":     0,
					"minimum":     0,
					"description": "Number of tasks to skip",
				},
				"start_time": map[string]interface{}{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 lower timestamp bound.",
				},
				"end_time": map[string]interface{}{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 upper timestamp bound.",
				},
			},
		},
	}

	s.tools["get_service_status"] = Tool{
		Name:        "get_service_status",
		Description: "Get health status of the work-logbook service",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

// Start starts the MCP server.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcp", s.handleHTTP)

	s.server = &http.Server{
		Addr:         ":" + strconv.Itoa(s.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("MCP streamable HTTP server listening on port %d at /v1/mcp", s.port)
		errCh <- s.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	setMCPHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method == http.MethodGet {
		http.Error(w, "server-to-client streaming is not supported", http.StatusMethodNotAllowed)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	defer r.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSONRPCError(w, nil, -32700, "Parse error")
		return
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		s.handleHTTPBatch(w, r.Context(), raw)
		return
	}

	var msg MCPMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		writeJSONRPCError(w, nil, -32600, "Invalid request")
		return
	}

	response := s.handleMessage(r.Context(), &msg)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	writeJSON(w, response)
}

func (s *Server) handleHTTPBatch(w http.ResponseWriter, ctx context.Context, raw json.RawMessage) {
	var messages []MCPMessage
	if err := json.Unmarshal(raw, &messages); err != nil || len(messages) == 0 {
		writeJSONRPCError(w, nil, -32600, "Invalid request")
		return
	}

	responses := make([]*MCPMessage, 0, len(messages))
	for i := range messages {
		response := s.handleMessage(ctx, &messages[i])
		if response != nil {
			responses = append(responses, response)
		}
	}

	if len(responses) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	writeJSON(w, responses)
}

func (s *Server) handleMessage(ctx context.Context, msg *MCPMessage) *MCPMessage {
	if msg.ID == nil && msg.Method != "" {
		return nil
	}

	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "tools/list":
		return s.handleToolsList(msg)
	case "tools/call":
		return s.handleToolCall(ctx, msg)
	default:
		return &MCPMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func setMCPHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, MCP-Protocol-Version, Mcp-Protocol-Version, Mcp-Session-Id")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("MCP-Protocol-Version", "2024-11-05")
}

func writeJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Failed to encode HTTP MCP response: %v", err)
	}
}

func writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	writeJSON(w, &MCPMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) handleInitialize(msg *MCPMessage) *MCPMessage {
	return &MCPMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mcp-logbook-server",
				"version": "1.0.0",
			},
		},
	}
}

func (s *Server) handleToolsList(msg *MCPMessage) *MCPMessage {
	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}

	return &MCPMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (s *Server) handleToolCall(ctx context.Context, msg *MCPMessage) *MCPMessage {
	params, ok := msg.Params.(map[string]interface{})
	if !ok {
		return toolError(msg.ID, "Invalid params")
	}

	toolName, ok := params["name"].(string)
	if !ok {
		return toolError(msg.ID, "Missing tool name")
	}

	var result *ToolResult
	var err error
	arguments := params["arguments"]

	switch toolName {
	case "add_work_entry":
		result, err = s.handleAddWorkEntry(ctx, arguments)
	case "get_task_entries":
		result, err = s.handleGetTaskEntries(ctx, arguments)
	case "list_tasks":
		result, err = s.handleListTasks(ctx, arguments)
	case "get_service_status":
		result, err = s.handleGetServiceStatus(ctx)
	default:
		return toolError(msg.ID, "Tool not found")
	}

	if err != nil {
		return &MCPMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result: &ToolResult{
				Content: []ContentBlock{
					{Type: "text", Text: "ERROR: " + err.Error()},
				},
				IsError: true,
			},
		}
	}

	return &MCPMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  result,
	}
}

func (s *Server) handleAddWorkEntry(ctx context.Context, arguments interface{}) (*ToolResult, error) {
	entry, err := workEntryFromArguments(arguments)
	if err != nil {
		return nil, err
	}

	if err := s.storage.StoreWorkEntries(ctx, []models.WorkEntry{entry}); err != nil {
		return nil, fmt.Errorf("failed to store work entry: %w", err)
	}

	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: "OK"},
		},
	}, nil
}

func (s *Server) handleGetTaskEntries(ctx context.Context, arguments interface{}) (*ToolResult, error) {
	args, ok := arguments.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid arguments")
	}

	orchestratorTaskID := stringArg(args, "orchestrator_task_id")
	if orchestratorTaskID == "" {
		return nil, fmt.Errorf("missing orchestrator_task_id")
	}

	agentTaskID := stringArg(args, "agent_task_id")
	if agentTaskID == "" {
		return nil, fmt.Errorf("missing agent_task_id")
	}

	filter := models.WorkEntryFilter{
		OrchestratorTaskID: orchestratorTaskID,
		AgentTaskID:        agentTaskID,
		AgentID:            stringArg(args, "agent_id"),
		Limit:              intArg(args, "limit", 100),
		Offset:             intArg(args, "offset", 0),
	}
	if err := applyTimeArgs(args, &filter); err != nil {
		return nil, err
	}

	result, err := s.storage.QueryWorkEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query task entries: %w", err)
	}

	return jsonToolResult(result)
}

func (s *Server) handleListTasks(ctx context.Context, arguments interface{}) (*ToolResult, error) {
	args, _ := arguments.(map[string]interface{})
	filter := models.WorkEntryFilter{
		Limit:  intArg(args, "limit", 100),
		Offset: intArg(args, "offset", 0),
	}
	if err := applyTimeArgs(args, &filter); err != nil {
		return nil, err
	}

	tasks, err := s.storage.ListTasks(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	return jsonToolResult(map[string]interface{}{
		"tasks": tasks,
	})
}

func (s *Server) handleGetServiceStatus(ctx context.Context) (*ToolResult, error) {
	storageStatus := s.storage.HealthCheck(ctx)
	return jsonToolResult(map[string]interface{}{
		"overall_status": storageStatus.Status,
		"timestamp":      time.Now().UTC(),
		"components": map[string]interface{}{
			"storage": storageStatus,
			"mcp_server": map[string]interface{}{
				"status":      "healthy",
				"port":        s.port,
				"tools_count": len(s.tools),
				"tools":       s.toolNames(),
			},
		},
	})
}

func workEntryFromArguments(arguments interface{}) (models.WorkEntry, error) {
	var entry models.WorkEntry
	data, err := json.Marshal(arguments)
	if err != nil {
		return entry, fmt.Errorf("failed to marshal work entry arguments: %w", err)
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return entry, fmt.Errorf("invalid work entry arguments: %w", err)
	}

	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(entry.GitBranch) == "" {
		entry.GitBranch = "NA"
	}
	if err := entry.Validate(); err != nil {
		return entry, fmt.Errorf("work entry validation failed: %w", err)
	}

	return entry, nil
}

func jsonToolResult(value interface{}) (*ToolResult, error) {
	resultJSON, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return &ToolResult{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

func toolError(id interface{}, message string) *MCPMessage {
	return &MCPMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    -32602,
			Message: message,
		},
	}
}

func intArg(args map[string]interface{}, key string, defaultValue int) int {
	if args == nil {
		return defaultValue
	}

	raw, ok := args[key]
	if !ok {
		return defaultValue
	}

	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return defaultValue
	}
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}

	value, _ := args[key].(string)
	return value
}

func applyTimeArgs(args map[string]interface{}, filter *models.WorkEntryFilter) error {
	startTime, err := timeArg(args, "start_time")
	if err != nil {
		return err
	}
	endTime, err := timeArg(args, "end_time")
	if err != nil {
		return err
	}
	filter.StartTime = startTime
	filter.EndTime = endTime
	return nil
}

func timeArg(args map[string]interface{}, key string) (time.Time, error) {
	raw := stringArg(args, key)
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: must be RFC3339", key)
	}
	return value, nil
}

func (s *Server) toolNames() []string {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	return names
}
