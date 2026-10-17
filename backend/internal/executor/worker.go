package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/policy"
)

type workerRequest struct {
	Command string `json:"command"`
	CWD     string `json:"cwd"`
}

type unixClient struct {
	client *http.Client
}

func NewUnixClient(socketPath string) Executor {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return unixClient{
		client: &http.Client{Transport: transport},
	}
}

func (c unixClient) Run(ctx context.Context, command string, cwd string) models.CommandExecuteResponse {
	return c.RunWithOutput(ctx, command, cwd, nil)
}

func (c unixClient) RunWithOutput(ctx context.Context, command string, cwd string, onOutput func(models.CommandOutputChunk)) models.CommandExecuteResponse {
	started := time.Now()
	body, err := json.Marshal(workerRequest{Command: command, CWD: cwd})
	if err != nil {
		return workerFailure(err, started)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://termind-executor/execute", bytes.NewReader(body))
	if err != nil {
		return workerFailure(err, started)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := c.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return models.CommandExecuteResponse{Status: models.CommandStatusCancelled, ExitCode: intPointer(130), Stderr: "Command cancelled.", DurationMS: int(time.Since(started).Milliseconds())}
		}
		return workerFailure(err, started)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return workerFailure(errors.New("executor worker returned an error"), started)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 8*1024*1024)
	eventName := ""
	data := make([]byte, 0, 4096)
	var streamedStdout strings.Builder
	var streamedStderr strings.Builder
	var result *models.CommandExecuteResponse
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if eventName == "output" {
				var chunk models.CommandOutputChunk
				if json.Unmarshal(data, &chunk) == nil {
					if chunk.Stream == "stdout" {
						streamedStdout.WriteString(chunk.Data)
					} else if chunk.Stream == "stderr" {
						streamedStderr.WriteString(chunk.Data)
					}
					if onOutput != nil {
						onOutput(chunk)
					}
				}
			} else if eventName == "complete" {
				var completed models.CommandExecuteResponse
				if json.Unmarshal(data, &completed) == nil {
					result = &completed
				}
			}
			eventName = ""
			data = data[:0]
			continue
		}
		if len(line) > 7 && line[:7] == "event: " {
			eventName = line[7:]
		} else if len(line) > 6 && line[:6] == "data: " {
			data = append(data, line[6:]...)
		}
	}
	if scanner.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return cancelledResponse(streamedStdout.String(), streamedStderr.String(), started)
		}
		return workerFailure(scanner.Err(), started)
	}
	if result == nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return cancelledResponse(streamedStdout.String(), streamedStderr.String(), started)
		}
		return workerFailure(errors.New("executor worker closed before sending a result"), started)
	}
	return *result
}

func cancelledResponse(stdout string, stderr string, started time.Time) models.CommandExecuteResponse {
	if stderr != "" {
		stderr += "\n"
	}
	return models.CommandExecuteResponse{
		Status:     models.CommandStatusCancelled,
		ExitCode:   intPointer(130),
		Stdout:     stdout,
		Stderr:     stderr + "Command cancelled.",
		DurationMS: int(time.Since(started).Milliseconds()),
	}
}

func workerFailure(err error, started time.Time) models.CommandExecuteResponse {
	code := 1
	return models.CommandExecuteResponse{
		Status:     models.CommandStatusFailed,
		ExitCode:   &code,
		Stderr:     "Executor worker error: " + err.Error(),
		DurationMS: int(time.Since(started).Milliseconds()),
	}
}

func intPointer(value int) *int {
	return &value
}

func NewWorkerHandler(runner Executor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /execute", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload workerRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload.Command == "" || payload.CWD == "" {
			http.Error(w, "invalid execution request", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		var writeMu sync.Mutex
		writeEvent := func(name string, value any) {
			encoded, err := json.Marshal(value)
			if err != nil {
				return
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, encoded)
			flusher.Flush()
		}

		if blocked, reason := policy.BlockedCommand(payload.Command); blocked {
			writeEvent("complete", models.CommandExecuteResponse{Status: models.CommandStatusBlocked, Stderr: reason})
			return
		}
		result := runner.RunWithOutput(r.Context(), payload.Command, payload.CWD, func(chunk models.CommandOutputChunk) {
			writeEvent("output", chunk)
		})
		writeEvent("complete", result)
	})
	return mux
}
