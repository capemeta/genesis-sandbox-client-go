package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/capemeta/genesis-sandbox-client-go/internal/genapi"
)

type Config struct {
	BaseURL        string
	Token          string
	Timeout        time.Duration
	HTTPClient     *http.Client
	MaxAttempts    int
	RetryBaseDelay time.Duration
	UserAgent      string
}

// ErrNotModified is returned by conditional requests (If-None-Match) when the
// server responds 304 Not Modified; the cached representation is still valid.
var ErrNotModified = errors.New("not modified")

// loopbackHosts are the only hosts allowed to use plain http, matching the
// Platform endpoint constraints enforced by the sibling SDKs.
var loopbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// maxJSONResponseBytes caps JSON decoding so a misbehaving server cannot
// exhaust client memory. It is a variable so tests can tighten it.
var maxJSONResponseBytes int64 = 16 << 20

func decodeJSONLimited(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxJSONResponseBytes {
		return fmt.Errorf("sandbox client: JSON response exceeds %d byte budget", maxJSONResponseBytes)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// APIError is a structured error returned by the Sandbox Service.
type APIError struct {
	StatusCode int
	ErrorCode  string
	Message    string
	RequestID  string
	Details    map[string]any
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.ErrorCode == "" {
		return fmt.Sprintf("sandbox API status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("sandbox API %s (status %d, request_id=%s): %s", e.ErrorCode, e.StatusCode, e.RequestID, e.Message)
}

func (e *APIError) Retryable() bool {
	if value, ok := e.Details["retryable"].(bool); ok {
		return value
	}
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

func (c *Client) rawRequest(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	return c.rawRequestWithHeaders(ctx, method, path, contentType, body, nil)
}

func (c *Client) rawRequestWithHeaders(ctx context.Context, method, path, contentType string, body io.Reader, headers http.Header) (*http.Response, error) {
	maxAttempts := c.attemptsFor(method, body == nil)
	delay := c.cfg.RetryBaseDelay
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, body)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		if contentType != "" {
			if body == nil && (method == http.MethodGet || method == http.MethodHead) {
				req.Header.Set("Accept", contentType)
			} else {
				req.Header.Set("Content-Type", contentType)
			}
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		if c.cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt+1 < maxAttempts {
				if err := waitRetry(ctx, delay); err != nil {
					return nil, err
				}
				delay *= 2
				continue
			}
			break
		}

		if isTransientStatus(resp.StatusCode) && attempt+1 < maxAttempts {
			retryDelay := retryAfter(resp, delay)
			_ = resp.Body.Close()
			if err := waitRetry(ctx, retryDelay); err != nil {
				return nil, err
			}
			delay *= 2
			continue
		}

		if resp.StatusCode == http.StatusNotModified {
			// Conditional request hit; let callers translate this into ErrNotModified.
			return resp, nil
		}
		if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
			return resp, nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, decodeAPIError(resp)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("sandbox request failed after %d attempts: %w", maxAttempts, lastErr)
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("sandbox client: BaseURL must be an absolute http(s) URL")
	}
	// 与 Platform 端点约束一致：拒绝内嵌凭据、查询、片段与路径前缀。
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("sandbox client: BaseURL must not contain credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("sandbox client: BaseURL must not contain a path")
	}
	if parsed.Scheme != "https" && !loopbackHosts[strings.ToLower(parsed.Hostname())] {
		return nil, fmt.Errorf("sandbox client: non-https BaseURL is only allowed for loopback hosts (localhost/127.0.0.1/::1)")
	}
	if cfg.Token != "" {
		// 拒绝空白边缘与 CR/LF，避免 Authorization 头注入。
		if strings.TrimSpace(cfg.Token) != cfg.Token || strings.ContainsAny(cfg.Token, "\r\n") {
			return nil, fmt.Errorf("sandbox client: Token must not contain surrounding whitespace or CR/LF")
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("sandbox client: Timeout must be positive")
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "genesis-sandbox-client-go/1"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.Timeout,
			// Bearer 身份只发往配置的 BaseURL：拒绝跟随重定向（3xx 按错误返回）。
			// 自定义 HTTPClient 的调用方需自行保证相同的重定向纪律。
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}, nil
}

func (c *Client) cleanupSessionCreateFailure(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.DeleteSession(ctx, sessionID)
}

func (c *Client) request(ctx context.Context, method, path string, body any, res any) error {
	var bodyBytes []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = data
	}

	maxAttempts := c.attemptsFor(method, true)
	if method == http.MethodPost && requestHasIdempotencyKey(body) {
		maxAttempts = c.cfg.MaxAttempts
	}
	delay := c.cfg.RetryBaseDelay
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		url := fmt.Sprintf("%s%s", c.cfg.BaseURL, path)
		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		if c.cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.cfg.UserAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt+1 < maxAttempts {
				if err := waitRetry(ctx, delay); err != nil {
					return err
				}
				delay *= 2
				continue
			}
			break
		}

		if isTransientStatus(resp.StatusCode) && attempt+1 < maxAttempts {
			retryDelay := retryAfter(resp, delay)
			_ = resp.Body.Close()
			if err := waitRetry(ctx, retryDelay); err != nil {
				return err
			}
			delay *= 2
			continue
		}

		if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return decodeAPIError(resp)
		}

		defer resp.Body.Close()
		if res != nil {
			if err := decodeJSONLimited(resp.Body, res); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("sandbox request failed after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) attemptsFor(method string, replayable bool) int {
	if !replayable {
		return 1
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return c.cfg.MaxAttempts
	default:
		return 1
	}
}

func requestHasIdempotencyKey(body any) bool {
	switch request := body.(type) {
	case SubmitJobRequest:
		return strings.TrimSpace(request.IdempotencyKey) != ""
	case CreateSessionRequest:
		return strings.TrimSpace(request.IdempotencyKey) != ""
	case genapi.SubmitJobRequest:
		return request.IdempotencyKey != nil && strings.TrimSpace(*request.IdempotencyKey) != ""
	case genapi.CreateSessionRequest:
		return request.IdempotencyKey != nil && strings.TrimSpace(*request.IdempotencyKey) != ""
	default:
		return false
	}
}

func isTransientStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfter(resp *http.Response, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func decodeAPIError(resp *http.Response) error {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	payload := genapi.ErrorResponse{}
	details := map[string]any{}
	if err := json.Unmarshal(data, &payload); err != nil {
		payload.Message = strings.TrimSpace(string(data))
	} else {
		details = errorDetailsMap(payload.Details)
	}
	if payload.Message == "" {
		payload.Message = http.StatusText(resp.StatusCode)
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		ErrorCode:  string(payload.ErrorCode),
		Message:    payload.Message,
		RequestID:  payload.RequestId,
		Details:    details,
		RetryAfter: retryAfter(resp, 0),
	}
}

func (c *Client) Lease(ctx context.Context, req LeaseRequest) (*SandboxLease, error) {
	if err := validateSelector(req.Environment, req.ResolutionID); err != nil {
		return nil, err
	}
	var lease genapi.SandboxLease
	if err := c.request(ctx, http.MethodPost, "/v1/sandboxes:lease", toGenLeaseRequest(req), &lease); err != nil {
		return nil, err
	}
	return fromGenSandboxLease(lease), nil
}

func (c *Client) Release(ctx context.Context, sandboxID string) error {
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sandboxes/%s:release", url.PathEscape(sandboxID)), nil, nil)
}

func (c *Client) Destroy(ctx context.Context, sandboxID string) error {
	return c.request(ctx, http.MethodDelete, fmt.Sprintf("/v1/sandboxes/%s", url.PathEscape(sandboxID)), nil, nil)
}

func (c *Client) ListSandboxes(ctx context.Context) ([]SandboxLease, error) {
	var leases []genapi.SandboxLease
	if err := c.request(ctx, http.MethodGet, "/v1/sandboxes", nil, &leases); err != nil {
		return nil, err
	}
	return fromGenSandboxLeases(leases), nil
}

func (c *Client) GetSandbox(ctx context.Context, sandboxID string) (*SandboxLease, error) {
	var lease genapi.SandboxLease
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/sandboxes/%s", url.PathEscape(sandboxID)), nil, &lease); err != nil {
		return nil, err
	}
	return fromGenSandboxLease(lease), nil
}

func (c *Client) Renew(ctx context.Context, sandboxID string, extendSeconds int) (*SandboxLease, error) {
	var lease genapi.SandboxLease
	payload := map[string]int{"extend_seconds": extendSeconds}
	if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sandboxes/%s:renew", url.PathEscape(sandboxID)), payload, &lease); err != nil {
		return nil, err
	}
	return fromGenSandboxLease(lease), nil
}

// SubmitJob 提交 LRO job 并立即返回 queued/running 状态。
func (c *Client) SubmitJob(ctx context.Context, req SubmitJobRequest) (*JobResult, error) {
	if err := validateSelector(req.Environment, req.ResolutionID); err != nil {
		return nil, err
	}
	if (req.Code == "") == (len(req.Command) == 0) {
		return nil, errors.New("sandbox client: exactly one of code or command is required")
	}
	var result genapi.JobResult
	if err := c.request(ctx, http.MethodPost, "/v1/jobs", toGenSubmitJobRequest(req), &result); err != nil {
		return nil, err
	}
	return fromGenJobResult(result), nil
}

// WaitJob 轮询现有 job，直到进入终态或 ctx 结束。
func (c *Client) WaitJob(ctx context.Context, jobID string) (*JobResult, error) {
	return c.WaitJobWithOptions(ctx, jobID, WaitJobOptions{})
}

func (c *Client) WaitJobWithOptions(ctx context.Context, jobID string, opts WaitJobOptions) (*JobResult, error) {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	for {
		job, err := c.GetJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		switch job.Status {
		case "succeeded", "failed", "cancelled", "timed_out", "interrupted":
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) SubmitJobAndWait(ctx context.Context, req SubmitJobRequest, wait time.Duration) (*JobResult, error) {
	if err := validateSelector(req.Environment, req.ResolutionID); err != nil {
		return nil, err
	}
	if (req.Code == "") == (len(req.Command) == 0) {
		return nil, errors.New("sandbox client: exactly one of code or command is required")
	}
	path := "/v1/jobs"
	if wait > 0 {
		path += "?wait=" + url.QueryEscape(wait.String())
	}
	var result genapi.JobResult
	if err := c.request(ctx, http.MethodPost, path, toGenSubmitJobRequest(req), &result); err != nil {
		return nil, err
	}
	return fromGenJobResult(result), nil
}

func (c *Client) ListJobs(ctx context.Context, status string, limit, offset int) (*JobList, error) {
	query := url.Values{}
	if status != "" {
		query.Set("status", status)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	path := "/v1/jobs"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var result genapi.JobList
	if err := c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return fromGenJobList(result), nil
}

func (c *Client) GetJob(ctx context.Context, jobID string) (*JobResult, error) {
	var result genapi.JobResult
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/jobs/%s", url.PathEscape(jobID)), nil, &result); err != nil {
		return nil, err
	}
	return fromGenJobResult(result), nil
}

func (c *Client) CancelJob(ctx context.Context, jobID string) error {
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/jobs/%s:cancel", url.PathEscape(jobID)), nil, nil)
}

func (c *Client) UploadJobFile(ctx context.Context, jobID string, name string, r io.Reader) (*Artifact, error) {
	path := fmt.Sprintf("/v1/jobs/%s/files?name=%s", url.PathEscape(jobID), url.QueryEscape(name))
	resp, err := c.rawRequest(ctx, http.MethodPost, path, "application/octet-stream", r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var art genapi.Artifact
	if err := decodeJSONLimited(resp.Body, &art); err != nil {
		return nil, err
	}
	result := fromGenArtifact(art)
	return &result, nil
}

func (c *Client) DownloadArtifact(ctx context.Context, artifactID string) (io.ReadCloser, error) {
	resp, err := c.rawRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/artifacts/%s", url.PathEscape(artifactID)), "", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) ListJobArtifacts(ctx context.Context, jobID string, offset, limit int) ([]Artifact, error) {
	query := url.Values{}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	path := fmt.Sprintf("/v1/jobs/%s/artifacts", url.PathEscape(jobID))
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var artifacts []genapi.Artifact
	if err := c.request(ctx, http.MethodGet, path, nil, &artifacts); err != nil {
		return nil, err
	}
	return fromGenArtifacts(&artifacts), nil
}

func (c *Client) JobLogs(ctx context.Context, jobID string, cursor int) (io.ReadCloser, error) {
	path := fmt.Sprintf("/v1/jobs/%s/logs", url.PathEscape(jobID))
	if cursor > 0 {
		path += fmt.Sprintf("?cursor=%d", cursor)
	}
	resp, err := c.rawRequest(ctx, http.MethodGet, path, "text/event-stream", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) ExecSession(ctx context.Context, sandboxID string, req ExecSessionRequest) (*ExecSessionResult, error) {
	var result genapi.ExecSessionResult
	path := fmt.Sprintf("/v1/sandboxes/%s/sessions", url.PathEscape(sandboxID))
	if err := c.request(ctx, http.MethodPost, path, toGenExecSessionRequest(req), &result); err != nil {
		return nil, err
	}
	return fromGenExecSessionResult(result), nil
}

func (c *Client) CreateWorkspace(ctx context.Context, req CreateWorkspaceRequest) (*Workspace, error) {
	var workspace genapi.Workspace
	if err := c.request(ctx, http.MethodPost, "/v1/workspaces", toGenCreateWorkspaceRequest(req), &workspace); err != nil {
		return nil, err
	}
	return fromGenWorkspace(workspace), nil
}

func (c *Client) GetWorkspace(ctx context.Context, workspaceID string) (*Workspace, error) {
	var workspace genapi.Workspace
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/workspaces/%s", url.PathEscape(workspaceID)), nil, &workspace); err != nil {
		return nil, err
	}
	return fromGenWorkspace(workspace), nil
}

func (c *Client) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	return c.request(ctx, http.MethodDelete, fmt.Sprintf("/v1/workspaces/%s", url.PathEscape(workspaceID)), nil, nil)
}

func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	if err := validateSelector(req.Environment, req.ResolutionID); err != nil {
		return nil, err
	}
	if req.TTLSeconds < 0 {
		return nil, errors.New("sandbox client: session TTL cannot be negative")
	}
	var session genapi.Session
	if err := c.request(ctx, http.MethodPost, "/v1/sessions", toGenCreateSessionRequest(req), &session); err != nil {
		return nil, err
	}
	if len(req.Env) > 0 {
		sessionID := derefString(session.SessionId)
		if _, err := c.PatchSessionContext(ctx, sessionID, SessionContext{Env: req.Env}); err != nil {
			if cleanupErr := c.cleanupSessionCreateFailure(sessionID); cleanupErr != nil {
				return nil, fmt.Errorf("apply session env: %w (cleanup session %s: %v)", err, sessionID, cleanupErr)
			}
			return nil, fmt.Errorf("apply session env: %w", err)
		}
	}
	return fromGenSession(session), nil
}

func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	var session genapi.Session
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/sessions/%s", url.PathEscape(sessionID)), nil, &session); err != nil {
		return nil, err
	}
	return fromGenSession(session), nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	return c.request(ctx, http.MethodDelete, fmt.Sprintf("/v1/sessions/%s", url.PathEscape(sessionID)), nil, nil)
}

// SuspendSession releases the ephemeral runtime while preserving the session workspace.
// If force is true, running execs are cancelled before suspending.
func (c *Client) SuspendSession(ctx context.Context, sessionID string, opts ...SuspendOption) (*Session, error) {
	var cfg suspendConfig
	for _, o := range opts {
		o(&cfg)
	}
	path := fmt.Sprintf("/v1/sessions/%s:suspend", url.PathEscape(sessionID))
	if cfg.Force {
		path += "?force=true"
	}
	var session genapi.Session
	if err := c.request(ctx, http.MethodPost, path, nil, &session); err != nil {
		return nil, err
	}
	return fromGenSession(session), nil
}

// SuspendOption configures SuspendSession behavior.
type SuspendOption func(*suspendConfig)

type suspendConfig struct {
	Force bool
}

// WithForce cancels all running execs before suspending.
func WithForce() SuspendOption {
	return func(c *suspendConfig) { c.Force = true }
}

// RenewSession 续期 session（心跳续约），同时延长底层沙箱租约。
// extendSeconds 为本次续期延长的秒数，服务端受 max_lease_timeout 硬上限约束。
func (c *Client) RenewSession(ctx context.Context, sessionID string, extendSeconds int) (*Session, error) {
	var session genapi.Session
	payload := map[string]int{"extend_seconds": extendSeconds}
	if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sessions/%s:renew", url.PathEscape(sessionID)), payload, &session); err != nil {
		return nil, err
	}
	return fromGenSession(session), nil
}

