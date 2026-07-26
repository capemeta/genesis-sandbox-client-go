package sandbox

import (
	"time"
)

const (
	// DefaultSessionTTL is the default server-side session TTL requested by Open.
	DefaultSessionTTL = 300

	// DefaultHeartbeatInterval is the fixed heartbeat interval used by the
	// auto-renew loop. It must stay well below the server lease timeout / 2.
	DefaultHeartbeatInterval = 30 * time.Second

	// DefaultHeartbeatExtendSeconds is how many seconds each heartbeat extends the lease.
	DefaultHeartbeatExtendSeconds = 90
)

// ---------------------------------------------------------------------------
// ExecResult — unified return type for Run / Wait
// ---------------------------------------------------------------------------

// ExecResult holds the output of a single execution.
type ExecResult struct {
	ExitCode             int
	Stdout               string
	Stderr               string
	StdoutTruncated      bool
	StderrTruncated      bool
	ErrorCode            string // timeout, cancelled, oom, etc.
	EffectiveEnvironment *EffectiveEnvironment
}

// OK returns true when ExitCode == 0 and no error code is set.
func (r *ExecResult) OK() bool { return r.ExitCode == 0 && r.ErrorCode == "" }

// ---------------------------------------------------------------------------
// Sandbox creation options (Option)
// ---------------------------------------------------------------------------

// sandboxOptions holds the resolved configuration for a Sandbox.
type sandboxOptions struct {
	// Environment selection
	environment  *EnvironmentSelector
	resolutionID string
	// Workspace
	workspaceID        string
	workspaceRetention string
	workspaceTTLSec    int
	// Session
	ttlSeconds     int
	statePolicy    string
	idempotencyKey string
	// Env & metadata
	env      map[string]string
	metadata map[string]string
	// Heartbeat
	heartbeatInterval time.Duration
	heartbeatExtend   int
	disableHeartbeat  bool
	logger            Logger
}

func defaultSandboxOptions() *sandboxOptions {
	return &sandboxOptions{
		ttlSeconds:        DefaultSessionTTL,
		heartbeatInterval: DefaultHeartbeatInterval,
		heartbeatExtend:   DefaultHeartbeatExtendSeconds,
	}
}

// Option is a functional option for New / QuickRun / RunBatch.
type Option func(*sandboxOptions)

// WithProfile sets an explicit profile name for environment selection.
func WithProfile(name string) Option {
	return func(o *sandboxOptions) {
		o.environment = &EnvironmentSelector{Profile: &ProfileRef{Name: name}}
		o.resolutionID = ""
	}
}

// WithProfileRevision sets an explicit profile with a pinned revision.
func WithProfileRevision(name, revision string) Option {
	return func(o *sandboxOptions) {
		o.environment = &EnvironmentSelector{Profile: &ProfileRef{Name: name, Revision: revision}}
		o.resolutionID = ""
	}
}

// WithHints provides capability hints for automatic profile selection.
func WithHints(capabilities ...string) Option {
	return func(o *sandboxOptions) {
		o.environment = &EnvironmentSelector{Hints: &EnvHints{Capabilities: capabilities}}
		o.resolutionID = ""
	}
}

// WithStrictHints sets strict capability hints (fail if not all satisfied).
func WithStrictHints(capabilities ...string) Option {
	return func(o *sandboxOptions) {
		o.environment = &EnvironmentSelector{Hints: &EnvHints{Capabilities: capabilities, Strict: true}}
		o.resolutionID = ""
	}
}

// WithResolutionID uses a pre-resolved environment ticket.
func WithResolutionID(id string) Option {
	return func(o *sandboxOptions) {
		o.resolutionID = id
		o.environment = nil
	}
}

// WithEnv sets job-level environment variables and the default session env.
// In Session mode, the SDK applies it immediately after CreateSession succeeds.
func WithEnv(env map[string]string) Option {
	return func(o *sandboxOptions) { o.env = env }
}

// WithWorkspaceID reuses an existing workspace instead of creating a new one.
func WithWorkspaceID(workspaceID string) Option {
	return func(o *sandboxOptions) { o.workspaceID = workspaceID }
}

// WithWorkspaceRetention configures the retention policy for a newly-created workspace.
func WithWorkspaceRetention(mode string, ttlSeconds int) Option {
	return func(o *sandboxOptions) {
		o.workspaceRetention = mode
		o.workspaceTTLSec = ttlSeconds
	}
}

// WithSessionTTL overrides the requested server-side session TTL in seconds.
func WithSessionTTL(seconds int) Option {
	return func(o *sandboxOptions) { o.ttlSeconds = seconds }
}

// WithMetadata attaches user metadata to the session/job request.
func WithMetadata(kv map[string]string) Option {
	return func(o *sandboxOptions) { o.metadata = kv }
}

// WithIdempotencyKey sets the idempotency key for session creation.
func WithIdempotencyKey(key string) Option {
	return func(o *sandboxOptions) { o.idempotencyKey = key }
}

// WithHeartbeatDisabled turns off the built-in auto-renew heartbeat.
func WithHeartbeatDisabled() Option {
	return func(o *sandboxOptions) { o.disableHeartbeat = true }
}

// WithHeartbeatInterval overrides the built-in heartbeat interval.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(o *sandboxOptions) { o.heartbeatInterval = d }
}

// Logger is an optional sink for non-fatal background diagnostics.
type Logger interface {
	Printf(format string, v ...any)
}

// WithLogger enables optional SDK diagnostics. The library is silent by default.
func WithLogger(logger Logger) Option {
	return func(o *sandboxOptions) { o.logger = logger }
}

// ---------------------------------------------------------------------------
// Exec options (ExecOption)
// ---------------------------------------------------------------------------

// execOptions holds per-execution configuration.
type execOptions struct {
	lang        string
	timeout     time.Duration
	workingDir  string
	env         map[string]string
	callbackURL string
}

// ExecOption is a functional option for Run / RunAsync.
type ExecOption func(*execOptions)

// WithLang marks the cmdOrCode argument as source code in the given language.
func WithLang(lang string) ExecOption {
	return func(o *execOptions) { o.lang = lang }
}

// WithTimeout sets a per-execution timeout.
func WithTimeout(d time.Duration) ExecOption {
	return func(o *execOptions) { o.timeout = d }
}

// WithWorkingDir sets the working directory for this execution.
func WithWorkingDir(dir string) ExecOption {
	return func(o *execOptions) { o.workingDir = dir }
}

// WithExecEnv sets per-execution environment variables (merged with session env).
func WithExecEnv(env map[string]string) ExecOption {
	return func(o *execOptions) { o.env = env }
}

// WithCallback sets the async completion callback URL.
func WithCallback(url string) ExecOption {
	return func(o *execOptions) { o.callbackURL = url }
}

func resolveExecOptions(opts []ExecOption) *execOptions {
	o := &execOptions{}
	for _, fn := range opts {
		fn(o)
	}
	return o
}
