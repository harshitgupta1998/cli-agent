# Local Terminal Agent - System Design

## 1. Purpose

Build a local-first terminal assistant that converts natural-language requests into safe, reviewable terminal actions, remembers command history as structured events, and helps the user retrieve and reuse useful commands later.

The product should not be framed as only "talk to your terminal." The stronger product is:

A terminal assistant that privately remembers how you work.

## 2. Goals

- Accept typed natural-language requests in a CLI/TUI.
- Generate command plans using a local LLM through Ollama.
- Keep execution controlled by the application, not the LLM.
- Require confirmation for risky, modifying, destructive, privileged, or ambiguous commands.
- Store command events, conversations, project context, and execution summaries locally.
- Support exact and semantic retrieval of prior commands.
- Keep data local by default: local model, local database, local embeddings, no telemetry.
- Add voice input later without changing the core execution pipeline.

## 3. Non-Goals for V1

- Fully autonomous multi-step task execution without user confirmation.
- Browser or desktop automation.
- Cloud sync.
- Team sharing.
- Rich web dashboard.
- Full shell replacement.
- Voice-first UX.

## 4. High-Level Architecture

```mermaid
flowchart TD
    User["User"]
    CLI["CLI / TUI"]
    Voice["Voice Input - Future"]
    STT["Local Speech-to-Text - Future"]
    Router["Intent Router"]
    Context["Context Collector"]
    Retrieval["Memory Retrieval"]
    Planner["LLM Planner - Ollama"]
    Policy["Policy and Risk Engine"]
    Confirm["User Confirmation"]
    Executor["Command Executor"]
    Summarizer["Result Summarizer"]
    Redactor["Privacy / Redaction Layer"]
    DB[("SQLite + FTS5")]
    Embeddings["Local Embeddings - Ollama"]

    User --> CLI
    User --> Voice
    Voice --> STT
    STT --> CLI

    CLI --> Router
    Router --> Context
    Router --> Retrieval
    Context --> Planner
    Retrieval --> Planner
    Planner --> Policy
    Policy --> Confirm
    Confirm --> Executor
    Executor --> Summarizer
    Summarizer --> Redactor
    Redactor --> DB
    DB --> Retrieval
    Summarizer --> Embeddings
    Embeddings --> DB
```

## 5. Core Principle

The LLM must never directly execute shell commands.

The LLM may produce:

- intent classification
- command proposal
- explanation
- provenance
- risk estimate
- structured plan

The application owns:

- policy enforcement
- confirmation rules
- command execution
- process control
- output capture
- memory writes
- redaction
- audit trail

## 6. Request Lifecycle

```mermaid
sequenceDiagram
    participant U as User
    participant UI as CLI/TUI
    participant R as Intent Router
    participant C as Context Collector
    participant M as Memory Retrieval
    participant L as Ollama Planner
    participant P as Policy Engine
    participant X as Executor
    participant DB as SQLite Memory

    U->>UI: "What is using port 8000?"
    UI->>R: Create user request
    R->>C: Collect cwd, OS, shell, git, project hints
    R->>M: Search relevant prior commands
    C-->>L: Environment context
    M-->>L: Related command memories
    L-->>P: Structured CommandPlan
    P-->>UI: Policy-reviewed plan
    UI->>U: Show command and ask confirmation
    U->>UI: Confirm
    UI->>X: Execute approved command
    X-->>UI: Stream stdout/stderr
    X-->>DB: Store command event
    DB-->>M: Command becomes searchable
```

## 7. Main Components

### 7.1 CLI / TUI

Primary user interface for V1.

Responsibilities:

- capture user input
- render proposed command plans
- ask for confirmation
- stream command output
- show retrieved command candidates
- allow command edits before execution
- support cancellation for long-running commands

Example:

```text
Termind > what is using port 8000?

Proposed command:
  lsof -i :8000

Reason:
  Lists processes listening on port 8000.

Risk:
  safe

Run? [Y/n/edit]
```

