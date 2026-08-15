CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL UNIQUE,
  repo_url_redacted TEXT,
  default_branch TEXT,
  detected_stack_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id),
  cwd TEXT NOT NULL,
  shell TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at TIMESTAMPTZ,
  summary TEXT
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS command_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  project_id TEXT REFERENCES projects(id),
  user_request TEXT NOT NULL,
  proposed_command TEXT,
  final_command TEXT NOT NULL,
  cwd TEXT NOT NULL,
  shell TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  required_confirmation BOOLEAN NOT NULL DEFAULT true,
  confirmation_status TEXT NOT NULL CHECK (
    confirmation_status IN ('not_required', 'approved', 'rejected', 'edited')
  ),
  exit_code INTEGER,
  stdout_summary TEXT,
  stderr_summary TEXT,
  duration_ms INTEGER,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at TIMESTAMPTZ,
  tags_json JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS command_embeddings (
  command_event_id TEXT PRIMARY KEY REFERENCES command_events(id),
  embedding_model TEXT NOT NULL,
  embedding BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_commands (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  command TEXT NOT NULL,
  label TEXT NOT NULL,
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_success_at TIMESTAMPTZ,
  UNIQUE(project_id, command)
);

CREATE TABLE IF NOT EXISTS user_preferences (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  value_json JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_command_events_project_id ON command_events(project_id);
CREATE INDEX IF NOT EXISTS idx_command_events_session_id ON command_events(session_id);
CREATE INDEX IF NOT EXISTS idx_command_events_started_at ON command_events(started_at DESC);

