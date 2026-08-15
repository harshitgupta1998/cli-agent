package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/harsgupta/termind/backend/internal/models"
)

type Agent interface {
	CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse
	Execute(payload models.CommandExecuteRequest) models.CommandExecuteResponse
	SearchMemory(query string, limit int) models.MemorySearchResponse
	Explain(command string) models.ExplainCommandResponse
	ProjectContext(cwd string) models.ProjectContext
	Phases() models.PhaseResponse
	MockCapabilities() models.MockCapabilityResponse
}

type MockAgent struct{}

func NewMockAgent() MockAgent {
	return MockAgent{}
}

func (m MockAgent) CreateRequest(payload models.UserRequestCreate) models.UserRequestResponse {
	intent, plan := m.inferPlan(payload.Input, payload.CWD)

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

func (m MockAgent) SearchMemory(query string, limit int) models.MemorySearchResponse {
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
	if len(filtered) == 0 {
		filtered = results
	}
	if limit > len(filtered) {
		limit = len(filtered)
	}

	return models.MemorySearchResponse{Results: filtered[:limit]}
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
		CurrentPhase: "phase_1",
		Phases: []models.Phase{
			{
				ID:          "phase_1",
				Name:        "Command Workbench",
				Status:      models.PhaseReady,
				Summary:     "Typed request to command plan, deterministic policy review, mock execution, and command-memory-shaped results.",
				Deliverable: "A working product skeleton that proves the review-run-remember loop without real shell execution.",
				Scope: []string{
					"React TypeScript command workbench",
					"Go API with typed request and response contracts",
					"Mock command planner",
					"Deterministic policy review scaffold",
					"Mock execution result capture",
					"Mock memory search",
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
				Status:      models.PhaseMocked,
				Summary:     "Replace static planner rules with Ollama structured output while keeping policy and execution owned by the app.",
				Deliverable: "Ollama-backed CommandPlan generation with schema validation and local-only mode.",
				Scope: []string{
					"Ollama client",
					"JSON schema validation",
					"Prompt templates",
					"Planner fallback handling",
					"Model health checks",
				},
				MockAPIs: []string{
					"GET /v1/mocks/capabilities",
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

func (m MockAgent) inferPlan(userInput string, cwd string) (string, models.CommandPlan) {
	normalized := strings.ToLower(userInput)

	if strings.Contains(normalized, "port") && strings.Contains(normalized, "8000") {
		return models.IntentExecuteCommand, models.CommandPlan{
			Command:              "lsof -i :8000",
			CWD:                  cwd,
			Risk:                 models.RiskSafe,
			RequiresConfirmation: true,
			Reason:               "Lists processes listening on port 8000.",
			Provenance: []string{
				"The user asked about port 8000.",
				"The command is read-only.",
				"lsof is commonly available on macOS and Linux.",
			},
			Alternatives: []models.AlternativeCommand{
				{
					Command: "netstat -vanp tcp | grep 8000",
					Reason:  "Alternative socket inspection command.",
				},
			},
		}
	}

	if strings.Contains(normalized, "start") || strings.Contains(normalized, "run") || strings.Contains(normalized, "dev") {
		return models.IntentExecuteCommand, models.CommandPlan{
			Command:              "go run ./cmd/server",
			CWD:                  cwd,
			Risk:                 models.RiskModifying,
			RequiresConfirmation: true,
			Reason:               "Starts the remembered Go backend API for this project.",
			Provenance: []string{
				"This project is now a Go backend.",
				"A similar command succeeded in the demo memory store.",
				"Long-running development servers should be confirmed.",
			},
			Alternatives: []models.AlternativeCommand{},
		}
	}

	if strings.Contains(normalized, "search") || strings.Contains(normalized, "find") || strings.Contains(normalized, "used") {
		return models.IntentSearchHistory, models.CommandPlan{
			Command:              "",
			CWD:                  cwd,
			Risk:                 models.RiskSafe,
			RequiresConfirmation: false,
			Reason:               "This request is best handled by command memory search.",
			Provenance:           []string{"The user asked to find a remembered command."},
			Alternatives:         []models.AlternativeCommand{},
		}
	}

	return models.IntentExecuteCommand, models.CommandPlan{
		Command:              "pwd",
		CWD:                  cwd,
		Risk:                 models.RiskSafe,
		RequiresConfirmation: true,
		Reason:               "Fallback mock command that shows the current working directory.",
		Provenance: []string{
			"No specialized mock planner rule matched this request.",
			"pwd is safe and read-only.",
		},
		Alternatives: []models.AlternativeCommand{},
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
