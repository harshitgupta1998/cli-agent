package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/harsgupta/termind/backend/internal/auth"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/services"
)

type Server struct {
	cfg   config.Config
	agent services.Agent
	auth  *auth.Manager
}

func NewServer(cfg config.Config, agent services.Agent, authManagers ...auth.Manager) Server {
	server := Server{cfg: cfg, agent: agent}
	if len(authManagers) > 0 {
		server.auth = &authManagers[0]
	}
	return server
}

func (s Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/auth/me", s.getIdentity)
	mux.HandleFunc("POST /v1/auth/tokens", s.createAPIToken)
	mux.HandleFunc("DELETE /v1/auth/tokens/{token_id}", s.revokeAPIToken)
	mux.HandleFunc("GET /v1/config", s.getConfig)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("GET /v1/sessions/{session_id}/messages", s.listSessionMessages)
	mux.HandleFunc("POST /v1/requests", s.submitRequest)
	mux.HandleFunc("POST /v1/commands/execute", s.executeCommand)
	mux.HandleFunc("POST /v1/commands/record", s.recordCommand)
	mux.HandleFunc("POST /v1/memory/search", s.searchMemory)
	mux.HandleFunc("POST /v1/memory/embeddings/backfill", s.backfillCommandEmbeddings)
	mux.HandleFunc("POST /v1/commands/explain", s.explainCommand)
	mux.HandleFunc("GET /v1/context/project", s.getProjectContext)
	mux.HandleFunc("GET /v1/phases", s.getPhases)
	mux.HandleFunc("GET /v1/mocks/capabilities", s.getMockCapabilities)
	mux.HandleFunc("GET /v1/voice/config", s.getVoiceConfig)
	mux.HandleFunc("POST /v1/voice/transcripts", s.createVoiceTranscript)

	return s.withCORS(s.withAuthentication(mux))
}

type principalContextKey struct{}

func principalFromContext(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(auth.Principal)
	return principal, ok
}

func (s Server) withAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if header == "" {
			if s.cfg.LocalOnlyMode == "true" {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "authentication_required")
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" || s.auth == nil {
			writeError(w, http.StatusUnauthorized, "invalid_bearer_token")
			return
		}
		principal, ok, err := s.auth.Authenticate(r.Context(), strings.TrimSpace(parts[1]))
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "authentication_unavailable")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_bearer_token")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s Server) getIdentity(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required")
		return
	}
	writeJSON(w, http.StatusOK, principal)
}

func (s Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok || s.auth == nil {
		writeError(w, http.StatusUnauthorized, "authentication_required")
		return
	}
	var payload struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	token, secret, err := s.auth.CreateToken(r.Context(), principal, payload.Name)
	if err != nil {
		if err.Error() == "token_name_required" || err.Error() == "token_name_too_long" {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "token_creation_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "secret": secret})
}

func (s Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok || s.auth == nil {
		writeError(w, http.StatusUnauthorized, "authentication_required")
		return
	}
	if err := s.auth.RevokeToken(r.Context(), principal, strings.TrimSpace(r.PathValue("token_id"))); err != nil {
		writeError(w, http.StatusNotFound, "token_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"runtime": "go",
	})
}

func (s Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"product":                  "Termind",
		"local_only_mode":          s.cfg.LocalOnlyMode == "true",
		"ollama_base_url":          s.cfg.OllamaBaseURL,
		"ollama_model":             s.cfg.OllamaModel,
		"ollama_embed_model":       s.cfg.OllamaEmbedModel,
		"planner_mode":             s.cfg.PlannerMode,
		"command_timeout_seconds":  int(s.cfg.CommandTimeout.Seconds()),
		"command_max_output_bytes": s.cfg.CommandMaxOutputBytes,
		"execution_allowed_roots":  s.cfg.ExecutionAllowedRoots,
		"execution_env_allowlist":  s.cfg.ExecutionEnvAllowlist,
		"database":                 "postgres",
		"backend_runtime":          "go",
		"mode":                     "phase_6_voice_input",
		"voice_input_enabled":      s.cfg.VoiceEnabled == "true",
		"voice_stt_provider":       s.cfg.VoiceSTTProvider,
		"voice_stt_command":        s.cfg.VoiceSTTCommand != "",
		"voice_max_audio_seconds":  s.cfg.VoiceMaxSeconds,
		"voice_max_audio_bytes":    s.cfg.VoiceMaxBytes,
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

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.streamCommand(w, r, payload)
		return
	}
	writeJSON(w, http.StatusOK, s.agent.Execute(payload))
}

func (s Server) streamCommand(w http.ResponseWriter, r *http.Request, payload models.CommandExecuteRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var writeMu sync.Mutex
	writeEvent := func(event string, payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write([]byte("event: " + event + "\ndata: ")); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	response := s.agent.ExecuteStream(r.Context(), payload, func(chunk models.CommandOutputChunk) {
		_ = writeEvent("output", chunk)
	})
	_ = writeEvent("complete", response)
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

func (s Server) backfillCommandEmbeddings(w http.ResponseWriter, r *http.Request) {
	var payload models.EmbeddingBackfillRequest
	if !decodeJSON(w, r, &payload) {
		return
	}

	writeJSON(w, http.StatusOK, s.agent.BackfillCommandEmbeddings(payload))
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

func (s Server) getVoiceConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agent.VoiceConfig())
}

func (s Server) createVoiceTranscript(w http.ResponseWriter, r *http.Request) {
	var payload models.VoiceTranscriptRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.MimeType) == "" {
		writeError(w, http.StatusBadRequest, "mime_type_required")
		return
	}

	response := s.agent.CreateVoiceTranscript(payload)
	switch response.Status {
	case "completed":
		writeJSON(w, http.StatusOK, response)
	case "payload_too_large":
		writeJSON(w, http.StatusRequestEntityTooLarge, response)
	case "audio_required", "unsupported_mime_type", "invalid_audio":
		writeJSON(w, http.StatusBadRequest, response)
	default:
		writeJSON(w, http.StatusNotImplemented, response)
	}
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
