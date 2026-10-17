INSERT INTO users(id, display_name, status)
VALUES ('usr_legacy_local', 'Legacy Local Owner', 'active')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(owner_user_id, root_path)
);

CREATE TABLE IF NOT EXISTS workspace_permissions (
  workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  policy_version INTEGER NOT NULL DEFAULT 1,
  execution_mode TEXT NOT NULL DEFAULT 'confirmation_required'
    CHECK (execution_mode IN ('disabled', 'confirmation_required', 'restricted_safe_autocommand')),
  writable_roots_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  allowed_commands_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  denied_commands_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  max_duration_seconds INTEGER NOT NULL DEFAULT 30 CHECK (max_duration_seconds > 0),
  max_output_bytes INTEGER NOT NULL DEFAULT 1048576 CHECK (max_output_bytes > 0),
  updated_by_user_id TEXT REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS workspace_id TEXT REFERENCES workspaces(id);
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS workspace_id TEXT REFERENCES workspaces(id);
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS workspace_id TEXT REFERENCES workspaces(id);
ALTER TABLE command_events ADD COLUMN IF NOT EXISTS workspace_id TEXT REFERENCES workspaces(id);
ALTER TABLE command_events ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id);
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id);
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS workspace_id TEXT REFERENCES workspaces(id);

INSERT INTO workspaces(id, owner_user_id, name, root_path)
SELECT
  'wsp_legacy_' || substr(md5(root_path), 1, 20),
  'usr_legacy_local',
  COALESCE(NULLIF(name, ''), 'Legacy workspace'),
  root_path
FROM projects
ON CONFLICT (owner_user_id, root_path) DO NOTHING;

INSERT INTO workspaces(id, owner_user_id, name, root_path)
SELECT DISTINCT
  'wsp_legacy_' || substr(md5(s.cwd), 1, 20),
  'usr_legacy_local',
  COALESCE(NULLIF(regexp_replace(s.cwd, '^.*/', ''), ''), 'Legacy workspace'),
  s.cwd
FROM sessions s
ON CONFLICT (owner_user_id, root_path) DO NOTHING;

UPDATE projects p
SET workspace_id = w.id
FROM workspaces w
WHERE p.workspace_id IS NULL
  AND w.owner_user_id = 'usr_legacy_local'
  AND w.root_path = p.root_path;

UPDATE sessions s
SET workspace_id = w.id, owner_user_id = 'usr_legacy_local'
FROM workspaces w
WHERE s.workspace_id IS NULL
  AND w.owner_user_id = 'usr_legacy_local'
  AND w.root_path = COALESCE(
    (SELECT p.root_path FROM projects p WHERE p.id = s.project_id),
    s.cwd
  );

UPDATE messages m
SET workspace_id = s.workspace_id
FROM sessions s
WHERE m.workspace_id IS NULL AND s.id = m.session_id;

UPDATE command_events ce
SET workspace_id = s.workspace_id, owner_user_id = s.owner_user_id
FROM sessions s
WHERE ce.workspace_id IS NULL AND s.id = ce.session_id;

UPDATE user_preferences
SET owner_user_id = 'usr_legacy_local'
WHERE owner_user_id IS NULL;

INSERT INTO workspace_permissions(workspace_id)
SELECT w.id
FROM workspaces w
LEFT JOIN workspace_permissions p ON p.workspace_id = w.id
WHERE p.workspace_id IS NULL;

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_root_path_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_workspace_root
  ON projects(workspace_id, root_path)
  WHERE workspace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_workspaces_owner ON workspaces(owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_owner_workspace ON sessions(owner_user_id, workspace_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_events_owner_workspace ON command_events(owner_user_id, workspace_id, started_at DESC);
