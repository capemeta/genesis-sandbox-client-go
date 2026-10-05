package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func originalSDKGovernance() *TrustedExecutionGovernance {
	g := &TrustedExecutionGovernance{WorkspaceId: "workspace", PlatformOperationId: "platform-operation", SourceIdentity: strings.Repeat("a", 64), RequestDigest: strings.Repeat("b", 64), AuthorizedWorkWrite: true}
	g.Context.TenantId, g.Context.UserId, g.Context.RunId, g.Context.TraceId, g.Context.AuthorizationScope, g.Context.SubjectKind = "tenant", "user", "run", "trace", "execution:write", "enterprise"
	g.Context.InvocationId, g.Context.ExecutionId, g.Context.AttemptId = "invocation", "execution", "attempt"
	g.LeaseRef = PlatformLeaseRef{ProviderId: "provider", LeaseId: "lease", Generation: 1, ExpiresAt: time.Now().UTC(), Workspace: PlatformWorkspaceRef{WorkspaceKey: "workspace", ProviderId: "provider", ResourceId: "workspace", Scope: "execution:write", Generation: 1}}
	return g
}

func TestPublicGovernanceExecTransmitsOnceAndRejectsUnenforcedWork(t *testing.T) {
	sent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"trusted_governance"`) || !strings.Contains(string(body), `"platform-operation"`) {
			t.Error("original governance not transmitted", string(body))
		}
		_, _ = w.Write([]byte(`{"exec_id":"original","session_id":"session","operation_id":"operation","status":"queued","stop_confirmed":false}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	g := originalSDKGovernance()
	if _, err := client.ExecSessionAsync(context.Background(), "session", ExecSessionRequest{OperationID: "operation", TrustedGovernance: g, Command: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	g.AuthorizedWorkWrite = false
	if _, err := client.ExecSessionAsync(context.Background(), "session", ExecSessionRequest{OperationID: "operation-2", TrustedGovernance: g}); err == nil {
		t.Fatal("unenforced read-only work submitted")
	}
	g.AuthorizedWorkWrite = true
	g.LeaseRef.Workspace.Scope = "foreign-scope"
	if _, err := client.ExecSessionAsync(context.Background(), "session", ExecSessionRequest{OperationID: "operation-scope", TrustedGovernance: g}); err == nil {
		t.Fatal("foreign lease scope submitted")
	}
	g.LeaseRef.Workspace.Scope = g.Context.AuthorizationScope
	g.Context.TraceId = "trace\u2028separator"
	if _, err := client.ExecSessionAsync(context.Background(), "session", ExecSessionRequest{OperationID: "operation-3", TrustedGovernance: g}); err == nil {
		t.Fatal("ambiguous Unicode identity submitted")
	}
	if sent != 1 {
		t.Fatal("governed request was replayed", sent)
	}
}

func TestPublicMaintenanceQueryAllowsEmptyCallsAndChecksScope(t *testing.T) {
	query := MaintenanceProofQuery{Context: PlatformCallContext{TenantId: "tenant", UserId: "user", RunId: "run", TraceId: "trace", AuthorizationScope: "workspace:write", SubjectKind: "enterprise"}, OperationId: "maintenance", WorkspaceId: "workspace", SourceIdentity: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(map[string]any{"context": query.Context, "operation_id": query.OperationId, "workspace_id": query.WorkspaceId, "source_identity": query.SourceIdentity})
	var tree any
	_ = json.Unmarshal(raw, &tree)
	raw, _ = json.Marshal(tree)
	sum := sha256.Sum256(raw)
	query.ClaimDigest = hex.EncodeToString(sum[:])
	sent := 0
	foreignRoot := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		if r.Method != "POST" || r.URL.Path != "/v1/workspaces/workspace/maintenance-proof:query" {
			t.Error("unexpected original query route", r.URL)
		}
		proof := MaintenanceSourceProof{Context: query.Context, OperationId: query.OperationId, ClaimDigest: query.ClaimDigest, SourceIdentity: query.SourceIdentity, RootDevice: 1, RootInode: 2, Calls: []MaintenanceCall{}, Leaves: []MaintenanceLeaf{{Path: "work", Device: 1, Inode: 3, Uid: 0, Gid: 65532, Directory: true, Mode: 0770}, {Path: "output", Device: 1, Inode: 4, Uid: 0, Gid: 65532, Directory: true, Mode: 01770}}}
		if foreignRoot {
			proof.Leaves = append(proof.Leaves, MaintenanceLeaf{Path: "output/unregistered", Device: 1, Inode: 5, Uid: 0, Gid: 100001, Directory: true, Mode: 0770})
		}
		_ = json.NewEncoder(w).Encode(proof)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := client.QueryWorkspaceMaintenanceProof(context.Background(), "workspace", query)
	if err != nil || proof == nil || len(proof.Calls) != 0 || sent != 1 {
		t.Fatal("file-only original proof rejected", proof, err, sent)
	}
	foreignRoot = true
	if _, err := client.QueryWorkspaceMaintenanceProof(context.Background(), "workspace", query); err == nil {
		t.Fatal("unregistered private output root accepted")
	}
	query.Context.TraceId = "trace\u2029separator"
	if _, err := client.QueryWorkspaceMaintenanceProof(context.Background(), "workspace", query); err == nil || sent != 2 {
		t.Fatal("ambiguous query identity sent")
	}
}

func TestMaintenanceOriginalCallRequiresLeaseBindingAndOutputRoot(t *testing.T) {
	query := MaintenanceProofQuery{Context: PlatformCallContext{TenantId: "tenant", UserId: "user", RunId: "run", TraceId: "trace", AuthorizationScope: "workspace:write", SubjectKind: "enterprise"}, OperationId: "maintenance", WorkspaceId: "workspace", SourceIdentity: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(map[string]any{"context": query.Context, "operation_id": query.OperationId, "workspace_id": query.WorkspaceId, "source_identity": query.SourceIdentity})
	var tree any
	_ = json.Unmarshal(raw, &tree)
	raw, _ = json.Marshal(tree)
	sum := sha256.Sum256(raw)
	query.ClaimDigest = hex.EncodeToString(sum[:])
	damage := ""
	sent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		invocation, execution, attempt, callID := "invocation", "execution", "attempt", "original-call"
		callContext := query.Context
		callContext.AuthorizationScope = "execution:write"
		callContext.InvocationId, callContext.ExecutionId, callContext.AttemptId = &invocation, &execution, &attempt
		call := MaintenanceCall{CallId: callID, Context: callContext, LeaseRef: originalSDKGovernance().LeaseRef,
			OperationId: "original-operation", RequestDigest: strings.Repeat("b", 64), ExecutionId: execution,
			OutputDirectory: "output/original-call", Uid: 100001, Gid: 100001, RootDevice: 1, RootInode: 5,
			AuthorizedWorkWrite: true, StopConfirmed: true}
		proof := MaintenanceSourceProof{Context: query.Context, OperationId: query.OperationId,
			ClaimDigest: query.ClaimDigest, SourceIdentity: query.SourceIdentity, RootDevice: 1, RootInode: 2,
			Calls: []MaintenanceCall{call}, Leaves: []MaintenanceLeaf{
				{Path: "work", Device: 1, Inode: 3, Uid: 0, Gid: 65532, Directory: true, Mode: 0770},
				{Path: "output", Device: 1, Inode: 4, Uid: 0, Gid: 65532, Directory: true, Mode: 01770},
				{Path: call.OutputDirectory, Device: 1, Inode: 5, Uid: 0, Gid: call.Gid, Directory: true, Mode: 0770, CallId: &callID}}}
		switch damage {
		case "missing-root":
			proof.Leaves = proof.Leaves[:2]
		case "wrong-scope":
			proof.Calls[0].LeaseRef.Workspace.Scope = "foreign-scope"
		case "missing-lease":
			proof.Calls[0].LeaseRef = PlatformLeaseRef{}
		}
		_ = json.NewEncoder(w).Encode(proof)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.QueryWorkspaceMaintenanceProof(context.Background(), "workspace", query); err != nil {
		t.Fatal("complete original proof rejected", err)
	}
	for _, damage = range []string{"missing-root", "wrong-scope", "missing-lease"} {
		if _, err := client.QueryWorkspaceMaintenanceProof(context.Background(), "workspace", query); err == nil {
			t.Fatal("incomplete original proof accepted", damage)
		}
	}
	if sent != 4 {
		t.Fatal("original query replayed", sent)
	}
}