### 7.2 Intent Router

Classifies user requests before planning.

Common intents:

- `execute_command`
- `search_history`
- `explain_command`
- `repeat_command`
- `summarize_session`
- `set_preference`
- `open_project`
- `unknown`

V1 can use simple heuristics plus LLM fallback. Over time this can become a structured LLM call.

### 7.3 Context Collector

Collects non-sensitive local context that helps produce better command plans.

Context examples:

- current working directory
- OS and shell
- git repository root
- git branch
- remote origin URL, optionally redacted
- package manager hints: `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`
- stack hints: `pyproject.toml`, `requirements.txt`, `go.mod`, `Cargo.toml`, `Dockerfile`
- project-specific successful commands
- recent command failures in the same project

### 7.4 LLM Planner

Uses Ollama to convert request + context + retrieved memories into a structured plan.

The planner returns JSON matching the `CommandPlan` schema.

Example:

```json
{
  "intent": "execute_command",
  "command": "lsof -i :8000",
  "cwd": "/Users/example/projects/api",
  "risk": "safe",
  "requires_confirmation": true,
  "reason": "Lists processes listening on port 8000.",
  "provenance": [
    "The user asked what is using port 8000.",
    "The command is read-only.",
    "lsof is available on macOS."
  ],
  "alternatives": [
    {
      "command": "netstat -vanp tcp | grep 8000",
      "reason": "Alternative network socket inspection command."
    }
  ]
}
```

### 7.5 Policy and Risk Engine

Deterministic gatekeeper.

The LLM risk estimate is advisory. The policy engine computes the final risk level.

Risk levels:

- `safe`: read-only commands like `pwd`, `ls`, `git status`, `lsof`
- `modifying`: filesystem or dependency changes like `mkdir`, `touch`, `npm install`
- `destructive`: data deletion/reset like `rm`, `git reset --hard`, `docker system prune`
- `privileged`: `sudo`, ownership changes, system service changes
- `networked`: commands that send data or touch remote resources
- `unknown`: commands that cannot be confidently classified

Policy examples:

```text
if command contains "sudo":
  risk = privileged
  confirmation = required

if command matches "rm -rf" or "git reset --hard":
  risk = destructive
  confirmation = required

if cwd is "/" or user home:
  confirmation = required

if command writes outside allowed project roots:
  confirmation = required
```

### 7.6 Command Executor

Runs only approved commands.

Responsibilities:

- execute in selected working directory
- stream stdout/stderr
- capture exit code
- support timeout
- support cancellation
- record duration
- optionally support PTY for interactive commands

V1 should support non-interactive commands first. PTY support can be added in V2.

### 7.7 Memory Store

Stores events, not only conversation transcripts.

The important event is:

```text
user intent
proposed command
final command
confirmation
execution result
project context
summary
retrieval metadata
```

SQLite is enough for V1:

- structured relational tables
- FTS5 for full-text search
- local file-based persistence
- simple backups

### 7.8 Retrieval Layer

Searches previous commands using structured ranking.

Ranking inputs:

- keyword match
- semantic similarity
- same project
- same working directory
- recency
- successful exit code
- user reused command before

Example scoring:

```text
score =
  0.35 semantic_similarity
+ 0.25 keyword_match
+ 0.15 same_project
+ 0.10 same_directory
+ 0.10 recency
+ 0.05 success_bonus
```

### 7.9 Privacy and Redaction

All sensitive memory writes pass through redaction.

Redact:

- API keys
- access tokens
- private keys
- `.env` values
- passwords
- URLs with embedded credentials
- AWS credentials
- GitHub tokens
- SSH private key material

Privacy modes:

```text
local_only:
  LLM: local Ollama
  embeddings: local Ollama
  database: local SQLite
  telemetry: disabled
  cloud APIs: disabled

hybrid_optional_future:
  cloud LLM allowed only after explicit user opt-in
```

