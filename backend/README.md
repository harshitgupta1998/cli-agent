# Termind Backend

Go API service for the Termind product template.

## Run

```bash
go run ./cmd/server
```

## CLI

Build:

```bash
go build -o ../bin/termind ./cmd/termind
```

Run against the local API:

```bash
../bin/termind
../bin/termind -once "what is using port 8000?"
```

The CLI asks the Go API for planning and policy, then executes approved commands locally from your current working directory.

## Local LLM Planning

The API uses Ollama for Phase 2 command planning when configured:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2:3b
PLANNER_MODE=ollama
COMMAND_TIMEOUT_SECONDS=30
```

`internal/planner` contains the Ollama planner and deterministic fallback planner.

## Safe Execution

`POST /v1/commands/execute` runs approved commands through the backend process runner. It captures stdout, stderr, exit code, and duration, applies a small destructive-command denylist, and records command events to Postgres when the store is available.

When the API is running in Docker, commands execute inside the backend container. Use `/app` as the frontend/default CWD for container execution. The CLI path is different: it gets plans from the API, then executes approved commands on the host from the directory where `termind` was launched.

## Test

```bash
go test ./...
```

The repository also includes Python integration tests for the running HTTP API:

```bash
cd ..
pytest
```

## Current Packages

```text
cmd/server          process entrypoint
internal/api        HTTP routing, CORS, validation, JSON helpers
internal/config     environment configuration
internal/models     request/response contracts
internal/planner    Ollama and rule planners
internal/services   agent service, policy, execution, roadmap metadata
```

## Persistent Memory

`POST /v1/sessions` now creates real Postgres session rows and creates or reuses a project by root path. Command records attach to the session's project, successful commands update `project_commands`, and memory search returns persisted command events rather than service-level fake results.

`GET /v1/sessions/{session_id}/messages` returns persisted user and assistant messages for a session.

## Phase Endpoints

```text
GET /v1/phases
GET /v1/mocks/capabilities
```

These endpoints make the roadmap executable in the app. Phase 3 is the current working product surface; later phases are represented as explicit mocked capabilities.

The next real implementation layers should be:

- `internal/policy` for deterministic command risk evaluation
- `internal/executor` for streaming and cancellation around the current process runner
- expanded `internal/memory` queries and cleanup tools
- real `internal/context` detection for git, package managers, and project stack
