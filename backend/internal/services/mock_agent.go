package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

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
	if payload.Confirmation.Status == "rejected" {
		return models.CommandExecuteResponse{
			CommandEventID: "cmd_" + shortID(),
			Status:         "rejected",
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
	case "uvicorn app.main:app --reload":
		stdout = "INFO: Uvicorn running on http://127.0.0.1:8000\nINFO: Application startup complete."
	case "pwd":
		stdout = payload.CWD
	}

	return models.CommandExecuteResponse{
		CommandEventID: "cmd_" + shortID(),
		Status:         "completed",
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
			CWD:            "/Users/example/projects/lifesummary-api",
			ExitCode:       0,
			Score:          0.94,
			MatchedReasons: []string{"keyword match", "same project", "successful command"},
			LastUsedAt:     "2026-08-14T19:20:00Z",
		},
		{
			CommandEventID: "cmd_demo_kill",
			Command:        "lsof -ti :8000 | xargs kill",
			UserRequest:    "kill the FastAPI process",
			CWD:            "/Users/example/projects/lifesummary-api",
			ExitCode:       0,
			Score:          0.88,
			MatchedReasons: []string{"semantic match", "same project", "successful command"},
			LastUsedAt:     "2026-08-13T16:45:00Z",
		},
		{
			CommandEventID: "cmd_demo_dev",
			Command:        "uvicorn app.main:app --reload",
			UserRequest:    "start the backend server",
			CWD:            "/Users/example/projects/lifesummary-api",
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
			Risk:     "modifying",
			Warnings: []string{"This can stop running processes.", "Confirm the port or process before running."},
		}
	}

	if strings.Contains(normalized, "rm") {
		return models.ExplainCommandResponse{
			Summary:  "Removes files or directories.",
			Risk:     "destructive",
			Warnings: []string{"This may permanently delete data."},
		}
	}

	return models.ExplainCommandResponse{
		Summary:  "This command is treated as a read-only or low-risk operation in the mock API.",
		Risk:     "safe",
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

func (m MockAgent) inferPlan(userInput string, cwd string) (string, models.CommandPlan) {
	normalized := strings.ToLower(userInput)

	if strings.Contains(normalized, "port") && strings.Contains(normalized, "8000") {
		return "execute_command", models.CommandPlan{
			Command:              "lsof -i :8000",
			CWD:                  cwd,
			Risk:                 "safe",
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
		return "execute_command", models.CommandPlan{
			Command:              "go run ./cmd/server",
			CWD:                  cwd,
			Risk:                 "modifying",
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
		return "search_history", models.CommandPlan{
			Command:              "",
			CWD:                  cwd,
			Risk:                 "safe",
			RequiresConfirmation: false,
			Reason:               "This request is best handled by command memory search.",
			Provenance:           []string{"The user asked to find a remembered command."},
			Alternatives:         []models.AlternativeCommand{},
		}
	}

	return "execute_command", models.CommandPlan{
		Command:              "pwd",
		CWD:                  cwd,
		Risk:                 "safe",
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
			risk = "destructive"
			warnings = append(warnings, "This command can permanently remove or reset data.")
			break
		}
	}

	for _, marker := range []string{"sudo", "chown -r", "chmod -r"} {
		if strings.Contains(command, marker) {
			risk = "privileged"
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
	return fmt.Sprintf("%x", time.Now().UnixNano())[:12]
}