## 8. Data Model

```mermaid
erDiagram
    PROJECTS ||--o{ SESSIONS : has
    SESSIONS ||--o{ MESSAGES : contains
    SESSIONS ||--o{ COMMAND_EVENTS : records
    COMMAND_EVENTS ||--o{ COMMAND_ARTIFACTS : produces
    COMMAND_EVENTS ||--o| COMMAND_EMBEDDINGS : has
    PROJECTS ||--o{ PROJECT_COMMANDS : learns
    USER_PREFERENCES ||--o{ PROJECTS : influences

    PROJECTS {
        string id PK
        string name
        string root_path
        string repo_url_redacted
        string default_branch
        string detected_stack_json
        datetime created_at
        datetime updated_at
    }

    SESSIONS {
        string id PK
        string project_id FK
        string cwd
        string shell
        datetime started_at
        datetime ended_at
        string summary
    }

    MESSAGES {
        string id PK
        string session_id FK
        string role
        text content
        datetime created_at
    }

    COMMAND_EVENTS {
        string id PK
        string session_id FK
        string project_id FK
        text user_request
        text proposed_command
        text final_command
        string cwd
        string shell
        string risk_level
        boolean required_confirmation
        string confirmation_status
        integer exit_code
        text stdout_summary
        text stderr_summary
        integer duration_ms
        datetime started_at
        datetime ended_at
        string tags_json
    }

    COMMAND_ARTIFACTS {
        string id PK
        string command_event_id FK
        string artifact_type
        string path
        string summary
        datetime created_at
    }

    COMMAND_EMBEDDINGS {
        string command_event_id PK
        string embedding_model
        blob embedding
        datetime created_at
    }

    PROJECT_COMMANDS {
        string id PK
        string project_id FK
        text command
        string label
        integer success_count
        integer failure_count
        datetime last_success_at
    }

    USER_PREFERENCES {
        string id PK
        string key
        string value_json
        datetime updated_at
    }
```

### 8.1 Tables

#### `projects`

Represents a detected workspace or repository.

```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL UNIQUE,
  repo_url_redacted TEXT,
  default_branch TEXT,
  detected_stack_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

#### `sessions`

Represents a conversation or terminal work session.

```sql
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id),
  cwd TEXT NOT NULL,
  shell TEXT NOT NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  summary TEXT
);
```

#### `messages`

Stores chat-like user and assistant messages.

```sql
CREATE TABLE messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
  content TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

#### `command_events`

The central memory object.

```sql
CREATE TABLE command_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  project_id TEXT REFERENCES projects(id),
  user_request TEXT NOT NULL,
  proposed_command TEXT,
  final_command TEXT NOT NULL,
  cwd TEXT NOT NULL,
  shell TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  required_confirmation INTEGER NOT NULL DEFAULT 1,
  confirmation_status TEXT NOT NULL CHECK (
    confirmation_status IN ('not_required', 'approved', 'rejected', 'edited')
  ),
  exit_code INTEGER,
  stdout_summary TEXT,
  stderr_summary TEXT,
  duration_ms INTEGER,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  tags_json TEXT NOT NULL DEFAULT '[]'
);
```

#### `command_events_fts`

Full-text index for command lookup.

```sql
CREATE VIRTUAL TABLE command_events_fts USING fts5(
  user_request,
  final_command,
  stdout_summary,
  stderr_summary,
  tags,
  content='command_events',
  content_rowid='rowid'
);
```

#### `command_embeddings`

Optional V2 semantic retrieval.

```sql
CREATE TABLE command_embeddings (
  command_event_id TEXT PRIMARY KEY REFERENCES command_events(id),
  embedding_model TEXT NOT NULL,
  embedding BLOB NOT NULL,
  created_at TEXT NOT NULL
);
```

#### `project_commands`

