# Termind Backend

Go API service for the Termind product template.

Current backend status: **Phase 6: Voice Input complete**.

Authentication checkpoint S2 adds bearer-token middleware and API-token lifecycle endpoints. Local mode remains anonymous and loopback-only by default; authenticated mode requires a bootstrap token and Postgres. Workspace ownership and per-workspace permissions are still pending.

Implemented backend capabilities:

- Ollama-backed command planning with rule fallback
- deterministic policy review and out-of-scope request gating
- approved command execution with streaming output, cancellation, timeout, and policy blocking
- a network-isolated Docker worker with configured working-directory roots, child environment allowlists, and bounded output capture
- Postgres-backed sessions, projects, messages, command events, and learned project commands
- Ollama-backed command-event embeddings, backfill, semantic search, and hybrid memory ranking
- hardened voice transcript API and runtime configuration
- command-based local speech-to-text adapter for `VOICE_STT_PROVIDER=command`
- optional bundled `faster-whisper` adapter for local transcription in Docker
- confirmed voice transcripts route through the same planning and policy path as typed prompts
- CLI audio-file voice wrapper through `termind -voice-audio`

## Run

```bash
cd ..
docker compose up --build
```

For direct local API execution without Docker, start the worker in one terminal. Both processes must use the same socket path and allowed workspace root:

```bash
cd backend
EXECUTOR_SOCKET_PATH=/tmp/termind-executor.sock EXECUTION_ALLOWED_ROOTS="$PWD" go run ./cmd/executor-worker
```

Then start the API in another terminal from `backend/`:

```bash
EXECUTOR_SOCKET_PATH=/tmp/termind-executor.sock EXECUTION_ALLOWED_ROOTS="$PWD" go run ./cmd/server
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
COMMAND_MAX_OUTPUT_BYTES=1048576
EXECUTION_ALLOWED_ROOTS=/app
EXECUTION_ENV_ALLOWLIST=PATH,HOME,LANG,LC_ALL,TERM,TMPDIR,GOPATH,GOCACHE,GOMODCACHE,NPM_CONFIG_CACHE
VOICE_INPUT_ENABLED=false
VOICE_STT_PROVIDER=disabled
VOICE_STT_COMMAND=
VOICE_MAX_AUDIO_BYTES=5242880
```

`internal/planner` contains the Ollama planner and deterministic fallback planner.
`internal/embeddings` contains the Ollama embedding client used by Phase 5 semantic recall work.

## Safe Execution

`POST /v1/commands/execute` runs approved commands through `internal/executor`. Clients that request `Accept: text/event-stream` receive stdout/stderr chunks followed by a final result event; disconnecting the request cancels the process group. Other clients receive the existing JSON response. The runner enforces `COMMAND_TIMEOUT_SECONDS`, `COMMAND_MAX_OUTPUT_BYTES`, `EXECUTION_ALLOWED_ROOTS`, and `EXECUTION_ENV_ALLOWLIST`, applies deterministic blocking through `internal/policy`, and records command events to Postgres when the store is available.

When the API is running in Docker, commands execute in a separate worker container with no network, a read-only container filesystem, and `/app` mounted as the writable project workspace. The host CLI path is different: it gets plans from the API, then executes approved commands on the host from the directory where `termind` was launched. The writable workspace is shared with approved commands; authentication and multi-user isolation are not implemented.

## Test

```bash
go test ./...
```

The repository also includes Python integration tests for the running HTTP API:

```bash
cd ..
pytest
```

Run the CLI voice end-to-end test:

```bash
go test ./cmd/termind -run TestVoiceAudioWrapperPlansExecutesAndRecordsTranscript -v
```

## Current Packages

```text
cmd/server          API process entrypoint
cmd/executor-worker isolated command worker entrypoint
internal/api        HTTP routing, CORS, validation, JSON helpers
internal/config     environment configuration
internal/executor   command runner, SSE streaming, and private Unix-socket worker client
internal/models     request/response contracts
internal/policy     deterministic command risk evaluation and execution blocking
internal/planner    Ollama and rule planners
internal/services   agent orchestration, persistence coordination, roadmap metadata
```

## Persistent Memory

`POST /v1/sessions` now creates real Postgres session rows and creates or reuses a project by root path. Command records attach to the session's project, successful commands update `project_commands`, and memory search returns persisted command events rather than service-level fake results.

`GET /v1/sessions/{session_id}/messages` returns persisted user and assistant messages for a session.

`GET /v1/context/project` now returns the stored project ID for a CWD, learned project commands, manifest-detected commands, lightweight git status when available, and stack hints from files such as `go.mod`, `package.json`, `pyproject.toml`, and `requirements.txt`.

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

These endpoints make the roadmap executable in the app. Phase 6 is the current working product surface; local STT packaging and live CLI recording are the next implementation layers.

## Voice Input

Phase 6.2 supports a command-based local STT adapter. Set:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND='path/to/stt-command {audio}'
```

The backend writes the submitted audio to a temporary local file and replaces `{audio}` with that file path. The command must print the transcript to stdout. This is designed to wrap tools such as `whisper.cpp` without making that binary a required dependency yet.

The transcript endpoint validates MIME type, base64 audio, and `VOICE_MAX_AUDIO_BYTES` before invoking the local command.
The frontend can now capture microphone audio, submit it for transcription, expose the transcript as editable prompt text, and send the confirmed transcript through the existing request planner. The host CLI can also send a local audio file with `termind -voice-audio ./request.wav`, confirm the returned transcript, and reuse the same local planning and execution path.

The backend image includes an optional local Whisper adapter based on `faster-whisper` and `ffmpeg`. Enable it with:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND=python3 /app/scripts/termind-transcribe-faster-whisper.py {audio}
TERMIND_STT_MODEL=tiny.en
TERMIND_STT_MODEL_DIR=/models/whisper
TERMIND_STT_DEVICE=cpu
TERMIND_STT_COMPUTE_TYPE=int8
TERMIND_STT_LANGUAGE=en
```

`docker-compose.yml` mounts `/models/whisper` as the `whisper-models` volume so the model is downloaded once and reused across backend restarts.

Manual Docker-backed voice smoke test without Whisper:

```text
VOICE_INPUT_ENABLED=true
VOICE_STT_PROVIDER=command
VOICE_STT_COMMAND=printf 'what is my current directory?'
```

```bash
cd ..
/Applications/Docker.app/Contents/Resources/bin/docker compose up -d --build
cd backend
go build -o ../bin/termind ./cmd/termind
printf 'fake audio' > /tmp/termind-request.wav
../bin/termind -yes -voice-audio /tmp/termind-request.wav
```

Next implementation priorities:

- add authentication, workspace ownership, and audit retention before multi-user deployment
- add per-workspace command permissions and a stronger approval policy
- add live microphone input and cross-platform STT packaging for the host CLI
- expand project-specific context detection and memory management
