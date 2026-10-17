package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/auth"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/planner"
	"github.com/harsgupta/termind/backend/internal/services"
)

type tokenRepository struct {
	principal auth.Principal
	hash      string
	revoked   bool
}

func (r *tokenRepository) EnsureUser(context.Context, auth.Principal) error { return nil }
func (r *tokenRepository) AuthenticateToken(_ context.Context, hash string) (auth.Principal, error) {
	if r.revoked || hash != r.hash {
		return auth.Principal{}, auth.ErrNotFound
	}
	return r.principal, nil
}
func (r *tokenRepository) CreateToken(_ context.Context, principal auth.Principal, name, hash string) (auth.Token, error) {
	r.principal, r.hash = principal, hash
	return auth.Token{ID: "tok_test", Name: name, CreatedAt: time.Now()}, nil
}
func (r *tokenRepository) RevokeToken(_ context.Context, _ auth.Principal, id string) error {
	if id != "tok_test" {
		return auth.ErrNotFound
	}
	r.revoked = true
	return nil
}

func TestRemoteModeRequiresValidBearer(t *testing.T) {
	agent := services.NewAgentService(nil, planner.NewRulePlanner(), time.Second)
	manager := auth.NewManager(nil, "bootstrap-token-at-least-32-characters", auth.Principal{UserID: "usr_owner", Name: "Owner"})
	handler := NewServer(config.Config{LocalOnlyMode: "false"}, agent, manager).Routes()

	for _, test := range []struct {
		name   string
		token  string
		status int
	}{{name: "missing", status: http.StatusUnauthorized}, {name: "valid", token: "bootstrap-token-at-least-32-characters", status: http.StatusOK}, {name: "invalid", token: "invalid", status: http.StatusUnauthorized}} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestTokenLifecycleEndpoints(t *testing.T) {
	repository := &tokenRepository{}
	principal := auth.Principal{UserID: "usr_owner", Name: "Owner"}
	manager := auth.NewManager(repository, "bootstrap-token-at-least-32-characters", principal)
	agent := services.NewAgentService(nil, planner.NewRulePlanner(), time.Second)
	handler := NewServer(config.Config{LocalOnlyMode: "false"}, agent, manager).Routes()

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/tokens", strings.NewReader(`{"name":"cli"}`))
	request.Header.Set("Authorization", "Bearer bootstrap-token-at-least-32-characters")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create token status = %d, body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Token  auth.Token `json:"token"`
		Secret string     `json:"secret"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || created.Token.ID != "tok_test" || repository.hash == created.Secret {
		t.Fatalf("unexpected create token response or plaintext persistence: %#v", created)
	}

	me := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	me.Header.Set("Authorization", "Bearer "+created.Secret)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusOK || !strings.Contains(meResponse.Body.String(), "usr_owner") {
		t.Fatalf("identity status=%d body=%s", meResponse.Code, meResponse.Body.String())
	}

	revoke := httptest.NewRequest(http.MethodDelete, "/v1/auth/tokens/tok_test", nil)
	revoke.Header.Set("Authorization", "Bearer bootstrap-token-at-least-32-characters")
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revoke)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}

	afterRevoke := httptest.NewRecorder()
	handler.ServeHTTP(afterRevoke, me)
	if afterRevoke.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d, want 401", afterRevoke.Code)
	}
}

func TestLocalModeAllowsAnonymousLegacyRequests(t *testing.T) {
	agent := services.NewAgentService(nil, planner.NewRulePlanner(), time.Second)
	handler := NewServer(config.Config{LocalOnlyMode: "true"}, agent).Routes()
	request := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("local anonymous config status = %d, want 200", response.Code)
	}
}

func TestApprovalTokenBindsCommandAndWorkspace(t *testing.T) {
	server := NewServer(config.Config{}, services.NewAgentService(nil, planner.NewRulePlanner(), time.Second))
	claims := approvalClaims{
		ApprovalID:    "apr_test",
		PolicyVersion: 1,
		UserID:        "usr_a", SessionID: "ses_a", WorkspaceID: "wsp_a", RequestID: "req_a",
		Command: "go test ./...", CWD: "/workspace", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}
	token := server.issueApprovalToken(claims)
	if !server.verifyApprovalToken(token, claims) {
		t.Fatal("valid approval token rejected")
	}
	claims.Command = "rm -rf ."
	if server.verifyApprovalToken(token, claims) {
		t.Fatal("approval token accepted a changed command")
	}
	claims.Command = "go test ./..."
	claims.WorkspaceID = "wsp_other"
	if server.verifyApprovalToken(token, claims) {
		t.Fatal("approval token accepted a different workspace")
	}
}
