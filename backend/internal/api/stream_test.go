package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/planner"
	"github.com/harsgupta/termind/backend/internal/services"
)

func TestExecuteCommandStreamsOutputAndCompletion(t *testing.T) {
	dir := t.TempDir()
	agent := services.NewAgentService(nil, planner.NewRulePlanner(), time.Second).
		WithExecutionControls([]string{dir}, 1024, []string{"PATH"})
	handler := NewServer(config.Config{CORSOrigin: "*", LocalOnlyMode: "true"}, agent).Routes()
	body := `{"command":"printf 'streamed'","cwd":"` + dir + `","confirmation":{"status":"approved"}}`
	request := httptest.NewRequest(http.MethodPost, "/v1/commands/execute", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if !strings.Contains(response.Body.String(), "event: output\ndata: {\"stream\":\"stdout\",\"data\":\"streamed\"}") {
		t.Fatalf("stream response missing output event: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event: complete\n") || !strings.Contains(response.Body.String(), `"status":"completed"`) {
		t.Fatalf("stream response missing completion event: %s", response.Body.String())
	}
}
