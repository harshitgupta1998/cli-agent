package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/harsgupta/termind/backend/internal/auth"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/policy"
	"github.com/harsgupta/termind/backend/internal/services"
)

type Server struct {
	cfg         config.Config
	agent       services.Agent
	auth        *auth.Manager
	approvalKey []byte
}

func NewServer(cfg config.Config, agent services.Agent, authManagers ...auth.Manager) Server {
	key := sha256.Sum256([]byte(cfg.CommandApprovalSecret))
	approvalKey := key[:]
	if cfg.CommandApprovalSecret == "" {
		approvalKey = make([]byte, 32)
		if _, err := rand.Read(approvalKey); err != nil {
			panic("initialize approval signing key: " + err.Error())
		}
	}
	server := Server{cfg: cfg, agent: agent, approvalKey: approvalKey}
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
	mux.HandleFunc("GET /v1/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /v1/workspaces", s.createWorkspace)
	mux.HandleFunc("GET /v1/workspaces/{workspace_id}", s.getWorkspace)
	mux.HandleFunc("PATCH /v1/workspaces/{workspace_id}", s.updateWorkspace)
	mux.HandleFunc("GET /v1/workspaces/{workspace_id}/permissions", s.getWorkspacePermissions)
	mux.HandleFunc("PUT /v1/workspaces/{workspace_id}/permissions", s.updateWorkspacePermissions)
	mux.HandleFunc("POST /v1/workspaces/{workspace_id}/adopt", s.adoptWorkspace)
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
				principal := auth.Principal{UserID: "usr_local", Name: "Local User"}
				if s.auth != nil && s.auth.LocalPrincipal().UserID != "" {
					principal = s.auth.LocalPrincipal()
				}
				ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
				next.ServeHTTP(w, r.WithContext(ctx))
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

func (s Server) requestPrincipal(r *http.Request) auth.Principal {
	if principal, ok := principalFromContext(r.Context()); ok {
		return principal
	}
	return auth.Principal{UserID: "usr_local", Name: "Local User"}
}

func (s Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	principal := s.requestPrincipal(r)
	writeJSON(w, http.StatusOK, s.agent.ListWorkspaces(principal.UserID))
}

func (s Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var payload models.WorkspaceCreateRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.RootPath) == "" {
		writeError(w, http.StatusBadRequest, "workspace_root_required")
		return
	}
	principal := s.requestPrincipal(r)
	workspace, err := s.agent.CreateWorkspace(principal.UserID, payload)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (s Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	principal := s.requestPrincipal(r)
	workspace, err := s.agent.GetWorkspace(principal.UserID, strings.TrimSpace(r.PathValue("workspace_id")))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	var payload models.WorkspaceUpdateRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	principal := s.requestPrincipal(r)
	workspace, err := s.agent.UpdateWorkspace(principal.UserID, strings.TrimSpace(r.PathValue("workspace_id")), payload.Name)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s Server) adoptWorkspace(w http.ResponseWriter, r *http.Request) {
	principal := s.requestPrincipal(r)
	workspace, err := s.agent.AdoptLegacyWorkspace(principal.UserID, strings.TrimSpace(r.PathValue("workspace_id")))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s Server) getWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	principal := s.requestPrincipal(r)
	permissions, err := s.agent.WorkspacePermissions(principal.UserID, strings.TrimSpace(r.PathValue("workspace_id")))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, permissions)
}

func (s Server) updateWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	var update models.WorkspacePermissionsUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	if err := policy.ValidateWorkspacePermissions(update); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	principal := s.requestPrincipal(r)
	workspaceID := strings.TrimSpace(r.PathValue("workspace_id"))
	if _, err := s.agent.GetWorkspace(principal.UserID, workspaceID); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	permissions, err := s.agent.UpdateWorkspacePermissions(principal.UserID, workspaceID, update)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, permissions)
}

func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memory.ErrWorkspaceNotFound):
		writeError(w, http.StatusNotFound, "workspace_not_found")
	case errors.Is(err, memory.ErrWorkspaceConflict):
		writeError(w, http.StatusConflict, "workspace_root_already_exists")
	case err.Error() == "workspace_root_required", err.Error() == "workspace_name_invalid":
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, "workspace_storage_unavailable")
	}
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
	principal := s.requestPrincipal(r)
	workspace, err := s.agent.ResolveWorkspace(principal.UserID, payload.WorkspaceID, payload.CWD, true)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	payload.OwnerUserID = principal.UserID
	payload.WorkspaceID = workspace.ID

	writeJSON(w, http.StatusOK, s.agent.CreateSession(payload))
}

