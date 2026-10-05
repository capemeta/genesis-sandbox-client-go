package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Run executes a command or code and waits for the result.
//   - Job mode (not opened): composes SubmitJob + WaitJob
//   - Session mode (after Open): calls ExecNamedSession (reuses container)
//
// The cmdOrCode argument is interpreted as:
//   - Source code if WithLang is provided (sent as "code" field)
//   - Shell command otherwise (sent as command: ["/bin/sh", "-c", cmdOrCode])
func (sb *Sandbox) Run(ctx context.Context, cmdOrCode string, opts ...ExecOption) (*ExecResult, error) {
	if sb.IsClosed() {
		return nil, ErrClosed
	}
	o := resolveExecOptions(opts)

	if sb.IsOpen() {
		return sb.runSession(ctx, cmdOrCode, o)
	}
	return sb.runJob(ctx, cmdOrCode, o)
}

// runSession executes via ExecNamedSession (Session mode).
func (sb *Sandbox) runSession(ctx context.Context, cmdOrCode string, o *execOptions) (*ExecResult, error) {
	req := buildExecSessionRequest(cmdOrCode, o)
	if defaults := sb.snapshotDefaultEnv(); len(defaults) > 0 {
		merged := make(map[string]string, len(defaults)+len(req.Env))
		for k, v := range defaults {
			merged[k] = v
		}
		for k, v := range req.Env {
			merged[k] = v
		}
		req.Env = merged
	}

	callCtx := ctx
	if o.timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	res, err := sb.client.ExecNamedSession(callCtx, sb.SessionID(), req)
	if err != nil {
		return nil, fmt.Errorf("sandbox.Run (session): %w", err)
	}
	return &ExecResult{
		ExitCode:             res.ExitCode,
		Stdout:               res.Stdout,
		Stderr:               res.Stderr,
		ErrorCode:            res.ErrorCode,
		EffectiveEnvironment: res.EffectiveEnvironment,
		StdoutTruncated:      res.StdoutTruncated,
		StderrTruncated:      res.StderrTruncated,
	}, nil
}

// runJob executes via SubmitJob + WaitJob (Job mode).
func (sb *Sandbox) runJob(ctx context.Context, cmdOrCode string, o *execOptions) (*ExecResult, error) {
	req := buildSubmitJobRequest(cmdOrCode, o, sb.opts)

	callCtx := ctx
	if o.timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	job, err := submitAndWaitJob(callCtx, sb.client, req)
	if err != nil {
		return nil, fmt.Errorf("sandbox.Run (job): %w", err)
	}
	return jobToExecResult(job), nil
}

// RunAsync submits an async exec (Session mode only) and returns an ExecHandle.
func (sb *Sandbox) RunAsync(ctx context.Context, cmdOrCode string, opts ...ExecOption) (*ExecHandle, error) {
	if !sb.IsOpen() {
		return nil, ErrNotOpened
	}
	o := resolveExecOptions(opts)
	req := buildExecSessionRequest(cmdOrCode, o)
	if defaults := sb.snapshotDefaultEnv(); len(defaults) > 0 {
		for key, value := range req.Env {
			defaults[key] = value
		}
		req.Env = defaults
	}
	if o.callbackURL != "" {
		req.CallbackURL = o.callbackURL
	}

	record, err := sb.client.ExecSessionAsync(ctx, sb.SessionID(), req)
	if err != nil {
		return nil, fmt.Errorf("sandbox.RunAsync: %w", err)
	}
	return &ExecHandle{
		client:    sb.client,
		sessionID: sb.SessionID(),
		execID:    record.ExecID,
	}, nil
}

// ---------------------------------------------------------------------------
// ExecHandle — async exec handle (Session mode)
// ---------------------------------------------------------------------------

// ExecHandle represents a running or completed async exec.
type ExecHandle struct {
	client    *Client
	sessionID string
	execID    string
}

// ID returns the exec identifier.
func (h *ExecHandle) ID() string { return h.execID }

// Wait polls GetExec until the exec reaches a terminal state.
func (h *ExecHandle) Wait(ctx context.Context) (*ExecResult, error) {
	return h.WaitWithOptions(ctx, WaitExecOptions{})
}

