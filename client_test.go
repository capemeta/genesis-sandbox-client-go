package sandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	client, err := NewClient(Config{BaseURL: "http://sandbox.invalid"})
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
	client, err := NewClient(Config{BaseURL: "http://sandbox.invalid"})
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

func TestPostWithIdempotencyKeyCanRetry(t *testing.T) {
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
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestCreateSessionEnvIsAppliedViaContextPatch(t *testing.T) {
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
		Env:         map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-1" {
		t.Fatalf("session_id = %q", session.SessionID)
	}
	if createCalls.Load() != 1 || patchCalls.Load() != 1 {
		t.Fatalf("createCalls=%d patchCalls=%d", createCalls.Load(), patchCalls.Load())
	}
}

func TestCreateSessionEnvPatchFailureCleansUpSession(t *testing.T) {
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
		Env:         map[string]string{"FOO": "bar"},
	})
	if err == nil {
		t.Fatal("CreateSession() error = nil, want env patch failure")
	}
	if deleteCalls.Load() != 1 {
		t.Fatalf("deleteCalls=%d, want 1", deleteCalls.Load())
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
				"items":[{"exec_id":"exec-1","session_id":"sess-1","status":"queued","exit_code":0}],
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
