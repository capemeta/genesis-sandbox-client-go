package sandbox

import "time"

// CatalogCard is a single profile entry in the environment catalog.
type CatalogCard struct {
	ProfileName     string           `json:"name"`
	ProfileRevision string           `json:"profile_revision"`
	DisplayName     string           `json:"display_name"`
	Description     string           `json:"description"`
	Capabilities    []string         `json:"capabilities"`
	Tags            []string         `json:"tags,omitempty"`
	UseWhen         []string         `json:"use_when,omitempty"`
	AvoidWhen       []string         `json:"avoid_when,omitempty"`
	Limits          *ProfileLimits   `json:"limits,omitempty"`
	Features        *ProfileFeatures `json:"features,omitempty"`
}

// CatalogResponse is the response from GET /v1/environment/catalog.
type CatalogResponse struct {
	Items                []CatalogCard `json:"items"`
	CapabilityVocabulary []string      `json:"capability_vocabulary,omitempty"`
	DefaultProfile       string        `json:"default_profile,omitempty"`
	NextOffset           int           `json:"next_offset,omitempty"`
	Total                int           `json:"total"`
	Revision             string        `json:"-"`
}

// EnvironmentResolution is the response from POST /v1/environment:resolve.
type EnvironmentResolution struct {
	ResolutionID    string   `json:"resolution_id"`
	ProfileName     string   `json:"profile_name"`
	ProfileRevision string   `json:"profile_revision,omitempty"`
	SelectionMode   string   `json:"selection_mode"`
	SelectionReason []string `json:"selection_reason,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	ExpiresAt       string   `json:"expires_at"`
}

// ExecRecord represents an async exec status record (Phase 2).
type ExecRecord struct {
	ExecID               string                `json:"exec_id"`
	SessionID            string                `json:"session_id"`
	Status               string                `json:"status"` // queued|running|succeeded|failed|cancelled
	ExitCode             int                   `json:"exit_code"`
	Stdout               string                `json:"stdout,omitempty"`
	Stderr               string                `json:"stderr,omitempty"`
	ErrorCode            string                `json:"error_code,omitempty"`
	StdoutTruncated      bool                  `json:"stdout_truncated,omitempty"`
	StderrTruncated      bool                  `json:"stderr_truncated,omitempty"`
	WorkingDir           string                `json:"working_dir,omitempty"`
	EffectiveEnvironment *EffectiveEnvironment `json:"effective_environment,omitempty"`
	LogsURL              string                `json:"logs_url,omitempty"`
	DurationMS           int64                 `json:"duration_ms,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	StartedAt            *time.Time            `json:"started_at,omitempty"`
	FinishedAt           *time.Time            `json:"finished_at,omitempty"`
}

type ExecRecordList struct {
	Items      []ExecRecord `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
	Total      int          `json:"total"`
}

type SandboxLease struct {
	SandboxID            string                `json:"sandbox_id"`
	LeaseID              string                `json:"lease_id"`
	TenantID             string                `json:"tenant_id"`
	WorkspaceID          string                `json:"workspace_id,omitempty"`
	RuntimeProfile       string                `json:"runtime_profile"`
	ProfileRevision      string                `json:"profile_revision,omitempty"`
	Status               string                `json:"status"`
	CreatedAt            time.Time             `json:"created_at"`
	ExpiresAt            time.Time             `json:"expires_at"`
	EffectivePolicy      interface{}           `json:"effective_policy"`
	EffectiveEnvironment *EffectiveEnvironment `json:"effective_environment,omitempty"`
	Metadata             map[string]string     `json:"metadata,omitempty"`
	ResourceVersion      int64                 `json:"resource_version"`
}

type JobResult struct {
	JobID                string                `json:"job_id"`
	TenantID             string                `json:"tenant_id"`
	WorkspaceID          string                `json:"workspace_id,omitempty"`
	SandboxID            string                `json:"sandbox_id"`
	TaskType             string                `json:"task_type,omitempty"`
	Operation            string                `json:"operation,omitempty"`
	Status               string                `json:"status"`
	ExitCode             int                   `json:"exit_code"`
	Stdout               string                `json:"stdout"`
	Stderr               string                `json:"stderr"`
	StdoutTruncated      bool                  `json:"stdout_truncated,omitempty"`
	StderrTruncated      bool                  `json:"stderr_truncated,omitempty"`
	ErrorCode            string                `json:"error_code,omitempty"`
	Error                string                `json:"error,omitempty"`
	ErrorMessage         string                `json:"-"` // Deprecated: use Error.
	DurationMS           int64                 `json:"duration_ms,omitempty"`
	StartedAt            *time.Time            `json:"started_at,omitempty"`
	FinishedAt           *time.Time            `json:"finished_at,omitempty"`
	OutputArtifacts      []Artifact            `json:"output_artifacts,omitempty"`
	LogsURL              string                `json:"logs_url,omitempty"`
	EffectiveEnvironment *EffectiveEnvironment `json:"effective_environment,omitempty"`
}

type JobList struct {
	Items      []JobResult `json:"items"`
	NextOffset int         `json:"next_offset,omitempty"`
	Total      int         `json:"total"`
}

type Artifact struct {
	ArtifactID  string    `json:"artifact_id"`
	TenantID    string    `json:"tenant_id"`
	WorkspaceID string    `json:"workspace_id"`
	JobID       string    `json:"job_id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	MIME        string    `json:"mime"`
	CreatedAt   time.Time `json:"created_at"`
}