func (c *Client) ExecNamedSession(ctx context.Context, sessionID string, req ExecSessionRequest) (*ExecSessionResult, error) {
	var result genapi.ExecSessionResult
	if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sessions/%s/exec", url.PathEscape(sessionID)), toGenExecSessionRequest(req), &result); err != nil {
		return nil, err
	}
	return fromGenExecSessionResult(result), nil
}

// ExecSessionAsync submits an async exec for the given session.
func (c *Client) ExecSessionAsync(ctx context.Context, sessionID string, req ExecSessionRequest) (*ExecRecord, error) {
	var record genapi.ExecRecord
	if err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sessions/%s/exec:async", url.PathEscape(sessionID)), toGenAsyncExecRequest(req), &record); err != nil {
		return nil, err
	}
	return fromGenExecRecord(record), nil
}

// GetExec retrieves the status of an async exec.
func (c *Client) GetExec(ctx context.Context, sessionID, execID string) (*ExecRecord, error) {
	var record genapi.ExecRecord
	path := fmt.Sprintf("/v1/sessions/%s/execs/%s", url.PathEscape(sessionID), url.PathEscape(execID))
	if err := c.request(ctx, http.MethodGet, path, nil, &record); err != nil {
		return nil, err
	}
	return fromGenExecRecord(record), nil
}

