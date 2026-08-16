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
```

`internal/planner` contains the Ollama planner and deterministic fallback planner.

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
internal/services   mock agent implementation
```

## Phase Endpoints

```text
GET /v1/phases
GET /v1/mocks/capabilities
```

These endpoints make the roadmap executable in the app. Phase 1 is the current working product surface; later phases are represented as explicit mocked capabilities.

The backend still uses mock responses. The next real implementation layers should be:

- `internal/policy` for deterministic command risk evaluation
- `internal/executor` for controlled command execution and streaming
- expanded `internal/memory` support for sessions, messages, and learned project commands
- `internal/ollama` for local LLM planning
