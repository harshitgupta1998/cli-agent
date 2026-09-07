package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/harsgupta/termind/backend/internal/embeddings"
	"github.com/harsgupta/termind/backend/internal/models"
)

type Store interface {
	EnsureProject(ctx context.Context, rootPath string) (string, error)
	CreateSession(ctx context.Context, payload models.SessionCreateRequest) (models.SessionCreateResponse, error)
	RecordMessage(ctx context.Context, sessionID string, role string, content string) error
	ListMessages(ctx context.Context, sessionID string) (models.MessageListResponse, error)
	ProjectCommands(ctx context.Context, cwd string) ([]models.ProjectCommand, error)
	RecordCommand(ctx context.Context, payload models.CommandRecordRequest) (models.CommandRecordResponse, error)
	SearchCommands(ctx context.Context, payload models.MemorySearchRequest) (models.MemorySearchResponse, error)
	BackfillCommandEmbeddings(ctx context.Context, payload models.EmbeddingBackfillRequest) (models.EmbeddingBackfillResponse, error)
	Close() error
}

type PostgresStore struct {
	db       *sql.DB
	embedder embeddings.Embedder
}

func NewPostgresStore(ctx context.Context, databaseURL string, embedder embeddings.Embedder) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return &PostgresStore{db: db, embedder: embedder}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) CreateSession(ctx context.Context, payload models.SessionCreateRequest) (models.SessionCreateResponse, error) {
	projectID, err := s.EnsureProject(ctx, payload.CWD)
	if err != nil {
		return models.SessionCreateResponse{}, err
	}

	sessionID := "ses_" + randomID()
	startedAt := time.Now().UTC()
	_, err = s.db.ExecContext(
		ctx,
		`
		INSERT INTO sessions (
			id,
			project_id,
			cwd,
			shell,
			started_at
		) VALUES ($1, $2, $3, $4, $5)
		`,
		sessionID,
		projectID,
		payload.CWD,
		payload.Shell,
		startedAt,
	)
	if err != nil {
		return models.SessionCreateResponse{}, fmt.Errorf("create session: %w", err)
	}

	return models.SessionCreateResponse{
		SessionID: sessionID,
		ProjectID: projectID,
		StartedAt: startedAt.Format(time.RFC3339),
	}, nil
}

func (s *PostgresStore) RecordMessage(ctx context.Context, sessionID string, role string, content string) error {
	_, err := s.db.ExecContext(
		ctx,
		`
		INSERT INTO messages (
			id,
			session_id,
			role,
			content,
			created_at
		) VALUES ($1, $2, $3, $4, now())
		`,
		"msg_"+randomID(),
		sessionID,
		role,
		content,
	)
	if err != nil {
		return fmt.Errorf("record message: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, sessionID string) (models.MessageListResponse, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			id,
			session_id,
			role,
			content,
			created_at
		FROM messages
		WHERE session_id = $1
		ORDER BY created_at ASC, id ASC
		`,
		sessionID,
	)
	if err != nil {
		return models.MessageListResponse{}, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := []models.Message{}
	for rows.Next() {
		var message models.Message
		var createdAt time.Time
		if err := rows.Scan(
			&message.ID,
			&message.SessionID,
			&message.Role,
			&message.Content,
			&createdAt,
		); err != nil {
			return models.MessageListResponse{}, fmt.Errorf("scan message: %w", err)
		}
		message.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return models.MessageListResponse{}, fmt.Errorf("iterate messages: %w", err)
	}

	return models.MessageListResponse{Messages: messages}, nil
}

func (s *PostgresStore) EnsureProject(ctx context.Context, rootPath string) (string, error) {
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE root_path = $1`, rootPath).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find project: %w", err)
	}

	projectID := "prj_" + randomID()
	name := projectName(rootPath)
	_, err = s.db.ExecContext(
		ctx,
		`
		INSERT INTO projects (
			id,
			name,
			root_path,
			detected_stack_json
		) VALUES ($1, $2, $3, '{}'::jsonb)
		`,
		projectID,
		name,
		rootPath,
	)
	if err != nil {
		return "", fmt.Errorf("create project: %w", err)
	}

	return projectID, nil
}

func (s *PostgresStore) RecordCommand(ctx context.Context, payload models.CommandRecordRequest) (models.CommandRecordResponse, error) {
	id := "cmd_" + randomID()
	confirmation := normalizeConfirmation(payload.Confirmation)

	_, err := s.db.ExecContext(
		ctx,
		`
		INSERT INTO command_events (
			id,
			session_id,
			project_id,
			user_request,
			proposed_command,
			final_command,
			cwd,
			shell,
			risk_level,
			required_confirmation,
			confirmation_status,
			exit_code,
			stdout_summary,
			stderr_summary,
			duration_ms,
			started_at,
			ended_at,
			tags_json
		) VALUES (
			$1, $2, (SELECT project_id FROM sessions WHERE id = $2), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now(), now(), '[]'::jsonb
		)
		`,
		id,
		payload.SessionID,
		payload.UserRequest,
		payload.ProposedCommand,
		payload.FinalCommand,
		payload.CWD,
		payload.Shell,
		payload.RiskLevel,
		payload.RiskLevel != models.RiskSafe,
		confirmation,
		payload.ExitCode,
		emptyToNull(payload.Stdout),
		emptyToNull(payload.Stderr),
		payload.DurationMS,
	)
	if err != nil {
		return models.CommandRecordResponse{}, fmt.Errorf("record command event: %w", err)
	}
	_ = s.upsertProjectCommand(ctx, payload)
	embeddingStatus := s.recordCommandEmbedding(id, payload)

	return models.CommandRecordResponse{
		CommandEventID:  id,
		Status:          models.CommandStatusCompleted,
		Message:         "Command event persisted to Postgres.",
		EmbeddingStatus: embeddingStatus,
		EmbeddingModel:  s.embeddingModel(),
	}, nil
}