func (s Server) listSessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}

	principal := s.requestPrincipal(r)
	if _, err := s.agent.SessionWorkspace(principal.UserID, sessionID); err != nil {
		writeError(w, http.StatusNotFound, "session_not_found")
		return
	}
	writeJSON(w, http.StatusOK, s.agent.ListMessages(principal.UserID, sessionID))
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
	principal := s.requestPrincipal(r)
	workspace, err := s.resolveRequestWorkspace(principal, payload.SessionID, payload.WorkspaceID, payload.CWD, true)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	payload.OwnerUserID = principal.UserID
	payload.WorkspaceID = workspace.ID

	response := s.agent.CreateRequest(payload)
	if response.Intent == models.IntentExecuteCommand && strings.TrimSpace(response.Plan.Command) != "" {
		claims := approvalClaims{
			UserID: principal.UserID, SessionID: payload.SessionID, WorkspaceID: workspace.ID,
			RequestID: response.RequestID, Command: response.Plan.Command, CWD: payload.CWD,
			ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
		}
		if s.cfg.LocalOnlyMode != "true" {
			permissions, err := s.agent.WorkspacePermissions(principal.UserID, workspace.ID)
			if err != nil {
				writeWorkspaceError(w, err)
				return
			}
			claims.PolicyVersion = permissions.PolicyVersion
			grantID, err := s.agent.CreateCommandApproval(claims.UserID, claims.WorkspaceID, claims.SessionID,
				claims.RequestID, claims.PolicyVersion, claims.Command, claims.CWD, time.Unix(claims.ExpiresAt, 0))
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "approval_storage_unavailable")
				return
			}
			claims.ApprovalID = grantID
			response.ApprovalToken = s.issueApprovalToken(claims)
		}
	}
	writeJSON(w, http.StatusOK, response)
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
	principal := s.requestPrincipal(r)
	if payload.SessionID != "" {
		workspace, err := s.resolveRequestWorkspace(principal, payload.SessionID, payload.WorkspaceID, payload.CWD, false)
		if err != nil {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		payload.WorkspaceID = workspace.ID
	} else if payload.WorkspaceID != "" {
		workspace, err := s.agent.ResolveWorkspace(principal.UserID, payload.WorkspaceID, payload.CWD, false)
		if err != nil {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		payload.WorkspaceID = workspace.ID
	} else if s.cfg.LocalOnlyMode != "true" {
		writeError(w, http.StatusBadRequest, "session_required")
		return
	}
	payload.OwnerUserID = principal.UserID
	if payload.WorkspaceID != "" {
		permissions, err := s.agent.WorkspacePermissions(principal.UserID, payload.WorkspaceID)
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		workspace, err := s.agent.GetWorkspace(principal.UserID, payload.WorkspaceID)
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		if code, message := policy.EvaluateWorkspaceCommand(permissions, workspace.RootPath, payload.CWD, payload.Command); code != "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": code, "message": message})
			return
		}
		payload.ExecutionTimeoutSeconds = permissions.MaxDurationSeconds
		payload.MaxOutputBytes = permissions.MaxOutputBytes
		payload.WorkspacePolicyVersion = permissions.PolicyVersion
		payload.WorkspacePolicyDecision = "allowed"
		if payload.Confirmation.Status == models.ConfirmationApproved {
			if payload.ApprovalToken == "" && s.cfg.LocalOnlyMode != "true" {
				writeError(w, http.StatusForbidden, "approval_token_required")
				return
			}
			if payload.ApprovalToken != "" {
				expected := approvalClaims{
					UserID: principal.UserID, SessionID: payload.SessionID, WorkspaceID: payload.WorkspaceID,
					RequestID: payload.RequestID, Command: payload.Command, CWD: payload.CWD,
					PolicyVersion: permissions.PolicyVersion,
				}
				claims, valid := s.parseApprovalToken(payload.ApprovalToken)
				if !valid || claims.ApprovalID == "" || !approvalClaimsMatch(claims, expected) {
					writeError(w, http.StatusForbidden, "approval_token_invalid")
					return
				}
				if err := s.agent.ConsumeCommandApproval(claims.ApprovalID, principal.UserID, payload.WorkspaceID,
					payload.SessionID, payload.RequestID, permissions.PolicyVersion, payload.Command, payload.CWD); err != nil {
					if errors.Is(err, memory.ErrApprovalInvalid) {
						writeError(w, http.StatusForbidden, "approval_token_invalid")
					} else {
						writeError(w, http.StatusServiceUnavailable, "approval_storage_unavailable")
					}
					return
				}
			}
		}
		resolvedCWD, err := policy.CanonicalWorkingDirectory(payload.CWD)
		if err != nil {
			writeError(w, http.StatusForbidden, "working_directory_invalid")
			return
		}
		payload.CWD = resolvedCWD
	}

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.streamCommand(w, r, payload)
		return
	}
	writeJSON(w, http.StatusOK, s.agent.Execute(payload))
}

