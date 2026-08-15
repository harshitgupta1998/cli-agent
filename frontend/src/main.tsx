import React, { FormEvent, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { CheckCircle2, Clock3, Database, Play, Search, ShieldCheck, Terminal, Zap } from 'lucide-react';
import './styles.css';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8000';
const defaultCwd = '/Users/example/projects/lifesummary-api';

type RiskLevel = 'safe' | 'modifying' | 'destructive' | 'privileged' | 'networked' | 'unknown';
type RiskTone = 'good' | 'warn' | 'bad';

type CommandPlan = {
  command: string;
  cwd: string;
  risk: RiskLevel;
  requires_confirmation: boolean;
  reason: string;
  provenance: string[];
  alternatives: Array<{
    command: string;
    reason: string;
  }>;
};

type UserRequestResponse = {
  request_id: string;
  intent: string;
  plan: CommandPlan;
  policy: {
    risk: RiskLevel;
    requires_confirmation: boolean;
    warnings: string[];
  };
};

type CommandExecution = {
  command_event_id: string;
  status: 'completed' | 'rejected';
  exit_code: number | null;
  stdout: string;
  stderr: string;
  duration_ms: number;
};

type MemorySearchResult = {
  command_event_id: string;
  command: string;
  user_request: string;
  cwd: string;
  exit_code: number;
  score: number;
  matched_reasons: string[];
  last_used_at: string;
};

type ProjectContext = {
  project_id: string;
  root_path: string;
  git: {
    branch: string;
    has_uncommitted_changes: boolean;
  };
  detected_stack: {
    language: string;
    framework: string;
    package_manager: string;
  };
  common_commands: Array<{
    label: string;
    command: string;
    success_count: number;
  }>;
};

async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  });

  if (!response.ok) {
    throw new Error(`Request failed with ${response.status}`);
  }

  return response.json() as Promise<T>;
}

