import React, { FormEvent, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  Clock3,
  Cpu,
  Database,
  Layers3,
  Mic,
  Play,
  RefreshCw,
  Search,
  Send,
  ShieldCheck,
  Square,
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
  ollama_embed_model: string;
  planner_mode: string;
  command_timeout_seconds: number;
  database: string;
  backend_runtime: string;
  mode: string;
  voice_input_enabled: boolean;
  voice_stt_provider: string;
  voice_stt_command: boolean;
  voice_max_audio_seconds: number;
  voice_max_audio_bytes: number;
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
  embedding_status?: 'stored' | 'failed' | 'skipped';
  embedding_model?: string;
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

type EmbeddingBackfillResponse = {
  status: 'completed' | 'partial' | 'skipped';
  scanned: number;
  stored: number;
  failed: number;
  skipped: number;
  embedding_model?: string;
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
  status: 'ready' | 'in_progress' | 'mocked' | 'planned' | 'blocked';
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

type VoiceConfig = {
  enabled: boolean;
  stt_provider: string;
  max_audio_seconds: number;
  max_audio_bytes: number;
  accepted_mime_types: string[];
  transcript_endpoint: string;
  status: string;
};

type VoiceTranscriptResponse = {
  status: string;
  transcript: string;
  confidence: number;
  stt_provider: string;
  requires_edit: boolean;
  next_endpoint: string;
  message: string;
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

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => {
      const value = reader.result;
      if (typeof value !== 'string') {
        reject(new Error('Audio recording could not be read'));
        return;
      }
      resolve(value.split(',')[1] || '');
    };
    reader.onerror = () => reject(new Error('Audio recording could not be read'));
    reader.readAsDataURL(blob);
  });
}

function bestRecordingMimeType(acceptedMimeTypes: string[]): string {
  if (typeof MediaRecorder === 'undefined') return '';
  const preferred = ['audio/webm', 'audio/mp4', 'audio/wav', 'audio/mpeg'];
  return preferred.find((mimeType) => acceptedMimeTypes.includes(mimeType) && MediaRecorder.isTypeSupported(mimeType)) || '';
}

