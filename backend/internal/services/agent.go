package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/planner"
)

type Agent interface {
	CreateSession(payload models.SessionCreateRequest) models.SessionCreateResponse
	ListMessages(sessionID string) models.MessageListResponse
	CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse
	Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse
	RecordCommand(payload models.CommandRecordRequest) models.CommandRecordResponse
	SearchMemory(payload models.MemorySearchRequest) models.MemorySearchResponse
	Explain(command string) models.ExplainCommandResponse
	ProjectContext(cwd string) models.ProjectContext
	Phases() models.PhaseResponse
	MockCapabilities() models.MockCapabilityResponse
}

type AgentService struct {
	memory         memory.Store
	planner        planner.Planner
	commandTimeout time.Duration
}

func NewAgentService(memoryStore memory.Store, commandPlanner planner.Planner, commandTimeout time.Duration) AgentService {
	return AgentService{memory: memoryStore, planner: commandPlanner, commandTimeout: commandTimeout}
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
		if persistedID := m.persistExecution(payload, response); persistedID != "" {
			response.CommandEventID = persistedID
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
	if persistedID := m.persistExecution(payload, response); persistedID != "" {
		response.CommandEventID = persistedID
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
		CommandEventID: "cmd_" + shortID(),
		Status:         models.CommandStatusCompleted,
		Message:        "Command event accepted by fallback recorder. Postgres persistence is unavailable.",
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

func (m AgentService) persistExecution(payload models.CommandExecuteRequest, response models.CommandExecuteResponse) string {
	if m.memory == nil {
		return ""
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
		return ""
	}
	return record.CommandEventID
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
		ProjectID: projectID,
		RootPath:  cwd,
		Git: models.GitContext{
			Branch:                "main",
			HasUncommittedChanges: true,
		},
		DetectedStack: models.DetectedStack{
			Language:       "go",
			Framework:      "net/http",
			PackageManager: "go modules",
		},
		CommonCommands: commonCommands,
	}
}

func (m AgentService) Phases() models.PhaseResponse {
	return models.PhaseResponse{
		CurrentPhase: "phase_4",
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
				MockAPIs: []string{
					"POST /v1/requests",
					"POST /v1/commands/execute",
					"POST /v1/memory/search",
					"GET /v1/context/project",
				},
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
				MockAPIs: []string{
					"GET /v1/config",
					"POST /v1/requests",
				},
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
				MockAPIs: []string{
					"POST /v1/commands/execute",
				},
			},
			{
				ID:          "phase_4",
				Name:        "Persistent Memory",
				Status:      models.PhaseInProgress,
				Summary:     "Persist real sessions, projects, and command events while removing fake command-history fallbacks.",
				Deliverable: "Postgres-backed sessions, projects, and searchable command history.",
				Scope: []string{
					"Repository layer",
					"Command event persistence",
					"Session persistence",
					"Message persistence",
					"Keyword search",
					"Project command learning",
				},
				MockAPIs: []string{
					"POST /v1/memory/search",
					"GET /v1/sessions/{session_id}/messages",
				},
			},
			{
				ID:          "phase_5",
				Name:        "Semantic Recall",
				Status:      models.PhaseMocked,
				Summary:     "Add local embeddings and hybrid ranking so users can find commands by intent, not only exact text.",
				Deliverable: "Hybrid keyword, semantic, recency, project, and success ranking.",
				Scope: []string{
					"Ollama embeddings",
					"Embedding storage",
					"Hybrid scorer",
					"Same-project boost",
					"Successful-command boost",
				},
				MockAPIs: []string{
					"POST /v1/memory/search",
				},
			},
			{
				ID:          "phase_6",
				Name:        "Voice Input",
				Status:      models.PhasePlanned,
				Summary:     "Add local speech-to-text as an input adapter after the typed command pipeline is stable.",
				Deliverable: "Microphone input feeding the same request pipeline as typed text.",
				Scope: []string{
					"Local speech-to-text adapter",
					"Transcript confirmation",
					"Same planning and policy pipeline",
				},
				MockAPIs: []string{
					"GET /v1/mocks/capabilities",
				},
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
				Status:      "in_progress",
				Description: "Sessions, projects, messages, and command events are persisted in Postgres; learned project commands remain next.",
				Endpoints:   []string{"POST /v1/memory/search"},
				NextSteps: []string{
					"Learn repeated project commands",
					"Replace static project context",
					"Add richer memory queries",
				},
			},
			{
				ID:          "semantic_recall",
				Phase:       "phase_5",
				Status:      "mocked",
				Description: "Search results include mock ranking reasons before local embeddings are implemented.",
				Endpoints:   []string{"POST /v1/memory/search"},
				NextSteps: []string{
					"Generate local embeddings with Ollama",
					"Store embeddings per command event",
					"Blend semantic and structured ranking signals",
				},
			},
			{
				ID:          "voice_input",
				Phase:       "phase_6",
				Status:      "planned",
				Description: "Voice is intentionally deferred until the typed planning and execution loop is stable.",
				Endpoints:   []string{"GET /v1/mocks/capabilities"},
				NextSteps: []string{
					"Choose local STT runtime",
					"Add transcript confirmation UI",
					"Send transcript through POST /v1/requests",
				},
			},
		},
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
