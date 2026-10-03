package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/planner"
	"github.com/harsgupta/termind/backend/internal/voice"
)

type Agent interface {
	CreateSession(payload models.SessionCreateRequest) models.SessionCreateResponse
	ListMessages(sessionID string) models.MessageListResponse
	CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse
	Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse
	RecordCommand(payload models.CommandRecordRequest) models.CommandRecordResponse
	SearchMemory(payload models.MemorySearchRequest) models.MemorySearchResponse
	BackfillCommandEmbeddings(payload models.EmbeddingBackfillRequest) models.EmbeddingBackfillResponse
	Explain(command string) models.ExplainCommandResponse
	ProjectContext(cwd string) models.ProjectContext
	Phases() models.PhaseResponse
	MockCapabilities() models.MockCapabilityResponse
	VoiceConfig() models.VoiceConfigResponse
	CreateVoiceTranscript(payload models.VoiceTranscriptRequest) models.VoiceTranscriptResponse
}

type AgentService struct {
	memory         memory.Store
	planner        planner.Planner
	commandTimeout time.Duration
	voiceEnabled   bool
	voiceProvider  string
	voiceMaxSecs   int
	voiceMaxBytes  int
	transcriber    voice.Transcriber
}

func NewAgentService(memoryStore memory.Store, commandPlanner planner.Planner, commandTimeout time.Duration) AgentService {
	return AgentService{
		memory:         memoryStore,
		planner:        commandPlanner,
		commandTimeout: commandTimeout,
		voiceEnabled:   false,
		voiceProvider:  "disabled",
		voiceMaxSecs:   30,
		voiceMaxBytes:  5 * 1024 * 1024,
		transcriber:    voice.DisabledTranscriber{},
	}
}

func (m AgentService) WithVoiceConfig(enabled bool, provider string, maxSeconds int) AgentService {
	m.voiceEnabled = enabled
	m.voiceProvider = strings.TrimSpace(provider)
	if m.voiceProvider == "" {
		m.voiceProvider = "disabled"
	}
	if maxSeconds > 0 {
		m.voiceMaxSecs = maxSeconds
	}
	return m
}

func (m AgentService) WithVoiceTranscriber(transcriber voice.Transcriber) AgentService {
	if transcriber != nil {
		m.transcriber = transcriber
	}
	return m
}

func (m AgentService) WithVoiceMaxBytes(maxBytes int) AgentService {
	if maxBytes > 0 {
		m.voiceMaxBytes = maxBytes
	}
	return m
}

func (m AgentService) CreateSession(payload models.SessionCreateRequest) models.SessionCreateResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		response, err := m.memory.CreateSession(ctx, payload)
		if err == nil {
			return response
		}
	}

	return models.SessionCreateResponse{
		SessionID: "ses_" + shortID(),
		ProjectID: "prj_" + shortID(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func (m AgentService) ListMessages(sessionID string) models.MessageListResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		response, err := m.memory.ListMessages(ctx, sessionID)
		if err == nil {
			return response
		}
	}

	return models.MessageListResponse{Messages: []models.Message{}}
}

func (m AgentService) CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse {
	if !isTerminalRequest(payload.Input) {
		response := models.UserRequestResponse{
			RequestID: "req_" + shortID(),
			Intent:    models.IntentUnknown,
			Plan: models.CommandPlan{
				Command:              "",
				CWD:                  payload.CWD,
				Risk:                 "unknown",
				RequiresConfirmation: false,
				Reason:               "This does not look like a terminal, project, shell, or command-memory request.",
				Provenance: []string{
					"Termind is scoped to local terminal assistance.",
					"The request did not include a recognizable command, tool, file, process, project, or terminal-memory goal.",
				},
				Alternatives: []models.AlternativeCommand{},
			},
			Policy: models.PolicyDecision{
				Risk:                 "unknown",
				RequiresConfirmation: false,
				Warnings:             []string{"Ask for a shell command, project action, process inspection, file operation, or command history search."},
			},
		}
		m.persistRequestMessages(payload, response)
		return response
	}

	intent, plan, err := m.planner.Plan(payload)
	if err != nil {
		intent, plan, _ = planner.NewRulePlanner().Plan(payload)
	}

	response := models.UserRequestResponse{
		RequestID: "req_" + shortID(),
		Intent:    intent,
		Plan:      plan,
		Policy:    evaluatePolicy(plan),
	}
	m.persistRequestMessages(payload, response)
	return response
}

