# Termind Backend

Go API service for the Termind product template.

Current backend status: **Phase 6.1: Voice Input contracts**.

Implemented backend capabilities:

- Ollama-backed command planning with rule fallback
- deterministic policy review and out-of-scope request gating
- approved command execution with timeout and blocking
- Postgres-backed sessions, projects, messages, command events, and learned project commands
- Ollama-backed command-event embeddings, backfill, semantic search, and hybrid memory ranking
- voice runtime config and placeholder transcript contracts
- command-based local speech-to-text adapter for `VOICE_STT_PROVIDER=command`

## Run

```bash
go run ./cmd/server
```

## CLI

Build:

```bash
go build -o ../bin/termind ./cmd/termind
```

Run against the local API:

```bash
../bin/termind
../bin/termind -once "what is using port 8000?"
```

The CLI asks the Go API for planning and policy, then executes approved commands locally from your current working directory.

Backfill missing command-event embeddings:

```bash
../bin/termind -backfill-embeddings -backfill-limit 50
```

## Local LLM Planning

The API uses Ollama for Phase 2 command planning when configured:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2:3b
OLLAMA_EMBED_MODEL=nomic-embed-text
PLANNER_MODE=ollama
COMMAND_TIMEOUT_SECONDS=30
VOICE_INPUT_ENABLED=false
VOICE_STT_PROVIDER=disabled
VOICE_STT_COMMAND=
VOICE_MAX_AUDIO_BYTES=5242880
```

`internal/planner` contains the Ollama planner and deterministic fallback planner.
`internal/embeddings` contains the Ollama embedding client used by Phase 5 semantic recall work.

## Safe Execution

`POST /v1/commands/execute` runs approved commands through the backend process runner. It captures stdout, stderr, exit code, and duration, applies a small destructive-command denylist, and records command events to Postgres when the store is available.

When the API is running in Docker, commands execute inside the backend container. Use `/app` as the frontend/default CWD for container execution. The CLI path is different: it gets plans from the API, then executes approved commands on the host from the directory where `termind` was launched.

## Test

```bash
go test ./...
```

The repository also includes Python integration tests for the running HTTP API:

```bash
cd ..
pytest
```

## Current Packages

```text
cmd/server          process entrypoint
internal/api        HTTP routing, CORS, validation, JSON helpers
internal/config     environment configuration
internal/models     request/response contracts
internal/planner    Ollama and rule planners
internal/services   agent service, policy, execution, roadmap metadata
```

## Persistent Memory

`POST /v1/sessions` now creates real Postgres session rows and creates or reuses a project by root path. Command records attach to the session's project, successful commands update `project_commands`, and memory search returns persisted command events rather than service-level fake results.

`GET /v1/sessions/{session_id}/messages` returns persisted user and assistant messages for a session.

`GET /v1/context/project` now returns the stored project ID for a CWD, learned project commands, lightweight git status when available, and stack hints from files such as `go.mod`, `package.json`, `pyproject.toml`, and `requirements.txt`.

## Semantic Recall

Phase 5 stores embeddings for new command events in `command_embeddings`, can backfill older command events through `POST /v1/memory/embeddings/backfill`, and uses semantic recall in `POST /v1/memory/search`.

Search ranking blends keyword match, semantic similarity, same-directory context, successful command history, and recency. Recording still succeeds when embedding generation fails, so missing local models do not break command memory.

## Phase Endpoints

```text
GET /v1/phases
GET /v1/mocks/capabilities
GET /v1/voice/config
POST /v1/voice/transcripts
```

These endpoints make the roadmap executable in the app. Phase 6.2 is the current working product surface; transcript hardening and microphone UI are the next implementation layers.

## Voice Input

Phase 6.2 supports a command-based local STT adapter. Set:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND='path/to/stt-command {audio}'
```

The backend writes the submitted audio to a temporary local file and replaces `{audio}` with that file path. The command must print the transcript to stdout. This is designed to wrap tools such as `whisper.cpp` without making that binary a required dependency yet.

The transcript endpoint validates MIME type, base64 audio, and `VOICE_MAX_AUDIO_BYTES` before invoking the local command.
The frontend can now capture microphone audio, submit it for transcription, and expose the transcript as editable prompt text before planning.

The next real implementation layers should be:

- `internal/policy` for deterministic command risk evaluation
- `internal/executor` for streaming and cancellation around the current process runner
- expanded `internal/memory` queries and cleanup tools
- richer `internal/context` detection from manifests and package scripts
- confirmed voice-to-planner flow polish
