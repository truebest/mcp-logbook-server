package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/truebest/mcp-logbook-server/pkg/models"
)

type mockWorkStorage struct {
	healthStatus models.HealthStatus
	tasks        []models.TaskInfo
	entries      []models.WorkEntry
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

	return &models.WorkEntryResult{
		Entries:    entries,
		TotalCount: len(entries),
		HasMore:    false,
	}, nil
}

func (m *mockWorkStorage) ListTasks(ctx context.Context, filter models.WorkEntryFilter) ([]models.TaskInfo, error) {
	return m.tasks, nil
}

func (m *mockWorkStorage) DeleteTaskEntries(ctx context.Context, orchestratorTaskID, agentTaskID string) (int64, error) {
	var kept []models.WorkEntry
	var deleted int64
	for _, entry := range m.entries {
		if entry.OrchestratorTaskID == orchestratorTaskID && entry.AgentTaskID == agentTaskID {
			deleted++
			continue
		}
		kept = append(kept, entry)
	}
	m.entries = kept
	return deleted, nil
}

func (m *mockWorkStorage) DeleteParentTaskEntries(ctx context.Context, orchestratorTaskID string) (int64, error) {
	var kept []models.WorkEntry
	var deleted int64
	for _, entry := range m.entries {
		if entry.OrchestratorTaskID == orchestratorTaskID {
			deleted++
			continue
		}
		kept = append(kept, entry)
	}
	m.entries = kept
	return deleted, nil
}

func (m *mockWorkStorage) HealthCheck(ctx context.Context) models.HealthStatus {
	return m.healthStatus
}

func (m *mockWorkStorage) Close() error {
	return nil
}

func newTestRouter(store *mockWorkStorage) *gin.Engine {
	gin.SetMode(gin.TestMode)
	server := NewServer(8080, store)
	router := gin.New()
	server.registerRoutes(router)
	return router
}

func TestServerHandleIndex(t *testing.T) {
	router := newTestRouter(&mockWorkStorage{})

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected html content type, got %q", contentType)
	}
	if !strings.Contains(w.Body.String(), "MCP Work Logbook") {
		t.Fatalf("expected UI shell in response body")
	}
	if !strings.Contains(w.Body.String(), "parent-task") {
		t.Fatalf("expected tree UI in response body")
	}
	if !strings.Contains(w.Body.String(), "summary-link") {
		t.Fatalf("expected clickable parent summary links in response body")
	}
	if !strings.Contains(w.Body.String(), "time-filter") {
		t.Fatalf("expected time filter in response body")
	}
	if !strings.Contains(w.Body.String(), "entry-block") {
		t.Fatalf("expected structured entry block rendering in response body")
	}
	if !strings.Contains(w.Body.String(), "color-scheme: light dark") {
		t.Fatalf("expected browser-driven color scheme support in response body")
	}
	if !strings.Contains(w.Body.String(), "prefers-color-scheme: dark") {
		t.Fatalf("expected dark theme media query in response body")
	}
}

func TestServerHandleHealthCheck(t *testing.T) {
	router := newTestRouter(&mockWorkStorage{
		healthStatus: models.HealthStatus{
			Status:    "healthy",
			Timestamp: time.Now().UTC(),
			Details:   map[string]string{"entry_count": "1"},
		},
	})

	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response["service"] != "mcp-logbook-server" {
		t.Fatalf("expected mcp-logbook-server service, got %v", response["service"])
	}
}