func (m AgentService) Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse {
	started := time.Now()
	if payload.Confirmation.Status == models.ConfirmationRejected {
		return models.CommandExecuteResponse{
			CommandEventID: "cmd_" + shortID(),
			Status:         models.CommandStatusRejected,
			ExitCode:       nil,
			Stdout:         "",
			Stderr:         "Command was rejected by the user.",
			DurationMS:     0,
		}
	}

	command := strings.TrimSpace(payload.Command)
	if blocked, reason := blockedCommand(command); blocked {
		response := models.CommandExecuteResponse{
			CommandEventID: "cmd_" + shortID(),
			Status:         models.CommandStatusBlocked,
			ExitCode:       nil,
			Stdout:         "",
			Stderr:         reason,
			DurationMS:     int(time.Since(started).Milliseconds()),
		}
		if record := m.persistExecution(payload, response); record.CommandEventID != "" {
			response.CommandEventID = record.CommandEventID
			response.EmbeddingStatus = record.EmbeddingStatus
			response.EmbeddingModel = record.EmbeddingModel
		}
		return response
	}

	timeout := m.commandTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = payload.CWD

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	durationMS := int(time.Since(started).Milliseconds())
	exitCode := 0
	status := models.CommandStatusCompleted
	if err != nil {
		exitCode = 1
		status = models.CommandStatusFailed
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			stderr.WriteString("\nCommand timed out.")
		}
	}

	response := models.CommandExecuteResponse{
		CommandEventID: "cmd_" + shortID(),
		Status:         status,
		ExitCode:       &exitCode,
		Stdout:         stdout.String(),
		Stderr:         stderr.String(),
		DurationMS:     durationMS,
	}
	if record := m.persistExecution(payload, response); record.CommandEventID != "" {
		response.CommandEventID = record.CommandEventID
		response.EmbeddingStatus = record.EmbeddingStatus
		response.EmbeddingModel = record.EmbeddingModel
	}
	return response
}

func (m AgentService) RecordCommand(payload models.CommandRecordRequest) models.CommandRecordResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		response, err := m.memory.RecordCommand(ctx, payload)
		if err == nil {
			return response
		}
	}

	return models.CommandRecordResponse{
		CommandEventID:  "cmd_" + shortID(),
		Status:          models.CommandStatusCompleted,
		Message:         "Command event accepted by fallback recorder. Postgres persistence is unavailable.",
		EmbeddingStatus: "skipped",
	}
}

func (m AgentService) SearchMemory(payload models.MemorySearchRequest) models.MemorySearchResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		response, err := m.memory.SearchCommands(ctx, payload)
		if err == nil {
			return response
		}
	}

	return models.MemorySearchResponse{Results: []models.MemorySearchResult{}}
}

func (m AgentService) BackfillCommandEmbeddings(payload models.EmbeddingBackfillRequest) models.EmbeddingBackfillResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		response, err := m.memory.BackfillCommandEmbeddings(ctx, payload)
		if err == nil {
			return response
		}
	}

	return models.EmbeddingBackfillResponse{Status: "skipped"}
}

func (m AgentService) persistRequestMessages(payload models.UserRequestCreate, response models.UserRequestResponse) {
	if m.memory == nil || strings.TrimSpace(payload.SessionID) == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = m.memory.RecordMessage(ctx, payload.SessionID, "user", payload.Input)
	content, err := json.Marshal(response)
	if err != nil {
		return
	}
	_ = m.memory.RecordMessage(ctx, payload.SessionID, "assistant", string(content))
}

