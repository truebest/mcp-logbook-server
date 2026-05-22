# MCP Logbook Server

MCP Logbook Server stores structured agent work-logbook entries grouped by an
orchestrator task ID and an agent task ID. Agents write progress through the MCP
`add_work_entry` tool, and humans can inspect, filter, export, delete, and review
entries in the browser.

## Screenshots

![MCP Logbook web UI](docs/screenshots/Screenshot%20from%202026-05-21%2018-40-16.png)

![MCP Logbook entry view](docs/screenshots/Screenshot%20from%202026-05-21%2018-40-24.png)

## Features

- MCP Streamable HTTP endpoint for agent writes and queries.
- Browser UI for reviewing work by orchestrator task and agent task.
- Structured entries with plain text, console output, and code blocks.
- SQLite storage with migrations and local file persistence.
- Read-only HTTP API for health, task lists, and entry inspection.
- Time filters for task lists, entry queries, and UI views.
- Markdown export and delete actions from the browser UI.
- Browser-driven light/dark theme via `prefers-color-scheme`.

## Architecture

```text
Agent / MCP client
    |
    | MCP Streamable HTTP
    | POST /v1/mcp on :8081
    v
MCP tools
    |-- add_work_entry
    |-- get_task_entries
    |-- list_tasks
    |-- get_service_status
    v
SQLite work_entries table
    ^
    |
Read-only HTTP API on :8080
    |-- GET /health
    |-- GET /v1/tasks
    |-- GET /v1/tasks/:orchestrator_task_id/entries
    |-- GET /v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries
    v
Browser UI on :8080
```

The write path is MCP-only. The HTTP API is intentionally read-only except for UI
management delete endpoints. There are no direct HTTP POST endpoints for creating
logbook entries.

## Quick Start

```bash
git clone https://github.com/truebest/mcp-logbook-server
cd mcp-logbook-server
docker compose up -d --build
```

Local endpoints:

- Web UI and read-only API: `http://localhost:8080/`
- Health check: `http://localhost:8080/health`
- Tasks JSON: `http://localhost:8080/v1/tasks`
- MCP Streamable HTTP: `http://localhost:8081/v1/mcp`

## Docker

The default Compose service is `mcp-logbook-server` and exposes ports `8080`,
`8081`, and `8082`.

```bash
docker compose up -d --build
docker compose logs -f mcp-logbook-server
docker compose ps
```

Persistent local files:

- `logs.db`: SQLite database, ignored by git.
- `config.yaml`: local runtime config, ignored by git.
- `config/api-keys.yaml.example`: example config for API keys.

## Configuration

The server reads config from `config.yaml` by default when run through Docker
Compose. Runtime data and secrets should stay local and are excluded by
`.gitignore`.

Relevant ports:

- `8080`: Web UI and read-only inspection API.
- `8081`: MCP Streamable HTTP endpoint at `/v1/mcp`.
- `8082`: reserved/optional.

## MCP Client Configuration

Configure your MCP client with Streamable HTTP:

```text
Name: mcp-logbook-server
Transport: HTTP (Streamable)
URL: http://localhost:8081/v1/mcp
Headers: none required for local use
```

Opening `/v1/mcp` in a browser may show `server-to-client streaming is not
supported`; that is expected for a plain browser GET. MCP clients should use POST
JSON-RPC requests.

## MCP Tools

### `add_work_entry`

Adds one structured work-logbook entry.

Required fields:

- `orchestrator_task_id`
- `agent_task_id`
- `agent_id`
- `git_branch`

Optional fields:

- `text`
- `content`
- `timestamp`
- `metadata`

At least one useful body must be non-empty: `text`, `content`, or both. Use `NA`
for unavailable values such as `git_branch`.

### `get_task_entries`

Reads entries for one orchestrator/agent-task pair.

Required fields:

- `orchestrator_task_id`
- `agent_task_id`

Optional filters:

- `agent_id`
- `start_time`: RFC3339 lower timestamp bound.
- `end_time`: RFC3339 upper timestamp bound.
- `limit`: default `100`, maximum `1000`.
- `offset`: default `0`.

