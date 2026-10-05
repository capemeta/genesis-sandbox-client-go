package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func controlClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(Config{BaseURL: server.URL, Token: "owner-token", MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSharedBindingStrictDTO(t *testing.T) {
	for _, input := range []string{
		`{"mode":"shared","storage_ref":"../root","resource_id":"r","binding_version":1}`,
		`{"mode":"shared","storage_ref":"s","resource_id":"r","binding_version":0}`,
		`{"mode":"shared","storage_ref":"s","resource_id":"r","binding_version":1,"owner_run_id":"forged"}`,
		`{"mode":"isolated","host_path":"/tmp"}`,
		`{"mode":"isolated","resource_id":"r"}`,
		`{"mode":"unknown"}`,
	} {
		t.Run(input, func(t *testing.T) {
			var binding WorkspaceBindingRequest
			if json.Unmarshal([]byte(input), &binding) == nil {
				t.Fatal("accepted unsafe binding")
			}
		})
	}
}

func TestSharedCreateBindingReachesWire(t *testing.T) {
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workspaces" || r.Header.Get("Authorization") != "Bearer owner-token" {
			t.Fatal(r.URL)
		}
		var request CreateWorkspaceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.WorkspaceBinding == nil || request.WorkspaceBinding.Mode != "shared" || request.WorkspaceBinding.StorageRef != "approved" || request.WorkspaceBinding.BindingVersion != 7 || request.RetentionMode != "explicit_delete" {
			t.Fatalf("lost binding: %+v", request)
		}
		io.WriteString(w, `{"workspace_id":"resource"}`)
	})
	_, err := client.CreateWorkspace(context.Background(), CreateWorkspaceRequest{WorkspaceID: "resource", RetentionMode: "explicit_delete", WorkspaceBinding: &WorkspaceBindingRequest{Mode: "shared", StorageRef: "approved", ResourceID: "resource", BindingVersion: 7}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateWorkspace(context.Background(), CreateWorkspaceRequest{WorkspaceID: "other", RetentionMode: "ttl", WorkspaceBinding: &WorkspaceBindingRequest{Mode: "shared", StorageRef: "approved", ResourceID: "resource", BindingVersion: 7}})
	if err == nil {
		t.Fatal("accepted mismatched workspace")
	}
}

func storageEvidence() string {
	return `{"storage_ref":"approved","resource_id":"resource","workspace_id":"resource","source_identity":"` + strings.Repeat("a", 64) + `","provisioner_id":"native","owner_tenant_id":"t","owner_user_id":"u","owner_run_id":"run","binding_version":7,"budget_bytes":33554432,"quota_mb":32,"persistent":true,"hard_quota":true,"hard_quota_verified":true,"shared_attachment":false}`
}

func TestStorageInspectionReportsClosedGateWithoutGrantingAttachment(t *testing.T) {
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/storage-resources/approved/resource" {
			t.Fatal(r.URL)
		}
		io.WriteString(w, storageEvidence())
	})
	resource, err := client.InspectSharedStorage(context.Background(), "approved", "resource")
	if err != nil || resource.SharedAttachment || !resource.HardQuotaVerified {
		t.Fatalf("resource=%+v err=%v", resource, err)
	}
}

func TestStorageEvidenceRejectsMissingAndHostPathFields(t *testing.T) {
	for _, body := range []string{strings.Replace(storageEvidence(), `,"hard_quota_verified":true`, "", 1), strings.Replace(storageEvidence(), `"shared_attachment":false`, `"shared_attachment":false,"host_path":"/secret"`, 1), strings.Replace(storageEvidence(), `"budget_bytes":33554432`, `"budget_bytes":-1`, 1)} {
		var result SharedStorageResource
		if json.Unmarshal([]byte(body), &result) == nil {
			t.Fatal("accepted incomplete storage evidence")
		}
	}
}