// StreamExecLogs opens an SSE stream for exec logs. Caller must close the returned ReadCloser.
func (c *Client) StreamExecLogs(ctx context.Context, sessionID, execID string, cursor int) (io.ReadCloser, error) {
	path := fmt.Sprintf("/v1/sessions/%s/execs/%s/logs", url.PathEscape(sessionID), url.PathEscape(execID))
	if cursor > 0 {
		path += fmt.Sprintf("?cursor=%d", cursor)
	}
	resp, err := c.rawRequest(ctx, http.MethodGet, path, "text/event-stream", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// CancelExec cancels a running or queued async exec.
func (c *Client) CancelExec(ctx context.Context, sessionID, execID string) error {
	path := fmt.Sprintf("/v1/sessions/%s/execs/%s:cancel", url.PathEscape(sessionID), url.PathEscape(execID))
	return c.request(ctx, http.MethodPost, path, nil, nil)
}

// GetSessionContext retrieves the session-level cwd and env.
func (c *Client) GetSessionContext(ctx context.Context, sessionID string) (*SessionContext, error) {
	var sc genapi.SessionContext
	path := fmt.Sprintf("/v1/sessions/%s/context", url.PathEscape(sessionID))
	if err := c.request(ctx, http.MethodGet, path, nil, &sc); err != nil {
		return nil, err
	}
	return fromGenSessionContext(sc), nil
}

// PatchSessionContext modifies the session-level cwd and/or env.
func (c *Client) PatchSessionContext(ctx context.Context, sessionID string, patch SessionContext) (*SessionContext, error) {
	var sc genapi.SessionContext
	path := fmt.Sprintf("/v1/sessions/%s/context", url.PathEscape(sessionID))
	if err := c.request(ctx, http.MethodPatch, path, toGenSessionContext(patch), &sc); err != nil {
		return nil, err
	}
	return fromGenSessionContext(sc), nil
}

// GetCatalog retrieves the environment profile catalog.
func (c *Client) GetCatalog(ctx context.Context, query CatalogQuery) (*CatalogResponse, error) {
	q := url.Values{}
	if query.Capability != "" {
		q.Set("capability", query.Capability)
	}
	if query.Tag != "" {
		q.Set("tag", query.Tag)
	}
	if query.Limit > 0 {
		q.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Offset > 0 {
		q.Set("offset", strconv.Itoa(query.Offset))
	}
	path := "/v1/environment/catalog"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var headers http.Header
	if query.IfNoneMatch != "" {
		headers = make(http.Header)
		headers.Set("If-None-Match", query.IfNoneMatch)
	}
	resp, err := c.rawRequestWithHeaders(ctx, http.MethodGet, path, "application/json", nil, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}
	var catalog genapi.EnvironmentCatalog
	if err := decodeJSONLimited(resp.Body, &catalog); err != nil {
		return nil, err
	}
	result := fromGenCatalogResponse(catalog)
	result.Revision = resp.Header.Get("ETag")
	return result, nil
}

// ResolveEnvironment performs a two-phase resolution and returns a principal-bound ticket.
func (c *Client) ResolveEnvironment(ctx context.Context, req ResolveEnvironmentRequest) (*EnvironmentResolution, error) {
	if err := validateSelector(&req.Environment, ""); err != nil {
		return nil, err
	}
	if req.TTLSeconds != 0 && req.TTLSeconds < 60 {
		return nil, errors.New("sandbox client: resolution TTL must be at least 60 seconds")
	}
	var resolution genapi.EnvironmentResolution
	if err := c.request(ctx, http.MethodPost, "/v1/environment:resolve", toGenResolveEnvironmentRequest(req), &resolution); err != nil {
		return nil, err
	}
	return fromGenEnvironmentResolution(resolution), nil
}

func validateSelector(selector *EnvironmentSelector, resolutionID string) error {
	if selector != nil && resolutionID != "" {
		return errors.New("sandbox client: resolution_id and environment are mutually exclusive")
	}
	if selector == nil {
		return nil
	}
	if (selector.Profile == nil) == (selector.Hints == nil) {
		return errors.New("sandbox client: environment must contain exactly one of profile or hints")
	}
	if selector.Profile != nil && strings.TrimSpace(selector.Profile.Name) == "" {
		return errors.New("sandbox client: profile name cannot be empty")
	}
	return nil
}

func sessionFilePath(sessionID, suffix string, query url.Values) string {
	pathValue := fmt.Sprintf("/v1/sessions/%s/%s", url.PathEscape(sessionID), suffix)
	if query == nil || len(query) == 0 {
		return pathValue
	}
	return pathValue + "?" + query.Encode()
}

func decodeDownloadInfo(resp *http.Response, fallbackPath string) *WorkspaceFileInfo {
	info := &WorkspaceFileInfo{
		Path:        firstNonEmpty(resp.Header.Get("X-Workspace-Path"), fallbackPath),
		SandboxPath: resp.Header.Get("X-Sandbox-Path"),
		Environment: firstNonEmpty(resp.Header.Get("X-Genesis-Environment"), "workspace"),
		MIME:        resp.Header.Get("Content-Type"),
		Kind:        "file",
	}
	info.SHA256 = strings.Trim(resp.Header.Get("ETag"), `"`)
	if base := path.Base(info.Path); base != "." && base != "/" {
		info.Name = base
	}
	if size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
		info.Size = size
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := params["filename"]; filename != "" {
				info.Name = filename
			}
		}
	}
	return info
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *Client) DownloadSessionFile(ctx context.Context, sessionID, workspacePath string) (io.ReadCloser, *WorkspaceFileInfo, error) {
	query := url.Values{}
	query.Set("path", workspacePath)
	resp, err := c.rawRequest(ctx, http.MethodGet, sessionFilePath(sessionID, "files", query), "", nil)
	if err != nil {
		return nil, nil, err
	}
	return resp.Body, decodeDownloadInfo(resp, workspacePath), nil
}

func (c *Client) UploadSessionFile(ctx context.Context, sessionID, workspacePath string, content io.Reader) (*WorkspaceFileInfo, error) {
	return c.UploadSessionFileConditional(ctx, sessionID, workspacePath, content, "", false)
}

// UploadSessionFileConditional performs an optimistic conditional write.
// ifMatch accepts the SHA-256/ETag returned by a previous read; createOnly maps to If-None-Match: *.
func (c *Client) UploadSessionFileConditional(ctx context.Context, sessionID, workspacePath string, content io.Reader, ifMatch string, createOnly bool) (*WorkspaceFileInfo, error) {
	query := url.Values{}
	query.Set("path", workspacePath)
	headers := make(http.Header)
	if strings.TrimSpace(ifMatch) != "" {
		headers.Set("If-Match", ifMatch)
	}
	if createOnly {
		headers.Set("If-None-Match", "*")
	}
	resp, err := c.rawRequestWithHeaders(ctx, http.MethodPut, sessionFilePath(sessionID, "files", query), "application/octet-stream", content, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info WorkspaceFileInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ListSessionFiles(ctx context.Context, sessionID, workspacePath string, recursive bool, limit int) (*WorkspaceListResult, error) {
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", strconv.FormatBool(recursive))
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var result WorkspaceListResult
	if err := c.request(ctx, http.MethodGet, sessionFilePath(sessionID, "files:list", query), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) StatSessionFile(ctx context.Context, sessionID, workspacePath string) (*WorkspaceFileInfo, error) {
	query := url.Values{}
	query.Set("path", workspacePath)
	var info WorkspaceFileInfo
	if err := c.request(ctx, http.MethodGet, sessionFilePath(sessionID, "files:stat", query), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) MkdirSessionDir(ctx context.Context, sessionID, workspacePath string) (*WorkspaceFileInfo, error) {
	query := url.Values{}
	query.Set("path", workspacePath)
	var info WorkspaceFileInfo
	if err := c.request(ctx, http.MethodPost, sessionFilePath(sessionID, "dirs", query), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) RemoveSessionFile(ctx context.Context, sessionID, workspacePath string, recursive bool) error {
	query := url.Values{}
	query.Set("path", workspacePath)
	query.Set("recursive", strconv.FormatBool(recursive))
	return c.request(ctx, http.MethodDelete, sessionFilePath(sessionID, "files", query), nil, nil)
}

func (c *Client) BuildDependencies(ctx context.Context, req BuildDependencyRequest) (*DependencyBuild, error) {
	if (req.Environment == nil) == (strings.TrimSpace(req.ResolutionID) == "") {
		return nil, fmt.Errorf("exactly one of environment or resolution_id is required")
	}
	if req.Environment != nil &&
		(req.Environment.Profile == nil || req.Environment.Hints != nil ||
			strings.TrimSpace(req.Environment.Profile.Name) == "" ||
			strings.TrimSpace(req.Environment.Profile.Revision) == "") {
		return nil, fmt.Errorf("environment must contain an exact profile name and revision")
	}
	var build genapi.DependencyBuild
	if err := c.request(ctx, http.MethodPost, "/v1/dependencies:build", toGenBuildDependencyRequest(req), &build); err != nil {
		return nil, err
	}
	return fromGenDependencyBuild(build), nil
}

func (c *Client) GetDependencyBuild(ctx context.Context, fingerprint string) (*DependencyBuild, error) {
	var build genapi.DependencyBuild
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/dependencies/%s", url.PathEscape(fingerprint)), nil, &build); err != nil {
		return nil, err
	}
	return fromGenDependencyBuild(build), nil
}

func (c *Client) StartGUI(ctx context.Context, sandboxID string, req StartGUIRequest) (*ViewerDescriptor, error) {
	var viewer genapi.ViewerDescriptor
	path := fmt.Sprintf("/v1/sandboxes/%s/gui:start", url.PathEscape(sandboxID))
	if err := c.request(ctx, http.MethodPost, path, toGenStartGUIRequest(req), &viewer); err != nil {
		return nil, err
	}
	return fromGenViewerDescriptor(viewer), nil
}

func (c *Client) StopGUI(ctx context.Context, sandboxID string) error {
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/sandboxes/%s/gui:stop", url.PathEscape(sandboxID)), nil, nil)
}

func (c *Client) GetViewer(ctx context.Context, sandboxID string) (*ViewerDescriptor, error) {
	var viewer genapi.ViewerDescriptor
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/sandboxes/%s/viewer", url.PathEscape(sandboxID)), nil, &viewer); err != nil {
		return nil, err
	}
	return fromGenViewerDescriptor(viewer), nil
}

func (c *Client) WaitViewer(ctx context.Context, sandboxID string, opts WaitViewerOptions) (*ViewerDescriptor, error) {
	if opts.PollInterval == 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		viewer, err := c.GetViewer(ctx, sandboxID)
		if err == nil && viewer.Ready && (opts.Kind == "" || viewer.Kind == opts.Kind) {
			return viewer, nil
		}
		if err != nil {
			if !isViewerWaitRetryable(err) {
				return nil, err
			}
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil && !errors.Is(ctx.Err(), context.Canceled) {
				return nil, fmt.Errorf("wait viewer timeout: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) PatchSandbox(ctx context.Context, sandboxID string, metadata map[string]string, resourceVersion int64) (*SandboxLease, error) {
	patch := make(SandboxMetadataPatch, len(metadata))
	for key, value := range metadata {
		patch[key] = &value
	}
	return c.PatchSandboxMetadata(ctx, sandboxID, patch, resourceVersion)
}

func (c *Client) PatchSandboxMetadata(ctx context.Context, sandboxID string, metadata SandboxMetadataPatch, resourceVersion int64) (*SandboxLease, error) {
	var lease genapi.SandboxLease
	payload := map[string]any{
		"metadata": toRawSandboxMetadataPatch(metadata),
	}
	headers := make(http.Header)
	headers.Set("If-Match", strconv.FormatInt(resourceVersion, 10))
	path := fmt.Sprintf("/v1/sandboxes/%s/metadata", url.PathEscape(sandboxID))
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal sandbox metadata patch: %w", err)
	}
	resp, err := c.rawRequestWithHeaders(ctx, http.MethodPatch, path, "application/json", bytes.NewReader(data), headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := decodeJSONLimited(resp.Body, &lease); err != nil {
		return nil, err
	}
	return fromGenSandboxLease(lease), nil
}

func (c *Client) ListAuditEvents(ctx context.Context, query AuditQuery) (*AuditEventList, error) {
	values := url.Values{}
	if query.PrincipalID != "" {
		values.Set("principal_id", query.PrincipalID)
	}
	if query.KeyID != "" {
		values.Set("key_id", query.KeyID)
	}
	if query.Action != "" {
		values.Set("action", query.Action)
	}
	if query.ResourceType != "" {
		values.Set("resource_type", query.ResourceType)
	}
	if query.ResourceID != "" {
		values.Set("resource_id", query.ResourceID)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Offset > 0 {
		values.Set("offset", strconv.Itoa(query.Offset))
	}
	path := "/v1/audits"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result genapi.AuditEventList
	if err := c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return fromGenAuditEventList(result), nil
}

func (c *Client) ListSessionExecs(ctx context.Context, sessionID string, query ExecListQuery) (*ExecRecordList, error) {
	values := url.Values{}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	path := fmt.Sprintf("/v1/sessions/%s/execs", url.PathEscape(sessionID))
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result genapi.ExecRecordList
	if err := c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return fromGenExecRecordList(result), nil
}

func toRawSandboxMetadataPatch(metadata SandboxMetadataPatch) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if value == nil {
			result[key] = nil
			continue
		}
		result[key] = *value
	}
	return result
}

func isViewerWaitRetryable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Retryable() {
		return true
	}
	switch apiErr.StatusCode {
	case http.StatusNotFound, http.StatusConflict:
		return true
	default:
		return false
	}
}
