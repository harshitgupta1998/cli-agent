import React, { FormEvent, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Bot,
  CheckCircle2,
  Clock3,
  Cpu,
  Database,
  Layers3,
  Play,
  RefreshCw,
  Search,
  ShieldCheck,
  Terminal,
  Zap,
} from 'lucide-react';
import './styles.css';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8000';
const defaultCwd = '/app';

type RiskLevel = 'safe' | 'modifying' | 'destructive' | 'privileged' | 'networked' | 'unknown';
type RiskTone = 'good' | 'warn' | 'bad';

const riskToneByLevel: Record<RiskLevel, RiskTone> = {
  safe: 'good',
  modifying: 'warn',
  networked: 'warn',
  destructive: 'bad',
  privileged: 'bad',
  unknown: 'warn',
};

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

type RuntimeConfig = {
  product: string;
  local_only_mode: boolean;
  ollama_base_url: string;
  ollama_model: string;
  planner_mode: string;
  command_timeout_seconds: number;
  database: string;
  backend_runtime: string;
  mode: string;
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

type Phase = {
  id: string;
  name: string;
  status: 'ready' | 'mocked' | 'planned' | 'blocked';
  summary: string;
  deliverable: string;
  scope: string[];
  mock_apis: string[];
};

type PhaseResponse = {
  current_phase: string;
  phases: Phase[];
};

type MockCapability = {
  id: string;
  phase: string;
  status: string;
  description: string;
  endpoints: string[];
  next_steps: string[];
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
  const [cwd, setCwd] = useState(defaultCwd);
  const [config, setConfig] = useState<RuntimeConfig | null>(null);
  const [plan, setPlan] = useState<UserRequestResponse | null>(null);
  const [execution, setExecution] = useState<CommandExecution | null>(null);
  const [memory, setMemory] = useState<MemorySearchResult[]>([]);
  const [project, setProject] = useState<ProjectContext | null>(null);
  const [phaseResponse, setPhaseResponse] = useState<PhaseResponse | null>(null);
  const [capabilities, setCapabilities] = useState<MockCapability[]>([]);
  const [loading, setLoading] = useState(false);
  const [bootError, setBootError] = useState<string | null>(null);

  const riskTone = useMemo<RiskTone>(() => {
    const risk = plan?.policy.risk || 'safe';
    return riskToneByLevel[risk];
  }, [plan]);

  useEffect(() => {
    async function boot() {
      setBootError(null);
      const runtime = await requestJson<RuntimeConfig>('/v1/config');
      setConfig(runtime);

      const session = await requestJson<{ session_id: string; project_id: string; started_at: string }>('/v1/sessions', {
        method: 'POST',
        body: JSON.stringify({ cwd, shell: 'zsh' }),
      });
      setSessionId(session.session_id);

      const context = await requestJson<ProjectContext>(`/v1/context/project?cwd=${encodeURIComponent(cwd)}`);
      setProject(context);

      const phases = await requestJson<PhaseResponse>('/v1/phases');
      setPhaseResponse(phases);

      const mockResponse = await requestJson<{ capabilities: MockCapability[] }>('/v1/mocks/capabilities');
      setCapabilities(mockResponse.capabilities);

      await searchMemory('backend port command');
    }

    boot().catch((error: Error) => setBootError(error.message));
  }, []);

  const currentPhase = useMemo(() => {
    return phaseResponse?.phases.find((phase) => phase.id === phaseResponse.current_phase);
  }, [phaseResponse]);

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
            cwd,
          }),
      });
      setPlan(response);
      const context = await requestJson<ProjectContext>(`/v1/context/project?cwd=${encodeURIComponent(cwd)}`);
      setProject(context);
      if (response.intent === 'search_history') {
        await searchMemory(input);
      }
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
            user_request: input,
            command: plan.plan.command,
            cwd: plan.plan.cwd,
            shell: 'sh',
            risk_level: plan.policy.risk,
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
        cwd,
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
              <span>Local LLM terminal agent</span>
            </div>
          </div>

        <nav className="nav-list">
          <button className="nav-item active"><Zap size={18} /> Agent</button>
          <button className="nav-item"><Database size={18} /> Memory</button>
          <button className="nav-item"><Bot size={18} /> Planner</button>
          <button className="nav-item"><ShieldCheck size={18} /> Policy</button>
          <button className="nav-item"><Layers3 size={18} /> Phases</button>
        </nav>

        <section className="status-panel">
          <div className="status-line">
            <span>Mode</span>
            <strong>Local only</strong>
          </div>
          <div className="status-line">
            <span>Planner</span>
            <strong>{config?.planner_mode || 'loading'}</strong>
          </div>
          <div className="status-line">
            <span>Model</span>
            <strong>{config?.ollama_model || 'loading'}</strong>
          </div>
          <div className="status-line">
            <span>Database</span>
            <strong>{config?.database || 'Postgres'}</strong>
          </div>
        </section>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <h1>Command workbench</h1>
            <p>{project?.root_path || cwd}</p>
          </div>
          <div className="topbar-actions">
            <div className="pill"><Cpu size={16} /> {config?.mode || 'checking runtime'}</div>
            <div className="pill"><Layers3 size={16} /> {phaseResponse?.current_phase || 'phase_2'}</div>
            <div className="pill"><Clock3 size={16} /> Session {sessionId}</div>
          </div>
        </header>

        {bootError && (
          <section className="error-strip">
            <strong>API unavailable</strong>
            <span>{bootError}</span>
          </section>
        )}

        <section className="phase-banner">
          <div>
            <div className="section-label">Current build target</div>
            <h2>{currentPhase?.name || 'Local LLM Planner'}</h2>
            <p>{currentPhase?.summary || 'Ollama-backed command planning, deterministic policy review, CLI execution, and persisted command memory.'}</p>
          </div>
          <div className="phase-checks">
            {(currentPhase?.scope || [
              'Ollama JSON command planner',
              'Rule planner fallback',
              'Policy gate before execution',
              'Postgres command memory',
            ]).slice(0, 4).map((item) => (
              <span key={item}><CheckCircle2 size={15} /> {item}</span>
            ))}
          </div>
        </section>

        <section className="command-panel">
          <div className="runtime-grid">
            <article>
              <span>Planner engine</span>
              <strong>{config?.planner_mode === 'ollama' ? 'Ollama local LLM' : config?.planner_mode || 'Loading'}</strong>
            </article>
            <article>
              <span>Model</span>
              <strong>{config?.ollama_model || 'Loading'}</strong>
            </article>
            <article>
              <span>Backend</span>
              <strong>{config?.backend_runtime || 'Go'}</strong>
            </article>
            <article>
              <span>Execution path</span>
              <strong>Backend shell, {config?.command_timeout_seconds || 30}s timeout</strong>
            </article>
          </div>

          <form onSubmit={submitRequest} className="prompt-row">
            <Terminal size={20} />
            <div className="prompt-fields">
              <input
                value={input}
                onChange={(event) => setInput(event.target.value)}
                placeholder="Ask for a command or search terminal memory"
              />
              <input
                value={cwd}
                onChange={(event) => setCwd(event.target.value)}
                placeholder="Working directory"
                aria-label="Working directory"
              />
            </div>
            <button type="submit" disabled={loading}>
              <Search size={18} />
              Plan
            </button>
          </form>

          {plan && (
            <div className="plan-grid">
              <div className="plan-main">
                <div className="section-label">{plan.intent === 'unknown' ? 'Out of scope' : 'Proposed command'}</div>
                <pre>{plan.plan.command || (plan.intent === 'unknown' ? 'No command planned' : 'Memory search request')}</pre>
                <p>{plan.plan.reason}</p>
                <div className="plan-meta">
                  <span>Intent: {plan.intent}</span>
                  <span>CWD: {plan.plan.cwd}</span>
                </div>
                <button className="primary-action" onClick={runCommand} disabled={loading || !plan.plan.command}>
                  <Play size={18} />
                  Run approved command
                </button>
              </div>

              <div className={`risk-card ${riskTone}`}>
                <div className="section-label">Policy decision</div>
                <strong>{plan.policy.risk}</strong>
                <span>{plan.policy.requires_confirmation ? 'Confirmation required' : 'No confirmation required'}</span>
                {plan.plan.provenance.length > 0 && (
                  <div className="evidence-list">
                    {plan.plan.provenance.map((item) => (
                      <p key={item}>{item}</p>
                    ))}
                  </div>
                )}
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

        <section className="phase-grid">
          {(phaseResponse?.phases || []).map((phase) => (
            <article key={phase.id} className={`phase-card ${phase.status}`}>
              <div className="phase-card-header">
                <strong>{phase.name}</strong>
                <span>{phase.status}</span>
              </div>
              <p>{phase.summary}</p>
              <div className="api-list">
                {phase.mock_apis.slice(0, 3).map((api) => (
                  <code key={api}>{api}</code>
                ))}
              </div>
            </article>
          ))}
        </section>

        <section className="lower-grid">
          <div className="surface">
            <div className="section-title">
              <Database size={18} />
              Remembered commands
              <button className="icon-button" onClick={() => searchMemory(input)} disabled={loading} title="Refresh memory">
                <RefreshCw size={16} />
              </button>
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
              <strong>{project?.detected_stack.framework || 'net/http'}</strong>
              <span>Package manager</span>
              <strong>{project?.detected_stack.package_manager || 'go modules'}</strong>
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

        <section className="surface mocks-surface">
          <div className="section-title">
            <Layers3 size={18} />
            Later phase mocks
          </div>
          <div className="capability-list">
            {capabilities.map((capability) => (
              <article key={capability.id} className="capability-item">
                <div>
                  <strong>{capability.id.replace(/_/g, ' ')}</strong>
                  <p>{capability.description}</p>
                </div>
                <span>{capability.phase}</span>
              </article>
            ))}
          </div>
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById('root') as HTMLElement).render(<App />);
