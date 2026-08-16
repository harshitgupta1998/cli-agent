# Termind

Termind is a local-first terminal memory agent. It helps users find, explain, approve, run, and remember terminal commands.

This repository starts as a dockerized product template with:

- React/Vite frontend
- Go backend
- Postgres database
- Mock API responses for command planning, execution, memory search, and project context

## Run Locally

```bash
cp .env.example .env
docker compose up --build
```

Open:

- Frontend: http://localhost:5173
- Backend health: http://localhost:8000/health
- Backend config: http://localhost:8000/v1/config

## Services

```text
frontend  React/Vite product UI
backend   Go application and mock agent API
database  Postgres with seed schema
```

## Backend API

The Go backend currently exposes:

```text
GET  /health
GET  /v1/config
GET  /v1/phases
GET  /v1/mocks/capabilities
POST /v1/sessions
POST /v1/requests
POST /v1/commands/execute
POST /v1/commands/record
POST /v1/memory/search
POST /v1/commands/explain
GET  /v1/context/project
```

## CLI Integration

The Docker backend plans commands and applies policy. The local CLI executes approved commands on your machine.

Build the CLI:

```bash
cd backend
go build -o ../bin/termind ./cmd/termind
```

Run one request:

```bash
../bin/termind -once "what is using port 8000?"
```

Start the interactive CLI:

```bash
../bin/termind
```

Optional API override:

```bash
TERMIND_API_BASE_URL=http://localhost:8000 ../bin/termind
```

For convenience, add it to your shell path:

```bash
export PATH="/Users/harsgupta/Desktop/Code BKP/cli-agent/bin:$PATH"
```

Then you can run:

```bash
termind
```

## Local LLM Planner

Phase 2 uses Ollama when available:

```bash
brew install ollama
ollama serve
ollama pull llama3.2:3b
```

The backend reads:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2:3b
PLANNER_MODE=ollama
```

If Ollama is offline or returns invalid JSON, the backend falls back to the deterministic rule planner.

## Phase Plan

Phase 1 is now solidified as the Command Workbench:

```text
typed request -> command plan -> policy review -> approval -> mock execution -> remembered event
```

Later phases are mocked in the API and visible in the frontend so implementation can replace one mock at a time.

See [docs/phase_plan.md](docs/phase_plan.md).

## Test

All checks:

```bash
make test
```

Backend unit/compile tests:

```bash
cd backend
go test ./...
```

Frontend type and build checks:

```bash
cd frontend
npm install
npm run typecheck
npm run build
```

Integration tests against the running API:

```bash
docker compose up -d --build
pytest
```

To point pytest at a different backend URL:

```bash
TERMIND_API_BASE_URL=http://localhost:8000 pytest
```

## Current Mock Flow

1. User types a terminal request in the frontend.
2. Frontend calls `POST /v1/requests`.
3. Backend returns a mock command plan.
4. User clicks run.
5. Frontend calls `POST /v1/commands/execute`.
6. Backend returns a mock execution result.
7. Memory search and project context endpoints return seeded examples.

## Next Development Steps

- Replace mock planner with Ollama structured output.
- Replace mock execution with a sandboxed command executor.
- Add deterministic policy rules.
- Persist real command events to Postgres.
- Add hybrid command retrieval.
- Add CLI/TUI package.
