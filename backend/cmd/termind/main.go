package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
)

type APIClient struct {
	baseURL string
	http    *http.Client
}

type localResult struct {
	stdout     string
	stderr     string
	exitCode   int
	durationMS int
}

func main() {
	apiURL := flag.String("api", env("TERMIND_API_BASE_URL", "http://localhost:8000"), "Termind API base URL")
	once := flag.String("once", "", "Run one request and exit")
	backfillEmbeddings := flag.Bool("backfill-embeddings", false, "Backfill missing command-event embeddings and exit")
	backfillLimit := flag.Int("backfill-limit", 50, "Maximum command events to backfill")
	yes := flag.Bool("yes", false, "Auto-approve safe commands only")
	timeout := flag.Duration("timeout", 30*time.Second, "Local command timeout")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		exitWithError(err)
	}

	client := APIClient{
		baseURL: strings.TrimRight(*apiURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}

	if err := client.waitHealthy(context.Background()); err != nil {
		exitWithError(fmt.Errorf("Termind API is not reachable at %s: %w", *apiURL, err))
	}

	if *backfillEmbeddings {
		response, err := client.backfillCommandEmbeddings(models.EmbeddingBackfillRequest{Limit: *backfillLimit})
		if err != nil {
			exitWithError(err)
		}
		fmt.Printf("Embedding backfill: %s\n", response.Status)
		fmt.Printf("scanned=%d stored=%d failed=%d skipped=%d model=%s\n", response.Scanned, response.Stored, response.Failed, response.Skipped, response.EmbeddingModel)
		return
	}

	session, err := client.createSession(cwd)
	if err != nil {
		exitWithError(err)
	}

	if strings.TrimSpace(*once) != "" {
		if err := handleRequest(client, session.SessionID, cwd, *once, *yes, *timeout); err != nil {
			exitWithError(err)
		}
		return
	}

	fmt.Printf("Termind CLI connected to %s\n", *apiURL)
	fmt.Println("Type a request, or use :quit to exit.")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Termind > ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == ":quit" || input == ":exit" {
			break
		}

		if err := handleRequest(client, session.SessionID, cwd, input, *yes, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		exitWithError(err)
	}
}

func handleRequest(client APIClient, sessionID string, cwd string, input string, autoYes bool, timeout time.Duration) error {
	plan, err := client.createRequest(models.UserRequestCreate{
		SessionID: sessionID,
		Input:     input,
		CWD:       cwd,
	})
	if err != nil {
		return err
	}

	if plan.Intent == models.IntentUnknown {
		printUnsupported(plan)
		return nil
	}

	if plan.Intent == models.IntentSearchHistory || strings.TrimSpace(plan.Plan.Command) == "" {
		return printMemorySearch(client, input, cwd)
	}

	printPlan(plan)

	approved := autoYes && plan.Policy.Risk == models.RiskSafe
	if !approved {
		var err error
		approved, err = askApproval()
		if err != nil {
			return err
		}
	}

	if !approved {
		fmt.Println("Skipped.")
		return nil
	}

	result, err := runLocalCommand(cwd, plan.Plan.Command, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
	}
	printResult(result)

	record, recordErr := client.recordCommand(models.CommandRecordRequest{
		SessionID:       sessionID,
		RequestID:       plan.RequestID,
		UserRequest:     input,
		ProposedCommand: plan.Plan.Command,
		FinalCommand:    plan.Plan.Command,
		CWD:             cwd,
		Shell:           env("SHELL", "sh"),
		RiskLevel:       plan.Policy.Risk,
		Confirmation:    models.ConfirmationApproved,
		ExitCode:        result.exitCode,
		Stdout:          summarize(result.stdout),
		Stderr:          summarize(result.stderr),
		DurationMS:      result.durationMS,
	})
	if recordErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record command event: %v\n", recordErr)
		return err
	}

	fmt.Printf("Recorded event: %s (%s)\n", record.CommandEventID, record.Message)
	return err
}