type approvalClaims struct {
	ApprovalID    string `json:"i"`
	PolicyVersion int    `json:"p"`
	UserID        string `json:"u"`
	SessionID     string `json:"s"`
	WorkspaceID   string `json:"w"`
	RequestID     string `json:"r"`
	Command       string `json:"c"`
	CWD           string `json:"d"`
	ExpiresAt     int64  `json:"e"`
}

func (s Server) issueApprovalToken(claims approvalClaims) string {
	encoded, err := json.Marshal(claims)
	if err != nil {
		return ""
	}
	payload := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, s.approvalKey)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s Server) verifyApprovalToken(token string, expected approvalClaims) bool {
	claims, valid := s.parseApprovalToken(token)
	return valid && approvalClaimsMatch(claims, expected)
}

func approvalClaimsMatch(claims approvalClaims, expected approvalClaims) bool {
	return claims.UserID == expected.UserID && claims.SessionID == expected.SessionID &&
		claims.WorkspaceID == expected.WorkspaceID && claims.RequestID == expected.RequestID &&
		claims.Command == expected.Command && claims.CWD == expected.CWD && claims.PolicyVersion == expected.PolicyVersion &&
		claims.ExpiresAt > time.Now().Unix()
}

func (s Server) parseApprovalToken(token string) (approvalClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return approvalClaims{}, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return approvalClaims{}, false
	}
	mac := hmac.New(sha256.New, s.approvalKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return approvalClaims{}, false
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return approvalClaims{}, false
	}
	var claims approvalClaims
	if json.Unmarshal(encoded, &claims) != nil || claims.ApprovalID == "" || claims.ExpiresAt <= time.Now().Unix() {
		return approvalClaims{}, false
	}
	return claims, true
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
	if strings.TrimSpace(payload.SessionID) == "" {
		writeError(w, http.StatusBadRequest, "session_id_required")
		return
	}
	if strings.TrimSpace(payload.CWD) == "" {
		writeError(w, http.StatusBadRequest, "cwd_required")
		return
	}
	principal := s.requestPrincipal(r)
	workspace, err := s.resolveRequestWorkspace(principal, payload.SessionID, payload.WorkspaceID, payload.CWD, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	payload.OwnerUserID = principal.UserID
	payload.WorkspaceID = workspace.ID
	permissions, err := s.agent.WorkspacePermissions(principal.UserID, workspace.ID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	payload.WorkspacePolicyVersion = permissions.PolicyVersion
	payload.WorkspacePolicyDecision = "host_execution_recorded_without_server_authorization"

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
	principal := s.requestPrincipal(r)
	if payload.WorkspaceID != "" && strings.TrimSpace(payload.CWD) == "" {
		if _, err := s.agent.GetWorkspace(principal.UserID, payload.WorkspaceID); err != nil {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
	} else if strings.TrimSpace(payload.CWD) != "" {
		workspace, err := s.agent.ResolveWorkspace(principal.UserID, payload.WorkspaceID, payload.CWD, false)
		if err != nil {
			if errors.Is(err, memory.ErrWorkspaceNotFound) {
				writeJSON(w, http.StatusOK, models.MemorySearchResponse{Results: []models.MemorySearchResult{}})
				return
			}
			writeWorkspaceError(w, err)
			return
		}
		payload.WorkspaceID = workspace.ID
	} else {
		writeError(w, http.StatusBadRequest, "workspace_or_cwd_required")
		return
	}
	payload.OwnerUserID = principal.UserID

	writeJSON(w, http.StatusOK, s.agent.SearchMemory(payload))
}

func (s Server) backfillCommandEmbeddings(w http.ResponseWriter, r *http.Request) {
	var payload models.EmbeddingBackfillRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	principal := s.requestPrincipal(r)
	if payload.WorkspaceID != "" {
		if _, err := s.agent.GetWorkspace(principal.UserID, payload.WorkspaceID); err != nil {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
	}
	payload.OwnerUserID = principal.UserID

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

	principal := s.requestPrincipal(r)
	workspace, err := s.agent.ResolveWorkspace(principal.UserID, "", cwd, true)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.agent.ProjectContext(principal.UserID, workspace.ID, cwd))
}

func (s Server) resolveRequestWorkspace(principal auth.Principal, sessionID string, workspaceID string, cwd string, create bool) (models.Workspace, error) {
	if sessionID != "" {
		workspace, err := s.agent.SessionWorkspace(principal.UserID, sessionID)
		if err == nil {
			if workspaceID != "" && workspaceID != workspace.ID {
				return models.Workspace{}, memory.ErrWorkspaceNotFound
			}
			if cwd != "" {
				return s.agent.ResolveWorkspace(principal.UserID, workspace.ID, cwd, false)
			}
			return workspace, nil
		}
		if s.cfg.LocalOnlyMode != "true" {
			return models.Workspace{}, memory.ErrWorkspaceNotFound
		}
	}
	return s.agent.ResolveWorkspace(principal.UserID, workspaceID, cwd, create)
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
