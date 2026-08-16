package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/planner"
)

type Agent interface {
	CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse
	Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse
	RecordCommand(payload models.CommandRecordRequest) models.CommandRecordResponse
	SearchMemory(payload models.MemorySearchRequest) models.MemorySearchResponse
	Explain(command string) models.ExplainCommandResponse
	ProjectContext(cwd string) models.ProjectContext
	Phases() models.PhaseResponse
	MockCapabilities() models.MockCapabilityResponse
}

type MockAgent struct {
	memory  memory.Store
	planner planner.Planner
}

func NewMockAgent(memoryStore memory.Store, commandPlanner planner.Planner) MockAgent {
	return MockAgent{memory: memoryStore, planner: commandPlanner}
}

func (m MockAgent) CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse {
	if !isTerminalRequest(payload.Input) {
		return models.UserRequestResponse{
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
	}

	intent, plan, err := m.planner.Plan(payload)
	if err != nil {
		intent, plan, _ = planner.NewRulePlanner().Plan(payload)
	}

	return models.UserRequestResponse{
		RequestID: "req_" + shortID(),
		Intent:    intent,
		Plan:      plan,
		Policy:    evaluatePolicy(plan),
	}
}

func (m MockAgent) Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse {
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

	exitCode := 0
	command := strings.TrimSpace(payload.Command)
	stdout := fmt.Sprintf("Mock execution complete for: %s", command)

	switch command {
	case "lsof -i :8000":
		stdout = "COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME\npython3  92841 user   12u  IPv4 0x1234      0t0  TCP *:8000 (LISTEN)"
	case "go run ./cmd/server":
		stdout = "Termind API listening on :8000"
	case "pwd":
		stdout = payload.CWD
	}

	return models.CommandExecuteResponse{
		CommandEventID: "cmd_" + shortID(),
		Status:         models.CommandStatusCompleted,
		ExitCode:       &exitCode,
		Stdout:         stdout,
		Stderr:         "",
		DurationMS:     238,
	}
}

func (m MockAgent) RecordCommand(payload models.CommandRecordRequest) models.CommandRecordResponse {
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
		Message:        "Command event accepted by mock recorder. Persistence comes in phase_4.",
	}
}

func (m MockAgent) SearchMemory(payload models.MemorySearchRequest) models.MemorySearchResponse {
	if m.memory != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		response, err := m.memory.SearchCommands(ctx, payload)
		if err == nil && len(response.Results) > 0 {
			return response
		}
	}

	query := payload.Query
	limit := payload.Limit
	if limit <= 0 {
		limit = 5
	}

	results := []models.MemorySearchResult{
		{
			CommandEventID: "cmd_demo_port",
			Command:        "lsof -i :8000",
			UserRequest:    "what is using port 8000?",
			CWD:            "/Users/example/projects/termind",
			ExitCode:       0,
			Score:          0.94,
			MatchedReasons: []string{"keyword match", "same project", "successful command"},
			LastUsedAt:     "2026-08-14T19:20:00Z",
		},
		{
			CommandEventID: "cmd_demo_kill",
			Command:        "lsof -ti :8000 | xargs kill",
			UserRequest:    "kill the backend process",
			CWD:            "/Users/example/projects/termind",
			ExitCode:       0,
			Score:          0.88,
			MatchedReasons: []string{"semantic match", "same project", "successful command"},
			LastUsedAt:     "2026-08-13T16:45:00Z",
		},
		{
			CommandEventID: "cmd_demo_dev",
			Command:        "go run ./cmd/server",
			UserRequest:    "start the backend server",
			CWD:            "/Users/example/projects/termind",
			ExitCode:       0,
			Score:          0.81,
			MatchedReasons: []string{"project command", "recency", "successful command"},
			LastUsedAt:     "2026-08-12T10:15:00Z",
		},
	}

	filtered := make([]models.MemorySearchResult, 0, len(results))
	needle := strings.ToLower(query)
	for _, result := range results {
		if strings.Contains(strings.ToLower(result.UserRequest), needle) || strings.Contains(strings.ToLower(result.Command), needle) {
			filtered = append(filtered, result)
		}
	}
	if limit > len(filtered) {
		limit = len(filtered)
	}

	return models.MemorySearchResponse{Results: filtered[:limit]}
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

func (m MockAgent) Explain(command string) models.ExplainCommandResponse {
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

func (m MockAgent) ProjectContext(cwd string) models.ProjectContext {
	return models.ProjectContext{
		ProjectID: "prj_demo",
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
		CommonCommands: []models.ProjectCommand{
			{
				Label:        "Run backend API",
				Command:      "go run ./cmd/server",
				SuccessCount: 8,
			},
			{
				Label:        "Inspect port 8000",
				Command:      "lsof -i :8000",
				SuccessCount: 4,
			},
		},
	}
}

func (m MockAgent) Phases() models.PhaseResponse {
	return models.PhaseResponse{
		CurrentPhase: "phase_2",
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
				Status:      models.PhaseMocked,
				Summary:     "Add controlled process execution with streaming, cancellation, timeout, and audit events.",
				Deliverable: "A real executor that can run approved commands safely from the Go backend.",
				Scope: []string{
					"Process runner",
					"PTY support",
					"Streaming stdout and stderr",
					"Cancellation",
					"Timeout policy",
					"Environment allowlist",
				},
				MockAPIs: []string{
					"POST /v1/commands/execute",
				},
			},
			{
				ID:          "phase_4",
				Name:        "Persistent Memory",
				Status:      models.PhaseMocked,
				Summary:     "Persist sessions, messages, command events, and project commands instead of returning static mock data.",
				Deliverable: "Postgres-backed command event store with searchable command history.",
				Scope: []string{
					"Repository layer",
					"Command event persistence",
					"Session persistence",
					"Project command learning",
					"Keyword search",
				},
				MockAPIs: []string{
					"POST /v1/memory/search",
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

func (m MockAgent) MockCapabilities() models.MockCapabilityResponse {
	return models.MockCapabilityResponse{
		Capabilities: []models.MockCapability{
			{
				ID:          "ollama_planner",
				Phase:       "phase_2",
				Status:      "mocked",
				Description: "Static planner rules stand in for Ollama structured CommandPlan output.",
				Endpoints:   []string{"POST /v1/requests"},
				NextSteps: []string{
					"Create an Ollama client",
					"Add request and response schema validation",
					"Add model availability checks",
				},
			},
			{
				ID:          "command_executor",
				Phase:       "phase_3",
				Status:      "mocked",
				Description: "Execution returns canned stdout, stderr, exit code, and duration without running a shell command.",
				Endpoints:   []string{"POST /v1/commands/execute"},
				NextSteps: []string{
					"Implement a controlled process runner",
					"Add streaming output",
					"Add cancellation and timeouts",
				},
			},
			{
				ID:          "memory_store",
				Phase:       "phase_4",
				Status:      "mocked",
				Description: "Memory search returns seeded command events while the database schema is already present.",
				Endpoints:   []string{"POST /v1/memory/search"},
				NextSteps: []string{
					"Add repository interfaces",
					"Persist command events after execution",
					"Search command_events with keyword ranking",
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