Stores learned command shortcuts per project.

```sql
CREATE TABLE project_commands (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  command TEXT NOT NULL,
  label TEXT NOT NULL,
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_success_at TEXT,
  UNIQUE(project_id, command)
);
```

#### `user_preferences`

Stores user-level behavior preferences.

```sql
CREATE TABLE user_preferences (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

## 9. API Design

The CLI can call Python services directly in-process for V1. These API contracts still define clear module boundaries and allow a future local HTTP daemon.

Base URL for future local daemon:

```text
http://127.0.0.1:48231
```

### 9.1 Create Session

```http
POST /v1/sessions
```

Request:

```json
{
  "cwd": "/Users/example/projects/api",
  "shell": "zsh"
}
```

Response:

```json
{
  "session_id": "ses_01JABC",
  "project_id": "prj_01JXYZ",
  "started_at": "2026-08-15T15:00:00Z"
}
```

### 9.2 Submit User Request

```http
POST /v1/requests
```

Request:

```json
{
  "session_id": "ses_01JABC",
  "input": "what is using port 8000?",
  "cwd": "/Users/example/projects/api"
}
```

Response:

```json
{
  "request_id": "req_01J123",
  "intent": "execute_command",
  "plan": {
    "command": "lsof -i :8000",
    "cwd": "/Users/example/projects/api",
    "risk": "safe",
    "requires_confirmation": true,
    "reason": "Lists processes listening on port 8000.",
    "provenance": [
      "The user asked what is using port 8000.",
      "The command is read-only."
    ]
  },
  "policy": {
    "risk": "safe",
    "requires_confirmation": true,
    "warnings": []
  }
}
```

### 9.3 Execute Approved Command

```http
POST /v1/commands/execute
```

Request:

```json
{
  "session_id": "ses_01JABC",
  "request_id": "req_01J123",
  "command": "lsof -i :8000",
  "cwd": "/Users/example/projects/api",
  "confirmation": {
    "status": "approved",
    "approved_at": "2026-08-15T15:01:00Z"
  }
}
```

Response:

```json
{
  "command_event_id": "cmd_01J789",
  "status": "completed",
  "exit_code": 0,
  "stdout": "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\n...",
  "stderr": "",
  "duration_ms": 238
}
```

For streaming output, use server-sent events or newline-delimited JSON:

```http
GET /v1/commands/{command_event_id}/stream
```

Event:

```json
{
  "type": "stdout",
  "data": "python3 12345 user ... TCP *:8000 (LISTEN)"
}
```

### 9.4 Search Command Memory

```http
POST /v1/memory/search
```

Request:

```json
{
  "query": "command to kill FastAPI process",
  "project_id": "prj_01JXYZ",
  "cwd": "/Users/example/projects/api",
  "limit": 5
}
```

Response:

```json
{
  "results": [
    {
      "command_event_id": "cmd_01J789",
      "command": "lsof -ti :8000 | xargs kill",
      "user_request": "kill the FastAPI process",
      "cwd": "/Users/example/projects/api",
      "exit_code": 0,
      "score": 0.91,
      "matched_reasons": [
        "same project",
        "semantic match",
        "successful command"
      ],
      "last_used_at": "2026-08-14T19:20:00Z"
    }
  ]
}
```

### 9.5 Explain Command

```http
POST /v1/commands/explain
```

Request:

```json
{
  "command": "lsof -ti :8000 | xargs kill",
  "cwd": "/Users/example/projects/api"
}
```

Response:

```json
{
  "summary": "Finds process IDs listening on port 8000 and sends them to kill.",
  "risk": "modifying",
  "warnings": [
    "This terminates running processes.",
    "It may kill more than one process if multiple match the port."
  ]
}
```

### 9.6 Get Project Context

```http
GET /v1/context/project?cwd=/Users/example/projects/api
```

Response:

```json
{
  "project_id": "prj_01JXYZ",
  "root_path": "/Users/example/projects/api",
  "git": {
    "branch": "main",
    "has_uncommitted_changes": true
  },
  "detected_stack": {
    "language": "python",
    "framework": "fastapi",
    "package_manager": "uv"
  },
  "common_commands": [
    {
      "label": "Run dev server",
      "command": "uvicorn app.main:app --reload",
      "success_count": 8
    }
  ]
}
```

### 9.7 Update Preference

```http
PUT /v1/preferences/{key}
```

Request:

```json
{
  "value": {
    "package_manager": "pnpm"
  }
}
```

Response:

```json
{
  "key": "javascript_defaults",
  "value": {
    "package_manager": "pnpm"
  },
  "updated_at": "2026-08-15T15:10:00Z"
}
```

## 10. Internal Service Interfaces

### 10.1 `Planner`

```python
class Planner:
    def create_plan(
        self,
        user_input: str,
        context: ProjectContext,
        memories: list[CommandMemory],
    ) -> CommandPlan:
        ...
