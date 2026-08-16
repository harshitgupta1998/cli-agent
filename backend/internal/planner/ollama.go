package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

type OllamaPlanner struct {
	baseURL string
	model   string
	client  *http.Client
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Format   string          `json:"format"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
}

type llmPlan struct {
	Intent               string                      `json:"intent"`
	Command              string                      `json:"command"`
	CWD                  string                      `json:"cwd"`
	Risk                 string                      `json:"risk"`
	RequiresConfirmation bool                        `json:"requires_confirmation"`
	Reason               string                      `json:"reason"`
	Provenance           []string                    `json:"provenance"`
	Alternatives         []models.AlternativeCommand `json:"alternatives"`
}

func NewOllamaPlanner(baseURL string, model string) OllamaPlanner {
	return OllamaPlanner{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (p OllamaPlanner) Plan(payload models.UserRequestCreate) (string, models.CommandPlan, error) {
	if strings.TrimSpace(p.baseURL) == "" || strings.TrimSpace(p.model) == "" {
		return "", models.CommandPlan{}, errors.New("ollama planner not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	requestPayload := ollamaChatRequest{
		Model:  p.model,
		Format: "json",
		Stream: false,
		Messages: []ollamaMessage{
			{
				Role: "system",
				Content: strings.TrimSpace(`
You are Termind's local command planner.
Return only valid JSON. Do not include markdown.
You do not execute commands. The application owns execution.
Prefer safe, inspectable commands. Use "search_history" intent when the user asks to find or recall a prior command.
Prefer standard POSIX/macOS shell commands. Avoid unnecessary grep/awk pipelines when a direct find, du, sort, head, lsof, or ps command is clearer.
For "largest files", prefer: find . -maxdepth 1 -type f -exec du -h {} + | sort -hr | head -n 10

JSON shape:
{
  "intent": "execute_command" | "search_history" | "unknown",
  "command": "shell command or empty string",
  "cwd": "working directory",
  "risk": "safe" | "modifying" | "destructive" | "privileged" | "networked" | "unknown",
  "requires_confirmation": true,
  "reason": "short reason",
  "provenance": ["why this was suggested"],
  "alternatives": [{"command": "...", "reason": "..."}]
}

Never classify sudo, rm, git reset --hard, docker system prune, chmod -R, chown -R, kubectl delete, or DROP TABLE as safe.
Never invent placeholder words inside shell pipelines. The command should be runnable as written.
`),
			},
			{
				Role: "user",
				Content: fmt.Sprintf(
					"User request: %q\nCurrent working directory: %q\nReturn a command plan JSON object.",
					payload.Input,
					payload.CWD,
				),
			},
		},
		Options: map[string]any{
			"temperature": 0,
		},
	}

	body, err := json.Marshal(requestPayload)
	if err != nil {
		return "", models.CommandPlan{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", models.CommandPlan{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return "", models.CommandPlan{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", models.CommandPlan{}, fmt.Errorf("ollama returned %d", response.StatusCode)
	}

	var chatResponse ollamaChatResponse
	if err := json.NewDecoder(response.Body).Decode(&chatResponse); err != nil {
		return "", models.CommandPlan{}, err
	}

	var rawPlan llmPlan
	if err := json.Unmarshal([]byte(chatResponse.Message.Content), &rawPlan); err != nil {
		return "", models.CommandPlan{}, fmt.Errorf("decode ollama plan: %w", err)
	}

	return normalizePlan(rawPlan, payload.CWD)
}

func normalizePlan(raw llmPlan, fallbackCWD string) (string, models.CommandPlan, error) {
	intent := strings.TrimSpace(raw.Intent)
	if intent == "" {
		intent = models.IntentExecuteCommand
	}
	if intent != models.IntentExecuteCommand && intent != models.IntentSearchHistory && intent != "unknown" {
		return "", models.CommandPlan{}, fmt.Errorf("invalid intent %q", intent)
	}

	cwd := strings.TrimSpace(raw.CWD)
	if cwd == "" {
		cwd = fallbackCWD
	}
	risk := normalizeRisk(raw.Risk)

	plan := models.CommandPlan{
		Command:              strings.TrimSpace(raw.Command),
		CWD:                  cwd,
		Risk:                 risk,
		RequiresConfirmation: true,
		Reason:               strings.TrimSpace(raw.Reason),
		Provenance:           raw.Provenance,
		Alternatives:         raw.Alternatives,
	}
	if raw.RequiresConfirmation {
		plan.RequiresConfirmation = true
	}
	if plan.Reason == "" {
		plan.Reason = "Generated by the local Ollama planner."
	}
	if len(plan.Provenance) == 0 {
		plan.Provenance = []string{"Generated by the local Ollama planner."}
	}
	if intent == models.IntentExecuteCommand && plan.Command == "" {
		return "", models.CommandPlan{}, errors.New("execute_command plan missing command")
	}

	return intent, plan, nil
}

func normalizeRisk(risk string) string {
	switch strings.TrimSpace(risk) {
	case models.RiskSafe, models.RiskModifying, models.RiskDestructive, models.RiskPrivileged, "networked", "unknown":
		return strings.TrimSpace(risk)
	default:
		return "unknown"
	}
}
