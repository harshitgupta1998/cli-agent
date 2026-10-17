# Termind Phase Plan

Current phase: **Phase 6 is complete**. Local speech-to-text is available through a command adapter, and confirmed transcripts reuse the normal planning, policy, confirmation, and execution path.

## Phase 1: Command Workbench

Phase 1 established the product shell and stable API contracts:

```text
typed request -> command plan -> policy review -> user approval -> safe execution -> remembered event
```

### Phase 1 Deliverables

- React TypeScript command workbench.
- Go HTTP API with stable contracts.
- Rule planner that returns structured command plans.
- Deterministic policy package for command risk evaluation.
- Initial execution contract for stdout, stderr, exit code, and duration.
- Postgres-backed command event recording.
- Memory search API over command events.
- Initial project context API.
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

## Roadmap Metadata

The backend exposes roadmap endpoints so future work has stable placeholders:

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
- Process execution is isolated in `internal/executor` and policy checks live in `internal/policy`.
- The executor streams stdout/stderr to SSE clients and keeps JSON responses for other clients.
- Disconnecting an SSE request cancels the process group.
- Captured output is bounded by `COMMAND_MAX_OUTPUT_BYTES`.
- Working directories are limited by `EXECUTION_ALLOWED_ROOTS`, and child environment variables are filtered by `EXECUTION_ENV_ALLOWLIST`.
- `COMMAND_TIMEOUT_SECONDS` bounds command runtime.
- Destructive and privileged patterns are blocked before execution.
- Docker Compose runs commands in a separate network-isolated worker with a read-only root filesystem and a writable project mount.
- The worker drops Linux capabilities, prevents privilege escalation, and has CPU, memory, and process limits.
- Execution events are persisted to Postgres when available.

Remaining work:

- authentication and workspace authorization are tracked as separate checkpoints below; do not expose the API remotely until they are complete
- approved commands can modify the configured workspace; this local product does not claim to safely execute arbitrary untrusted shell code

## Authentication and Workspace Security

Implementation contract: [security_model.md](security_model.md). These checkpoints must be completed and verified in order before remote or multi-user deployment:

- S1: security contract and compatibility decisions - implemented
- S2: user identity, opaque hashed API tokens, auth middleware, and fail-closed remote mode - implemented
- S3.1: explicit workspace/permission schema and legacy-data backfill - implemented
- S3.2: workspace APIs, owner-scoped persistence/API access, and explicit legacy workspace adoption - implemented
- S4: versioned workspace command permissions and one-time approval binding, enforced before executor dispatch - implemented
- S5: CLI/frontend workspace and credential integration, end-to-end isolation tests, and deployment hardening

At every checkpoint, run the focused tests and the existing Go, frontend, and integration suites before committing. Local-only mode must remain loopback-bound and explicitly single-user; CORS is not an authentication mechanism.

## Phase 4: Persistent Memory

Expand persistence beyond command events into full sessions, messages, learned project commands, and richer repository queries.

Implemented shape:

- `POST /v1/sessions` creates real session rows.
- Projects are created or reused by `root_path`.
- User and assistant messages are recorded for each planned request.
- Command events attach to the session's project.
- Memory search returns persisted command events or an empty result set.
- Successful executions update learned project commands.
- Project context detects git state, common stack files, package scripts, Makefile targets, and standard manifest commands when available.

Future refinements:

- memory pruning controls

## Phase 5: Semantic Recall

Add local embeddings and hybrid ranking.

Implemented split:

- 5.1: add embedding model config and Ollama embedding client - implemented
- 5.2: embed new command events and store vectors in `command_embeddings` - implemented
- 5.3: backfill existing command events - implemented
- 5.4: add semantic search path - implemented
- 5.5: add hybrid ranking - implemented
- 5.6: improve frontend semantic recall UX - implemented

Implemented shape:

- generate local embeddings with Ollama using `OLLAMA_EMBED_MODEL`
- store vectors in `command_embeddings`
- combine keyword match, semantic similarity, same project, same directory, recency, and success signals
- expose provenance for why a command memory was retrieved
- backfill missing embeddings through `POST /v1/memory/embeddings/backfill` or `termind -backfill-embeddings`

## Phase 6: Voice Input

Add local speech-to-text as an input adapter.

Planned checkpoints:

- 6.1: voice capability metadata, runtime config, and placeholder contracts - implemented
- 6.2: command-based local speech-to-text adapter - implemented
- 6.3: transcript endpoint hardening for accepted MIME types, base64 validation, and audio size limits - implemented
- 6.4: frontend microphone control and transcript confirmation UI - implemented
- 6.5: route confirmed transcripts through the existing `POST /v1/requests` planning and policy flow - implemented
- 6.6: host CLI voice command wrapper for terminal-first usage - implemented

Phase 6 success criteria:

- voice input never bypasses command planning, policy, or confirmation
- transcript text is user-visible and editable before command planning
- audio and transcript handling stays local by default
- typed input keeps working exactly as it does after Phase 5
