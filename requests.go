package sandbox

// CatalogQuery holds query parameters for GetCatalog.
type CatalogQuery struct {
	Capability  string // Filter by capability (comma-separated AND)
	Tag         string // Filter by tag
	Limit       int
	Offset      int
	IfNoneMatch string // ETag conditional request
}

type ResolveEnvironmentRequest struct {
	Environment EnvironmentSelector `json:"environment"`
	TTLSeconds  int                 `json:"ttl_seconds,omitempty"`
}

type LeaseRequest struct {
	WorkspaceID  string               `json:"workspace_id,omitempty"`
	Environment  *EnvironmentSelector `json:"environment,omitempty"`
	ResolutionID string               `json:"resolution_id,omitempty"`
	Metadata     map[string]string    `json:"metadata,omitempty"`
}

type SubmitJobRequest struct {
	Environment             *EnvironmentSelector `json:"environment,omitempty"`
	ResolutionID            string               `json:"resolution_id,omitempty"`
	Code                    string               `json:"code,omitempty"`
	Command                 []string             `json:"command,omitempty"`
	Language                string               `json:"language,omitempty"`
	TimeoutSeconds          int                  `json:"timeout_seconds,omitempty"`
	QueueWaitTimeoutSeconds int                  `json:"queue_wait_timeout_seconds,omitempty"`
	WorkspaceID             string               `json:"workspace_id,omitempty"`
	SessionID               string               `json:"session_id,omitempty"`
	IdempotencyKey          string               `json:"idempotency_key,omitempty"`
	CallbackURL             string               `json:"callback_url,omitempty"`
	InputArtifactIDs        []string             `json:"input_artifact_ids,omitempty"`
	Env                     map[string]string    `json:"env,omitempty"`
	Metadata                map[string]string    `json:"metadata,omitempty"`
}

type ExecSessionRequest struct {
	Command        []string          `json:"command,omitempty"`
	Code           string            `json:"code,omitempty"`
	Language       string            `json:"language,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	CallbackURL    string            `json:"callback_url,omitempty"` // Async exec only; sync endpoints ignore it.
}

type CreateWorkspaceRequest struct {
	WorkspaceID   string            `json:"workspace_id,omitempty"`
	RetentionMode string            `json:"retention_mode,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	TTLSeconds    int               `json:"ttl_seconds,omitempty"`
	QuotaMB       int               `json:"quota_mb,omitempty"`
}

type CreateSessionRequest struct {
	Environment         *EnvironmentSelector `json:"environment,omitempty"`
	ResolutionID        string               `json:"resolution_id,omitempty"`
	WorkspaceID         string               `json:"workspace_id,omitempty"`
	StatePolicy         string               `json:"state_policy,omitempty"`
	TTLSeconds          int                  `json:"ttl_seconds,omitempty"`
	WorkspaceRetention  string               `json:"workspace_retention,omitempty"`
	WorkspaceTTLSeconds int                  `json:"workspace_ttl_seconds,omitempty"`
	IdempotencyKey      string               `json:"idempotency_key,omitempty"`
	Env                 map[string]string    `json:"env,omitempty"` // SDK convenience: create session, then patch session context env.
	Metadata            map[string]string    `json:"metadata,omitempty"`
}

type BuildDependencyRequest struct {
	Environment  *EnvironmentSelector `json:"environment,omitempty"`
	ResolutionID string               `json:"resolution_id,omitempty"`
	Language     string               `json:"language,omitempty"`
	Manifest     string               `json:"manifest,omitempty"`
	Lockfile     string               `json:"lockfile,omitempty"`
	Packages     []string             `json:"packages,omitempty"`
}

type StartGUIRequest struct {
	Kind       string            `json:"kind,omitempty"`
	Resolution string            `json:"resolution,omitempty"`
	TTLSeconds int               `json:"ttl_seconds,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type AuditQuery struct {
	PrincipalID  string
	KeyID        string
	Action       string
	ResourceType string
	ResourceID   string
	Limit        int
	Offset       int
}

type ExecListQuery struct {
	Limit  int
	Cursor string
}

type SandboxMetadataPatch map[string]*string
