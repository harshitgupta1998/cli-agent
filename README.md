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

The core local-first product is implemented through **Phase 6: Voice Input** and security checkpoints **S1-S5**. It is ready for local single-user evaluation, but is not yet a production-ready hosted or team product.

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
- S1-S4: bearer auth, owner-scoped workspaces, per-workspace command permissions, and one-time approval binding
- S5: authenticated CLI and browser clients, workspace selection, voice parity, and client-side host CLI policy checks

The host CLI runs commands on the local machine. Its workspace policy checks are client-side guardrails; the browser uses the backend worker for server-enforced permissions and isolation. See [docs/security_model.md](docs/security_model.md) before considering remote use.

## Run Locally

```bash
cp .env.example .env
docker compose up --build
```

On macOS, if Docker reports that `docker-credential-desktop` cannot be found, run Compose with Docker's bundled helpers on `PATH`:

```bash
PATH="/Applications/Docker.app/Contents/Resources/bin:$PATH" \
  /Applications/Docker.app/Contents/Resources/bin/docker compose up --build
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
GET  /v1/workspaces/{workspace_id}/permissions
PUT  /v1/workspaces/{workspace_id}/permissions
```

The local default is unauthenticated and intended for one user on the same machine; direct host runs bind to loopback, and Compose publishes the API only on `127.0.0.1`. To exercise bearer authentication, set `TERMIND_BOOTSTRAP_TOKEN` to a unique random secret of at least 32 characters in `.env` and send it as `Authorization: Bearer ...`. Authenticated deployment additionally requires `LOCAL_ONLY_MODE=false` and a stable random `COMMAND_APPROVAL_SECRET` of at least 32 characters; put TLS in front of any remote deployment.

Create a persistent API token using the bootstrap token. The returned `secret` is shown only once; Postgres stores its SHA-256 hash:

```bash
curl -sS http://localhost:8000/v1/auth/tokens \
  -H "Authorization: Bearer $TERMIND_BOOTSTRAP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-cli"}'
```

Use the returned token for later `/v1/auth/me` requests, and revoke it with `DELETE /v1/auth/tokens/{token_id}`.

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

The Docker backend plans commands and applies policy. The local CLI executes approved commands on your machine. It also fetches workspace permissions and checks the command, working directory, execution mode, timeout, and output bounds before starting the host process. Because this executor runs on the client machine, this check is a client-side guardrail, not a server-enforced sandbox; use the API executor for commands that require server-enforced workspace policy.

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

Authenticated CLI use:

```bash
export TERMIND_API_TOKEN="<token returned by POST /v1/auth/tokens>"
../bin/termind -once "what is using port 8000?"
```

The CLI sends the bearer token on all API requests, including speech transcription. It resolves the current workspace from the session created for the current directory. To choose a particular owned workspace, set `TERMIND_WORKSPACE_ID` or pass `-workspace <workspace-id>`. Workspace IDs can be listed with `GET /v1/workspaces`. The `-yes` flag never skips a workspace's `confirmation_required` policy.

In the browser, paste the API token into the token field in the sidebar. It is kept in `sessionStorage` for that tab, not persisted across browser sessions. Use the workspace selector in the workbench header to switch owned workspaces; typed requests, microphone transcription, planning, memory lookup, execution streams, and cancellation all use the same bearer identity and active workspace.

Do not treat host CLI policy checks as a multi-user security boundary. A local user can execute commands outside Termind, and the remote server cannot sandbox a process started by a client. Remote and shared use should route execution through the backend worker and keep its workspace paths mounted and authorized there.

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

## Closure Readiness

The implementation checkpoints are complete for a local-first alpha. Latest verification: `go test ./...`, `go vet ./...`, frontend typecheck/build, and all 26 Python integration tests passed. A live Compose build and run also succeeded; the frontend planned and streamed `pwd` through the executor, and an authenticated-mode CLI smoke test created and revoked a temporary token.

For a **local single-user release**, the project is close: roughly 85-90% by feature scope. Remaining release work is mostly packaging and polish: cross-platform CLI/STT setup, clearer token lifecycle controls, audit retention, operational diagnostics, and testing on a clean install.

For a **hosted or multi-user release**, it is materially farther along, roughly 60-70%. The main architecture decision is command execution from a user's host: current CLI policy checks cannot be enforced by the server. A hosted product needs a trusted local agent/worker enrollment model or must limit command execution to server-managed workspaces. It also needs a production token/session UX, explicit workspace sharing/roles if teams are supported, deployment monitoring and retention, and security testing of the remote configuration.

Suggested final checkpoints:

1. Choose and document the release target: local-only single-user or hosted/team use.
2. If hosted, design and implement a trusted host-agent execution protocol before claiming workspace policy is enforceable for CLI commands.
3. Finish user-facing token lifecycle and workspace onboarding for the chosen target.
4. Add CI and release packaging, then validate a clean install and backup/restore/migration procedure.