type ExecSessionResult struct {
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
	Environment     string `json:"environment"`
	SessionID       string `json:"session_id,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	SandboxID       string `json:"sandbox_id,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
}

type Workspace struct {
	WorkspaceID   string            `json:"workspace_id"`
	TenantID      string            `json:"tenant_id"`
	UserID        string            `json:"user_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	RetentionMode string            `json:"retention_mode"`
	ExpiresAt     *time.Time        `json:"expires_at,omitempty"`
	QuotaMB       int               `json:"quota_mb,omitempty"`
}

type Session struct {
	SessionID            string                `json:"session_id"`
	TenantID             string                `json:"tenant_id"`
	UserID               string                `json:"user_id,omitempty"`
	WorkspaceID          string                `json:"workspace_id"`
	RuntimeProfile       string                `json:"runtime_profile"`
	ProfileRevision      string                `json:"profile_revision,omitempty"`
	StatePolicy          string                `json:"state_policy"`
	ActiveSandboxID      string                `json:"active_sandbox_id,omitempty"`
	Status               string                `json:"status"`
	CreatedAt            time.Time             `json:"created_at"`
	ExpiresAt            time.Time             `json:"expires_at"`
	Metadata             map[string]string     `json:"metadata,omitempty"`
	ResourceVersion      int64                 `json:"resource_version"`
	EffectiveEnvironment *EffectiveEnvironment `json:"effective_environment,omitempty"`
}

type DependencyBuild struct {
	Fingerprint     string    `json:"fingerprint"`
	TenantID        string    `json:"tenant_id"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	ProfileName     string    `json:"profile_name"`
	ProfileRevision string    `json:"profile_revision"`
	Language        string    `json:"language"`
	Status          string    `json:"status"`
	CachePath       string    `json:"cache_path,omitempty"`
	ReadOnly        bool      `json:"read_only"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AuditEvent struct {
	EventID      string    `json:"event_id"`
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id,omitempty"`
	PrincipalID  string    `json:"principal_id,omitempty"`
	KeyID        string    `json:"key_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id,omitempty"`
	Status       string    `json:"status"`
	Message      string    `json:"message,omitempty"`
	TraceID      string    `json:"trace_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AuditEventList struct {
	Items      []AuditEvent `json:"items"`
	NextOffset int          `json:"next_offset,omitempty"`
	Total      int          `json:"total"`
}

type ViewerDescriptor struct {
	SandboxID      string    `json:"sandbox_id"`
	RuntimeProfile string    `json:"runtime_profile"`
	Status         string    `json:"status"`
	Kind           string    `json:"kind"`
	Ready          bool      `json:"ready"`
	PageURL        string    `json:"page_url,omitempty"`
	WebSocketURL   string    `json:"websocket_url,omitempty"`
	ProxyURL       string    `json:"proxy_url,omitempty"`
	AccessToken    string    `json:"access_token,omitempty"`
	TokenHeader    string    `json:"token_header,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	RenewOnAccess  bool      `json:"renew_on_access"`
}
