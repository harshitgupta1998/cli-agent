# Termind Phase Plan

## Phase 1: Command Workbench

Phase 1 proves the product loop without executing real shell commands:

```text
typed request -> command plan -> policy review -> user approval -> mock execution -> remembered event
```

### Phase 1 Deliverables

- React TypeScript command workbench.
- Go HTTP API with stable contracts.
- Mock planner that returns structured command plans.
- Deterministic policy scaffold.
- Mock executor that returns stdout, stderr, exit code, and duration.
- Postgres-backed command event recording.
- Memory search over persisted command events, with seeded mock examples as fallback.
- Project context mock.
- Docker Compose stack for frontend, backend, and Postgres.

### Phase 1 Success Criteria

- `docker compose up --build` starts all services.
- Frontend can create a session.
- User can submit a natural-language request.
- Backend returns a structured `CommandPlan`.
- Backend returns a policy decision.
- User can run the approved mock command.
- Backend returns a command-event-like execution result.
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

Replace mock execution with a controlled process runner.

Mocked today by:

- `POST /v1/commands/execute`
- canned stdout/stderr/exit-code responses

## Phase 4: Persistent Memory

Expand persistence beyond Phase 1 command events into full sessions, messages, learned project commands, and richer repository queries.

Mocked today by:

- static session creation
- static project context
- seeded fallback memory examples

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
