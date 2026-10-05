package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

const defaultTimeout = 30 * time.Second

type Runner struct {
	timeout time.Duration
}

func NewRunner(timeout time.Duration) Runner {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return Runner{timeout: timeout}
}

func (r Runner) Run(ctx context.Context, command string, cwd string) models.CommandExecuteResponse {
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = cwd

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	durationMS := int(time.Since(started).Milliseconds())
	exitCode := 0
	status := models.CommandStatusCompleted
	if err != nil {
		exitCode = 1
		status = models.CommandStatusFailed
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if runCtx.Err() == context.DeadlineExceeded {
			stderr.WriteString("\nCommand timed out.")
		}
	}

	return models.CommandExecuteResponse{
		Status:     status,
		ExitCode:   &exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: durationMS,
	}
}
