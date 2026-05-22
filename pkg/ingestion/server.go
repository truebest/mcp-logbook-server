package ingestion

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/truebest/mcp-logbook-server/pkg/models"
	"github.com/truebest/mcp-logbook-server/pkg/storage"
)

// Server exposes the HTTP API for agent work-log entries.
type Server struct {
	port    int
	storage storage.WorkStorage
	server  *http.Server
}

// NewServer creates a new HTTP ingestion server.
func NewServer(port int, store storage.WorkStorage) *Server {
	return &Server{
		port:    port,
		storage: store,
	}
}

// Start starts the HTTP server and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	s.registerRoutes(router)

	s.server = &http.Server{
		Addr:         ":" + strconv.Itoa(s.port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
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

func (s *Server) registerRoutes(router *gin.Engine) {
	router.GET("/", s.handleIndex)
	router.GET("/health", s.handleHealthCheck)
	router.GET("/v1/tasks/:orchestrator_task_id/entries", s.handleGetParentTaskEntries)
	router.DELETE("/v1/tasks/:orchestrator_task_id/entries", s.handleDeleteParentTaskEntries)
	router.GET("/v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries", s.handleGetTaskEntries)
	router.DELETE("/v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries", s.handleDeleteTaskEntries)
	router.GET("/v1/tasks", s.handleListTasks)
}

func (s *Server) handleIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(indexHTML))
}

func (s *Server) handleHealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	storageStatus := s.storage.HealthCheck(ctx)
	statusCode := http.StatusOK
	if storageStatus.Status != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, gin.H{
		"status":    storageStatus.Status,
		"timestamp": time.Now().UTC(),
		"service":   "mcp-logbook-server",
		"storage":   storageStatus,
	})
}

func (s *Server) handleGetTaskEntries(c *gin.Context) {
	startTime, endTime, ok := parseTimeQueries(c)
	if !ok {
		return
	}

	filter := models.WorkEntryFilter{
		OrchestratorTaskID: c.Param("orchestrator_task_id"),
		AgentTaskID:        c.Param("agent_task_id"),
		Limit:              parseIntQuery(c, "limit", 100),
		Offset:             parseIntQuery(c, "offset", 0),
		AgentID:            c.Query("agent_id"),
		StartTime:          startTime,
		EndTime:            endTime,
	}

	result, err := s.storage.QueryWorkEntries(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "QUERY_ERROR",
				"message": "Failed to query work entries",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleGetParentTaskEntries(c *gin.Context) {
	startTime, endTime, ok := parseTimeQueries(c)
	if !ok {
		return
	}

	filter := models.WorkEntryFilter{
		OrchestratorTaskID: c.Param("orchestrator_task_id"),
		Limit:              parseIntQuery(c, "limit", 100),
		Offset:             parseIntQuery(c, "offset", 0),
		AgentID:            c.Query("agent_id"),
		StartTime:          startTime,
		EndTime:            endTime,
	}

	result, err := s.storage.QueryWorkEntries(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "QUERY_ERROR",
				"message": "Failed to query orchestrator task entries",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleDeleteTaskEntries(c *gin.Context) {
	orchestratorTaskID := c.Param("orchestrator_task_id")
	agentTaskID := c.Param("agent_task_id")

	deleter, ok := s.storage.(interface {
		DeleteTaskEntries(ctx context.Context, orchestratorTaskID, agentTaskID string) (int64, error)
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_UNSUPPORTED",
				"message": "Storage does not support deleting work entries",
			},
		})
		return
	}

	deleted, err := deleter.DeleteTaskEntries(c.Request.Context(), orchestratorTaskID, agentTaskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": "Failed to delete work entries",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted_count": deleted,
	})
}

func (s *Server) handleDeleteParentTaskEntries(c *gin.Context) {
	orchestratorTaskID := c.Param("orchestrator_task_id")

	deleter, ok := s.storage.(interface {
		DeleteParentTaskEntries(ctx context.Context, orchestratorTaskID string) (int64, error)
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_UNSUPPORTED",
				"message": "Storage does not support deleting orchestrator task entries",
			},
		})
		return
	}

	deleted, err := deleter.DeleteParentTaskEntries(c.Request.Context(), orchestratorTaskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": "Failed to delete orchestrator task entries",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted_count": deleted,
	})
}

func (s *Server) handleListTasks(c *gin.Context) {
	startTime, endTime, ok := parseTimeQueries(c)
	if !ok {
		return
	}

	tasks, err := s.storage.ListTasks(c.Request.Context(), models.WorkEntryFilter{
		Limit:     parseIntQuery(c, "limit", 100),
		Offset:    parseIntQuery(c, "offset", 0),
		StartTime: startTime,
		EndTime:   endTime,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "QUERY_ERROR",
				"message": "Failed to list tasks",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
	})
}