func (h *ExecHandle) WaitWithOptions(ctx context.Context, opts WaitExecOptions) (*ExecResult, error) {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	for {
		record, err := h.client.GetExec(ctx, h.sessionID, h.execID)
		if err != nil {
			return nil, fmt.Errorf("ExecHandle.Wait: %w", err)
		}
		switch record.Status {
		case "succeeded", "failed", "cancelled", "timed_out", "interrupted":
			return &ExecResult{
				ExitCode:        record.ExitCode,
				Stdout:          record.Stdout,
				Stderr:          record.Stderr,
				StdoutTruncated: record.StdoutTruncated,
				StderrTruncated: record.StderrTruncated,
				ErrorCode:       record.ErrorCode,
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Cancel cancels a running or queued exec.
func (h *ExecHandle) Cancel(ctx context.Context) (*ExecRecord, error) {
	return h.client.CancelExec(ctx, h.sessionID, h.execID)
}

// Logs opens an SSE log stream. Caller must close the returned ReadCloser.
func (h *ExecHandle) Logs(ctx context.Context, cursor int) (io.ReadCloser, error) {
	return h.client.StreamExecLogs(ctx, h.sessionID, h.execID, cursor)
}

// Events opens an incrementally decoded SSE log stream. Caller must close it.
func (h *ExecHandle) Events(ctx context.Context, cursor int) (*SSEStream, error) {
	return h.client.ExecLogEvents(ctx, h.sessionID, h.execID, cursor)
}

// Status returns the current exec record.
func (h *ExecHandle) Status(ctx context.Context) (*ExecRecord, error) {
	return h.client.GetExec(ctx, h.sessionID, h.execID)
}

// ---------------------------------------------------------------------------
// Layer 3 — Scenario helpers (syntax sugar)
// ---------------------------------------------------------------------------

// RunPython executes Python code (equivalent to Run(ctx, code, WithLang("python"))).
func (sb *Sandbox) RunPython(ctx context.Context, code string) (*ExecResult, error) {
	return sb.Run(ctx, code, WithLang("python"))
}

// RunShell executes a shell script (equivalent to Run(ctx, script, WithLang("shell"))).
func (sb *Sandbox) RunShell(ctx context.Context, script string) (*ExecResult, error) {
	return sb.Run(ctx, script, WithLang("shell"))
}

// RunNode executes Node.js code (equivalent to Run(ctx, code, WithLang("javascript"))).
func (sb *Sandbox) RunNode(ctx context.Context, code string) (*ExecResult, error) {
	return sb.Run(ctx, code, WithLang("javascript"))
}

// QuickRun executes a single command via a one-shot Job.
// No Workspace or Session is created. For simple one-shot execution.
func QuickRun(ctx context.Context, client *Client, command string, opts ...Option) (*ExecResult, error) {
	o := defaultSandboxOptions()
	for _, fn := range opts {
		fn(o)
	}
	req := SubmitJobRequest{
		Command:      []string{"/bin/sh", "-c", command},
		Environment:  o.environment,
		ResolutionID: o.resolutionID,
		Env:          o.env,
		Metadata:     o.metadata,
	}
	job, err := submitAndWaitJob(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("sandbox.QuickRun: %w", err)
	}
	return jobToExecResult(job), nil
}

// QuickPython executes Python code via a one-shot Job.
func QuickPython(ctx context.Context, client *Client, code string, opts ...Option) (*ExecResult, error) {
	o := defaultSandboxOptions()
	for _, fn := range opts {
		fn(o)
	}
	req := SubmitJobRequest{
		Code:         code,
		Language:     "python",
		Environment:  o.environment,
		ResolutionID: o.resolutionID,
		Env:          o.env,
		Metadata:     o.metadata,
	}
	job, err := submitAndWaitJob(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("sandbox.QuickPython: %w", err)
	}
	return jobToExecResult(job), nil
}

func submitAndWaitJob(ctx context.Context, client *Client, req SubmitJobRequest) (*JobResult, error) {
	job, err := client.SubmitJobAndWait(ctx, req, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if isTerminalJobStatus(job.Status) {
		return job, nil
	}
	return client.WaitJob(ctx, job.JobID)
}

// ---------------------------------------------------------------------------
// Request builders
// ---------------------------------------------------------------------------

func buildExecSessionRequest(cmdOrCode string, o *execOptions) ExecSessionRequest {
	req := ExecSessionRequest{
		WorkingDir:     o.workingDir,
		Env:            o.env,
		TimeoutSeconds: int(o.timeout.Seconds()),
		CallbackURL:    o.callbackURL,
	}
	if o.lang != "" && normalizeExecLanguage(o.lang) != "" {
		req.Code = cmdOrCode
		req.Language = normalizeExecLanguage(o.lang)
	} else {
		req.Command = []string{"/bin/sh", "-c", cmdOrCode}
	}
	return req
}

func buildSubmitJobRequest(cmdOrCode string, o *execOptions, sbOpts *sandboxOptions) SubmitJobRequest {
	req := SubmitJobRequest{
		Environment: sbOpts.environment,
		Env:         sbOpts.env,
		Metadata:    sbOpts.metadata,
	}
	if sbOpts.resolutionID != "" {
		req.ResolutionID = sbOpts.resolutionID
	}
	if o.timeout > 0 {
		req.TimeoutSeconds = int(o.timeout.Seconds())
	}
	if o.env != nil {
		// Per-exec env overrides sandbox-level env
		merged := make(map[string]string)
		for k, v := range sbOpts.env {
			merged[k] = v
		}
		for k, v := range o.env {
			merged[k] = v
		}
		req.Env = merged
	}
	if o.lang != "" {
		if lang := normalizeExecLanguage(o.lang); lang != "" {
			req.Code = cmdOrCode
			req.Language = lang
		} else {
			req.Command = []string{"/bin/sh", "-c", cmdOrCode}
		}
	} else {
		req.Command = []string{"/bin/sh", "-c", cmdOrCode}
	}
	return req
}

func jobToExecResult(job *JobResult) *ExecResult {
	if job == nil {
		return &ExecResult{ErrorCode: "nil_response"}
	}
	return &ExecResult{
		ExitCode:             job.ExitCode,
		Stdout:               job.Stdout,
		Stderr:               job.Stderr,
		StdoutTruncated:      job.StdoutTruncated,
		StderrTruncated:      job.StderrTruncated,
		ErrorCode:            job.ErrorCode,
		EffectiveEnvironment: job.EffectiveEnvironment,
	}
}

func normalizeExecLanguage(lang string) string {
	switch lower := strings.ToLower(lang); lower {
	case "python", "python3":
		return "python"
	case "node", "javascript", "js":
		return "javascript"
	case "typescript", "ts":
		return "typescript"
	default:
		return ""
	}
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "timed_out", "interrupted":
		return true
	default:
		return false
	}
}
