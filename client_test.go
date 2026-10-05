package sandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestModelsDoNotExposeCallerIdentity(t *testing.T) {
	for _, request := range []any{
		LeaseRequest{},
		SubmitJobRequest{},
		CreateWorkspaceRequest{},
		CreateSessionRequest{},
		BuildDependencyRequest{},
	} {
		requestType := reflect.TypeOf(request)
		for _, fieldName := range []string{"TenantID", "UserID"} {
			if _, exists := requestType.FieldByName(fieldName); exists {
				t.Errorf("%s must not expose %s", requestType.Name(), fieldName)
			}
		}
	}
}

func TestNewClientValidatesBaseURL(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "localhost:18010"}); err == nil {
		t.Fatal("NewClient() error = nil, want invalid URL error")
	}
}

func TestSubmitJobValidatesSelectorAndExecutionLocally(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://sandbox.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SubmitJob(context.Background(), SubmitJobRequest{
		Code:         "print(1)",
		ResolutionID: "resolution-1",
		Environment:  &EnvironmentSelector{Profile: &ProfileRef{Name: "python"}},
	})
	if err == nil {
		t.Fatal("selector conflict error = nil")
	}
	_, err = client.SubmitJob(context.Background(), SubmitJobRequest{})
	if err == nil {
		t.Fatal("missing code/command error = nil")
	}
}

func TestResolveEnvironmentUsesRequestEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q", contentType)
		}
		body, _ := io.ReadAll(r.Body)
		if got := string(body); !strings.Contains(got, `"environment":{"profile":{"name":"office-basic"}}`) {
			t.Errorf("resolve request body = %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resolution_id":"res-1","profile_name":"office-basic"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResolveEnvironment(context.Background(), ResolveEnvironmentRequest{
		Environment: EnvironmentSelector{Profile: &ProfileRef{Name: "office-basic"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveEnvironmentCanBindProductDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if got := string(body); !strings.Contains(got, `"environment":{"hints":{}}`) {
			t.Errorf("resolve request body = %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resolution_id":"res-1","profile_name":"code-basic"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResolveEnvironment(context.Background(), ResolveEnvironmentRequest{
		Environment: EnvironmentSelector{Hints: &EnvHints{}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDependenciesRequiresImmutableEnvironment(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://sandbox.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.BuildDependencies(context.Background(), BuildDependencyRequest{}); err == nil {
		t.Fatal("missing environment error = nil")
	}
	if _, err := client.BuildDependencies(context.Background(), BuildDependencyRequest{
		Environment: &EnvironmentSelector{Profile: &ProfileRef{Name: "code-basic"}},
	}); err == nil {
		t.Fatal("missing profile revision error = nil")
	}
}

func TestPostIsNotRetriedAndReturnsStructuredAPIError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{
			"error_code":"EXECD_UNAVAILABLE",
			"message":"execd unavailable",
			"request_id":"req_test",
			"details":{"retryable":true,"resource_type":"execd"}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		MaxAttempts:    5,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SubmitJob(context.Background(), SubmitJobRequest{Code: "print(1)"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("POST calls = %d, want 1", calls.Load())
	}
	if apiErr.ErrorCode != "EXECD_UNAVAILABLE" || apiErr.RequestID != "req_test" || !apiErr.Retryable() {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
}

func TestIdempotentGetRetriesTransientResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error_code":"UNAVAILABLE","message":"retry","details":{"retryable":true}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job-1","status":"succeeded","exit_code":0}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		MaxAttempts:    3,
		RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "succeeded" || calls.Load() != 2 {
		t.Fatalf("job=%+v calls=%d", job, calls.Load())
	}
}

func TestPostWithIdempotencyKeyDoesNotReplayUnknownSideEffect(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error_code":"UNAVAILABLE","message":"retry","details":{"retryable":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"job_id":"job-1","status":"queued"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL: server.URL, MaxAttempts: 3, RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SubmitJob(context.Background(), SubmitJobRequest{
		Code: "print(1)", IdempotencyKey: "request-1",
	}); err == nil {
		t.Fatal("unknown mutation must return an error, not replay")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestCreateSessionDoesNotPatchRemovedEnvironmentContext(t *testing.T) {
	var createCalls atomic.Int32
	var patchCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			createCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"env":`) {
				t.Fatalf("create session body leaked env: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"session_id":"sess-1",
				"workspace_id":"ws-1",
				"runtime_profile":"python",
				"state_policy":"session",
				"status":"active",
				"created_at":"2026-07-26T00:00:00Z",
				"expires_at":"2026-07-26T01:00:00Z",
				"resource_version":1
			}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/sessions/sess-1/context":
			patchCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"env":{"FOO":"bar"}`) {
				t.Fatalf("patch session context body = %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"env":{"FOO":"bar"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	session, err := client.CreateSession(context.Background(), CreateSessionRequest{
		StatePolicy: "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-1" {
		t.Fatalf("session_id = %q", session.SessionID)
	}
	if createCalls.Load() != 1 || patchCalls.Load() != 0 {
		t.Fatalf("createCalls=%d patchCalls=%d", createCalls.Load(), patchCalls.Load())
	}
}

func TestCreateSessionDoesNotDeleteSuccessfulAllocationForRemovedEnvironmentContext(t *testing.T) {
	var deleteCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"session_id":"sess-1",
				"workspace_id":"ws-1",
				"runtime_profile":"python",
				"state_policy":"session",
				"status":"active",
				"created_at":"2026-07-26T00:00:00Z",
				"expires_at":"2026-07-26T01:00:00Z",
				"resource_version":1
			}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/sessions/sess-1/context":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
				"error_code":"INVALID_ARGUMENT",
				"message":"bad env",
				"request_id":"req-1",
				"details":{"retryable":false}
			}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sessions/sess-1":
			deleteCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.CreateSession(context.Background(), CreateSessionRequest{
		StatePolicy: "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleteCalls.Load() != 0 {
		t.Fatalf("deleteCalls=%d, want 0", deleteCalls.Load())
	}
}

func TestSyncExecDoesNotSendCallbackURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess-1/exec" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"callback_url"`) {
			t.Fatalf("sync exec body leaked callback_url: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exit_code":0,"stdout":"ok","stderr":"","environment":"sandbox"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.ExecNamedSession(context.Background(), "sess-1", ExecSessionRequest{
		Command:     []string{"echo", "ok"},
		CallbackURL: "https://hooks.example.com/done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "ok" {
		t.Fatalf("result=%+v", result)
	}
}

func TestAsyncExecSendsCallbackURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/sess-1/exec:async" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"callback_url":"https://hooks.example.com/done"`) {
			t.Fatalf("async exec body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exec_id":"exec-1","session_id":"sess-1","status":"queued"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	record, err := client.ExecSessionAsync(context.Background(), "sess-1", ExecSessionRequest{
		Command:     []string{"echo", "ok"},
		CallbackURL: "https://hooks.example.com/done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ExecID != "exec-1" {
		t.Fatalf("exec_id = %q", record.ExecID)
	}
}

func TestSubmitJobAndWaitUsesWaitQueryAndLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/jobs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("wait") != "5s" {
			t.Fatalf("wait query = %q", r.URL.Query().Get("wait"))
		}
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		if !strings.Contains(payload, `"language":"python"`) {
			t.Fatalf("request body = %s", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job-1","status":"succeeded","exit_code":0,"duration_ms":12}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	job, err := client.SubmitJobAndWait(context.Background(), SubmitJobRequest{
		Code:     "print(1)",
		Language: "python",
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != "job-1" || job.Status != "succeeded" || job.DurationMS != 12 {
		t.Fatalf("job = %+v", job)
	}
}

func TestPatchSandboxMetadataUsesIfMatchAndSupportsDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/sandboxes/sb-1/metadata" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if match := r.Header.Get("If-Match"); match != "3" {
			t.Fatalf("If-Match = %q, want 3", match)
		}
		body, _ := io.ReadAll(r.Body)
		payload := string(body)
		if strings.Contains(payload, "resource_version") {
			t.Fatalf("legacy resource_version leaked into body: %s", payload)
		}
		if !strings.Contains(payload, `"owner":"alice"`) || !strings.Contains(payload, `"obsolete":null`) {
			t.Fatalf("patch body = %s", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sandbox_id":"sb-1",
			"lease_id":"lease-1",
			"tenant_id":"tenant-a",
			"runtime_profile":"code-polyglot-basic",
			"status":"leased",
			"resource_version":4
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	lease, err := client.PatchSandboxMetadata(context.Background(), "sb-1", SandboxMetadataPatch{
		"owner":    MetadataValue("alice"),
		"obsolete": MetadataDelete(),
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ResourceVersion != 4 {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestWaitViewerFailsFastOnPermissionDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{
			"error_code":"PERMISSION_DENIED",
			"message":"viewer forbidden",
			"request_id":"req-1",
			"details":{"retryable":false}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.WaitViewer(context.Background(), "sb-1", WaitViewerOptions{
		PollInterval: 10 * time.Millisecond,
		Timeout:      100 * time.Millisecond,
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestListAuditEventsAndSessionExecs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/audits":
			if r.URL.Query().Get("principal_id") != "p-1" || r.URL.Query().Get("limit") != "10" {
				t.Fatalf("audit query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{
				"items":[{"event_id":"evt-1","tenant_id":"tenant-a","action":"job.submit","resource_type":"job","status":"ok"}],
				"total":1
			}`))
		case "/v1/sessions/sess-1/execs":
			if r.URL.Query().Get("cursor") != "next-1" || r.URL.Query().Get("limit") != "5" {
				t.Fatalf("exec query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{
				"items":[{"exec_id":"exec-1","operation_id":"original","session_id":"sess-1","status":"queued","exit_code":0}],
				"next_cursor":"next-2",
				"total":1
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	audits, err := client.ListAuditEvents(context.Background(), AuditQuery{PrincipalID: "p-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if audits.Total != 1 || len(audits.Items) != 1 || audits.Items[0].EventID != "evt-1" {
		t.Fatalf("audits = %+v", audits)
	}

	execs, err := client.ListSessionExecs(context.Background(), "sess-1", ExecListQuery{Limit: 5, Cursor: "next-1"})
	if err != nil {
		t.Fatal(err)
	}
	if execs.Total != 1 || execs.NextCursor != "next-2" || len(execs.Items) != 1 || execs.Items[0].ExecID != "exec-1" {
		t.Fatalf("execs = %+v", execs)
	}
}

func TestNewClientEndpointSecurityConstraints(t *testing.T) {
	for _, url := range []string{
		"https://sandbox.example.com",
		"http://localhost:18010",
		"http://LOCALHOST:18010", // host comparison is case-insensitive
		"http://127.0.0.1:18010",
		"http://[::1]:18010",
	} {
		if _, err := NewClient(Config{BaseURL: url}); err != nil {
			t.Errorf("NewClient(%q) error = %v, want nil", url, err)
		}
	}
	for _, url := range []string{
		"http://sandbox.example.com", // non-https, non-loopback
		"http://10.0.0.8:18010",      // non-https private address
		"https://sandbox.example.com/api",
		"https://user:pass@sandbox.example.com",
		"https://sandbox.example.com?x=1",
		"https://sandbox.example.com#frag",
	} {
		if _, err := NewClient(Config{BaseURL: url}); err == nil {
			t.Errorf("NewClient(%q) error = nil, want rejection", url)
		}
	}
	for _, token := range []string{" leading", "trailing ", "bad\r\ntoken"} {
		if _, err := NewClient(Config{BaseURL: "https://sandbox.example.com", Token: token}); err == nil {
			t.Errorf("NewClient(token=%q) error = nil, want rejection", token)
		}
	}
}

func TestRedirectIsRefusedAndCredentialsNeverForwarded(t *testing.T) {
	var paths []string
	var authHeaders []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/v1/jobs/redirect-me" {
			w.Header().Set("Location", "/v1/leak")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job-1"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Token: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetJob(context.Background(), "redirect-me")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", apiErr.StatusCode)
	}
	// 重定向目标从未被请求：凭据不可能被转发到其他地址。
	if len(paths) != 1 || paths[0] != "/v1/jobs/redirect-me" {
		t.Fatalf("paths = %v, want single original request", paths)
	}
	if authHeaders[0] != "Bearer secret-token" {
		t.Fatalf("Authorization = %q", authHeaders[0])
	}
}

func TestWaitJobReturnsInterruptedAsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"job-1","status":"interrupted","error_code":"SERVICE_RESTART_INTERRUPTED"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.WaitJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", job.Status)
	}
}

func TestExecHandleWaitReturnsInterruptedAsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exec_id":"exec-1","session_id":"sess-1","status":"interrupted","error_code":"SERVICE_RESTART_INTERRUPTED"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	handle := &ExecHandle{client: client, sessionID: "sess-1", execID: "exec-1"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "SERVICE_RESTART_INTERRUPTED" {
		t.Fatalf("error_code = %q", result.ErrorCode)
	}
}

func TestJSONResponseBudgetIsEnforced(t *testing.T) {
	original := maxJSONResponseBytes
	maxJSONResponseBytes = 64
	defer func() { maxJSONResponseBytes = original }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"` + strings.Repeat("x", 200) + `"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetJob(context.Background(), "job-1")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want budget exceeded", err)
	}
}

func TestSessionLookupResumeAndExecLookup(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/sessions:lookup":
			if r.URL.Query().Get("idempotency_key") != "idem-1" {
				t.Errorf("lookup query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"session_id":"sess-1","workspace_id":"ws-1","idempotency_key":"idem-1","status":"active"}`))
		case r.Method == "POST" && r.URL.Path == "/v1/sessions/sess-1:resume":
			_, _ = w.Write([]byte(`{"session_id":"sess-1","status":"active","active_sandbox_id":"sbx-9"}`))
		case r.Method == "GET" && r.URL.Path == "/v1/sessions/sess-1/execs:lookup":
			if r.URL.Query().Get("operation_id") != "op-1" {
				t.Errorf("exec lookup query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"exec_id":"exec-1","operation_id":"op-1","session_id":"sess-1","status":"succeeded"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess, err := client.LookupSession(ctx, "idem-1")
	if err != nil || sess.SessionID != "sess-1" {
		t.Fatalf("LookupSession = %+v err=%v", sess, err)
	}
	resumed, err := client.ResumeSession(ctx, "sess-1")
	if err != nil || resumed.ActiveSandboxID != "sbx-9" {
		t.Fatalf("ResumeSession = %+v err=%v", resumed, err)
	}
	record, err := client.GetExecByOperation(ctx, "sess-1", "op-1")
	if err != nil || record.ExecID != "exec-1" || record.Status != "succeeded" {
		t.Fatalf("GetExecByOperation = %+v err=%v", record, err)
	}
	if len(paths) != 3 {
		t.Fatalf("unexpected call sequence: %v", paths)
	}
}

func TestExecSessionResultParsesStatusAndEffectiveEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exec_id":"exec-7","status":"succeeded","exit_code":0,"stdout":"ok","stderr":"",
			"error_code":"","effective_environment":{"profile_name":"code-polyglot-basic","selection_mode":"profile"},
			"session_id":"sess-1","workspace_id":"ws-1","environment":"sandbox"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ExecNamedSession(context.Background(), "sess-1", ExecSessionRequest{Code: "print(1)"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExecID != "exec-7" || result.Status != "succeeded" || result.Stdout != "ok" {
		t.Fatalf("ExecSessionResult = %+v", result)
	}
	if result.EffectiveEnvironment == nil || result.EffectiveEnvironment.ProfileName != "code-polyglot-basic" {
		t.Fatalf("effective environment not parsed: %+v", result.EffectiveEnvironment)
	}
}
