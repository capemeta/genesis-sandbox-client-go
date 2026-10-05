package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGenAdapterResolveEnvironmentRequest(t *testing.T) {
	req := ResolveEnvironmentRequest{
		Environment: EnvironmentSelector{
			Profile: &ProfileRef{Name: "office-basic", Revision: "r1"},
		},
		TTLSeconds: 120,
	}

	body, err := json.Marshal(toGenResolveEnvironmentRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, `"environment":{"profile":{"name":"office-basic","revision":"r1"}}`) {
		t.Fatalf("body = %s", got)
	}
	if !strings.Contains(got, `"ttl_seconds":120`) {
		t.Fatalf("body = %s", got)
	}
}

func TestGenAdapterSubmitJobRequest(t *testing.T) {
	req := SubmitJobRequest{
		Environment:             &EnvironmentSelector{Hints: &EnvHints{Capabilities: []string{"runtime.python"}, Strict: true}},
		ResolutionID:            "resolution-1",
		Code:                    "print(1)",
		Language:                "python",
		TimeoutSeconds:          30,
		QueueWaitTimeoutSeconds: 5,
		WorkspaceID:             "ws-1",
		SessionID:               "sess-1",
		IdempotencyKey:          "idem-1",
		CallbackURL:             "https://hooks.example.com/job",
		InputArtifactIDs:        []string{"art-1"},
		Env:                     map[string]string{"FOO": "bar"},
		Metadata:                map[string]string{"trace": "abc"},
	}

	body, err := json.Marshal(toGenSubmitJobRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{
		`"resolution_id":"resolution-1"`,
		`"code":"print(1)"`,
		`"language":"python"`,
		`"timeout_seconds":30`,
		`"queue_wait_timeout_seconds":5`,
		`"workspace_id":"ws-1"`,
		`"session_id":"sess-1"`,
		`"idempotency_key":"idem-1"`,
		`"callback_url":"https://hooks.example.com/job"`,
		`"input_artifact_ids":["art-1"]`,
		`"env":{"FOO":"bar"}`,
		`"metadata":{"trace":"abc"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body = %s, want substring %s", got, want)
		}
	}
}

func TestGenAdapterCreateSessionRequestOmitsConvenienceEnv(t *testing.T) {
	req := CreateSessionRequest{
		Environment:         &EnvironmentSelector{Profile: &ProfileRef{Name: "python"}},
		WorkspaceID:         "ws-1",
		StatePolicy:         "session",
		TTLSeconds:          300,
		WorkspaceRetention:  "ttl",
		WorkspaceTTLSeconds: 600,
		IdempotencyKey:      "idem-1",
		Metadata:            map[string]string{"trace": "abc"},
	}

	body, err := json.Marshal(toGenCreateSessionRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if strings.Contains(got, `"env"`) {
		t.Fatalf("body leaked convenience env field: %s", got)
	}
	for _, want := range []string{
		`"workspace_id":"ws-1"`,
		`"state_policy":"session"`,
		`"ttl_seconds":300`,
		`"workspace_retention":"ttl"`,
		`"workspace_ttl_seconds":600`,
		`"idempotency_key":"idem-1"`,
		`"metadata":{"trace":"abc"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body = %s, want substring %s", got, want)
		}
	}
}

func TestGenAdapterSyncExecOmitsCallbackURL(t *testing.T) {
	req := ExecSessionRequest{
		Command:        []string{"echo", "ok"},
		Language:       "python",
		WorkingDir:     "/workspace",
		Env:            map[string]string{"FOO": "bar"},
		TimeoutSeconds: 15,
		CallbackURL:    "https://hooks.example.com/async-only",
	}

	body, err := json.Marshal(toGenExecSessionRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if strings.Contains(got, `"callback_url"`) {
		t.Fatalf("sync body leaked callback_url: %s", got)
	}
}

func TestGenAdapterAsyncExecIncludesCallbackURL(t *testing.T) {
	req := ExecSessionRequest{
		Command:        []string{"echo", "ok"},
		Language:       "python",
		WorkingDir:     "/workspace",
		Env:            map[string]string{"FOO": "bar"},
		TimeoutSeconds: 15,
		CallbackURL:    "https://hooks.example.com/async-only",
	}

	body, err := json.Marshal(toGenAsyncExecRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, `"callback_url":"https://hooks.example.com/async-only"`) {
		t.Fatalf("async body = %s", got)
	}
}

func TestDecodeAPIErrorPreservesStructuredDetails(t *testing.T) {
	resp := httptest.NewRecorder()
	resp.Header().Set("Content-Type", "application/json")
	resp.Header().Set("Retry-After", "3")
	resp.WriteHeader(http.StatusTooManyRequests)
	_, _ = resp.WriteString(`{
		"error_code":"QUEUE_FULL",
		"message":"queue is full",
		"request_id":"req-123",
		"details":{"retryable":true,"resource_type":"queue","retry_after_seconds":3,"hint":"slow down"}
	}`)

	err := decodeAPIError(resp.Result())
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %T, want *APIError", err)
	}
	if apiErr.ErrorCode != "QUEUE_FULL" || apiErr.Message != "queue is full" || apiErr.RequestID != "req-123" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if apiErr.RetryAfter != 3*time.Second {
		t.Fatalf("RetryAfter = %v", apiErr.RetryAfter)
	}
	if retryable, _ := apiErr.Details["retryable"].(bool); !retryable {
		t.Fatalf("details = %+v", apiErr.Details)
	}
	if apiErr.Details["resource_type"] != "queue" || apiErr.Details["hint"] != "slow down" {
		t.Fatalf("details = %+v", apiErr.Details)
	}
}