func (s *PostgresStore) upsertProjectCommand(ctx context.Context, payload models.CommandRecordRequest) error {
	command := strings.TrimSpace(payload.FinalCommand)
	if command == "" {
		return nil
	}

	successCount := 0
	failureCount := 1
	if payload.ExitCode == 0 {
		successCount = 1
		failureCount = 0
	}

	_, err := s.db.ExecContext(
		ctx,
		`
		INSERT INTO project_commands (
			id,
			project_id,
			command,
			label,
			success_count,
			failure_count,
			last_success_at
		)
		SELECT
			$1,
			project_id,
			$2,
			$3,
			$4,
			$5,
			CASE WHEN $4 > 0 THEN now() ELSE NULL END
		FROM sessions
		WHERE id = $6
		ON CONFLICT (project_id, command)
		DO UPDATE SET
			success_count = project_commands.success_count + EXCLUDED.success_count,
			failure_count = project_commands.failure_count + EXCLUDED.failure_count,
			last_success_at = CASE
				WHEN EXCLUDED.success_count > 0 THEN now()
				ELSE project_commands.last_success_at
			END
		`,
		"pcmd_"+randomID(),
		command,
		commandLabel(command),
		successCount,
		failureCount,
		payload.SessionID,
	)
	if err != nil {
		return fmt.Errorf("upsert project command: %w", err)
	}
	return nil
}

func (s *PostgresStore) recordCommandEmbedding(commandEventID string, payload models.CommandRecordRequest) string {
	if s.embedder == nil {
		return "skipped"
	}

	input := embeddingInput(payload)
	if input == "" {
		return "skipped"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vector, err := s.embedder.Embed(ctx, input)
	if err != nil {
		return "failed"
	}
	encoded, err := json.Marshal(vector)
	if err != nil {
		return "failed"
	}

	_, err = s.db.ExecContext(
		ctx,
		`
		INSERT INTO command_embeddings (
			command_event_id,
			embedding_model,
			embedding,
			created_at
		) VALUES ($1, $2, $3, now())
		ON CONFLICT (command_event_id)
		DO UPDATE SET
			embedding_model = EXCLUDED.embedding_model,
			embedding = EXCLUDED.embedding,
			created_at = now()
		`,
		commandEventID,
		s.embedder.Model(),
		encoded,
	)
	if err != nil {
		return "failed"
	}
	return "stored"
}

func (s *PostgresStore) BackfillCommandEmbeddings(ctx context.Context, payload models.EmbeddingBackfillRequest) (models.EmbeddingBackfillResponse, error) {
	response := models.EmbeddingBackfillResponse{
		Status:         "completed",
		EmbeddingModel: s.embeddingModel(),
	}
	if s.embedder == nil {
		response.Status = "skipped"
		return response, nil
	}

	limit := payload.Limit
	if limit <= 0 || limit > 250 {
		limit = 50
	}

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			ce.id,
			ce.session_id,
			ce.user_request,
			COALESCE(ce.proposed_command, ce.final_command),
			ce.final_command,
			ce.cwd,
			ce.shell,
			ce.risk_level,
			ce.confirmation_status,
			COALESCE(ce.exit_code, 0),
			COALESCE(ce.stdout_summary, ''),
			COALESCE(ce.stderr_summary, ''),
			COALESCE(ce.duration_ms, 0)
		FROM command_events ce
		LEFT JOIN command_embeddings emb
			ON emb.command_event_id = ce.id
			AND emb.embedding_model = $1
		WHERE emb.command_event_id IS NULL
		ORDER BY ce.started_at ASC
		LIMIT $2
		`,
		s.embedder.Model(),
		limit,
	)
	if err != nil {
		return models.EmbeddingBackfillResponse{}, fmt.Errorf("select command events for embedding backfill: %w", err)
	}
	defer rows.Close()

	type backfillTarget struct {
		id      string
		payload models.CommandRecordRequest
	}
	targets := []backfillTarget{}
	for rows.Next() {
		var target backfillTarget
		if err := rows.Scan(
			&target.id,
			&target.payload.SessionID,
			&target.payload.UserRequest,
			&target.payload.ProposedCommand,
			&target.payload.FinalCommand,
			&target.payload.CWD,
			&target.payload.Shell,
			&target.payload.RiskLevel,
			&target.payload.Confirmation,
			&target.payload.ExitCode,
			&target.payload.Stdout,
			&target.payload.Stderr,
			&target.payload.DurationMS,
		); err != nil {
			return models.EmbeddingBackfillResponse{}, fmt.Errorf("scan command event for embedding backfill: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return models.EmbeddingBackfillResponse{}, fmt.Errorf("iterate command events for embedding backfill: %w", err)
	}

	for _, target := range targets {
		response.Scanned++
		switch s.recordCommandEmbedding(target.id, target.payload) {
		case "stored":
			response.Stored++
		case "failed":
			response.Failed++
		default:
			response.Skipped++
		}
	}
	if response.Failed > 0 {
		response.Status = "partial"
	}
	return response, nil
}

func (s *PostgresStore) embeddingModel() string {
	if s.embedder == nil {
		return ""
	}
	return s.embedder.Model()
}

func embeddingInput(payload models.CommandRecordRequest) string {
	parts := []string{
		"user request: " + strings.TrimSpace(payload.UserRequest),
		"command: " + strings.TrimSpace(payload.FinalCommand),
		"cwd: " + strings.TrimSpace(payload.CWD),
		"stdout: " + strings.TrimSpace(payload.Stdout),
		"stderr: " + strings.TrimSpace(payload.Stderr),
	}

	nonEmpty := []string{}
	for _, part := range parts {
		if !strings.HasSuffix(part, ": ") {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "\n")
}

func (s *PostgresStore) ProjectCommands(ctx context.Context, cwd string) ([]models.ProjectCommand, error) {
	projectID, err := s.EnsureProject(ctx, cwd)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			label,
			command,
			success_count
		FROM project_commands
		WHERE project_id = $1
		ORDER BY success_count DESC, last_success_at DESC NULLS LAST, command ASC
		LIMIT 8
		`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list project commands: %w", err)
	}
	defer rows.Close()

	commands := []models.ProjectCommand{}
	for rows.Next() {
		var command models.ProjectCommand
		if err := rows.Scan(&command.Label, &command.Command, &command.SuccessCount); err != nil {
			return nil, fmt.Errorf("scan project command: %w", err)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project commands: %w", err)
	}

	return commands, nil
}

func projectName(rootPath string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rootPath), "/")
	if trimmed == "" {
		return "Unknown Project"
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

func commandLabel(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "Command"
	}
	if len(command) <= 64 {
		return command
	}
	return command[:64]
}