func (m AgentService) persistExecution(payload models.CommandExecuteRequest, response models.CommandExecuteResponse) models.CommandRecordResponse {
	if m.memory == nil {
		return models.CommandRecordResponse{}
	}

	exitCode := 0
	if response.ExitCode != nil {
		exitCode = *response.ExitCode
	}
	confirmation := payload.Confirmation.Status
	if response.Status == models.CommandStatusBlocked {
		confirmation = models.ConfirmationRejected
	}
	risk := payload.RiskLevel
	if risk == "" {
		risk = "unknown"
	}
	shell := payload.Shell
	if shell == "" {
		shell = "sh"
	}
	userRequest := payload.UserRequest
	if userRequest == "" {
		userRequest = payload.Command
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	record, err := m.memory.RecordCommand(ctx, models.CommandRecordRequest{
		SessionID:       payload.SessionID,
		RequestID:       payload.RequestID,
		UserRequest:     userRequest,
		ProposedCommand: payload.Command,
		FinalCommand:    payload.Command,
		CWD:             payload.CWD,
		Shell:           shell,
		RiskLevel:       risk,
		Confirmation:    confirmation,
		ExitCode:        exitCode,
		Stdout:          summarize(response.Stdout),
		Stderr:          summarize(response.Stderr),
		DurationMS:      response.DurationMS,
	})
	if err != nil {
		return models.CommandRecordResponse{}
	}
	return record
}

func blockedCommand(command string) (bool, string) {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return true, "Empty commands cannot be executed."
	}

	blockedMarkers := []string{
		"rm -rf",
		"git reset --hard",
		"git clean",
		"docker system prune",
		"mkfs",
		":(){",
		"dd if=",
		"drop table",
		"shutdown",
		"reboot",
	}
	for _, marker := range blockedMarkers {
		if strings.Contains(normalized, marker) {
			return true, "Command blocked by Termind safety policy."
		}
	}
	if strings.Contains(normalized, "sudo ") {
		return true, "Privileged commands are blocked in backend execution for now."
	}

	return false, ""
}

func summarize(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 4000 {
		return value
	}
	return value[:4000]
}

func isTerminalRequest(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return false
	}

	terminalMarkers := []string{
		"terminal", "shell", "command", "cli", "script", "process", "port", "server",
		"file", "folder", "directory", "repo", "repository", "project", "path", "cwd",
		"git", "docker", "compose", "npm", "node", "go ", "golang", "python", "pytest",
		"test", "build", "run", "start", "stop", "kill", "list", "show", "find", "search",
		"largest", "disk", "memory", "env", "logs", "error", "install", "make ",
		"lsof", "pwd", "ls", "cd ", "du ", "df ", "ps ", "grep", "curl",
	}
	for _, marker := range terminalMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}

	firstToken := normalized
	if fields := strings.Fields(normalized); len(fields) > 0 {
		firstToken = fields[0]
	}
	switch firstToken {
	case "ls", "pwd", "cd", "cat", "tail", "head", "mkdir", "touch", "cp", "mv", "rm", "find", "du", "df", "ps", "kill", "curl", "make":
		return true
	default:
		return false
	}
}

func (m AgentService) Explain(command string) models.ExplainCommandResponse {
	normalized := strings.ToLower(command)

	if strings.Contains(normalized, "kill") {
		return models.ExplainCommandResponse{
			Summary:  "Finds matching process IDs and terminates them.",
			Risk:     models.RiskModifying,
			Warnings: []string{"This can stop running processes.", "Confirm the port or process before running."},
		}
	}

	if strings.Contains(normalized, "rm") {
		return models.ExplainCommandResponse{
			Summary:  "Removes files or directories.",
			Risk:     models.RiskDestructive,
			Warnings: []string{"This may permanently delete data."},
		}
	}

	return models.ExplainCommandResponse{
		Summary:  "This command is treated as a read-only or low-risk operation in the mock API.",
		Risk:     models.RiskSafe,
		Warnings: []string{},
	}
}