func parseIntQuery(c *gin.Context, key string, defaultValue int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}

	return value
}

func parseTimeQueries(c *gin.Context) (time.Time, time.Time, bool) {
	startTime, ok := parseTimeQuery(c, "start_time")
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	endTime, ok := parseTimeQuery(c, "end_time")
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	return startTime, endTime, true
}

func parseTimeQuery(c *gin.Context, key string) (time.Time, bool) {
	raw := c.Query(key)
	if raw == "" {
		return time.Time{}, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_TIME_FILTER",
				"message": key + " must be an RFC3339 timestamp",
			},
		})
		return time.Time{}, false
	}
	return value, true
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MCP Work Logbook</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --panel-soft: #f0f4f7;
      --child-panel: #fbfcfd;
      --active-panel: #e8f3f1;
      --line: #d8dee6;
      --text: #1f2933;
      --muted: #657386;
      --accent: #0f766e;
      --accent-strong: #0b5f59;
      --danger: #b42318;
      --danger-border: #efb6af;
      --danger-soft: #fff1f0;
      --control-bg: #ffffff;
      --control-hover-bg: #f8fafc;
      --control-hover-border: #aab7c4;
      --disabled-text: #9aa6b2;
      --disabled-bg: #f4f6f8;
      --status-idle: #94a3b8;
      --code-bg: #111827;
      --code-text: #e5e7eb;
      --shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0f1419;
        --panel: #171d24;
        --panel-soft: #202833;
        --child-panel: #131a21;
        --active-panel: #12332f;
        --line: #2d3744;
        --text: #e6edf3;
        --muted: #9aa8b8;
        --accent: #2dd4bf;
        --accent-strong: #5eead4;
        --danger: #f87171;
        --danger-border: #7f2d2d;
        --danger-soft: #331818;
        --control-bg: #10161d;
        --control-hover-bg: #1d2630;
        --control-hover-border: #536273;
        --disabled-text: #687586;
        --disabled-bg: #141b23;
        --status-idle: #64748b;
        --code-bg: #0b1016;
        --code-text: #dbe7f3;
        --shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    button, input, select {
      font: inherit;
    }
    .app {
      min-height: 100vh;
      display: grid;
      grid-template-rows: auto 1fr;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 14px 18px;
      background: var(--panel);
      border-bottom: 1px solid var(--line);
    }
    h1 {
      margin: 0;
      font-size: 18px;
      font-weight: 650;
    }
    .header-meta {
      display: flex;
      align-items: center;
      gap: 10px;
      color: var(--muted);
      white-space: nowrap;
    }
    .status-dot {
      width: 9px;
      height: 9px;
      border-radius: 50%;
      background: var(--status-idle);
      display: inline-block;
    }
    .status-dot.ok { background: var(--accent); }
    .status-dot.error { background: var(--danger); }
    .layout {
      display: grid;
      grid-template-columns: minmax(300px, 380px) 1fr;
      min-height: 0;
    }
    aside {
      min-width: 0;
      background: var(--panel);
      border-right: 1px solid var(--line);
      display: grid;
      grid-template-rows: auto 1fr;
    }
    .toolbar {
      padding: 12px;
      border-bottom: 1px solid var(--line);
      display: grid;
      grid-template-columns: 1fr auto auto;
      gap: 8px;
    }
    input[type="search"], select {
      width: 100%;
      min-width: 0;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 8px 10px;
      color: var(--text);
      background: var(--control-bg);
    }
    select {
      width: auto;
      max-width: 150px;
    }
    button {
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--control-bg);
      color: var(--text);
      padding: 8px 10px;
      cursor: pointer;
      min-height: 36px;
    }
    button:hover {
      border-color: var(--control-hover-border);
      background: var(--control-hover-bg);
    }
    .task-list {
      overflow: auto;
      min-height: 0;
    }
    .task {
      width: 100%;
      display: block;
      text-align: left;
      border: 0;
      border-bottom: 1px solid var(--line);
      border-radius: 0;
      padding: 12px;
      background: transparent;
    }
    .task.parent-task {
      font-weight: 600;
    }
    .task.child-task {
      padding-left: 28px;
      background: var(--child-panel);
    }
    .task.child-task .task-title {
      font-weight: 600;
    }
    .task:hover {
      background: var(--panel-soft);
    }
    .task.active {
      background: var(--active-panel);
      box-shadow: inset 3px 0 0 var(--accent);
    }
    .task-caret {
      display: inline-block;
      min-width: 16px;
      color: var(--muted);
    }
    .task-title {
      display: flex;
      justify-content: space-between;
      gap: 10px;
      font-weight: 650;
      margin-bottom: 5px;
    }
    .task-sub {
      color: var(--muted);
      font-size: 12px;
      word-break: break-word;
    }
    main {
      min-width: 0;
      display: grid;
      grid-template-rows: auto 1fr;
    }
    .details-head {
      padding: 14px 18px;
      border-bottom: 1px solid var(--line);
      background: var(--panel);
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 14px;
    }
    .details-title {
      margin: 0 0 4px;
      font-size: 16px;
      font-weight: 650;
      word-break: break-word;
    }
    .details-sub {
      color: var(--muted);
      font-size: 12px;
    }
    .detail-actions {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
      gap: 8px;
    }
    .detail-actions button:disabled {
      color: var(--disabled-text);
      cursor: not-allowed;
      background: var(--disabled-bg);
    }
    .danger-button {
      color: var(--danger);
      border-color: var(--danger-border);
    }
    .danger-button:hover:not(:disabled) {
      background: var(--danger-soft);
      border-color: var(--danger);
    }
    .entries {
      overflow: auto;
      padding: 16px 18px 28px;
    }
    .entry {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
      margin-bottom: 12px;
      padding: 13px 14px;
    }
    .entry-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px 12px;
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 8px;
    }
    .entry-agent {
      color: var(--accent-strong);
      font-weight: 650;
    }
    .entry-text {
      white-space: pre-wrap;
      word-break: break-word;
    }
    .entry-block {
      margin-top: 10px;
    }
    .entry-block-title {
      color: var(--muted);
      font-size: 12px;
      font-weight: 650;
      margin-bottom: 4px;
    }
    .entry-block-text {
      white-space: pre-wrap;
      word-break: break-word;
    }
    .entry-block pre {
      margin-top: 0;
    }
    .console-command {
      color: var(--accent-strong);
      font-size: 12px;
      margin-bottom: 4px;
      word-break: break-word;
    }
    .summary-list {
      margin: 0;
      padding-left: 18px;
      color: var(--text);
    }
    .summary-list li {
      margin: 4px 0;
    }
    .summary-link {
      border: 0;
      border-radius: 0;
      background: transparent;
      color: var(--accent-strong);
      cursor: pointer;
      font-weight: 650;
      min-height: 0;
      padding: 0;
      text-decoration: underline;
      text-underline-offset: 2px;
    }
    .summary-link:hover {
      background: transparent;
      color: var(--accent);
    }
    details {
      margin-top: 10px;
    }
    summary {
      cursor: pointer;
      color: var(--muted);
      font-size: 12px;
    }
    pre {
      overflow: auto;
      margin: 8px 0 0;
      padding: 10px;
      border-radius: 6px;
      background: var(--code-bg);
      color: var(--code-text);
      font-size: 12px;
      line-height: 1.4;
    }
    .empty, .error, .loading {
      color: var(--muted);
      padding: 22px;
      text-align: center;
    }
    .error {
      color: var(--danger);
    }
    @media (max-width: 760px) {
      header {
        align-items: flex-start;
        flex-direction: column;
      }
      .header-meta {
        white-space: normal;
      }
      .layout {
        grid-template-columns: 1fr;
        grid-template-rows: minmax(220px, 38vh) 1fr;
      }
      .toolbar {
        grid-template-columns: 1fr auto;
      }
      .toolbar select {
        grid-column: 1 / -1;
        max-width: none;
        width: 100%;
      }
      aside {
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
      .details-head {
        flex-direction: column;
      }
    }
  </style>
</head>
<body>
  <div class="app">
    <header>
      <h1>MCP Work Logbook</h1>
      <div class="header-meta">
        <span id="status-dot" class="status-dot"></span>
        <span id="status-text">Checking service</span>
      </div>
    </header>
    <div class="layout">
      <aside>
        <div class="toolbar">
          <input id="filter" type="search" placeholder="Filter by task or agent" autocomplete="off">
          <select id="time-filter" aria-label="Time filter">
            <option value="all">All</option>
            <option value="24h">Last 24h</option>
            <option value="7d">Last 7d</option>
            <option value="30d">Last 30d</option>
          </select>
          <button id="refresh" type="button">Refresh</button>
        </div>
        <div id="task-list" class="task-list">
          <div class="loading">Loading tasks</div>
        </div>
      </aside>
      <main>
        <div class="details-head">
          <div>
            <p id="details-title" class="details-title">Select a task</p>
            <div id="details-sub" class="details-sub">Work entries will appear here.</div>
          </div>
          <div class="detail-actions">
            <button id="export-md" type="button" disabled>Export MD</button>
            <button id="delete-task" class="danger-button" type="button" disabled>Delete</button>
          </div>
        </div>
        <div id="entries" class="entries">
          <div class="empty">No task selected.</div>
        </div>
      </main>
    </div>
  </div>

  <script>
    const state = {
      tasks: [],
      groups: [],
      expanded: {},
      selected: null,
      entries: [],
      filter: '',
      timeRange: 'all',
      autoRefreshMs: 5000,
      refreshing: false
    };

    const els = {
      statusDot: document.getElementById('status-dot'),
      statusText: document.getElementById('status-text'),
      filter: document.getElementById('filter'),
      timeFilter: document.getElementById('time-filter'),
      refresh: document.getElementById('refresh'),
      taskList: document.getElementById('task-list'),
      entries: document.getElementById('entries'),
      detailsTitle: document.getElementById('details-title'),
      detailsSub: document.getElementById('details-sub'),
      exportMd: document.getElementById('export-md'),
      deleteTask: document.getElementById('delete-task')
    };

    function taskKey(task) {
      return task.orchestrator_task_id + '/' + task.agent_task_id;
    }

    function selectedKey() {
      if (!state.selected) return '';
      if (state.selected.type === 'parent') return 'parent:' + state.selected.orchestrator_task_id;
      return 'subtask:' + taskKey(state.selected.task);
    }

    function uniqueValues(values) {
      const seen = {};
      const result = [];
      values.forEach(function(value) {
        if (!value || seen[value]) return;
        seen[value] = true;
        result.push(value);
      });
      return result.sort();
    }

    function formatDate(value) {
      if (!value) return 'NA';
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return value;
      return date.toLocaleString();
    }

    function timestampValue(value) {
      const date = new Date(value);
      return Number.isNaN(date.getTime()) ? 0 : date.getTime();
    }

    function timeFilterParams() {
      if (state.timeRange === 'all') return '';
      const durations = {
        '24h': 24 * 60 * 60 * 1000,
        '7d': 7 * 24 * 60 * 60 * 1000,
        '30d': 30 * 24 * 60 * 60 * 1000
      };
      const duration = durations[state.timeRange];
      if (!duration) return '';
      return '&start_time=' + encodeURIComponent(new Date(Date.now() - duration).toISOString());
    }

    function timeFilterLabel() {
      if (state.timeRange === '24h') return 'Last 24h';
      if (state.timeRange === '7d') return 'Last 7d';
      if (state.timeRange === '30d') return 'Last 30d';
      return 'All time';
    }

    function setStatus(kind, text) {
      els.statusDot.className = 'status-dot ' + kind;
      els.statusText.textContent = text;
    }

    function setActionsEnabled(enabled) {
      els.exportMd.disabled = !enabled;
      els.deleteTask.disabled = !enabled;
    }

    async function loadHealth() {
      try {
        const res = await fetch('/health', { cache: 'no-store' });
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        const count = data.storage && data.storage.details ? data.storage.details.entry_count : 'NA';
        setStatus(data.status === 'healthy' ? 'ok' : 'error', data.status + ' - entries: ' + count);
      } catch (err) {
        setStatus('error', 'Health check failed');
      }
    }

    async function loadTasks(options) {
      options = options || {};
      if (!options.silent) {
        els.taskList.innerHTML = '<div class="loading">Loading tasks</div>';
      }
      try {
        const res = await fetch('/v1/tasks?limit=1000' + timeFilterParams(), { cache: 'no-store' });
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        state.tasks = Array.isArray(data.tasks) ? data.tasks : [];
        state.groups = buildGroups(state.tasks);
        renderTasks();
      } catch (err) {
        els.taskList.innerHTML = '<div class="error">Failed to load tasks.</div>';
      }
    }

    function buildGroups(tasks) {
      const byParent = {};
      tasks.forEach(function(task) {
        const parentID = task.orchestrator_task_id || 'NA';
        if (!byParent[parentID]) {
          byParent[parentID] = {
            orchestrator_task_id: parentID,
            entry_count: 0,
            subtask_count: 0,
            last_entry_at: '',
            agents: [],
            git_branches: [],
            children: []
          };
        }

        const group = byParent[parentID];
        group.children.push(task);
        group.entry_count += task.entry_count || 0;
        group.subtask_count = group.children.length;
        if (timestampValue(task.last_entry_at) > timestampValue(group.last_entry_at)) {
          group.last_entry_at = task.last_entry_at;
        }
        group.agents = group.agents.concat(Array.isArray(task.agents) ? task.agents : []);
        group.git_branches = group.git_branches.concat(Array.isArray(task.git_branches) ? task.git_branches : []);
      });

      return Object.keys(byParent).map(function(parentID) {
        const group = byParent[parentID];
        group.agents = uniqueValues(group.agents);
        group.git_branches = uniqueValues(group.git_branches);
        group.children.sort(function(a, b) {
          if (a.agent_task_id === group.orchestrator_task_id && b.agent_task_id !== group.orchestrator_task_id) return -1;
          if (b.agent_task_id === group.orchestrator_task_id && a.agent_task_id !== group.orchestrator_task_id) return 1;
          return timestampValue(b.last_entry_at) - timestampValue(a.last_entry_at);
        });
        return group;
      }).sort(function(a, b) {
        return timestampValue(b.last_entry_at) - timestampValue(a.last_entry_at);
      });
    }

    function groupMatches(group, query) {
      return [
        group.orchestrator_task_id,
        group.agents.join(' '),
        group.git_branches.join(' ')
      ].join(' ').toLowerCase().includes(query);
    }

    function taskMatches(task, query) {
      const agents = Array.isArray(task.agents) ? task.agents.join(' ') : '';
      const branches = Array.isArray(task.git_branches) ? task.git_branches.join(' ') : '';
      return [
        task.orchestrator_task_id,
        task.agent_task_id,
        agents,
        branches
      ].join(' ').toLowerCase().includes(query);
    }

    function visibleGroups() {
      const query = state.filter.trim().toLowerCase();
      if (!query) return state.groups;
      return state.groups.map(function(group) {
        if (groupMatches(group, query)) return group;
        const children = group.children.filter(function(task) {
          return taskMatches(task, query);
        });
        if (children.length === 0) return null;
        const clone = Object.assign({}, group);
        clone.children = children;
        return clone;
      }).filter(Boolean);
    }

    function renderTasks() {
      const groups = visibleGroups();
      if (groups.length === 0) {
        els.taskList.innerHTML = '<div class="empty">No tasks found.</div>';
        return;
      }

      const activeKey = selectedKey();
      els.taskList.textContent = '';
      groups.forEach(function(group) {
        renderParentRow(group, activeKey);
        if (state.expanded[group.orchestrator_task_id]) {
          group.children.forEach(function(task) {
            renderChildRow(task, activeKey);
          });
        }
      });
    }

    function renderParentRow(group, activeKey) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'task parent-task';
      if (activeKey === 'parent:' + group.orchestrator_task_id) {
        button.className += ' active';
      }

      const title = document.createElement('div');
      title.className = 'task-title';

      const ids = document.createElement('span');
      const caret = document.createElement('span');
      caret.className = 'task-caret';
      caret.textContent = state.expanded[group.orchestrator_task_id] ? '-' : '+';
      ids.appendChild(caret);
      ids.appendChild(document.createTextNode(group.orchestrator_task_id));
      title.appendChild(ids);

      const count = document.createElement('span');
      count.textContent = String(group.entry_count || 0);
      title.appendChild(count);

      const sub = document.createElement('div');
      sub.className = 'task-sub';
      sub.textContent = group.subtask_count + ' agent tasks - Last: ' + formatDate(group.last_entry_at);

      button.appendChild(title);
      button.appendChild(sub);
      button.addEventListener('click', function() {
        state.expanded[group.orchestrator_task_id] = !state.expanded[group.orchestrator_task_id];
        const fullGroup = state.groups.find(function(item) {
          return item.orchestrator_task_id === group.orchestrator_task_id;
        }) || group;
        selectParent(fullGroup);
      });
      els.taskList.appendChild(button);
    }

    function renderChildRow(task, activeKey) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'task child-task';
      if (activeKey === 'subtask:' + taskKey(task)) {
        button.className += ' active';
      }

      const title = document.createElement('div');
      title.className = 'task-title';

      const ids = document.createElement('span');
      ids.textContent = task.agent_task_id;
      title.appendChild(ids);

      const count = document.createElement('span');
      count.textContent = String(task.entry_count || 0);
      title.appendChild(count);

      const sub = document.createElement('div');
      sub.className = 'task-sub';
      const agents = Array.isArray(task.agents) && task.agents.length > 0 ? task.agents.join(', ') : 'NA';
      const branches = Array.isArray(task.git_branches) && task.git_branches.length > 0 ? task.git_branches.join(', ') : 'NA';
      sub.textContent = 'Last: ' + formatDate(task.last_entry_at) + ' - Agents: ' + agents + ' - Branches: ' + branches;

      button.appendChild(title);
      button.appendChild(sub);
      button.addEventListener('click', function(event) {
        event.stopPropagation();
        selectSubTask(task);
      });
      els.taskList.appendChild(button);
    }

    function selectParent(group) {
      state.selected = { type: 'parent', orchestrator_task_id: group.orchestrator_task_id, group: group };
      state.entries = [];
      setActionsEnabled(true);
      renderTasks();
      els.detailsTitle.textContent = group.orchestrator_task_id;
      els.detailsSub.textContent = group.subtask_count + ' agent tasks, ' + group.entry_count + ' entries - ' + timeFilterLabel();
      renderParentSummary(group);
    }

    function renderParentSummary(group) {
      els.entries.textContent = '';
      const card = document.createElement('article');
      card.className = 'entry';

      const meta = document.createElement('div');
      meta.className = 'entry-meta';
      meta.textContent = 'Orchestrator task - Last: ' + formatDate(group.last_entry_at);

      const list = document.createElement('ul');
      list.className = 'summary-list';
      group.children.forEach(function(task) {
        const item = document.createElement('li');

        const link = document.createElement('button');
        link.type = 'button';
        link.className = 'summary-link';
        link.textContent = task.agent_task_id;
        link.addEventListener('click', function() {
          state.expanded[task.orchestrator_task_id] = true;
          selectSubTask(task);
        });

        item.appendChild(link);
        item.appendChild(document.createTextNode(': ' + task.entry_count + ' entries, last ' + formatDate(task.last_entry_at)));
        list.appendChild(item);
      });

      card.appendChild(meta);
      card.appendChild(list);
      els.entries.appendChild(card);
    }

    async function selectSubTask(task, options) {
      options = options || {};
      state.selected = { type: 'subtask', task: task };
      els.detailsTitle.textContent = taskKey(task);
      if (!options.silent) {
        state.entries = [];
        setActionsEnabled(false);
        renderTasks();
        els.detailsSub.textContent = 'Loading entries';
        els.entries.innerHTML = '<div class="loading">Loading entries</div>';
      }

      const path = '/v1/tasks/' + encodeURIComponent(task.orchestrator_task_id) +
        '/agent-tasks/' + encodeURIComponent(task.agent_task_id) + '/entries?limit=1000' + timeFilterParams();

      try {
        const res = await fetch(path, { cache: 'no-store' });
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        const entries = Array.isArray(data.entries) ? data.entries : [];
        state.entries = entries;
        els.detailsSub.textContent = entries.length + ' of ' + data.total_count + ' entries - ' + timeFilterLabel();
        setActionsEnabled(true);
        renderEntries(entries);
      } catch (err) {
        state.entries = [];
        setActionsEnabled(true);
        els.detailsSub.textContent = 'Failed to load entries';
        els.entries.innerHTML = '<div class="error">Failed to load entries.</div>';
      }
    }

    function renderEntries(entries) {
      if (entries.length === 0) {
        els.entries.innerHTML = '<div class="empty">No entries for this task.</div>';
        return;
      }

      els.entries.textContent = '';
      entries.forEach(function(entry) {
        const card = document.createElement('article');
        card.className = 'entry';

        const meta = document.createElement('div');
        meta.className = 'entry-meta';

        const agent = document.createElement('span');
        agent.className = 'entry-agent';
        agent.textContent = entry.agent_id || 'NA';
        meta.appendChild(agent);

        const time = document.createElement('span');
        time.textContent = formatDate(entry.timestamp);
        meta.appendChild(time);

        const branch = document.createElement('span');
        branch.textContent = 'branch: ' + (entry.git_branch || 'NA');
        meta.appendChild(branch);

        const subTask = document.createElement('span');
        subTask.textContent = 'agent task: ' + (entry.agent_task_id || 'NA');
        meta.appendChild(subTask);

        const id = document.createElement('span');
        id.textContent = entry.id || 'NA';
        meta.appendChild(id);

        card.appendChild(meta);
        renderEntryBody(card, entry);

        if (entry.metadata && Object.keys(entry.metadata).length > 0) {
          const details = document.createElement('details');
          const summary = document.createElement('summary');
          summary.textContent = 'Metadata';
          const pre = document.createElement('pre');
          pre.textContent = JSON.stringify(entry.metadata, null, 2);
          details.appendChild(summary);
          details.appendChild(pre);
          card.appendChild(details);
        }

        els.entries.appendChild(card);
      });
    }

    function renderEntryBody(card, entry) {
      if (entry.text) {
        const text = document.createElement('div');
        text.className = 'entry-text';
        text.textContent = entry.text;
        card.appendChild(text);
      }

      const content = Array.isArray(entry.content) ? entry.content : [];
      content.forEach(function(block) {
        renderContentBlock(card, block);
      });

      if (!entry.text && content.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'entry-text';
        empty.textContent = '';
        card.appendChild(empty);
      }
    }

    function renderContentBlock(card, block) {
      const section = document.createElement('section');
      section.className = 'entry-block';

      const type = block && block.type ? block.type : 'text';
      const titleText = block && block.title ? block.title : '';
      if (titleText || type === 'code' || type === 'console') {
        const title = document.createElement('div');
        title.className = 'entry-block-title';
        if (titleText) {
          title.textContent = titleText;
        } else if (type === 'code') {
          title.textContent = block.language ? 'Code - ' + block.language : 'Code';
        } else {
          title.textContent = 'Console';
        }
        section.appendChild(title);
      }

      if (type === 'console' && block.command) {
        const command = document.createElement('div');
        command.className = 'console-command';
        command.textContent = '$ ' + block.command + (Number.isInteger(block.exit_code) ? ' (exit ' + block.exit_code + ')' : '');
        section.appendChild(command);
      }

      if (type === 'code' || type === 'console') {
        const pre = document.createElement('pre');
        pre.textContent = block && block.text ? block.text : '';
        section.appendChild(pre);
      } else {
        const text = document.createElement('div');
        text.className = 'entry-block-text';
        text.textContent = block && block.text ? block.text : '';
        section.appendChild(text);
      }

      card.appendChild(section);
    }

    async function fetchParentEntries(orchestratorTaskID) {
      const entries = [];
      let offset = 0;
      const limit = 1000;

      while (true) {
        const path = '/v1/tasks/' + encodeURIComponent(orchestratorTaskID) + '/entries?limit=' + limit + '&offset=' + offset + timeFilterParams();
        const res = await fetch(path, { cache: 'no-store' });
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        const page = Array.isArray(data.entries) ? data.entries : [];
        entries.push.apply(entries, page);
        if (!data.has_more || page.length === 0) break;
        offset += page.length;
      }

      return entries;
    }

    function markdownEscape(value) {
      return String(value || '').replace(/\r\n/g, '\n');
    }

    function appendContentMarkdown(lines, block) {
      const type = block && block.type ? block.type : 'text';
      const title = block && block.title ? block.title : '';
      const body = markdownEscape(block && block.text ? block.text : '');

      if (title) {
        lines.push('### ' + markdownEscape(title));
        lines.push('');
      }

      if (type === 'console') {
        if (block.command) {
          const exit = Number.isInteger(block.exit_code) ? ' (exit ' + block.exit_code + ')' : '';
          lines.push('Command: ' + String.fromCharCode(96) + markdownEscape(block.command) + String.fromCharCode(96) + exit);
          lines.push('');
        }
        lines.push('~~~text');
        lines.push(body);
        lines.push('~~~');
        lines.push('');
        return;
      }

      if (type === 'code') {
        lines.push('~~~' + markdownEscape(block.language || ''));
        lines.push(body);
        lines.push('~~~');
        lines.push('');
        return;
      }

      lines.push(body);
      lines.push('');
    }

    function appendEntryMarkdown(lines, entry, index) {
      lines.push('## ' + (index + 1) + '. ' + formatDate(entry.timestamp));
      lines.push('');
      lines.push('- Agent: ' + markdownEscape(entry.agent_id || 'NA'));
      lines.push('- Git branch: ' + markdownEscape(entry.git_branch || 'NA'));
      lines.push('- Agent task: ' + markdownEscape(entry.agent_task_id || 'NA'));
      lines.push('- Entry ID: ' + markdownEscape(entry.id || 'NA'));
      lines.push('');
      if (entry.text) {
        lines.push(markdownEscape(entry.text));
        lines.push('');
      }
      const content = Array.isArray(entry.content) ? entry.content : [];
      content.forEach(function(block) {
        appendContentMarkdown(lines, block);
      });
      if (entry.metadata && Object.keys(entry.metadata).length > 0) {
        lines.push('');
        lines.push('~~~json');
        lines.push(JSON.stringify(entry.metadata, null, 2));
        lines.push('~~~');
      }
      lines.push('');
    }

    function buildSubTaskMarkdown(task, entries) {
      const lines = [
        '# Work Logbook: ' + taskKey(task),
        '',
        '- Orchestrator task: ' + markdownEscape(task.orchestrator_task_id),
        '- Agent task: ' + markdownEscape(task.agent_task_id),
        '- Branches: ' + markdownEscape((task.git_branches || ['NA']).join(', ')),
        '- Entries: ' + entries.length,
        '- Time filter: ' + markdownEscape(timeFilterLabel()),
        '- Exported at: ' + new Date().toISOString(),
        ''
      ];
      entries.forEach(function(entry, index) {
        appendEntryMarkdown(lines, entry, index);
      });
      return lines.join('\n');
    }

    function buildParentMarkdown(group, entries) {
      const lines = [
        '# Work Logbook: ' + group.orchestrator_task_id,
        '',
        '- Orchestrator task: ' + markdownEscape(group.orchestrator_task_id),
        '- Agent tasks: ' + group.subtask_count,
        '- Entries: ' + entries.length,
        '- Time filter: ' + markdownEscape(timeFilterLabel()),
        '- Branches: ' + markdownEscape((group.git_branches || ['NA']).join(', ')),
        '- Exported at: ' + new Date().toISOString(),
        ''
      ];

      const bySubTask = {};
      entries.forEach(function(entry) {
        const agentTaskID = entry.agent_task_id || 'NA';
        if (!bySubTask[agentTaskID]) bySubTask[agentTaskID] = [];
        bySubTask[agentTaskID].push(entry);
      });

      Object.keys(bySubTask).sort().forEach(function(agentTaskID) {
        lines.push('# Agent task: ' + markdownEscape(agentTaskID));
        lines.push('');
        bySubTask[agentTaskID].forEach(function(entry, index) {
          appendEntryMarkdown(lines, entry, index);
        });
      });

      return lines.join('\n');
    }

    function downloadMarkdown(name, markdown) {
      const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = name.replace(/[^a-zA-Z0-9._-]+/g, '_') + '.md';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    }

    async function exportMarkdown() {
      if (!state.selected) return;

      if (state.selected.type === 'parent') {
        const group = state.selected.group;
        const entries = await fetchParentEntries(group.orchestrator_task_id);
        downloadMarkdown(group.orchestrator_task_id, buildParentMarkdown(group, entries));
        return;
      }

      const task = state.selected.task;
      downloadMarkdown(taskKey(task), buildSubTaskMarkdown(task, state.entries));
    }

    async function deleteSelectedTask() {
      if (!state.selected) return;

      let path;
      let message;
      if (state.selected.type === 'parent') {
        const group = state.selected.group;
        path = '/v1/tasks/' + encodeURIComponent(group.orchestrator_task_id) + '/entries';
        message = 'Delete all entries for orchestrator task ' + group.orchestrator_task_id + ' across ' + group.subtask_count + ' agent tasks?';
      } else {
        const task = state.selected.task;
        path = '/v1/tasks/' + encodeURIComponent(task.orchestrator_task_id) +
          '/agent-tasks/' + encodeURIComponent(task.agent_task_id) + '/entries';
        message = 'Delete all entries for ' + taskKey(task) + '?';
      }

      if (!window.confirm(message)) return;

      setActionsEnabled(false);
      try {
        const res = await fetch(path, { method: 'DELETE' });
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        state.selected = null;
        state.entries = [];
        els.detailsTitle.textContent = 'Select a task';
        els.detailsSub.textContent = 'Deleted entries: ' + data.deleted_count;
        els.entries.innerHTML = '<div class="empty">Task entries deleted.</div>';
        setActionsEnabled(false);
        await refreshAll({ silent: true });
      } catch (err) {
        els.detailsSub.textContent = 'Delete failed';
        els.entries.innerHTML = '<div class="error">Failed to delete entries.</div>';
        setActionsEnabled(true);
      }
    }

    async function refreshAll(options) {
      options = options || {};
      if (state.refreshing) return;

      state.refreshing = true;
      try {
        await loadHealth();
        await loadTasks(options);

        if (state.selected) {
          if (state.selected.type === 'parent') {
            const group = state.groups.find(function(item) {
              return item.orchestrator_task_id === state.selected.orchestrator_task_id;
            });
            if (group) {
              state.selected.group = group;
              renderParentSummary(group);
              els.detailsTitle.textContent = group.orchestrator_task_id;
              els.detailsSub.textContent = group.subtask_count + ' agent tasks, ' + group.entry_count + ' entries - ' + timeFilterLabel();
              setActionsEnabled(true);
            }
          } else {
            const selectedTaskKey = taskKey(state.selected.task);
            const latestTask = state.tasks.find(function(task) {
              return taskKey(task) === selectedTaskKey;
            });
            if (latestTask) {
              await selectSubTask(latestTask, { silent: true });
            }
          }
        }
      } finally {
        state.refreshing = false;
      }
    }

    function startAutoRefresh() {
      window.setInterval(function() {
        if (document.visibilityState === 'visible') {
          refreshAll({ silent: true });
        }
      }, state.autoRefreshMs);
    }

    els.filter.addEventListener('input', function(event) {
      state.filter = event.target.value;
      renderTasks();
    });

    els.timeFilter.addEventListener('change', function(event) {
      state.timeRange = event.target.value;
      refreshAll({ silent: false });
    });

    els.exportMd.addEventListener('click', exportMarkdown);
    els.deleteTask.addEventListener('click', deleteSelectedTask);

    els.refresh.addEventListener('click', function() {
      refreshAll({ silent: false });
    });

    refreshAll({ silent: false });
    startAutoRefresh();
  </script>
</body>
</html>`
