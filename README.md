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
POST /v1/sessions
POST /v1/requests
POST /v1/commands/execute
POST /v1/memory/search
POST /v1/commands/explain
GET  /v1/context/project
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
