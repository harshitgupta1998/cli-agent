CREATE TABLE IF NOT EXISTS command_approvals (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT NOT NULL REFERENCES users(id),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  session_id TEXT,
  request_id TEXT NOT NULL,
  policy_version INTEGER NOT NULL DEFAULT 1,
  command_hash BYTEA NOT NULL,
  cwd TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_command_approvals_expiry ON command_approvals(expires_at);
