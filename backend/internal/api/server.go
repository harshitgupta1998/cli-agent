package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/services"
)

type Server struct {
	cfg   config.Config
	agent services.Agent
}

func NewServer(cfg config.Config, agent services.Agent) Server {
	return Server{cfg: cfg, agent: agent}
}

func (s Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/config", s.getConfig)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("GET /v1/sessions/{session_id}/messages", s.listSessionMessages)
	mux.HandleFunc("POST /v1/requests", s.submitRequest)
	mux.HandleFunc("POST /v1/commands/execute", s.executeCommand)
	mux.HandleFunc("POST /v1/commands/record", s.recordCommand)
	mux.HandleFunc("POST /v1/memory/search", s.searchMemory)
	mux.HandleFunc("POST /v1/commands/explain", s.explainCommand)
	mux.HandleFunc("GET /v1/context/project", s.getProjectContext)
	mux.HandleFunc("GET /v1/phases", s.getPhases)
	mux.HandleFunc("GET /v1/mocks/capabilities", s.getMockCapabilities)

	return s.withCORS(mux)
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"runtime": "go",
	})
}

func (s Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"product":                 "Termind",
		"local_only_mode":         s.cfg.LocalOnlyMode == "true",
		"ollama_base_url":         s.cfg.OllamaBaseURL,
		"ollama_model":            s.cfg.OllamaModel,
		"ollama_embed_model":      s.cfg.OllamaEmbedModel,
		"planner_mode":            s.cfg.PlannerMode,
		"command_timeout_seconds": int(s.cfg.CommandTimeout.Seconds()),
		"database":                "postgres",
		"backend_runtime":         "go",
		"mode":                    "phase_5_semantic_recall",
	})
}

func (s Server) createSession(w http.ResponseWriter, r *http.Request) {
	var payload models.SessionCreateRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.CWD) == "" {
		writeError(w, http.StatusBadRequest, "cwd_required")
		return
	}
	if strings.TrimSpace(payload.Shell) == "" {
		payload.Shell = "zsh"
	}

	writeJSON(w, http.StatusOK, s.agent.CreateSession(payload))
}

func (s Server) listSessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.ListMessages(sessionID))
}

func (s Server) submitRequest(w http.ResponseWriter, r *http.Request) {
	var payload models.UserRequestCreate
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Input) == "" {
		writeError(w, http.StatusBadRequest, "input_required")
		return
	}
	if strings.TrimSpace(payload.CWD) == "" {
		writeError(w, http.StatusBadRequest, "cwd_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.CreateRequest(payload))
}

func (s Server) executeCommand(w http.ResponseWriter, r *http.Request) {
	var payload models.CommandExecuteRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Command) == "" {
		writeError(w, http.StatusBadRequest, "command_required")
		return
	}
	if payload.Confirmation.Status != models.ConfirmationApproved && payload.Confirmation.Status != models.ConfirmationRejected {
		writeError(w, http.StatusBadRequest, "valid_confirmation_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.Execute(payload))
}

func (s Server) recordCommand(w http.ResponseWriter, r *http.Request) {
	var payload models.CommandRecordRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.FinalCommand) == "" {
		writeError(w, http.StatusBadRequest, "final_command_required")
		return
	}
	if strings.TrimSpace(payload.CWD) == "" {
		writeError(w, http.StatusBadRequest, "cwd_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.RecordCommand(payload))
}

func (s Server) searchMemory(w http.ResponseWriter, r *http.Request) {
	var payload models.MemorySearchRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Query) == "" {
		writeError(w, http.StatusBadRequest, "query_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.SearchMemory(payload))
}

func (s Server) explainCommand(w http.ResponseWriter, r *http.Request) {
	var payload models.ExplainCommandRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Command) == "" {
		writeError(w, http.StatusBadRequest, "command_required")
		return
	}

	writeJSON(w, http.StatusOK, s.agent.Explain(payload.Command))
}

func (s Server) getProjectContext(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		cwd = "/Users/example/projects/termind"
	}

	writeJSON(w, http.StatusOK, s.agent.ProjectContext(cwd))
}

func (s Server) getPhases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agent.Phases())
}

func (s Server) getMockCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agent.MockCapabilities())
}

func (s Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.CORSOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	if decoder.Decode(&struct{}{}) == nil {
		writeError(w, http.StatusBadRequest, "invalid_json_multiple_objects")
		return false
	}

	return true
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		if !errors.Is(err, http.ErrHandlerTimeout) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
