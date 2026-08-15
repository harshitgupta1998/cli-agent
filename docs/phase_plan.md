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
- Mock memory search shaped like future persisted command events.
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
- Memory panel displays relevant prior commands.

## Later Phase Mocks

The backend exposes these planning endpoints so future work has stable placeholders:

```text
GET /v1/phases
GET /v1/mocks/capabilities
```

## Phase 2: Local LLM Planning

Replace static planner rules with Ollama structured output.

Mocked today by:

- `POST /v1/requests`
- static command-plan rules
- provenance strings

## Phase 3: Safe Execution

Replace mock execution with a controlled process runner.

Mocked today by:

- `POST /v1/commands/execute`
- canned stdout/stderr/exit-code responses

## Phase 4: Persistent Memory

Persist command events, sessions, messages, and project commands.

Mocked today by:

- Postgres schema in `database/init`
- static memory results from `POST /v1/memory/search`

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