func (m AgentService) ProjectContext(cwd string) models.ProjectContext {
	projectID := "prj_unknown"
	commonCommands := []models.ProjectCommand{}
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if id, err := m.memory.EnsureProject(ctx, cwd); err == nil {
			projectID = id
		}
		commands, err := m.memory.ProjectCommands(ctx, cwd)
		if err == nil {
			commonCommands = commands
		}
	}
	if len(commonCommands) == 0 {
		commonCommands = []models.ProjectCommand{}
	}

	return models.ProjectContext{
		ProjectID:        projectID,
		RootPath:         cwd,
		Git:              detectGit(cwd),
		DetectedStack:    detectStack(cwd),
		CommonCommands:   commonCommands,
		ManifestCommands: detectManifestCommands(cwd),
	}
}

func detectGit(cwd string) models.GitContext {
	branchOutput, err := exec.Command("git", "-C", cwd, "branch", "--show-current").Output()
	if err != nil {
		return models.GitContext{
			Branch:                "unknown",
			HasUncommittedChanges: false,
		}
	}

	statusOutput, err := exec.Command("git", "-C", cwd, "status", "--porcelain").Output()
	hasChanges := false
	if err == nil {
		hasChanges = strings.TrimSpace(string(statusOutput)) != ""
	}

	branch := strings.TrimSpace(string(branchOutput))
	if branch == "" {
		branch = "unknown"
	}
	return models.GitContext{
		Branch:                branch,
		HasUncommittedChanges: hasChanges,
	}
}

func detectStack(cwd string) models.DetectedStack {
	switch {
	case fileExists(cwd, "go.mod"):
		return models.DetectedStack{
			Language:       "go",
			Framework:      "net/http",
			PackageManager: "go modules",
		}
	case fileExists(cwd, "package.json"):
		return models.DetectedStack{
			Language:       "typescript",
			Framework:      "react/vite",
			PackageManager: detectNodePackageManager(cwd),
		}
	case fileExists(cwd, "pyproject.toml"):
		return models.DetectedStack{
			Language:       "python",
			Framework:      "unknown",
			PackageManager: "pyproject",
		}
	case fileExists(cwd, "requirements.txt"):
		return models.DetectedStack{
			Language:       "python",
			Framework:      "unknown",
			PackageManager: "pip",
		}
	default:
		return models.DetectedStack{
			Language:       "unknown",
			Framework:      "unknown",
			PackageManager: "unknown",
		}
	}
}

func detectNodePackageManager(cwd string) string {
	switch {
	case fileExists(cwd, "pnpm-lock.yaml"):
		return "pnpm"
	case fileExists(cwd, "yarn.lock"):
		return "yarn"
	case fileExists(cwd, "package-lock.json"):
		return "npm"
	default:
		return "npm"
	}
}

func detectManifestCommands(cwd string) []models.ManifestCommand {
	commands := []models.ManifestCommand{}
	seen := map[string]bool{}
	add := func(label string, command string, source string) {
		label = strings.TrimSpace(label)
		command = strings.TrimSpace(command)
		source = strings.TrimSpace(source)
		if label == "" || command == "" || seen[command] {
			return
		}
		seen[command] = true
		commands = append(commands, models.ManifestCommand{
			Label:   label,
			Command: command,
			Source:  source,
		})
	}

	if fileExists(cwd, "go.mod") {
		add("Go test", "go test ./...", "go.mod")
		add("Go run server", "go run ./cmd/server", "go.mod")
	}
	if fileExists(cwd, "docker-compose.yml") {
		add("Docker Compose up", "docker compose up -d --build", "docker-compose.yml")
	}
	if fileExists(cwd, "requirements.txt") {
		add("Python tests", "pytest", "requirements.txt")
	}
	if fileExists(cwd, "requirements-dev.txt") {
		add("Python tests", "pytest", "requirements-dev.txt")
	}

	for _, script := range packageScripts(cwd) {
		manager := detectNodePackageManager(cwd)
		add(manager+" "+script, manager+" run "+script, "package.json")
	}
	for _, target := range makefileTargets(cwd) {
		add("make "+target, "make "+target, "Makefile")
	}

	return commands
}