func printPlan(plan models.UserRequestResponse) {
	fmt.Println()
	fmt.Println("Proposed command:")
	fmt.Printf("  %s\n", plan.Plan.Command)
	fmt.Println()
	fmt.Printf("Risk: %s\n", plan.Policy.Risk)
	fmt.Printf("Confirmation required: %t\n", plan.Policy.RequiresConfirmation)
	fmt.Printf("Reason: %s\n", plan.Plan.Reason)
	if len(plan.Plan.Provenance) > 0 {
		fmt.Println("Why:")
		for _, item := range plan.Plan.Provenance {
			fmt.Printf("  - %s\n", item)
		}
	}
	if len(plan.Policy.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range plan.Policy.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	fmt.Println()
}

func printUnsupported(plan models.UserRequestResponse) {
	fmt.Println()
	fmt.Println("I can help with terminal, project, shell, process, file, and command-memory requests.")
	if strings.TrimSpace(plan.Plan.Reason) != "" {
		fmt.Printf("Reason: %s\n", plan.Plan.Reason)
	}
	if len(plan.Policy.Warnings) > 0 {
		fmt.Println("Try:")
		for _, warning := range plan.Policy.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	fmt.Println()
}

func printResult(result localResult) {
	fmt.Println("COMMAND RESULT")
	fmt.Printf("exit=%d duration=%dms\n", result.exitCode, result.durationMS)
	if strings.TrimSpace(result.stdout) != "" {
		fmt.Println("stdout:")
		fmt.Print(result.stdout)
		if !strings.HasSuffix(result.stdout, "\n") {
			fmt.Println()
		}
	}
	if strings.TrimSpace(result.stderr) != "" {
		fmt.Println("stderr:")
		fmt.Print(result.stderr)
		if !strings.HasSuffix(result.stderr, "\n") {
			fmt.Println()
		}
	}
}

func askApproval() (bool, error) {
	fmt.Print("Run locally? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func runLocalCommand(cwd string, command string, timeout time.Duration) (localResult, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := int(time.Since(started).Milliseconds())

	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			stderr.WriteString(fmt.Sprintf("\ncommand timed out after %s", timeout))
		}
	}

	return localResult{
		stdout:     stdout.String(),
		stderr:     stderr.String(),
		exitCode:   exitCode,
		durationMS: duration,
	}, err
}

func printMemorySearch(client APIClient, query string, cwd string) error {
	response, err := client.searchMemory(models.MemorySearchRequest{
		Query: query,
		CWD:   cwd,
		Limit: 5,
	})
	if err != nil {
		return err
	}

	if len(response.Results) == 0 {
		fmt.Println("No relevant commands found.")
		return nil
	}

	fmt.Println("Relevant commands:")
	for index, item := range response.Results {
		fmt.Printf("%d. %s\n", index+1, item.Command)
		fmt.Printf("   %s | score %.0f%% | %s\n", item.UserRequest, item.Score*100, strings.Join(item.MatchedReasons, ", "))
	}
	return nil
}

func (c APIClient) waitHealthy(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	for {
		var payload map[string]string
		if err := c.get("/health", &payload); err == nil && payload["status"] == "ok" {
			return nil
		}

		select {
		case <-deadline.Done():
			return deadline.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c APIClient) createSession(cwd string) (models.SessionCreateResponse, error) {
	var response models.SessionCreateResponse
	err := c.post("/v1/sessions", models.SessionCreateRequest{CWD: cwd, Shell: env("SHELL", "sh")}, &response)
	return response, err
}

func (c APIClient) createRequest(payload models.UserRequestCreate) (models.UserRequestResponse, error) {
	var response models.UserRequestResponse
	err := c.post("/v1/requests", payload, &response)
	return response, err
}

func (c APIClient) recordCommand(payload models.CommandRecordRequest) (models.CommandRecordResponse, error) {
	var response models.CommandRecordResponse
	err := c.post("/v1/commands/record", payload, &response)
	return response, err
}

func (c APIClient) searchMemory(payload models.MemorySearchRequest) (models.MemorySearchResponse, error) {
	var response models.MemorySearchResponse
	err := c.post("/v1/memory/search", payload, &response)
	return response, err
}

func (c APIClient) backfillCommandEmbeddings(payload models.EmbeddingBackfillRequest) (models.EmbeddingBackfillResponse, error) {
	var response models.EmbeddingBackfillResponse
	err := c.post("/v1/memory/embeddings/backfill", payload, &response)
	return response, err
}

func (c APIClient) get(path string, target any) error {
	request, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(request, target)
}

func (c APIClient) post(path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	return c.do(request, target)
}

func (c APIClient) do(request *http.Request, target any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("api returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, target)
}

func summarize(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 2000 {
		return value
	}
	return value[:2000] + "... [truncated]"
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
