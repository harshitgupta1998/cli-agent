# Termind

Termind is a local-first terminal memory agent. It helps users find, explain, approve, run, and remember terminal commands.

This repository starts as a dockerized product template with:

- React/Vite frontend
- Go backend
- Postgres database
- Ollama-backed command planning
- Controlled backend command execution
- Postgres command-event memory
- Persistent sessions, projects, messages, and learned project commands
- Local semantic recall with Ollama embeddings

## Current Status

Termind has completed **Phase 6: Voice Input**.

Implemented:

- Phase 1: command workbench and API contracts
- Phase 2: local Ollama command planning with rule fallback
- Phase 3: safe backend execution with timeout, output capture, blocking, and audit records
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

Next:

- Production STT setup, executor streaming/cancellation, and richer project context detection

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
```

## Architecture Snapshot

```text
React frontend ─┐
                ├─> Go API ─> Ollama on host
Host CLI ───────┘        ├─> Postgres
                         └─> backend container shell execution

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
- captures stdout, stderr, exit code, and duration
- blocks destructive or privileged patterns such as `rm -rf`, `git reset --hard`, `docker system prune`, and `sudo`
- records execution events into Postgres when available

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
6. Frontend calls `POST /v1/commands/execute` to run inside the backend container, or the CLI runs locally on the user's machine.
7. Execution captures stdout, stderr, exit code, and duration.
8. Command events are persisted to Postgres and become searchable.
9. Sessions, projects, and user/assistant messages are persisted to Postgres.
10. Successful commands are learned as project common commands.
11. Project context detects git state and stack hints when available.
12. New command events are embedded with Ollama.
13. Memory search blends keyword, semantic, directory, success, and recency signals.
12. New command events are embedded locally with Ollama when the embedding model is available.

Note: frontend execution runs inside the Docker backend container, so the default web CWD is `/app`. The CLI executes from the real host directory where `termind` is launched.

## Next Development Steps

- Move policy and execution into dedicated packages.
- Add streaming output and cancellation for backend execution.
- Backfill embeddings for existing command events.
- Add hybrid semantic command retrieval with local Ollama embeddings.