function App() {
  const [sessionId, setSessionId] = useState('ses_demo');
  const [input, setInput] = useState('what is using port 8000?');
  const [plan, setPlan] = useState<UserRequestResponse | null>(null);
  const [execution, setExecution] = useState<CommandExecution | null>(null);
  const [memory, setMemory] = useState<MemorySearchResult[]>([]);
  const [project, setProject] = useState<ProjectContext | null>(null);
  const [loading, setLoading] = useState(false);

  const riskTone = useMemo<RiskTone>(() => {
    const risk = plan?.policy.risk || 'safe';
    return {
      safe: 'good',
      modifying: 'warn',
      networked: 'warn',
      destructive: 'bad',
      privileged: 'bad',
      unknown: 'warn',
    }[risk];
  }, [plan]);

  useEffect(() => {
    async function boot() {
      const session = await requestJson<{ session_id: string; project_id: string; started_at: string }>('/v1/sessions', {
        method: 'POST',
        body: JSON.stringify({ cwd: defaultCwd, shell: 'zsh' }),
      });
      setSessionId(session.session_id);

      const context = await requestJson<ProjectContext>(`/v1/context/project?cwd=${encodeURIComponent(defaultCwd)}`);
      setProject(context);

      await searchMemory('backend port command');
    }

    boot().catch(console.error);
  }, []);

  async function submitRequest(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    setLoading(true);
    setExecution(null);

    try {
      const response = await requestJson<UserRequestResponse>('/v1/requests', {
        method: 'POST',
        body: JSON.stringify({
          session_id: sessionId,
          input,
          cwd: defaultCwd,
        }),
      });
      setPlan(response);
    } finally {
      setLoading(false);
    }
  }

  async function runCommand() {
    if (!plan?.plan.command) return;

    setLoading(true);
    try {
      const response = await requestJson<CommandExecution>('/v1/commands/execute', {
        method: 'POST',
        body: JSON.stringify({
          session_id: sessionId,
          request_id: plan.request_id,
          command: plan.plan.command,
          cwd: plan.plan.cwd,
          confirmation: {
            status: 'approved',
            approved_at: new Date().toISOString(),
          },
        }),
      });
      setExecution(response);
      await searchMemory(input);
    } finally {
      setLoading(false);
    }
  }

  async function searchMemory(query: string) {
    const response = await requestJson<{ results: MemorySearchResult[] }>('/v1/memory/search', {
      method: 'POST',
      body: JSON.stringify({
        query,
        project_id: 'prj_demo',
        cwd: defaultCwd,
        limit: 5,
      }),
    });
    setMemory(response.results);
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <Terminal size={24} />
          <div>
            <strong>Termind</strong>
            <span>Private terminal memory</span>
          </div>
        </div>

        <nav className="nav-list">
          <button className="nav-item active"><Zap size={18} /> Agent</button>
          <button className="nav-item"><Database size={18} /> Memory</button>
          <button className="nav-item"><ShieldCheck size={18} /> Policy</button>
        </nav>

        <section className="status-panel">
          <div className="status-line">
            <span>Mode</span>
            <strong>Local only</strong>
          </div>
          <div className="status-line">
            <span>Backend</span>
            <strong>Mock API</strong>
          </div>
          <div className="status-line">
            <span>Database</span>
            <strong>Postgres</strong>
          </div>
        </section>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <h1>Command workbench</h1>
            <p>{project?.root_path || defaultCwd}</p>
          </div>
          <div className="pill"><Clock3 size={16} /> Session {sessionId}</div>
        </header>

        <section className="command-panel">
          <form onSubmit={submitRequest} className="prompt-row">
            <Terminal size={20} />
            <input
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder="Ask for a command or search terminal memory"
            />
            <button type="submit" disabled={loading}>
              <Search size={18} />
              Plan
            </button>
          </form>

          {plan && (
            <div className="plan-grid">
              <div className="plan-main">
                <div className="section-label">Proposed command</div>
                <pre>{plan.plan.command || 'Memory search request'}</pre>
                <p>{plan.plan.reason}</p>
                <button className="primary-action" onClick={runCommand} disabled={loading || !plan.plan.command}>
                  <Play size={18} />
                  Run approved command
                </button>
              </div>

              <div className={`risk-card ${riskTone}`}>
                <div className="section-label">Policy decision</div>
                <strong>{plan.policy.risk}</strong>
                <span>{plan.policy.requires_confirmation ? 'Confirmation required' : 'No confirmation required'}</span>
                {plan.policy.warnings.map((warning) => (
                  <p key={warning}>{warning}</p>
                ))}
              </div>
            </div>
          )}

          {execution && (
            <div className="terminal-output">
              <div className="output-header">
                <span>Execution result</span>
                <span>exit {execution.exit_code ?? 'n/a'} · {execution.duration_ms}ms</span>
              </div>
              <pre>{execution.stdout || execution.stderr}</pre>
            </div>
          )}
        </section>

        <section className="lower-grid">
          <div className="surface">
            <div className="section-title">
              <Database size={18} />
              Remembered commands
            </div>
            <div className="memory-list">
              {memory.map((item) => (
                <article key={item.command_event_id} className="memory-item">
                  <div>
                    <code>{item.command}</code>
                    <p>{item.user_request}</p>
                  </div>
                  <span>{Math.round(item.score * 100)}%</span>
                </article>
              ))}
            </div>
          </div>

          <div className="surface">
            <div className="section-title">
              <CheckCircle2 size={18} />
              Project signals
            </div>
            <div className="signal-grid">
              <span>Stack</span>
              <strong>{project?.detected_stack.framework || 'fastapi'}</strong>
              <span>Package manager</span>
              <strong>{project?.detected_stack.package_manager || 'uv'}</strong>
              <span>Branch</span>
              <strong>{project?.git.branch || 'main'}</strong>
            </div>
            <div className="command-stack">
              {(project?.common_commands || []).map((command) => (
                <code key={command.command}>{command.command}</code>
              ))}
            </div>
          </div>
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById('root') as HTMLElement).render(<App />);
