package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/services"
)

type Server struct {
	cfg   config.Config
	agent services.MockAgent
}

func NewServer(cfg config.Config, agent services.MockAgent) Server {
	return Server{cfg: cfg, agent: agent}
}

func (s Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/config", s.getConfig)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("POST /v1/requests", s.submitRequest)
	mux.HandleFunc("POST /v1/commands/execute", s.executeCommand)
	mux.HandleFunc("POST /v1/memory/search", s.searchMemory)
	mux.HandleFunc("POST /v1/commands/explain", s.explainCommand)
	mux.HandleFunc("GET /v1/context/project", s.getProjectContext)

	return s.withCORS(mux)
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"product":         "Termind",
		"local_only_mode": s.cfg.LocalOnlyMode == "true",
		"ollama_base_url": s.cfg.OllamaBaseURL,
		"database":        "postgres",
		"backend_runtime": "go",
		"mode":            "mock",
	})
}

func (s Server) createSession(w http.ResponseWriter, r *http.Request) {
	var payload models.SessionCreateRequest
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, models.SessionCreateResponse{
		SessionID: "ses_demo",
		ProjectID: "prj_demo",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s Server) submitRequest(w http.ResponseWriter, r *http.Request) {
	var payload models.UserRequestCreate
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, s.agent.CreateRequest(payload))
}

func (s Server) executeCommand(w http.ResponseWriter, r *http.Request) {
	var payload models.CommandExecuteRequest
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, s.agent.Execute(payload))
}

func (s Server) searchMemory(w http.ResponseWriter, r *http.Request) {
	var payload models.MemorySearchRequest
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, s.agent.SearchMemory(payload.Query, payload.Limit))
}

func (s Server) explainCommand(w http.ResponseWriter, r *http.Request) {
	var payload models.ExplainCommandRequest
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, s.agent.Explain(payload.Command))
}

func (s Server) getProjectContext(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		cwd = "/Users/example/projects/lifesummary-api"
	}

	writeJSON(w, http.StatusOK, s.agent.ProjectContext(cwd))
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

	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
