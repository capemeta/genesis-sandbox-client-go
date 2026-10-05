// Package sandbox provides a high-level, dual-mode client for Genesis Sandbox.
//
// Default Job mode: each Run submits and waits for an independent Job.
// After Open(): enters Session mode where Run reuses the same container (ExecNamedSession).
// File operations (Upload/Download/Files) auto-trigger Open() if not already open.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrNotOpened is returned when a Session-only operation is called before Open().
var ErrNotOpened = errors.New("sandbox: session not opened; call Open() first")
var ErrClosed = errors.New("sandbox: client object is closed")

// Sandbox is the high-level Genesis Sandbox client object.
// Default Job mode (each Run gets an independent container).
// Call Open() to enter Session mode (container reuse across Runs).
type Sandbox struct {
	client *Client
	opts   *sandboxOptions

	mu          sync.RWMutex
	workspaceID string // set by Open() or WithWorkspaceID
	sessionID   string // set by Open()
	opened      bool
	closed      bool

	// heartbeat (Session mode only)
	cancel context.CancelFunc
	wg     sync.WaitGroup

	lifecycleMu      sync.Mutex
	heartbeatStopped bool
	heartbeatErr     error

	// defaultEnv 是客户端侧默认执行环境；服务端会话上下文已不支持 env（协议仅 cwd），
	// 每次 Run/RunAsync 与 per-call WithExecEnv 合并下发（per-call 优先）。
	defaultEnvMu sync.RWMutex
	defaultEnv   map[string]string
}

// New creates a Sandbox object. No Session/Workspace is created, no heartbeat started.
// If WithWorkspaceID is provided, that workspace will be reused on Open().
func New(client *Client, opts ...Option) (*Sandbox, error) {
	if client == nil {
		return nil, errors.New("sandbox: client is required")
	}
	o := defaultSandboxOptions()
	for _, fn := range opts {
		if fn == nil {
			return nil, errors.New("sandbox: nil option")
		}
		fn(o)
	}
	if err := validateSandboxOptions(o); err != nil {
		return nil, err
	}
	sb := &Sandbox{
		client:      client,
		opts:        o,
		workspaceID: o.workspaceID,
	}
	return sb, nil
}

func validateSandboxOptions(o *sandboxOptions) error {
	if o.ttlSeconds <= 0 {
		return errors.New("sandbox: session TTL must be positive")
	}
	if o.workspaceRetention != "" &&
		o.workspaceRetention != "ttl" &&
		o.workspaceRetention != "explicit_delete" {
		return fmt.Errorf("sandbox: unsupported workspace retention %q", o.workspaceRetention)
	}
	if o.workspaceRetention == "ttl" && o.workspaceTTLSec <= 0 {
		return errors.New("sandbox: workspace TTL must be positive when retention is ttl")
	}
	if o.heartbeatInterval < 0 || o.heartbeatExtend < 0 {
		return errors.New("sandbox: heartbeat interval and extension cannot be negative")
	}
	if o.environment != nil && o.environment.Profile != nil &&
		strings.TrimSpace(o.environment.Profile.Name) == "" {
		return errors.New("sandbox: profile name cannot be empty")
	}
	if o.environment != nil && o.environment.Hints != nil &&
		len(o.environment.Hints.Capabilities) == 0 {
		return errors.New("sandbox: at least one capability hint is required")
	}
	return nil
}

// Open explicitly enters Session mode: creates a Session (with Workspace) and starts heartbeat.
// After Open(), Run uses ExecNamedSession. Suspend/SetCwd/SetEnv/RunAsync become available.
// Safe to call concurrently with ensureOpen (file ops); only one session is ever created.
func (sb *Sandbox) Open(ctx context.Context) error {
	sb.lifecycleMu.Lock()
	defer sb.lifecycleMu.Unlock()
	if sb.IsClosed() {
		return ErrClosed
	}
	if sb.IsOpen() {
		return nil
	}
	return sb.doOpen(ctx)
}

// ensureOpen auto-triggers Open() for file operations (transparent upgrade).
func (sb *Sandbox) ensureOpen(ctx context.Context) error {
	return sb.Open(ctx)
}

