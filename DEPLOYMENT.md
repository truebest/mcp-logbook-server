# MCP Logbook Server Deployment

This document describes the current deployment model for MCP Logbook Server.
The server is designed to run as a small local or LAN service with Docker
Compose, SQLite persistence, a browser UI, and an MCP Streamable HTTP endpoint.

## Runtime Model

```text
MCP client / agent
    |
    | Streamable HTTP
    | http://localhost:8081/v1/mcp
    v
mcp-logbook-server
    |-- MCP tools for writes and reads
    |-- Web UI and read API on :8080
    v
logs.db SQLite database
```

Agents write entries through MCP, primarily with `add_work_entry`. Humans review
entries through the browser UI on port `8080`.

## Requirements

- Docker and Docker Compose.
- A writable project directory for `logs.db`.
- Optional: Go 1.23+ for local development without Docker.

## Quick Deployment

```bash
git clone https://github.com/truebest/mcp-logbook-server
cd mcp-logbook-server
docker compose up -d --build
```

Check the service:

```bash
docker compose ps
curl http://localhost:8080/health
```

Open the web UI:

```text
http://localhost:8080/
```

Configure MCP clients with:

```text
Name: mcp-logbook-server
Transport: HTTP (Streamable)
URL: http://localhost:8081/v1/mcp
Headers: none required for local use
```

Opening `/v1/mcp` directly in a browser may show
`server-to-client streaming is not supported`. That is expected for a plain
browser request; MCP clients should use JSON-RPC over Streamable HTTP.

## Ports

- `8080`: browser UI and read/management HTTP API.
- `8081`: MCP Streamable HTTP endpoint at `/v1/mcp`.
- `8082`: reserved by the default compose file.

If another service already uses these ports, edit `docker-compose.yml` and map
different host ports to the same container ports.

## Persistent Files

The default Compose setup stores runtime files next to the project:

- `logs.db`: SQLite database.
- `config.yaml`: local runtime configuration.

Both files are ignored by git. Back up `logs.db` if the work-logbook data matters.

Example backup:

```bash
cp logs.db "logs.db.backup-$(date +%Y%m%d-%H%M%S)"
```

## Configuration

The container starts with:

```bash
./server --config /root/config.yaml
```

The default `config.yaml` contains:

```yaml
server:
  ingestion_port: 8080
  mcp_port: 8081

storage:
  type: sqlite
  connection_string: "./logs.db"
```

For a LAN deployment, bind the host ports in Docker Compose as usual:

```yaml
ports:
  - "8080:8080"
  - "8081:8081"
```

Then use the host address from your MCP client, for example:

```text
http://<host>:8081/v1/mcp
```

## MCP Tools

The deployed server exposes these MCP tools:

- `add_work_entry`: create a work-logbook entry.
- `get_task_entries`: read entries for one orchestrator task and agent task.
- `list_tasks`: list available orchestrator/agent task groups.
- `get_service_status`: return service and storage status.

`add_work_entry` requires:

- `orchestrator_task_id`
- `agent_task_id`
- `agent_id`
- `git_branch`

Use `NA` for values an agent cannot determine, such as an unavailable branch.

## HTTP API

The HTTP API is for inspection and UI management. New entries should be written
through MCP, not through HTTP POST.

Read endpoints:

- `GET /health`
- `GET /v1/tasks`
- `GET /v1/tasks/:orchestrator_task_id/entries`
- `GET /v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries`

Management endpoints used by the UI:

- `DELETE /v1/tasks/:orchestrator_task_id/entries`
- `DELETE /v1/tasks/:orchestrator_task_id/agent-tasks/:agent_task_id/entries`

## Operations

View logs:

```bash
docker compose logs -f mcp-logbook-server
```

Restart:

```bash
docker compose restart mcp-logbook-server
```

Rebuild after code changes:

```bash
docker compose up -d --build
```

Stop:

```bash
docker compose down
```

## Updating

```bash
git pull
docker compose up -d --build
curl http://localhost:8080/health
```

The SQLite schema is migrated by the server on startup. Keep a copy of `logs.db`
before deploying changes that affect stored data.

## Local Development

Run focused tests:

```bash
go test ./cmd/server ./pkg/models ./pkg/mcp ./pkg/ingestion
```

Run the server without Docker:

```bash
go run ./cmd/server --config config.yaml
```

## Notes

- README.md is the main feature and API reference.
- Deployment examples use `localhost`; replace it with your server hostname or IP
  when connecting from another machine on the network.
