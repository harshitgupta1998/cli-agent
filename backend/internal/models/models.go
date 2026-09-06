package models

const (
	IntentExecuteCommand = "execute_command"
	IntentSearchHistory  = "search_history"
	IntentUnknown        = "unknown"

	RiskSafe        = "safe"
	RiskModifying   = "modifying"
	RiskDestructive = "destructive"
	RiskPrivileged  = "privileged"

	ConfirmationApproved = "approved"
	ConfirmationRejected = "rejected"

	CommandStatusCompleted = "completed"
	CommandStatusRejected  = "rejected"
	CommandStatusBlocked   = "blocked"
	CommandStatusFailed    = "failed"
)

type PhaseStatus string

const (
	PhaseReady      PhaseStatus = "ready"
	PhaseInProgress PhaseStatus = "in_progress"
	PhaseMocked     PhaseStatus = "mocked"
	PhasePlanned    PhaseStatus = "planned"
	PhaseBlocked    PhaseStatus = "blocked"
)

type Phase struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Status      PhaseStatus `json:"status"`
	Summary     string      `json:"summary"`
	Deliverable string      `json:"deliverable"`
	Scope       []string    `json:"scope"`
	MockAPIs    []string    `json:"mock_apis"`
}

type PhaseResponse struct {
	CurrentPhase string  `json:"current_phase"`
	Phases       []Phase `json:"phases"`
}

type MockCapability struct {
	ID          string   `json:"id"`
	Phase       string   `json:"phase"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Endpoints   []string `json:"endpoints"`
	NextSteps   []string `json:"next_steps"`
}

type MockCapabilityResponse struct {
	Capabilities []MockCapability `json:"capabilities"`
}

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type MessageListResponse struct {
	Messages []Message `json:"messages"`
}

type SessionCreateRequest struct {
	CWD   string `json:"cwd"`
	Shell string `json:"shell"`
}

type SessionCreateResponse struct {
	SessionID string `json:"session_id"`
	ProjectID string `json:"project_id"`
	StartedAt string `json:"started_at"`
}

type UserRequestCreate struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	CWD       string `json:"cwd"`
}

type AlternativeCommand struct {
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

type CommandPlan struct {
	Command              string               `json:"command"`
	CWD                  string               `json:"cwd"`
	Risk                 string               `json:"risk"`
	RequiresConfirmation bool                 `json:"requires_confirmation"`
	Reason               string               `json:"reason"`
	Provenance           []string             `json:"provenance"`
	Alternatives         []AlternativeCommand `json:"alternatives"`
}

type PolicyDecision struct {
	Risk                 string   `json:"risk"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Warnings             []string `json:"warnings"`
}

type UserRequestResponse struct {
	RequestID string         `json:"request_id"`
	Intent    string         `json:"intent"`
	Plan      CommandPlan    `json:"plan"`
	Policy    PolicyDecision `json:"policy"`
}

type Confirmation struct {
	Status     string `json:"status"`
	ApprovedAt string `json:"approved_at,omitempty"`
}

type CommandExecuteRequest struct {
	SessionID    string       `json:"session_id"`
	RequestID    string       `json:"request_id"`
	UserRequest  string       `json:"user_request,omitempty"`
	Command      string       `json:"command"`
	CWD          string       `json:"cwd"`
	Shell        string       `json:"shell,omitempty"`
	RiskLevel    string       `json:"risk_level,omitempty"`
	Confirmation Confirmation `json:"confirmation"`
}

type CommandExecuteResponse struct {
	CommandEventID  string `json:"command_event_id"`
	Status          string `json:"status"`
	ExitCode        *int   `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	DurationMS      int    `json:"duration_ms"`
	EmbeddingStatus string `json:"embedding_status,omitempty"`
	EmbeddingModel  string `json:"embedding_model,omitempty"`
}

type CommandRecordRequest struct {
	SessionID       string `json:"session_id"`
	RequestID       string `json:"request_id"`
	UserRequest     string `json:"user_request"`
	ProposedCommand string `json:"proposed_command"`
	FinalCommand    string `json:"final_command"`
	CWD             string `json:"cwd"`
	Shell           string `json:"shell"`
	RiskLevel       string `json:"risk_level"`
	Confirmation    string `json:"confirmation"`
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	DurationMS      int    `json:"duration_ms"`
}

type CommandRecordResponse struct {
	CommandEventID  string `json:"command_event_id"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	EmbeddingStatus string `json:"embedding_status,omitempty"`
	EmbeddingModel  string `json:"embedding_model,omitempty"`
}

type MemorySearchRequest struct {
	Query     string `json:"query"`
	ProjectID string `json:"project_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Limit     int    `json:"limit"`
}

type MemorySearchResult struct {
	CommandEventID string   `json:"command_event_id"`
	Command        string   `json:"command"`
	UserRequest    string   `json:"user_request"`
	CWD            string   `json:"cwd"`
	ExitCode       int      `json:"exit_code"`
	Score          float64  `json:"score"`
	MatchedReasons []string `json:"matched_reasons"`
	LastUsedAt     string   `json:"last_used_at"`
}

type MemorySearchResponse struct {
	Results []MemorySearchResult `json:"results"`
}

type ExplainCommandRequest struct {
	Command string `json:"command"`
	CWD     string `json:"cwd"`
}

type ExplainCommandResponse struct {
	Summary  string   `json:"summary"`
	Risk     string   `json:"risk"`
	Warnings []string `json:"warnings"`
}

type ProjectContext struct {
	ProjectID      string           `json:"project_id"`
	RootPath       string           `json:"root_path"`
	Git            GitContext       `json:"git"`
	DetectedStack  DetectedStack    `json:"detected_stack"`
	CommonCommands []ProjectCommand `json:"common_commands"`
}

type GitContext struct {
	Branch                string `json:"branch"`
	HasUncommittedChanges bool   `json:"has_uncommitted_changes"`
}

type DetectedStack struct {
	Language       string `json:"language"`
	Framework      string `json:"framework"`
	PackageManager string `json:"package_manager"`
}

type ProjectCommand struct {
	Label        string `json:"label"`
	Command      string `json:"command"`
	SuccessCount int    `json:"success_count"`
}