func (s *PostgresStore) SearchCommands(ctx context.Context, payload models.MemorySearchRequest) (models.MemorySearchResponse, error) {
	limit := payload.Limit
	if limit <= 0 || limit > 25 {
		limit = 5
	}

	query := strings.TrimSpace(payload.Query)
	likeQuery := "%" + query + "%"

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT
			id,
			final_command,
			user_request,
			cwd,
			COALESCE(exit_code, 0),
			started_at,
			CASE
				WHEN LOWER(final_command) = LOWER($1) THEN 1.00
				WHEN final_command ILIKE $2 THEN 0.92
				WHEN user_request ILIKE $2 THEN 0.88
				WHEN COALESCE(stdout_summary, '') ILIKE $2 THEN 0.72
				WHEN COALESCE(stderr_summary, '') ILIKE $2 THEN 0.62
				ELSE 0.45
			END AS score
		FROM command_events
		WHERE
			final_command ILIKE $2
			OR user_request ILIKE $2
			OR COALESCE(stdout_summary, '') ILIKE $2
			OR COALESCE(stderr_summary, '') ILIKE $2
		ORDER BY score DESC, started_at DESC
		LIMIT $3
		`,
		query,
		likeQuery,
		limit,
	)
	if err != nil {
		return models.MemorySearchResponse{}, fmt.Errorf("search command events: %w", err)
	}
	defer rows.Close()

	results := []models.MemorySearchResult{}
	for rows.Next() {
		var result models.MemorySearchResult
		var startedAt time.Time
		if err := rows.Scan(
			&result.CommandEventID,
			&result.Command,
			&result.UserRequest,
			&result.CWD,
			&result.ExitCode,
			&startedAt,
			&result.Score,
		); err != nil {
			return models.MemorySearchResponse{}, fmt.Errorf("scan command event: %w", err)
		}
		result.LastUsedAt = startedAt.UTC().Format(time.RFC3339)
		result.MatchedReasons = matchedReasons(result, payload)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return models.MemorySearchResponse{}, fmt.Errorf("iterate command events: %w", err)
	}

	return models.MemorySearchResponse{Results: results}, nil
}

func matchedReasons(result models.MemorySearchResult, payload models.MemorySearchRequest) []string {
	reasons := []string{"keyword match"}
	if payload.CWD != "" && result.CWD == payload.CWD {
		reasons = append(reasons, "same directory")
	}
	if result.ExitCode == 0 {
		reasons = append(reasons, "successful command")
	}
	return reasons
}

func normalizeConfirmation(value string) string {
	switch value {
	case "not_required", models.ConfirmationApproved, models.ConfirmationRejected, "edited":
		return value
	default:
		return models.ConfirmationApproved
	}
}

func emptyToNull(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func randomID() string {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