func TestWorkspaceLifecycleAndLookupWire(t *testing.T) {
	seen := map[string]int{}
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method+" "+r.URL.RequestURI()]++
		switch {
		case strings.HasSuffix(r.URL.Path, "workspace-view"):
			if r.URL.Query().Get("action") == "prepare" {
				io.WriteString(w, preparationUnknown())
				break
			}
			io.WriteString(w, `{"os":"linux","arch":"amd64","image_digest":"digest","mechanisms":["container_boundary"],"resource_limits":{"workspace_disk_bytes":33554432},"network_mode":"disabled","view_state":"sealed","runtime_versions":{},"runtime_executables":{}}`)
		case strings.HasSuffix(r.URL.Path, "workspace:purge"):
			io.WriteString(w, `{"operation_id":"purge:1","request_digest":"`+strings.Repeat("a", 64)+`","status":"purged","session_id":"s","workspace_id":"resource","profile_revision":"revision","tenant_id":"t","principal_id":"p","user_id":"u"}`)
		case strings.HasSuffix(r.URL.Path, "execs:lookup"):
			io.WriteString(w, `{"exec_id":"e","operation_id":"op+1","session_id":"s","status":"succeeded"}`)
		case r.URL.Path == "/v1/sessions:lookup":
			io.WriteString(w, `{"session_id":"s","workspace_id":"resource","idempotency_key":"key+1"}`)
		default:
			io.WriteString(w, `{"session_id":"s","workspace_id":"resource"}`)
		}
	})
	ctx := context.Background()
	if _, err := client.GetWorkspaceView(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PrepareWorkspaceView(ctx, "s", WorkspacePreparationRequest{OperationID: "prepare:1", RequestDigest: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if facts, err := client.SealWorkspaceView(ctx, "s"); err != nil || facts.OS != "linux" {
		t.Fatal(facts, err)
	}
	if _, err := client.GetSessionHistory(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PurgeSessionWorkspace(ctx, "s", WorkspacePurgeRequest{OperationID: "purge:1", RequestDigest: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LookupSession(ctx, "key+1"); err != nil {
		t.Fatal(err)
	}
	if record, err := client.GetExecByOperation(ctx, "s", "op+1"); err != nil || record.OperationID != "op+1" {
		t.Fatal(record, err)
	}
	if _, err := client.ResumeSession(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GET /v1/sessions/s/workspace-view", "POST /v1/sessions/s/workspace-view?action=prepare", "POST /v1/sessions/s/workspace-view?action=seal", "GET /v1/sessions/s/history", "POST /v1/sessions/s/workspace:purge", "GET /v1/sessions:lookup?idempotency_key=key%2B1", "GET /v1/sessions/s/execs:lookup?operation_id=op%2B1", "POST /v1/sessions/s:resume"} {
		if seen[key] != 1 {
			t.Fatalf("missing request %s: %v", key, seen)
		}
	}
}

func TestExecutableFalseCASAndUnknownMutationDoesNotRetry(t *testing.T) {
	calls := 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PATCH" || r.URL.Query().Get("path") != "work/a b.sh" || r.Header.Get("If-Match") != `"sha"` {
			t.Fatal(r)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"executable":false,"expected_executable":true}` {
			t.Fatal(string(body))
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error_code":"UNKNOWN","message":"unknown"}`)
	})
	expected := true
	if _, err := client.SetSessionFileExecutable(context.Background(), "s", "work/a b.sh", `"sha"`, SetExecutableRequest{Executable: false, ExpectedExecutable: &expected}); err == nil {
		t.Fatal("missing unknown result")
	}
	if calls != 1 {
		t.Fatalf("unsafe replay: %d", calls)
	}
}

func TestExecutableResponseAndResolveFactsMapping(t *testing.T) {
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			io.WriteString(w, `{"path":"work/a.sh","kind":"file","executable":true}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"include_facts":true`) {
			t.Fatal(string(body))
		}
		io.WriteString(w, `{"facts":{"os":"linux","arch":"amd64","mechanisms":["container_boundary"],"image_digest":"digest","resource_limits":{"private_disk_bytes":100},"runtime_executables":{},"runtime_versions":{},"network_mode":"disabled","view_state":"sealed"}}`)
	})
	file, err := client.SetSessionFileExecutable(context.Background(), "s", "work/a.sh", "", SetExecutableRequest{Executable: true})
	if err != nil || !file.Executable {
		t.Fatal(file, err)
	}
	resolution, err := client.ResolveEnvironment(context.Background(), ResolveEnvironmentRequest{IncludeFacts: true, Environment: EnvironmentSelector{Profile: &ProfileRef{Name: "python"}}})
	if err != nil || resolution.Facts == nil || resolution.Facts.ResourceLimits["private_disk_bytes"] != 100 {
		t.Fatal(resolution, err)
	}
}

