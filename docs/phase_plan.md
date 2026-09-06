# Termind Phase Plan

## Phase 1: Command Workbench

Phase 1 established the product shell and stable API contracts:

```text
typed request -> command plan -> policy review -> user approval -> safe execution -> remembered event
```

### Phase 1 Deliverables

- React TypeScript command workbench.
- Go HTTP API with stable contracts.
- Rule planner that returns structured command plans.
- Deterministic policy scaffold.
- Initial execution contract for stdout, stderr, exit code, and duration.
- Postgres-backed command event recording.
- Memory search API over command events.
- Project context mock.
- Docker Compose stack for frontend, backend, and Postgres.

### Phase 1 Success Criteria

- `docker compose up --build` starts all services.
- Frontend can create a session.
- User can submit a natural-language request.
- Backend returns a structured `CommandPlan`.
- Backend returns a policy decision.
- User can review and approve a proposed command.
- Backend returns a command-event-shaped execution result.
- CLI records locally executed command events through `POST /v1/commands/record`.
- Memory search returns persisted command events.

## Later Phase Mocks

The backend exposes these planning endpoints so future work has stable placeholders:

```text
GET /v1/phases
GET /v1/mocks/capabilities
```

## Phase 2: Local LLM Planning

Replace static planner rules with Ollama structured output.

Implemented shape:

- `POST /v1/requests` calls Ollama `/api/chat` when `PLANNER_MODE=ollama`.
- The planner asks Ollama for JSON-only `CommandPlan` output.
- The app validates the response and keeps deterministic policy review.
- Rule-based planning remains as fallback when Ollama is offline or invalid.
- Default local model: `llama3.2:3b`.

## Phase 3: Safe Execution

Implemented controlled backend process execution.

Implemented shape:

- `POST /v1/commands/execute` requires explicit approval before running.
- The backend runs commands with `sh -c` from the requested `cwd`.
- The executor captures stdout, stderr, exit code, and duration.
- `COMMAND_TIMEOUT_SECONDS` bounds command runtime.
- Destructive and privileged patterns are blocked before execution.
- Execution events are persisted to Postgres when available.

## Phase 4: Persistent Memory

Expand persistence beyond command events into full sessions, messages, learned project commands, and richer repository queries.

Implemented shape:

- `POST /v1/sessions` creates real session rows.
- Projects are created or reused by `root_path`.
- Command events attach to the session's project.
- Memory search returns persisted command events or an empty result set.

Remaining work:

- message persistence
- learned project commands
- static project context

## Phase 5: Semantic Recall

Add local embeddings and hybrid ranking.

Mocked today by:

- memory search scores
- matched reasons such as `semantic match`, `same project`, and `successful command`

## Phase 6: Voice Input

Add local speech-to-text as an input adapter.

Mocked today by:

- phase capability metadata only
- the typed pipeline that voice will feed into later
