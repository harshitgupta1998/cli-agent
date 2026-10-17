# Termind

Termind is a local-first terminal memory agent. It helps users find, explain, approve, run, and remember terminal commands.

This repository starts as a dockerized product template with:

- React/Vite frontend
- Go backend
- Postgres database
- Ollama-backed command planning
- Controlled backend command execution
- Streaming command output with cancellation
- Postgres command-event memory
- Persistent sessions, projects, messages, and learned project commands
- Local semantic recall with Ollama embeddings

## Current Status

Termind has completed **Phase 6: Voice Input**.

Implemented:

- Phase 1: command workbench and API contracts
- Phase 2: local Ollama command planning with rule fallback
- Phase 3: isolated execution worker with streaming, cancellation, output limits, policy checks, and audit records
- Phase 4: persistent sessions, projects, messages, command events, and learned project commands
- Phase 5.1: embedding model config and Ollama embedding client
- Phase 5.2: embeddings for new command events
- Phase 5.3: embedding backfill for existing command events
- Phase 5.4: semantic memory search
- Phase 5.5: hybrid keyword, semantic, project, success, and recency ranking
- Phase 5.6: frontend semantic recall UX
- Phase 6.1: voice runtime config and placeholder transcript contracts
- Phase 6.2: command-based local speech-to-text adapter
- Phase 6.3: transcript endpoint hardening for MIME type, base64, and size limits
- Phase 6.4: frontend microphone and transcript confirmation UI
- Phase 6.5: route confirmed transcripts through planning and policy
- Phase 6.6: host CLI voice command wrapper

Remaining development priorities:

- Workspace ownership and owner-scoped API persistence are implemented. Workspace-specific command permissions and authenticated CLI/frontend flows remain before remote or multi-user deployment. See [docs/security_model.md](docs/security_model.md).
- Improve cross-platform speech-to-text setup and live microphone input in the host CLI.
- Extend project context detection and tune semantic recall using real usage.

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
backend   Go application API
executor  network-isolated worker for approved command processes
database  Postgres schema, learned at runtime
```

## Backend API

The Go backend currently exposes:

```text
GET  /health
GET  /v1/config
GET  /v1/phases
GET  /v1/mocks/capabilities
POST /v1/sessions
GET  /v1/sessions/{session_id}/messages
POST /v1/requests
POST /v1/commands/execute
POST /v1/commands/record
POST /v1/memory/search
POST /v1/memory/embeddings/backfill
GET  /v1/voice/config
POST /v1/voice/transcripts
POST /v1/commands/explain
GET  /v1/context/project
GET  /v1/auth/me
POST /v1/auth/tokens
DELETE /v1/auth/tokens/{token_id}
GET  /v1/workspaces
POST /v1/workspaces
GET  /v1/workspaces/{workspace_id}
PATCH /v1/workspaces/{workspace_id}
POST /v1/workspaces/{workspace_id}/adopt
```

The local default is unauthenticated and intended for one user on the same machine; direct host runs bind to loopback, and Compose publishes the API only on `127.0.0.1`. To exercise bearer authentication, set `TERMIND_BOOTSTRAP_TOKEN` to a unique random secret of at least 32 characters in `.env` and send it as `Authorization: Bearer ...`. Authenticated deployment additionally requires `LOCAL_ONLY_MODE=false`; do not expose it publicly until workspace ownership and permission checkpoints are complete, and put TLS in front of any remote deployment.

Create a persistent API token using the bootstrap token. The returned `secret` is shown only once; Postgres stores its SHA-256 hash:

```bash
curl -sS http://localhost:8000/v1/auth/tokens \
  -H "Authorization: Bearer $TERMIND_BOOTSTRAP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-cli"}'
```

Use the returned token for later `/v1/auth/me` requests, and revoke it with `DELETE /v1/auth/tokens/{token_id}`. The CLI and browser do not yet manage or attach these tokens; that is a later checkpoint.

`POST /v1/commands/execute` returns JSON by default. Send `Accept: text/event-stream` for `output` events followed by a `complete` event. Closing the request cancels the worker process group.

The workspace schema migration is additive and runs once when the backend starts. Existing records are assigned to the reserved `usr_legacy_local` owner, not to the bootstrap identity; back up Postgres before upgrading. Adopt a legacy workspace explicitly with `POST /v1/workspaces/{workspace_id}/adopt`; the authenticated caller becomes its owner, and a duplicate root returns `409`.

## Architecture Snapshot

```text
React frontend ─┐
                ├─> Go API ─> Ollama on host
Host CLI ───────┘        ├─> Postgres
                         └─> private Unix socket ─> isolated executor container

Host CLI execution path:
Host CLI ─> Go API for planning/policy ─> local host shell ─> Go API record endpoint
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

Transcribe a local audio file, confirm the transcript, and route it through the same planner/policy flow:

```bash
../bin/termind -voice-audio ./request.wav
```

With the default `.env.example` voice settings, this command should fail clearly with `voice transcription failed: not_implemented`. That is expected until `VOICE_INPUT_ENABLED=true` and a command STT provider are configured.

Backfill command embeddings:

```bash
../bin/termind -backfill-embeddings -backfill-limit 50
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

## Test Voice End To End

Run the automated CLI voice flow test:

```bash
cd "/Users/harsgupta/Desktop/Code BKP/cli-agent/backend"
go test ./cmd/termind -run TestVoiceAudioWrapperPlansExecutesAndRecordsTranscript -v
```

That test uses a fake API and validates the full CLI path: audio file payload, transcript response, transcript confirmation, `POST /v1/requests`, local command execution, and `POST /v1/commands/record`.

To manually test against the Docker backend without installing Whisper yet, configure a fake command STT provider in `.env`:

```bash
cd "/Users/harsgupta/Desktop/Code BKP/cli-agent"
cp .env.example .env
```

Edit `.env`:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND=printf 'what is my current directory?'
```

Restart the stack:

```bash
/Applications/Docker.app/Contents/Resources/bin/docker compose up -d --build
```

Build and run the CLI voice command:

```bash
cd backend
go build -o ../bin/termind ./cmd/termind
printf 'fake audio' > /tmp/termind-request.wav
../bin/termind -yes -voice-audio /tmp/termind-request.wav
```

Expected flow:

1. CLI posts `/tmp/termind-request.wav` to `POST /v1/voice/transcripts`.
2. Backend runs `VOICE_STT_COMMAND` and returns `what is my current directory?`.
3. CLI asks `Use transcript as request? [Y/n]`; press Enter.
4. CLI sends the transcript through `POST /v1/requests`.
5. CLI runs the approved safe command locally because `-yes` is set.
6. CLI records the result through `POST /v1/commands/record`.

To use the built-in local Whisper adapter instead of the fake command, set:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND=python3 /app/scripts/termind-transcribe-faster-whisper.py {audio}
TERMIND_STT_MODEL=tiny.en
TERMIND_STT_DEVICE=cpu
TERMIND_STT_COMPUTE_TYPE=int8
TERMIND_STT_LANGUAGE=en
```

Then rebuild:

```bash
/Applications/Docker.app/Contents/Resources/bin/docker compose up -d --build
```

The first transcription downloads the Whisper model into the `whisper-models` Docker volume. After that, browser microphone input and `../bin/termind -voice-audio ./request.wav` both use the same local STT path.

## Local LLM Planner

Phase 2 uses Ollama when available:

```bash
brew install ollama
ollama serve
ollama pull llama3.2:3b
ollama pull nomic-embed-text
```

The backend reads:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2:3b
OLLAMA_EMBED_MODEL=nomic-embed-text
PLANNER_MODE=ollama
COMMAND_TIMEOUT_SECONDS=30
```

If Ollama is offline or returns invalid JSON, the backend falls back to the deterministic rule planner.

## Safe Execution

Phase 3 runs approved commands through the Go backend:

- requires `confirmation.status=approved`
- runs with `sh -c` from the requested `cwd`
- delegates process execution to `backend/internal/executor` and risk checks to `backend/internal/policy`
- captures stdout, stderr, exit code, and duration
- streams output to the frontend and terminates the command process group when cancelled
- restricts working directories to `EXECUTION_ALLOWED_ROOTS`, filters inherited child-process environment variables, and caps captured output
- blocks known destructive or privileged patterns such as `rm -rf`, `git reset --hard`, `docker system prune`, and `sudo`
- records execution events into Postgres when available

Docker Compose publishes frontend, backend, and Postgres ports on loopback. The executor has no network, a read-only container filesystem, a writable project mount, no Linux capabilities, no privilege escalation, and CPU, memory, process, timeout, and output limits. The project mount remains writable by approved commands, so review commands before approval and do not expose this local prototype to untrusted users.

To inspect the stream manually, run this in a terminal and press Ctrl+C to cancel:

```bash
curl -N http://localhost:8000/v1/commands/execute \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  --data-binary '{"command":"for i in 1 2 3 4 5 6; do echo tick-$i; sleep 1; done","cwd":"/app","confirmation":{"status":"approved"}}'
```

The response emits `output` events as text arrives and a `complete` event when the command ends. Closing the request cancels the process group.

## Phase Plan

The current flow is:

```text
typed request -> command plan -> policy review -> approval -> safe execution -> remembered event -> semantic recall
```

The frontend shows current phase status, runtime config, remembered command scores, semantic match reasons, and a backfill control.

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

## Current Product Flow

1. User types a terminal request in the frontend or CLI.
2. Client calls `POST /v1/requests`.
3. Backend uses Ollama for a structured command plan, with rule fallback.
4. Backend applies deterministic policy and risk checks.
5. User approves or rejects the proposed command.
6. Frontend calls `POST /v1/commands/execute` and streams output from the backend container; the CLI runs locally on the user's machine.
7. Execution reports stdout, stderr, exit code, duration, and final status.
8. Command events are persisted to Postgres and become searchable.
9. Sessions, projects, and user/assistant messages are persisted to Postgres.
10. Successful commands are learned as project common commands.
11. Project context detects git state and stack hints when available.
12. Memory search blends keyword, semantic, directory, success, and recency signals.
13. New command events are embedded with Ollama when the embedding model is available.
Note: frontend execution runs inside the Docker backend container, so the default web CWD is `/app`. The CLI executes from the real host directory where `termind` is launched.

## Remaining Product Work

- **Execution permissions:** add and enforce per-workspace command permissions and bind approvals to the exact command and workspace.
- **Multi-user product:** add authenticated CLI/frontend credential and workspace flows, user onboarding, and audit retention controls before remote or team deployment.
- **Voice and context:** make host CLI speech-to-text setup cross-platform, add live microphone input, and expand project-specific command detection.
- **Quality:** tune planner and semantic recall against representative user traces, with health checks and useful diagnostics.
