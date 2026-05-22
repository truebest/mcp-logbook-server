package ingestion

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerDoesNotExposeLegacyWriteEndpoints(t *testing.T) {
	router := newTestRouter(&mockWorkStorage{})

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "old single log ingest endpoint",
			method: http.MethodPost,
			path:   "/v1/logs",
		},
		{
			name:   "old batch log ingest endpoint",
			method: http.MethodPost,
			path:   "/v1/logs/batch",
		},
		{
			name:   "work entry HTTP write endpoint",
			method: http.MethodPost,
			path:   "/v1/tasks/KOD-123/agent-tasks/KOD-124/entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
			}
		})
	}
}