### `list_tasks`

Lists orchestrator/agent-task pairs that have entries.

Optional filters:

- `start_time`: RFC3339 lower timestamp bound.
- `end_time`: RFC3339 upper timestamp bound.
- `limit`: default `100`, maximum `1000`.
- `offset`: default `0`.

### `get_service_status`

Returns service, MCP, and storage health information.

## Entry Model

Entries are grouped by two required task IDs:

- `orchestrator_task_id`: top-level task, workflow, ticket, run, or coordination ID.
- `agent_task_id`: delegated task, child task, ticket, work item, or agent-local task ID.

Each entry also stores:

- `agent_id`: stable agent name.
- `git_branch`: current branch, or `NA` when unavailable.
- `text`: short summary or fallback text.
- `content`: optional structured blocks for readable details.
- `timestamp`: RFC3339 timestamp; current UTC time is used when omitted.
- `metadata`: optional free-form metadata.

## Structured Content

Supported `content` block types:

- `text`: normal readable text.
- `console`: terminal output, optionally with `command` and `exit_code`.
- `code`: code snippet, optionally with `language`.

Example `add_work_entry` payload:

```json
{
  "orchestrator_task_id": "KOD-494",
  "agent_task_id": "KOD-569",
  "agent_id": "codex",
  "git_branch": "feature/work-logbook-content",
  "text": "Implemented structured content rendering.",
  "content": [
    {
      "type": "text",
      "text": "Added schema, storage, MCP, UI rendering, and Markdown export support."
    },
    {
      "type": "console",
      "title": "Verification",
      "command": "go test ./pkg/mcp ./pkg/ingestion",
      "exit_code": 0,
      "text": "ok github.com/truebest/mcp-logbook-server/pkg/mcp\nok github.com/truebest/mcp-logbook-server/pkg/ingestion"
    },
    {
      "type": "code",
      "language": "go",
      "title": "Model",
      "text": "type WorkEntryContentBlock struct { ... }"
    }
  ]
}
```

## HTTP API

Read endpoints:

- `GET /health`
- `GET /v1/tasks?limit=1000&start_time=<RFC3339>&end_time=<RFC3339>`
- `GET /v1/tasks/:orchestrator_task_id/entries?limit=1000&offset=0`
- `GET /v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries?limit=1000&offset=0`

Management endpoints used by the browser UI:

- `DELETE /v1/tasks/:orchestrator_task_id/entries`
- `DELETE /v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries`

Create/write endpoints are not exposed over HTTP. Use MCP `add_work_entry`.

## Web UI

The browser UI shows a tree:

- orchestrator tasks as root folders
- agent tasks as children
- entries as a timeline for the selected agent task

The UI supports:

- browser-driven light/dark theme via `prefers-color-scheme`
- text search across task IDs, agents, and branches
- time presets: `All`, `Last 24h`, `Last 7d`, `Last 30d`
- Markdown export for an orchestrator task or a single agent task
- delete for an orchestrator task or a single agent task
- auto-refresh while the page is visible

## Agent Prompt

```text
Log meaningful task progress to the MCP work logbook using add_work_entry.
Use orchestrator_task_id and agent_task_id from the active task system, your stable agent name as agent_id,
and the current git branch as git_branch. Use NA for unavailable values.
Use text for a short summary and content blocks for details, console output, or code.
Log start, meaningful progress, blockers, decisions, verification, and final completion.
At the very end, only after committing, add a detailed final report.
Keep entries useful but not noisy.
```

## Development

```bash
go mod download
go test ./pkg/mcp ./pkg/ingestion
go build -o bin/mcp-logbook-server cmd/server/main.go
```

The focused tests above cover the active MCP and browser/API surface. Some older
packages remain from the base project and may need cleanup before `go test ./...`
is fully meaningful.

## Repository Notes

- Repository: `https://github.com/truebest/mcp-logbook-server`
- Go module: `github.com/truebest/mcp-logbook-server`
- Runtime database and local config are ignored by git.

Based on https://github.com/kerlexov/bubac.
