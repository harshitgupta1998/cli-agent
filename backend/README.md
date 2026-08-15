# Termind Backend

Go API service for the Termind product template.

## Run

```bash
go run ./cmd/server
```

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
- `internal/memory` for Postgres persistence
- `internal/ollama` for local LLM planning
