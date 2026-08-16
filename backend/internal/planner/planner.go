package planner

import (
	"strings"

	"github.com/harsgupta/termind/backend/internal/models"
)

type Planner interface {
	Plan(payload models.UserRequestCreate) (string, models.CommandPlan, error)
}

type RulePlanner struct{}

func NewRulePlanner() RulePlanner {
	return RulePlanner{}
}

func (p RulePlanner) Plan(payload models.UserRequestCreate) (string, models.CommandPlan, error) {
	normalized := strings.ToLower(payload.Input)

	if strings.Contains(normalized, "port") && strings.Contains(normalized, "8000") {
		return models.IntentExecuteCommand, models.CommandPlan{
			Command:              "lsof -i :8000",
			CWD:                  payload.CWD,
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
		}, nil
	}

	if strings.Contains(normalized, "start") || strings.Contains(normalized, "run") || strings.Contains(normalized, "dev") {
		return models.IntentExecuteCommand, models.CommandPlan{
			Command:              "go run ./cmd/server",
			CWD:                  payload.CWD,
			Risk:                 models.RiskModifying,
			RequiresConfirmation: true,
			Reason:               "Starts the remembered Go backend API for this project.",
			Provenance: []string{
				"This project is now a Go backend.",
				"A similar command succeeded in the demo memory store.",
				"Long-running development servers should be confirmed.",
			},
			Alternatives: []models.AlternativeCommand{},
		}, nil
	}

	if strings.Contains(normalized, "search") || strings.Contains(normalized, "find") || strings.Contains(normalized, "used") {
		return models.IntentSearchHistory, models.CommandPlan{
			Command:              "",
			CWD:                  payload.CWD,
			Risk:                 models.RiskSafe,
			RequiresConfirmation: false,
			Reason:               "This request is best handled by command memory search.",
			Provenance:           []string{"The user asked to find a remembered command."},
			Alternatives:         []models.AlternativeCommand{},
		}, nil
	}

	return models.IntentExecuteCommand, models.CommandPlan{
		Command:              "pwd",
		CWD:                  payload.CWD,
		Risk:                 models.RiskSafe,
		RequiresConfirmation: true,
		Reason:               "Fallback command that shows the current working directory.",
		Provenance: []string{
			"No specialized planner rule matched this request.",
			"pwd is safe and read-only.",
		},
		Alternatives: []models.AlternativeCommand{},
	}, nil
}

type FallbackPlanner struct {
	primary  Planner
	fallback Planner
}

func NewFallbackPlanner(primary Planner, fallback Planner) FallbackPlanner {
	return FallbackPlanner{primary: primary, fallback: fallback}
}

func (p FallbackPlanner) Plan(payload models.UserRequestCreate) (string, models.CommandPlan, error) {
	if p.primary != nil {
		intent, plan, err := p.primary.Plan(payload)
		if err == nil {
			return intent, plan, nil
		}
	}
	return p.fallback.Plan(payload)
}