func (sb *Sandbox) doOpen(ctx context.Context) error {
	req := CreateSessionRequest{
		Environment:         sb.opts.environment,
		ResolutionID:        sb.opts.resolutionID,
		WorkspaceID:         sb.opts.workspaceID,
		StatePolicy:         "session",
		TTLSeconds:          sb.opts.ttlSeconds,
		WorkspaceRetention:  sb.opts.workspaceRetention,
		WorkspaceTTLSeconds: sb.opts.workspaceTTLSec,
		IdempotencyKey:      sb.opts.idempotencyKey,
		Metadata:            sb.opts.metadata,
	}
	session, err := sb.client.CreateSession(ctx, req)
	if err != nil {
		return fmt.Errorf("sandbox.Open: create session: %w", err)
	}

	sb.mu.Lock()
	sb.sessionID = session.SessionID
	sb.workspaceID = session.WorkspaceID
	sb.opened = true
	sb.mu.Unlock()
	sb.defaultEnvMu.Lock()
	sb.defaultEnv = make(map[string]string, len(sb.opts.env))
	for key, value := range sb.opts.env {
		sb.defaultEnv[key] = value
	}
	sb.defaultEnvMu.Unlock()

	// Start heartbeat
	heartbeatOn := !sb.opts.disableHeartbeat && sb.opts.heartbeatInterval > 0 && sb.opts.heartbeatExtend > 0
	if heartbeatOn {
		hbCtx, cancel := context.WithCancel(context.Background())
		sb.cancel = cancel
		sb.wg.Add(1)
		go sb.heartbeatLoop(hbCtx)
	}

	sb.logf("opened session=%s workspace=%s heartbeat=%v", session.SessionID, session.WorkspaceID, heartbeatOn)
	return nil
}

// IsOpen returns whether the Sandbox is in Session mode.
func (sb *Sandbox) IsOpen() bool {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.opened
}

func (sb *Sandbox) IsClosed() bool {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.closed
}

// SessionID returns the session identifier (empty if not opened).
func (sb *Sandbox) SessionID() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.sessionID
}

// WorkspaceID returns the workspace identifier (empty if not opened and no WithWorkspaceID).
func (sb *Sandbox) WorkspaceID() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.workspaceID
}

// SetCwd modifies the session-level working directory (Session mode only).
func (sb *Sandbox) SetCwd(ctx context.Context, cwd string) error {
	if !sb.IsOpen() {
		return ErrNotOpened
	}
	_, err := sb.client.PatchSessionContext(ctx, sb.SessionID(), SessionContext{Cwd: cwd})
	return err
}

// SetEnv sets client-side default environment variables merged into every
// subsequent exec (Session mode only; per-call WithExecEnv takes precedence).
// The server-side session context no longer accepts env (cwd only).
func (sb *Sandbox) SetEnv(_ context.Context, env map[string]string) error {
	if !sb.IsOpen() {
		return ErrNotOpened
	}
	merged := make(map[string]string, len(env))
	for k, v := range env {
		merged[k] = v
	}
	sb.defaultEnvMu.Lock()
	sb.defaultEnv = merged
	sb.defaultEnvMu.Unlock()
	return nil
}

func (sb *Sandbox) snapshotDefaultEnv() map[string]string {
	sb.defaultEnvMu.RLock()
	defer sb.defaultEnvMu.RUnlock()
	if len(sb.defaultEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(sb.defaultEnv))
	for k, v := range sb.defaultEnv {
		out[k] = v
	}
	return out
}

// Resume recreates or validates the runtime for a suspended/active logical session
// (Session mode only). Exec also lazily resumes; Resume is the explicit pre-flight check.
func (sb *Sandbox) Resume(ctx context.Context) error {
	if !sb.IsOpen() {
		return ErrNotOpened
	}
	_, err := sb.client.ResumeSession(ctx, sb.SessionID())
	return err
}

// Suspend releases the runtime (container) while preserving the session workspace.
// Only available in Session mode.
func (sb *Sandbox) Suspend(ctx context.Context) error {
	if !sb.IsOpen() {
		return ErrNotOpened
	}
	_, err := sb.client.SuspendSession(ctx, sb.SessionID())
	return err
}

func (sb *Sandbox) Close() error {
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sb.CloseContext(closeCtx)
}

// CloseContext cleans up resources:
//   - If opened: stops heartbeat + deletes Session (workspace follows retention policy)
//   - If not opened: no-op
//   - Workspaces provided via WithWorkspaceID are not deleted (user-owned)
//
// CloseContext is idempotent and safe to call multiple times (use defer).
func (sb *Sandbox) CloseContext(ctx context.Context) error {
	sb.lifecycleMu.Lock()
	defer sb.lifecycleMu.Unlock()
	if sb.IsClosed() {
		return nil
	}
	if !sb.IsOpen() {
		sb.mu.Lock()
		sb.closed = true
		sb.mu.Unlock()
		return nil
	}

	if !sb.heartbeatStopped && sb.cancel != nil {
		sb.cancel()
		sb.wg.Wait()
		sb.heartbeatStopped = true
	}

	sid := sb.SessionID()
	sb.logf("closing session=%s", sid)
	if err := sb.client.DeleteSession(ctx, sid); err != nil {
		return fmt.Errorf("sandbox.CloseContext: delete session %s: %w", sid, err)
	}
	sb.mu.Lock()
	sb.opened = false
	sb.closed = true
	sb.mu.Unlock()
	sb.logf("closed session=%s", sid)
	return nil
}

func (sb *Sandbox) logf(format string, args ...any) {
	if sb == nil || sb.opts == nil || sb.opts.logger == nil {
		return
	}
	sb.opts.logger.Printf(format, args...)
}
