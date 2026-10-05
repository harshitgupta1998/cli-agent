package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

func TestRunnerRunCapturesSuccessfulOutput(t *testing.T) {
	response := NewRunner(time.Second).Run(context.Background(), "printf 'hello'", "")

	if response.Status != models.CommandStatusCompleted {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.ExitCode == nil || *response.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", response.ExitCode)
	}
	if response.Stdout != "hello" {
		t.Fatalf("Stdout = %q, want hello", response.Stdout)
	}
}

func TestRunnerRunCapturesExitCodeAndStderr(t *testing.T) {
	response := NewRunner(time.Second).Run(context.Background(), "printf 'nope' >&2; exit 7", "")

	if response.Status != models.CommandStatusFailed {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.ExitCode == nil || *response.ExitCode != 7 {
		t.Fatalf("ExitCode = %v, want 7", response.ExitCode)
	}
	if response.Stderr != "nope" {
		t.Fatalf("Stderr = %q, want nope", response.Stderr)
	}
}

func TestRunnerRunTimesOut(t *testing.T) {
	response := NewRunner(10*time.Millisecond).Run(context.Background(), "sleep 1", "")

	if response.Status != models.CommandStatusFailed {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.ExitCode == nil {
		t.Fatal("ExitCode = nil, want value")
	}
	if !strings.Contains(response.Stderr, "Command timed out.") {
		t.Fatalf("Stderr = %q, want timeout message", response.Stderr)
	}
}
