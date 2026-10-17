package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

const defaultTimeout = 30 * time.Second
const defaultMaxOutputBytes = 1024 * 1024

type Runner struct {
	timeout        time.Duration
	allowedRoots   []string
	maxOutputBytes int
	envAllowlist   []string
}

type Executor interface {
	Run(ctx context.Context, command string, cwd string) models.CommandExecuteResponse
	RunWithOutput(ctx context.Context, command string, cwd string, onOutput func(models.CommandOutputChunk)) models.CommandExecuteResponse
}

type LimitedExecutor interface {
	RunWithLimits(ctx context.Context, command string, cwd string, timeout time.Duration, maxOutputBytes int, onOutput func(models.CommandOutputChunk)) models.CommandExecuteResponse
}

func NewRunner(timeout time.Duration, allowedRoots []string, maxOutputBytes int, envAllowlist []string) Executor {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	return Runner{
		timeout:        timeout,
		allowedRoots:   append([]string(nil), allowedRoots...),
		maxOutputBytes: maxOutputBytes,
		envAllowlist:   append([]string(nil), envAllowlist...),
	}
}

func (r Runner) Run(ctx context.Context, command string, cwd string) models.CommandExecuteResponse {
	return r.RunWithOutput(ctx, command, cwd, nil)
}

func (r Runner) RunWithOutput(ctx context.Context, command string, cwd string, onOutput func(models.CommandOutputChunk)) models.CommandExecuteResponse {
	return r.RunWithLimits(ctx, command, cwd, r.timeout, r.maxOutputBytes, onOutput)
}

func (r Runner) RunWithLimits(ctx context.Context, command string, cwd string, timeout time.Duration, maxOutputBytes int, onOutput func(models.CommandOutputChunk)) models.CommandExecuteResponse {
	if timeout <= 0 || timeout > r.timeout {
		timeout = r.timeout
	}
	if maxOutputBytes <= 0 || maxOutputBytes > r.maxOutputBytes {
		maxOutputBytes = r.maxOutputBytes
	}
	started := time.Now()
	resolvedCWD, err := ValidateWorkingDirectory(cwd, r.allowedRoots)
	if err != nil {
		return models.CommandExecuteResponse{
			Status:     models.CommandStatusBlocked,
			Stderr:     err.Error(),
			DurationMS: int(time.Since(started).Milliseconds()),
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = resolvedCWD
	cmd.Env = allowedEnvironment(os.Environ(), r.envAllowlist)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second

	output := &outputCapture{maxBytes: maxOutputBytes, onOutput: onOutput}
	cmd.Stdout = outputWriter{capture: output, stream: "stdout"}
	cmd.Stderr = outputWriter{capture: output, stream: "stderr"}

	err = cmd.Run()
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
			output.append("stderr", "\nCommand timed out.")
		} else if runCtx.Err() == context.Canceled {
			status = models.CommandStatusCancelled
			exitCode = 130
			output.append("stderr", "\nCommand cancelled.")
		}
	}

	return models.CommandExecuteResponse{
		Status:     status,
		ExitCode:   &exitCode,
		Stdout:     output.value("stdout"),
		Stderr:     output.value("stderr"),
		DurationMS: durationMS,
	}
}

func ValidateWorkingDirectory(cwd string, allowedRoots []string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", errors.New("Working directory is required.")
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", errors.New("Working directory does not exist or cannot be resolved.")
	}
	resolvedCWD, err = filepath.Abs(resolvedCWD)
	if err != nil {
		return "", errors.New("Working directory cannot be resolved.")
	}
	info, err := os.Stat(resolvedCWD)
	if err != nil || !info.IsDir() {
		return "", errors.New("Working directory must be an existing directory.")
	}
	for _, root := range allowedRoots {
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			continue
		}
		resolvedRoot, rootErr = filepath.Abs(resolvedRoot)
		if rootErr != nil {
			continue
		}
		relative, relErr := filepath.Rel(resolvedRoot, resolvedCWD)
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return resolvedCWD, nil
		}
	}
	return "", errors.New("Working directory is outside configured execution roots.")
}

func allowedEnvironment(environment []string, allowlist []string) []string {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, key := range allowlist {
		if key = strings.TrimSpace(key); key != "" {
			allowed[key] = struct{}{}
		}
	}
	filtered := make([]string, 0, len(allowed))
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

type outputWriter struct {
	capture *outputCapture
	stream  string
}

func (w outputWriter) Write(data []byte) (int, error) {
	w.capture.write(w.stream, data)
	return len(data), nil
}

type outputCapture struct {
	mu        sync.Mutex
	maxBytes  int
	usedBytes int
	values    map[string][]byte
	truncated bool
	onOutput  func(models.CommandOutputChunk)
}

func (c *outputCapture) write(stream string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = map[string][]byte{}
	}
	remaining := c.maxBytes - c.usedBytes
	if remaining <= 0 {
		c.markTruncated()
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
		chunk := append([]byte(nil), data...)
		c.values[stream] = append(c.values[stream], chunk...)
		c.usedBytes += len(chunk)
		if c.onOutput != nil && len(chunk) > 0 {
			c.onOutput(models.CommandOutputChunk{Stream: stream, Data: string(chunk)})
		}
		c.markTruncated()
		return
	}
	chunk := append([]byte(nil), data...)
	c.values[stream] = append(c.values[stream], chunk...)
	c.usedBytes += len(chunk)
	if c.onOutput != nil && len(chunk) > 0 {
		c.onOutput(models.CommandOutputChunk{Stream: stream, Data: string(chunk)})
	}
}

func (c *outputCapture) append(stream string, text string) {
	c.write(stream, []byte(text))
}

func (c *outputCapture) markTruncated() {
	if c.truncated {
		return
	}
	c.truncated = true
	marker := fmt.Sprintf("\n[output truncated at %d bytes]\n", c.maxBytes)
	if c.values == nil {
		c.values = map[string][]byte{}
	}
	c.values["stderr"] = append(c.values["stderr"], marker...)
	if c.onOutput != nil {
		c.onOutput(models.CommandOutputChunk{Stream: "stderr", Data: marker})
	}
}

func (c *outputCapture) value(stream string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.values[stream])
}
