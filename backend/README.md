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

## Current Packages

```text
cmd/server          process entrypoint
internal/api        HTTP routing, CORS, validation, JSON helpers
internal/config     environment configuration
internal/models     request/response contracts
internal/services   mock agent implementation
```

The backend still uses mock responses. The next real implementation layers should be:

- `internal/policy` for deterministic command risk evaluation
- `internal/executor` for controlled command execution and streaming
- `internal/memory` for Postgres persistence
- `internal/ollama` for local LLM planning
