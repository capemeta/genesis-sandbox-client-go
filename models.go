package sandbox

import "time"

// EnvironmentSelector is the unified entry point for environment selection.
// Exactly one of Profile or Hints should be provided.
type EnvironmentSelector struct {
	Profile *ProfileRef `json:"profile,omitempty"`
	Hints   *EnvHints   `json:"hints,omitempty"`
}

// ProfileRef specifies an explicit profile by name (and optional revision).
type ProfileRef struct {
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
}

// EnvHints provides capability-based hints for automatic profile selection.
type EnvHints struct {
	Capabilities []string `json:"capabilities,omitempty"`
	Strict       bool     `json:"strict,omitempty"`
	Description  string   `json:"description,omitempty"`
}

// EffectiveEnvironment describes the resolved environment in API responses.
type EffectiveEnvironment struct {
	ProfileName     string   `json:"profile_name"`
	ProfileRevision string   `json:"profile_revision,omitempty"`
	SelectionMode   string   `json:"selection_mode"`
	SelectionReason []string `json:"selection_reason,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Degraded        bool     `json:"degraded,omitempty"`
}

// ProfileLimits describes resource constraints of a profile.
type ProfileLimits struct {
	MaxExecTimeoutSeconds int   `json:"max_exec_timeout_seconds,omitempty"`
	MaxSessionTTLSeconds  int   `json:"max_session_ttl_seconds,omitempty"`
	WorkspaceQuotaMB      int   `json:"workspace_quota_mb,omitempty"`
	MaxLogBytes           int64 `json:"max_log_bytes,omitempty"`
	MaxConcurrentExecs    int   `json:"max_concurrent_execs,omitempty"`
}

// ProfileFeatures describes optional features supported by a profile.
type ProfileFeatures struct {
	Viewer        bool `json:"viewer,omitempty"`
	SuspendResume bool `json:"suspend_resume,omitempty"`
}

// SessionContext represents the mutable session-level cwd/env state.
type SessionContext struct {
	Cwd string            `json:"cwd"`
	Env map[string]string `json:"env,omitempty"`
}

type WorkspaceFileInfo struct {
	Path        string    `json:"path"`
	SandboxPath string    `json:"sandbox_path,omitempty"`
	Environment string    `json:"environment"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	MIME        string    `json:"mime,omitempty"`
	ModTime     time.Time `json:"mod_time,omitempty"`
}

type WorkspaceListResult struct {
	Path      string              `json:"path"`
	Entries   []WorkspaceFileInfo `json:"entries"`
	Truncated bool                `json:"truncated"`
	Limit     int                 `json:"limit"`
}

type WaitViewerOptions struct {
	Kind         string
	PollInterval time.Duration
	Timeout      time.Duration
}

type WaitJobOptions struct {
	PollInterval time.Duration
}

type WaitExecOptions struct {
	PollInterval time.Duration
}

func MetadataValue(value string) *string {
	return &value
}

func MetadataDelete() *string {
	return nil
}
