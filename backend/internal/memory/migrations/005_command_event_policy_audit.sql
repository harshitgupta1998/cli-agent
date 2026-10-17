ALTER TABLE command_events
  ADD COLUMN IF NOT EXISTS workspace_policy_version INTEGER,
  ADD COLUMN IF NOT EXISTS workspace_policy_decision TEXT;