```

### 10.2 `PolicyEngine`

```python
class PolicyEngine:
    def evaluate(self, plan: CommandPlan) -> PolicyDecision:
        ...
```

### 10.3 `Executor`

```python
class Executor:
    def run(
        self,
        command: str,
        cwd: str,
        timeout_seconds: int | None = None,
    ) -> ExecutionResult:
        ...
```

### 10.4 `MemoryRepository`

```python
class MemoryRepository:
    def save_command_event(self, event: CommandEvent) -> str:
        ...

    def search_commands(
        self,
        query: str,
        project_id: str | None,
        cwd: str | None,
        limit: int,
    ) -> list[CommandSearchResult]:
        ...
```

## 11. Suggested Project Structure

```text
cli-agent/
  termind/
    cli/
      app.py
      renderer.py

    agent/
      planner.py
      prompts.py
      schemas.py

    context/
      collector.py
      git.py
      project.py
      shell.py

    execution/
      executor.py
      policy.py
      risk.py

    memory/
      database.py
      migrations.py
      retrieval.py
      embeddings.py

    privacy/
      redaction.py
      filters.py

    voice/
      stt.py
      microphone.py

    config/
      settings.py

  tests/
  pyproject.toml
  README.md
```

## 12. V1 Build Plan

### Milestone 1: Local Command Planner

- CLI prompt loop
- Ollama client
- structured `CommandPlan` schema
- simple context collector
- plan rendering

### Milestone 2: Safe Execution

- deterministic policy engine
- confirmation prompt
- command executor
- stdout/stderr capture
- cancellation and timeout

### Milestone 3: Memory

- SQLite schema
- command event persistence
- session persistence
- FTS5 search
- command search UI

### Milestone 4: Retrieval-Augmented Planning

- retrieve recent and relevant commands before planning
- add project command memory
- show provenance for suggestions

### Milestone 5: Semantic Search

- local embeddings through Ollama
- embedding storage
- hybrid ranking

### Milestone 6: Voice Input

- local speech-to-text
- microphone input
- same request pipeline as typed input

## 13. Open Design Decisions

- Should V1 be pure CLI or use a richer terminal UI library like Textual?
- Should the app run as a short-lived CLI process or a persistent local daemon?
- Should full stdout/stderr be stored, or only redacted summaries by default?
- Which commands are allowed to bypass confirmation?
- Should users be able to define per-project command aliases?
- Should memory be encrypted at rest?
- Should project detection be path-based, git-root-based, or both?

## 14. Recommended V1 Architecture Choice

Use a simple in-process Python CLI first:

```text
CLI process
  -> Planner service
  -> Policy service
  -> Executor service
  -> SQLite memory repository
```

Avoid a local HTTP server until there is a second client, such as a desktop UI, web UI, or shell integration.

This keeps the system easy to debug while preserving clean boundaries for a future daemon.