func TestExecutionOperationAndEffectiveEnvironmentMapping(t *testing.T) {
	request := ExecSessionRequest{OperationID: "stable"}
	for _, dto := range []any{toGenExecSessionRequest(request), toGenAsyncExecRequest(request)} {
		data, err := json.Marshal(dto)
		if err != nil || !strings.Contains(string(data), `"operation_id":"stable"`) {
			t.Fatal(string(data), err)
		}
	}
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"succeeded","exit_code":0,"stdout":"ok","stderr":"","effective_environment":{"profile_name":"python","profile_revision":"r7","degraded":false}}`)
	})
	result, err := client.ExecNamedSession(context.Background(), "s", request)
	if err != nil || result.Status != "succeeded" || result.EffectiveEnvironment == nil || result.EffectiveEnvironment.ProfileRevision != "r7" {
		t.Fatal(result, err)
	}
}

func TestFacadeDefaultEnvironmentAppliesToSyncAndAsyncWithoutContextPatch(t *testing.T) {
	calls := 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions":
			io.WriteString(w, `{"session_id":"s","workspace_id":"w"}`)
		case "/v1/sessions/s/exec", "/v1/sessions/s/exec:async":
			calls++
			var request ExecSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Env["DEFAULT"] != "original" || request.Env["OVERRIDE"] != "call" {
				t.Fatal(request.Env)
			}
			io.WriteString(w, `{"exec_id":"e","session_id":"s","status":"succeeded","exit_code":0,"stdout":"","stderr":""}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	environment := map[string]string{"DEFAULT": "original", "OVERRIDE": "default"}
	sandbox, err := New(client, WithEnv(environment), WithHeartbeatDisabled())
	if err != nil {
		t.Fatal(err)
	}
	if err := sandbox.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	environment["DEFAULT"] = "mutated"
	if _, err := sandbox.Run(context.Background(), "true", WithExecEnv(map[string]string{"OVERRIDE": "call"})); err != nil {
		t.Fatal(err)
	}
	if _, err := sandbox.RunAsync(context.Background(), "true", WithExecEnv(map[string]string{"OVERRIDE": "call"})); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestOnlyReadOnlyVerbsReplayTransientResponses(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, transport := range []string{"json", "raw"} {
			t.Run(method+"/"+transport, func(t *testing.T) {
				calls := 0
				client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.WriteHeader(http.StatusServiceUnavailable)
					io.WriteString(w, `{"error_code":"UNKNOWN","message":"ambiguous effect"}`)
				})
				client.cfg.RetryBaseDelay = time.Millisecond
				var err error
				if transport == "json" {
					err = client.request(context.Background(), method, "/v1/test", nil, nil)
				} else {
					_, err = client.rawRequestWithHeaders(context.Background(), method, "/v1/test", "", nil, nil)
				}
				if err == nil {
					t.Fatal("expected transient error")
				}
				want := 1
				if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
					want = 3
				}
				if calls != want {
					t.Fatalf("calls=%d want=%d", calls, want)
				}
			})
		}
	}
}

func TestWorkspaceStatAndRemovePreserveMissingPath404(t *testing.T) {
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error_code":"WORKSPACE_PATH_NOT_FOUND","message":"workspace path not found"}`)
	})
	_, statErr := client.StatSessionFile(context.Background(), "s", "work/missing")
	removeErr := client.RemoveSessionFile(context.Background(), "s", "work/missing", false)
	for _, err := range []error{statErr, removeErr} {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.ErrorCode != "WORKSPACE_PATH_NOT_FOUND" {
			t.Fatalf("lost missing path error: %v", err)
		}
	}
}
