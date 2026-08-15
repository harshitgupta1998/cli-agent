INSERT INTO projects (
  id,
  name,
  root_path,
  repo_url_redacted,
  default_branch,
  detected_stack_json
) VALUES (
  'prj_demo',
  'Termind API',
  '/Users/example/projects/termind',
  'github.com/example/termind',
  'main',
  '{"language":"go","framework":"net/http","package_manager":"go modules"}'
) ON CONFLICT (id) DO NOTHING;

INSERT INTO sessions (
  id,
  project_id,
  cwd,
  shell,
  summary
) VALUES (
  'ses_demo',
  'prj_demo',
  '/Users/example/projects/termind',
  'zsh',
  'Demo session with remembered Go backend commands.'
) ON CONFLICT (id) DO NOTHING;

INSERT INTO command_events (
  id,
  session_id,
  project_id,
  user_request,
  proposed_command,
  final_command,
  cwd,
  shell,
  risk_level,
  required_confirmation,
  confirmation_status,
  exit_code,
  stdout_summary,
  stderr_summary,
  duration_ms,
  tags_json
) VALUES
(
  'cmd_demo_port',
  'ses_demo',
  'prj_demo',
  'what is using port 8000?',
  'lsof -i :8000',
  'lsof -i :8000',
  '/Users/example/projects/termind',
  'zsh',
  'safe',
  true,
  'approved',
  0,
  'python3 is listening on TCP port 8000.',
  '',
  238,
  '["network", "debugging", "port"]'
),
(
  'cmd_demo_dev',
  'ses_demo',
  'prj_demo',
  'start the backend server',
  'go run ./cmd/server',
  'go run ./cmd/server',
  '/Users/example/projects/termind',
  'zsh',
  'modifying',
  true,
  'approved',
  0,
  'Go backend API started on http://127.0.0.1:8000.',
  '',
  1042,
  '["backend", "go", "dev-server"]'
) ON CONFLICT (id) DO NOTHING;

INSERT INTO project_commands (
  id,
  project_id,
  command,
  label,
  success_count,
  failure_count,
  last_success_at
) VALUES (
  'pcmd_demo_dev',
  'prj_demo',
  'go run ./cmd/server',
  'Run backend API',
  8,
  1,
  now()
) ON CONFLICT (project_id, command) DO NOTHING;