function App() {
  const [sessionId, setSessionId] = useState('ses_demo');
  const [input, setInput] = useState('what is using port 8000?');
  const [cwd, setCwd] = useState(defaultCwd);
  const [config, setConfig] = useState<RuntimeConfig | null>(null);
  const [voiceConfig, setVoiceConfig] = useState<VoiceConfig | null>(null);
  const [mediaRecorder, setMediaRecorder] = useState<MediaRecorder | null>(null);
  const [voiceStatus, setVoiceStatus] = useState<'idle' | 'recording' | 'transcribing'>('idle');
  const [voiceTranscript, setVoiceTranscript] = useState<VoiceTranscriptResponse | null>(null);
  const [voiceError, setVoiceError] = useState<string | null>(null);
  const [requestSource, setRequestSource] = useState<'typed' | 'voice'>('typed');
  const [plan, setPlan] = useState<UserRequestResponse | null>(null);
  const [execution, setExecution] = useState<CommandExecution | null>(null);
  const [memory, setMemory] = useState<MemorySearchResult[]>([]);
  const [backfill, setBackfill] = useState<EmbeddingBackfillResponse | null>(null);
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

      const voice = await requestJson<VoiceConfig>('/v1/voice/config');
      setVoiceConfig(voice);

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

  const activeCapabilities = useMemo(() => {
    return capabilities.filter((capability) => capability.status !== 'planned');
  }, [capabilities]);

  const plannedCapabilities = useMemo(() => {
    return capabilities.filter((capability) => capability.status === 'planned');
  }, [capabilities]);

  async function submitRequest(event?: FormEvent<HTMLFormElement>, source: 'typed' | 'voice' = 'typed') {
    event?.preventDefault();
    setLoading(true);
    setExecution(null);

    try {
      const prompt = input.trim();
      const response = await requestJson<UserRequestResponse>('/v1/requests', {
        method: 'POST',
        body: JSON.stringify({
          session_id: sessionId,
          input: prompt,
          cwd,
        }),
      });
      setInput(prompt);
      setPlan(response);
      setRequestSource(source);
      const context = await requestJson<ProjectContext>(`/v1/context/project?cwd=${encodeURIComponent(cwd)}`);
      setProject(context);
      if (response.intent === 'search_history') {
        await searchMemory(prompt);
      }
    } finally {
      setLoading(false);
    }
  }

  async function submitVoiceTranscript() {
    await submitRequest(undefined, 'voice');
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

  async function startVoiceCapture() {
    setVoiceError(null);
    setVoiceTranscript(null);

    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') {
      setVoiceError('This browser does not support microphone recording.');
      return;
    }

    const acceptedMimeTypes = voiceConfig?.accepted_mime_types || ['audio/webm'];
    const mimeType = bestRecordingMimeType(acceptedMimeTypes);
    if (!mimeType) {
      setVoiceError('No browser recording format matches the backend voice contract.');
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const chunks: BlobPart[] = [];
      const recorder = new MediaRecorder(stream, { mimeType });

      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) {
          chunks.push(event.data);
        }
      };
      recorder.onstop = () => {
        stream.getTracks().forEach((track) => track.stop());
        setMediaRecorder(null);
        void transcribeVoiceBlob(new Blob(chunks, { type: recorder.mimeType || mimeType }));
      };

      recorder.start();
      setMediaRecorder(recorder);
      setVoiceStatus('recording');

      window.setTimeout(() => {
        if (recorder.state === 'recording') {
          recorder.stop();
        }
      }, (voiceConfig?.max_audio_seconds || 30) * 1000);
    } catch (error) {
      setVoiceStatus('idle');
      setVoiceError(error instanceof Error ? error.message : 'Microphone permission was not granted.');
    }
  }

  function stopVoiceCapture() {
    if (mediaRecorder?.state === 'recording') {
      mediaRecorder.stop();
    }
  }

  async function transcribeVoiceBlob(blob: Blob) {
    setVoiceStatus('transcribing');
    setVoiceError(null);

    try {
      if (voiceConfig?.max_audio_bytes && blob.size > voiceConfig.max_audio_bytes) {
        setVoiceError(`Recording is larger than ${Math.round(voiceConfig.max_audio_bytes / 1024 / 1024)} MB.`);
        return;
      }

      const audioBase64 = await blobToBase64(blob);
      const mimeType = blob.type || bestRecordingMimeType(voiceConfig?.accepted_mime_types || []);
      const response = await fetch(`${API_BASE_URL}${voiceConfig?.transcript_endpoint || '/v1/voice/transcripts'}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          audio_base64: audioBase64,
          mime_type: mimeType,
          language: 'en',
        }),
      });
      const transcript = await response.json() as VoiceTranscriptResponse;
      setVoiceTranscript(transcript);
      if (transcript.transcript) {
        setInput(transcript.transcript);
        setPlan(null);
        setExecution(null);
      }
      if (!response.ok && transcript.message) {
        setVoiceError(transcript.message);
      }
    } catch (error) {
      setVoiceError(error instanceof Error ? error.message : 'Voice transcription failed.');
    } finally {
      setVoiceStatus('idle');
    }
  }

  async function backfillEmbeddings() {
    setLoading(true);
    try {
      const response = await requestJson<EmbeddingBackfillResponse>('/v1/memory/embeddings/backfill', {
        method: 'POST',
        body: JSON.stringify({ limit: 50 }),
      });
      setBackfill(response);
      await searchMemory(input);
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <Terminal size={24} />
          <div>
            <strong>Termind</strong>
            <span>Local terminal copilot</span>
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
            <span>Embeddings</span>
            <strong>{config?.ollama_embed_model || 'loading'}</strong>
          </div>
          <div className="status-line">
            <span>Database</span>
            <strong>{config?.database || 'Postgres'}</strong>
          </div>
          <div className="status-line">
            <span>Voice</span>
            <strong>{config?.voice_input_enabled ? config.voice_stt_provider : 'planned'}</strong>
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
            <div className="pill"><Layers3 size={16} /> {phaseResponse?.current_phase || 'phase_6'}</div>
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
            <h2>{currentPhase?.name || 'Voice Input'}</h2>
            <p>{currentPhase?.summary || 'Voice runtime config and transcript contracts are being added before local speech-to-text.'}</p>
          </div>
          <div className="phase-checks">
            {(currentPhase?.scope || [
              '6.1 voice capability metadata and config',
              '6.2 local speech-to-text adapter spike',
              '6.3 transcript endpoint',
              '6.4 frontend microphone and transcript confirmation',
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
              <span>Planner model</span>
              <strong>{config?.ollama_model || 'Loading'}</strong>
            </article>
            <article>
              <span>Embedding model</span>
              <strong>{config?.ollama_embed_model || 'Loading'}</strong>
            </article>
            <article>
              <span>Execution path</span>
              <strong>Backend shell, {config?.command_timeout_seconds || 30}s timeout</strong>
            </article>
            <article>
              <span>Voice input</span>
              <strong>{config?.voice_input_enabled ? `${config.voice_stt_provider}, ${config.voice_max_audio_seconds}s` : 'STT disabled'}</strong>
            </article>
          </div>

          <form onSubmit={submitRequest} className="prompt-row">
            <Terminal size={20} />
            <div className="prompt-fields">
              <input
                value={input}
                onChange={(event) => {
                  setInput(event.target.value);
                  setRequestSource('typed');
                }}
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

          <div className="voice-panel">
            <div className="voice-actions">
              <button
                className={voiceStatus === 'recording' ? 'voice-button recording' : 'voice-button'}
                onClick={voiceStatus === 'recording' ? stopVoiceCapture : startVoiceCapture}
                disabled={voiceStatus === 'transcribing'}
                type="button"
              >
                {voiceStatus === 'recording' ? <Square size={18} /> : <Mic size={18} />}
                {voiceStatus === 'recording' ? 'Stop recording' : voiceStatus === 'transcribing' ? 'Transcribing' : 'Record voice'}
              </button>
              <span>
                {voiceConfig?.stt_provider || 'disabled'} · {voiceConfig?.max_audio_seconds || 30}s · {voiceConfig?.accepted_mime_types.join(', ') || 'loading'}
              </span>
            </div>

            {(voiceTranscript || voiceError) && (
              <div className={voiceTranscript?.status === 'completed' ? 'voice-result good' : 'voice-result warn'}>
                <div>
                  {voiceTranscript?.status === 'completed' ? <CheckCircle2 size={18} /> : <AlertCircle size={18} />}
                  <strong>{voiceTranscript?.status || 'recording_error'}</strong>
                  {voiceTranscript?.confidence ? <span>{Math.round(voiceTranscript.confidence * 100)}% confidence</span> : null}
                </div>
                <p>{voiceTranscript?.message || voiceError}</p>
                {voiceTranscript?.transcript && (
                  <>
                    <textarea
                      value={input}
                      onChange={(event) => setInput(event.target.value)}
                      aria-label="Editable voice transcript"
                    />
                    <button
                      className="voice-confirm-button"
                      onClick={submitVoiceTranscript}
                      disabled={loading || !input.trim()}
                      type="button"
                    >
                      <Send size={18} />
                      Plan transcript
                    </button>
                  </>
                )}
              </div>
            )}
          </div>

          {plan && (
            <div className="plan-grid">
              <div className="plan-main">
                <div className="section-label">{plan.intent === 'unknown' ? 'Out of scope' : 'Proposed command'}</div>
                <pre>{plan.plan.command || (plan.intent === 'unknown' ? 'No command planned' : 'Memory search request')}</pre>
                <p>{plan.plan.reason}</p>
                <div className="plan-meta">
                  <span>Source: {requestSource}</span>
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
                <span>
                  exit {execution.exit_code ?? 'n/a'} · {execution.duration_ms}ms
                  {execution.embedding_status ? ` · embedding ${execution.embedding_status}` : ''}
                </span>
              </div>
              {execution.embedding_model && (
                <div className="output-meta">Stored with {execution.embedding_model}</div>
              )}
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
              {phase.mock_apis.length > 0 ? (
                <div className="api-list">
                  {phase.mock_apis.slice(0, 3).map((api) => (
                    <code key={api}>{api}</code>
                  ))}
                </div>
              ) : (
                <div className="api-list complete">
                  <code>Implemented</code>
                </div>
              )}
            </article>
          ))}
        </section>

        <section className="lower-grid">
          <div className="surface">
            <div className="section-title">
              <Database size={18} />
              Remembered commands
              <button className="small-action" onClick={backfillEmbeddings} disabled={loading}>
                Backfill
              </button>
              <button className="icon-button" onClick={() => searchMemory(input)} disabled={loading} title="Refresh memory">
                <RefreshCw size={16} />
              </button>
            </div>
            {backfill && (
              <div className="memory-status">
                {backfill.status}: scanned {backfill.scanned}, stored {backfill.stored}, failed {backfill.failed}
              </div>
            )}
            <div className="memory-list">
              {memory.map((item) => (
                <article key={item.command_event_id} className="memory-item">
                  <div>
                    <code>{item.command}</code>
                    <p>{item.user_request}</p>
                    <div className="reason-list">
                      {item.matched_reasons.map((reason) => (
                        <span key={reason}>{reason}</span>
                      ))}
                    </div>
                  </div>
                  <span>{Math.round(item.score * 100)}%</span>
                </article>
              ))}
              {memory.length === 0 && (
                <div className="empty-state">No relevant command memory found.</div>
              )}
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
            Capability roadmap
          </div>
          <div className="capability-list">
            {activeCapabilities.map((capability) => (
              <article key={capability.id} className="capability-item">
                <div>
                  <strong>{capability.id.replace(/_/g, ' ')}</strong>
                  <p>{capability.description}</p>
                </div>
                <span>{capability.phase}</span>
              </article>
            ))}
            {plannedCapabilities.map((capability) => (
              <article key={capability.id} className="capability-item planned">
                <div>
                  <strong>{capability.id.replace(/_/g, ' ')}</strong>
                  <p>{capability.description}</p>
                </div>
                <span>{capability.status}</span>
              </article>
            ))}
          </div>
        </section>
      </section>
    </main>
  );
}

createRoot(document.getElementById('root') as HTMLElement).render(<App />);