func packageScripts(cwd string) []string {
	path := filepath.Join(cwd, "package.json")
	content, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return []string{}
	}
	preferred := []string{"dev", "test", "typecheck", "build", "lint", "preview", "start"}
	return orderedKeys(manifest.Scripts, preferred)
}

func makefileTargets(cwd string) []string {
	path := filepath.Join(cwd, "Makefile")
	content, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	targets := map[string]string{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ".") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "\t") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		target := strings.TrimSpace(parts[0])
		if target == "" || strings.ContainsAny(target, " =$") {
			continue
		}
		targets[target] = target
	}
	preferred := []string{"dev", "test", "test-backend", "test-frontend", "test-integration", "build-cli", "build-api", "stop"}
	return orderedKeys(targets, preferred)
}

func orderedKeys(values map[string]string, preferred []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, key := range preferred {
		if _, ok := values[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	for key := range values {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

func fileExists(cwd string, name string) bool {
	_, err := os.Stat(filepath.Join(cwd, name))
	return err == nil
}

func (m AgentService) Phases() models.PhaseResponse {
	return models.PhaseResponse{
		CurrentPhase: "phase_6",
		Phases: []models.Phase{
			{
				ID:          "phase_1",
				Name:        "Command Workbench",
				Status:      models.PhaseReady,
				Summary:     "Typed request to command plan, deterministic policy review, CLI execution, and persisted command memory.",
				Deliverable: "A working product skeleton that proves the review-run-remember loop through the local CLI.",
				Scope: []string{
					"React TypeScript command workbench",
					"Go API with typed request and response contracts",
					"Rule planner fallback",
					"Deterministic policy review scaffold",
					"CLI local command execution",
					"Postgres command event persistence",
					"Docker Compose for frontend, backend, and Postgres",
				},
				MockAPIs: []string{},
			},
			{
				ID:          "phase_2",
				Name:        "Local LLM Planning",
				Status:      models.PhaseReady,
				Summary:     "Use Ollama structured output for command planning while keeping policy and execution owned by the app.",
				Deliverable: "Ollama-backed CommandPlan generation with validation, fallback handling, and local-only mode.",
				Scope: []string{
					"Ollama chat client",
					"JSON plan validation",
					"Command-planner prompt",
					"Rule planner fallback handling",
					"Runtime config surface",
				},
				MockAPIs: []string{},
			},
			{
				ID:          "phase_3",
				Name:        "Safe Execution",
				Status:      models.PhaseReady,
				Summary:     "Run approved commands through a controlled Go executor with timeout, output capture, blocking, and audit events.",
				Deliverable: "A backend executor that runs approved commands and persists command events.",
				Scope: []string{
					"Process runner",
					"stdout and stderr capture",
					"Timeout policy",
					"Destructive command blocking",
					"Postgres audit persistence",
				},
				MockAPIs: []string{},
			},
			{
				ID:          "phase_4",
				Name:        "Persistent Memory",
				Status:      models.PhaseReady,
				Summary:     "Persist real sessions, projects, messages, command events, and learned project commands.",
				Deliverable: "Postgres-backed session memory, command history, and project command learning.",
				Scope: []string{
					"Repository layer",
					"Command event persistence",
					"Session persistence",
					"Message persistence",
					"Keyword search",
					"Project command learning",
				},
				MockAPIs: []string{},
			},
			{
				ID:          "phase_5",
				Name:        "Semantic Recall",
				Status:      models.PhaseReady,
				Summary:     "Generate local command-event embeddings and retrieve memory with hybrid semantic ranking.",
				Deliverable: "Ollama-backed embeddings, backfill, semantic query search, and hybrid ranking.",
				Scope: []string{
					"Embedding model config",
					"Ollama embedding client",
					"New command-event embeddings",
					"Command-event embedding backfill",
					"Semantic query embeddings",
					"Semantic memory fallback",
					"Hybrid keyword/semantic ranking",
					"Frontend semantic match reasons",
				},
				MockAPIs: []string{},
			},
			{
				ID:          "phase_6",
				Name:        "Voice Input",
				Status:      models.PhaseReady,
				Summary:     "Add local speech-to-text as an input adapter while preserving the typed planning and policy pipeline.",
				Deliverable: "Voice runtime config, transcript contracts, microphone confirmation UI, and confirmed transcript routing.",
				Scope: []string{
					"6.1 voice capability metadata and config - implemented",
					"6.2 command-based local speech-to-text adapter - implemented",
					"6.3 transcript endpoint hardening - implemented",
					"6.4 frontend microphone and transcript confirmation - implemented",
					"6.5 route confirmed transcript through planning and policy - implemented",
					"6.6 CLI voice command wrapper - implemented",
				},
				MockAPIs: []string{},
			},
		},
	}
}

func (m AgentService) MockCapabilities() models.MockCapabilityResponse {
	return models.MockCapabilityResponse{
		Capabilities: []models.MockCapability{
			{
				ID:          "ollama_planner",
				Phase:       "phase_2",
				Status:      "ready",
				Description: "Ollama generates structured CommandPlan output with deterministic fallback.",
				Endpoints:   []string{"POST /v1/requests"},
				NextSteps: []string{
					"Tune prompts with real user traces",
					"Add model health checks",
					"Add stricter command validation",
				},
			},
			{
				ID:          "command_executor",
				Phase:       "phase_3",
				Status:      "ready",
				Description: "Execution runs approved commands with timeout, captures output, blocks dangerous commands, and records audit events.",
				Endpoints:   []string{"POST /v1/commands/execute"},
				NextSteps: []string{
					"Add streaming output",
					"Add cancellation",
					"Add environment allowlist controls",
				},
			},
			{
				ID:          "memory_store",
				Phase:       "phase_4",
				Status:      "ready",
				Description: "Sessions, projects, messages, command events, and learned project commands are persisted in Postgres.",
				Endpoints:   []string{"POST /v1/memory/search"},
				NextSteps: []string{
					"Add richer memory queries",
					"Add memory pruning controls",
					"Prepare hybrid semantic ranking",
				},
			},
			{
				ID:          "semantic_recall",
				Phase:       "phase_5",
				Status:      "ready",
				Description: "New command events are embedded with Ollama, and memory search blends keyword, semantic, project, success, and recency signals.",
				Endpoints:   []string{"POST /v1/memory/search"},
				NextSteps: []string{
					"Tune ranking weights with real command traces",
					"Add pgvector or sqlite-vss when candidate sets outgrow in-app scoring",
				},
			},
			{
				ID:          "voice_input",
				Phase:       "phase_6",
				Status:      "ready",
				Description: "Voice runtime config, browser microphone capture, editable transcript confirmation, confirmed transcript planning, and CLI audio-file transcription are available.",
				Endpoints:   []string{"GET /v1/voice/config", "POST /v1/voice/transcripts"},
				NextSteps: []string{
					"Configure a production local STT command such as whisper.cpp",
					"Add live microphone capture to the host CLI after selecting a cross-platform recorder",
				},
			},
		},
	}
}

func (m AgentService) VoiceConfig() models.VoiceConfigResponse {
	status := "planned"
	if m.voiceEnabled {
		status = "configured"
	}
	return models.VoiceConfigResponse{
		Enabled:            m.voiceEnabled,
		STTProvider:        m.voiceProvider,
		MaxAudioSeconds:    m.voiceMaxSecs,
		MaxAudioBytes:      m.voiceMaxBytes,
		AcceptedMimeTypes:  voice.AcceptedMimeTypes(),
		TranscriptEndpoint: "/v1/voice/transcripts",
		Status:             status,
	}
}

func (m AgentService) CreateVoiceTranscript(payload models.VoiceTranscriptRequest) models.VoiceTranscriptResponse {
	validation := m.validateVoiceTranscript(payload)
	if validation.Status != "" {
		return validation
	}

	if !m.voiceEnabled || m.transcriber == nil {
		return models.VoiceTranscriptResponse{
			Status:       "not_implemented",
			Transcript:   "",
			Confidence:   0,
			STTProvider:  m.voiceProvider,
			RequiresEdit: true,
			NextEndpoint: "/v1/requests",
			Message:      "Voice input is configured, but no local speech-to-text provider is available.",
		}
	}

	timeout := time.Duration(m.voiceMaxSecs) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := m.transcriber.Transcribe(ctx, voice.Input{
		AudioBase64: payload.AudioBase64,
		MimeType:    payload.MimeType,
		Language:    payload.Language,
	})
	if err != nil {
		status := "invalid_audio"
		message := "Audio could not be transcribed."
		if errors.Is(err, voice.ErrUnavailable) {
			status = "not_implemented"
			message = "Voice input is configured, but no local speech-to-text provider is available."
		}
		return models.VoiceTranscriptResponse{
			Status:       status,
			Transcript:   "",
			Confidence:   0,
			STTProvider:  m.voiceProvider,
			RequiresEdit: true,
			NextEndpoint: "/v1/requests",
			Message:      message,
		}
	}

	return models.VoiceTranscriptResponse{
		Status:       "completed",
		Transcript:   result.Transcript,
		Confidence:   result.Confidence,
		STTProvider:  m.voiceProvider,
		RequiresEdit: true,
		NextEndpoint: "/v1/requests",
		Message:      "Transcript created locally. Review or edit it before planning a command.",
	}
}

func (m AgentService) validateVoiceTranscript(payload models.VoiceTranscriptRequest) models.VoiceTranscriptResponse {
	if strings.TrimSpace(payload.AudioBase64) == "" {
		return m.voiceError("audio_required", "Audio payload is required.")
	}
	if !voice.IsAcceptedMimeType(payload.MimeType) {
		return m.voiceError("unsupported_mime_type", "Audio MIME type is not supported.")
	}
	audio, err := voice.DecodeAudio(payload.AudioBase64)
	if err != nil {
		return m.voiceError("invalid_audio", "Audio payload must be valid base64.")
	}
	if len(audio) == 0 {
		return m.voiceError("audio_required", "Audio payload is required.")
	}
	if m.voiceMaxBytes > 0 && len(audio) > m.voiceMaxBytes {
		return m.voiceError("payload_too_large", "Audio payload exceeds the configured size limit.")
	}
	return models.VoiceTranscriptResponse{}
}

func (m AgentService) voiceError(status string, message string) models.VoiceTranscriptResponse {
	return models.VoiceTranscriptResponse{
		Status:       status,
		Transcript:   "",
		Confidence:   0,
		STTProvider:  m.voiceProvider,
		RequiresEdit: true,
		NextEndpoint: "/v1/requests",
		Message:      message,
	}
}

func evaluatePolicy(plan models.CommandPlan) models.PolicyDecision {
	command := strings.ToLower(plan.Command)
	risk := plan.Risk
	warnings := []string{}

	for _, marker := range []string{"rm -rf", "git reset --hard", "git clean", "docker system prune"} {
		if strings.Contains(command, marker) {
			risk = models.RiskDestructive
			warnings = append(warnings, "This command can permanently remove or reset data.")
			break
		}
	}

	for _, marker := range []string{"sudo", "chown -r", "chmod -r"} {
		if strings.Contains(command, marker) {
			risk = models.RiskPrivileged
			warnings = append(warnings, "This command requests elevated or broad system permissions.")
			break
		}
	}

	return models.PolicyDecision{
		Risk:                 risk,
		RequiresConfirmation: plan.RequiresConfirmation || risk != "safe",
		Warnings:             warnings,
	}
}

func shortID() string {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes)
}
