package executor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

func TestRunnerRunCapturesSuccessfulOutput(t *testing.T) {
	response := NewRunner(time.Second, []string{"/"}, 1024, []string{"PATH"}).Run(context.Background(), "printf 'hello'", "/")

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
	response := NewRunner(time.Second, []string{"/"}, 1024, []string{"PATH"}).Run(context.Background(), "printf 'nope' >&2; exit 7", "/")

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
	response := NewRunner(10*time.Millisecond, []string{"/"}, 1024, []string{"PATH"}).Run(context.Background(), "sleep 1", "/")

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

func TestRunnerStreamsOutputAndBoundsCapture(t *testing.T) {
	dir := t.TempDir()
	var chunks strings.Builder
	response := NewRunner(time.Second, []string{dir}, 8, []string{"PATH"}).RunWithOutput(
		context.Background(),
		"printf first; sleep 0.05; printf second",
		dir,
		func(chunk models.CommandOutputChunk) {
			if chunk.Stream == "stdout" {
				chunks.WriteString(chunk.Data)
			}
		},
	)

	if response.Stdout != "firstsec" {
		t.Fatalf("Stdout = %q, want bounded output firstsec", response.Stdout)
	}
	if chunks.String() != response.Stdout {
		t.Fatalf("streamed stdout = %q, want %q", chunks.String(), response.Stdout)
	}
	if !strings.Contains(response.Stderr, "output truncated") {
		t.Fatalf("Stderr = %q, want truncation marker", response.Stderr)
	}
}

func TestRunnerCancellationKillsCommand(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	response := NewRunner(5*time.Second, []string{dir}, 1024, []string{"PATH"}).Run(ctx, "sleep 5", dir)

	if response.Status != models.CommandStatusCancelled {
		t.Fatalf("Status = %q, want cancelled", response.Status)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took %s, want under 1s", time.Since(started))
	}
}

func TestRunnerBlocksWorkingDirectoryOutsideAllowedRoots(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	response := NewRunner(time.Second, []string{allowed}, 1024, []string{"PATH"}).Run(context.Background(), "pwd", outside)

	if response.Status != models.CommandStatusBlocked {
		t.Fatalf("Status = %q, want blocked", response.Status)
	}
	if !strings.Contains(response.Stderr, "outside configured execution roots") {
		t.Fatalf("Stderr = %q, want root restriction message", response.Stderr)
	}
}

func TestUnixWorkerStreamsExecution(t *testing.T) {
	dir := t.TempDir()
	socketPath := fmt.Sprintf("/tmp/termind-worker-test-%d.sock", os.Getpid())
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewWorkerHandler(NewRunner(time.Second, []string{dir}, 1024, []string{"PATH"}))}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	var chunks strings.Builder
	response := NewUnixClient(socketPath).RunWithOutput(
		context.Background(), "printf 'worker-output'", dir,
		func(chunk models.CommandOutputChunk) { chunks.WriteString(chunk.Data) },
	)
	if response.Status != models.CommandStatusCompleted || response.Stdout != "worker-output" {
		t.Fatalf("response = %#v, want completed worker output", response)
	}
	if chunks.String() != response.Stdout {
		t.Fatalf("streamed output = %q, want %q", chunks.String(), response.Stdout)
	}
}

func TestUnixWorkerCancellationPreservesStreamedOutput(t *testing.T) {
	dir := t.TempDir()
	socketPath := fmt.Sprintf("/tmp/termind-worker-cancel-%d.sock", os.Getpid())
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewWorkerHandler(NewRunner(5*time.Second, []string{dir}, 1024, []string{"PATH"}))}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	response := NewUnixClient(socketPath).RunWithOutput(
		ctx,
		"printf before; sleep 5",
		dir,
		func(models.CommandOutputChunk) { cancel() },
	)
	if response.Status != models.CommandStatusCancelled {
		t.Fatalf("Status = %q, want cancelled", response.Status)
	}
	if response.Stdout != "before" {
		t.Fatalf("Stdout = %q, want preserved partial output", response.Stdout)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took %s, want under 1s", time.Since(started))
	}
}