func TestServerHandleListTasks(t *testing.T) {
	now := time.Now().UTC()
	router := newTestRouter(&mockWorkStorage{
		tasks: []models.TaskInfo{
			{
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-124",
				EntryCount:         2,
				LastEntryAt:        now,
				Agents:             []string{"codex"},
				GitBranches:        []string{"feature/logbook-ui"},
			},
		},
	})

	req, err := http.NewRequest(http.MethodGet, "/v1/tasks", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response struct {
		Tasks []models.TaskInfo `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(response.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(response.Tasks))
	}
	if response.Tasks[0].OrchestratorTaskID != "KOD-123" || response.Tasks[0].AgentTaskID != "KOD-124" {
		t.Fatalf("unexpected task response: %+v", response.Tasks[0])
	}
	if len(response.Tasks[0].GitBranches) != 1 || response.Tasks[0].GitBranches[0] != "feature/logbook-ui" {
		t.Fatalf("unexpected git branches response: %+v", response.Tasks[0].GitBranches)
	}
}

func TestServerHandleGetTaskEntries(t *testing.T) {
	now := time.Now().UTC()
	router := newTestRouter(&mockWorkStorage{
		entries: []models.WorkEntry{
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440000",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-124",
				Text:               "Implemented browser UI",
				Content: []models.WorkEntryContentBlock{
					{Type: "console", Text: "ok pkg", Command: "go test ./pkg/ingestion"},
				},
				AgentID:   "codex",
				GitBranch: "feature/logbook-ui",
				Timestamp: now,
			},
		},
	})

	req, err := http.NewRequest(http.MethodGet, "/v1/tasks/KOD-123/agent-tasks/KOD-124/entries", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response models.WorkEntryResult
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.TotalCount != 1 {
		t.Fatalf("expected 1 entry, got %d", response.TotalCount)
	}
	if response.Entries[0].Text != "Implemented browser UI" {
		t.Fatalf("unexpected entry response: %+v", response.Entries[0])
	}
	if response.Entries[0].GitBranch != "feature/logbook-ui" {
		t.Fatalf("unexpected git branch response: %+v", response.Entries[0])
	}
	if len(response.Entries[0].Content) != 1 || response.Entries[0].Content[0].Type != "console" {
		t.Fatalf("unexpected structured content response: %+v", response.Entries[0].Content)
	}

	req, err = http.NewRequest(http.MethodGet, "/v1/tasks/KOD-123/agent-tasks/KOD-124/entries?start_time="+now.Add(time.Hour).Format(time.RFC3339), nil)
	if err != nil {
		t.Fatalf("failed to create time-filtered request: %v", err)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode time-filtered response: %v", err)
	}
	if response.TotalCount != 0 {
		t.Fatalf("expected 0 entries after time filter, got %d", response.TotalCount)
	}
}

func TestServerHandleGetParentTaskEntries(t *testing.T) {
	now := time.Now().UTC()
	router := newTestRouter(&mockWorkStorage{
		entries: []models.WorkEntry{
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440000",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-124",
				Text:               "Child one",
				AgentID:            "codex",
				GitBranch:          "feature/one",
				Timestamp:          now,
			},
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440001",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-125",
				Text:               "Child two",
				AgentID:            "codex",
				GitBranch:          "feature/two",
				Timestamp:          now,
			},
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440002",
				OrchestratorTaskID: "KOD-999",
				AgentTaskID:        "KOD-999",
				Text:               "Other parent",
				AgentID:            "codex",
				GitBranch:          "feature/other",
				Timestamp:          now,
			},
		},
	})

	req, err := http.NewRequest(http.MethodGet, "/v1/tasks/KOD-123/entries", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response models.WorkEntryResult
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.TotalCount != 2 {
		t.Fatalf("expected 2 parent entries, got %d", response.TotalCount)
	}
}

func TestServerHandleDeleteTaskEntries(t *testing.T) {
	store := &mockWorkStorage{
		entries: []models.WorkEntry{
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440000",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-124",
				Text:               "Delete me",
				AgentID:            "codex",
				GitBranch:          "feature/delete",
				Timestamp:          time.Now().UTC(),
			},
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440001",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-125",
				Text:               "Keep me",
				AgentID:            "codex",
				GitBranch:          "feature/keep",
				Timestamp:          time.Now().UTC(),
			},
		},
	}
	router := newTestRouter(store)

	req, err := http.NewRequest(http.MethodDelete, "/v1/tasks/KOD-123/agent-tasks/KOD-124/entries", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response struct {
		DeletedCount int64 `json:"deleted_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.DeletedCount != 1 {
		t.Fatalf("expected 1 deleted entry, got %d", response.DeletedCount)
	}
	if len(store.entries) != 1 || store.entries[0].AgentTaskID != "KOD-125" {
		t.Fatalf("unexpected remaining entries: %+v", store.entries)
	}
}

func TestServerHandleDeleteParentTaskEntries(t *testing.T) {
	store := &mockWorkStorage{
		entries: []models.WorkEntry{
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440000",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-124",
				Text:               "Delete me",
				AgentID:            "codex",
				GitBranch:          "feature/delete",
				Timestamp:          time.Now().UTC(),
			},
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440001",
				OrchestratorTaskID: "KOD-123",
				AgentTaskID:        "KOD-125",
				Text:               "Delete me too",
				AgentID:            "codex",
				GitBranch:          "feature/delete",
				Timestamp:          time.Now().UTC(),
			},
			{
				ID:                 "550e8400-e29b-41d4-a716-446655440002",
				OrchestratorTaskID: "KOD-999",
				AgentTaskID:        "KOD-999",
				Text:               "Keep me",
				AgentID:            "codex",
				GitBranch:          "feature/keep",
				Timestamp:          time.Now().UTC(),
			},
		},
	}
	router := newTestRouter(store)

	req, err := http.NewRequest(http.MethodDelete, "/v1/tasks/KOD-123/entries", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response struct {
		DeletedCount int64 `json:"deleted_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.DeletedCount != 2 {
		t.Fatalf("expected 2 deleted entries, got %d", response.DeletedCount)
	}
	if len(store.entries) != 1 || store.entries[0].OrchestratorTaskID != "KOD-999" {
		t.Fatalf("unexpected remaining entries: %+v", store.entries)
	}
}

func TestServerRejectsHTTPWritePath(t *testing.T) {
	router := newTestRouter(&mockWorkStorage{})

	req, err := http.NewRequest(http.MethodPost, "/v1/tasks/KOD-123/agent-tasks/KOD-124/entries", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}
