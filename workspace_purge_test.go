package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOriginalPurgeReadOnlyMissingAndUnknown(t *testing.T) {
	writes, reads := 0, 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads++
			w.WriteHeader(404)
			io.WriteString(w, `{"error_code":"NOT_FOUND","message":"missing"}`)
			return
		}
		writes++
		var request WorkspacePurgeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.OperationID != "original" || request.RequestDigest != strings.Repeat("a", 64) {
			t.Fatal("lost identity")
		}
		io.WriteString(w, `{"operation_id":"original","request_digest":"`+request.RequestDigest+`","status":"unknown","session_id":"s","workspace_id":"w","profile_revision":"rev","tenant_id":"t","principal_id":"p","user_id":"u"}`)
	})
	request := WorkspacePurgeRequest{OperationID: "original", RequestDigest: strings.Repeat("a", 64)}
	if result, err := client.LookupWorkspacePurge(context.Background(), "s", request); err != nil || result != nil {
		t.Fatalf("missing=%+v %v", result, err)
	}
	if result, err := client.PurgeSessionWorkspace(context.Background(), "s", request); err != nil || result.Status != "unknown" {
		t.Fatalf("unknown=%+v %v", result, err)
	}
	if writes != 1 || reads != 1 {
		t.Fatalf("requests=%d/%d", writes, reads)
	}
}

func TestOriginalPurgeStrictReceipt(t *testing.T) {
	for _, body := range []string{`{"purged":true}`, `{"status":"unknown"}`, `{"status":"unknown","status":"purged"}`} {
		var receipt WorkspacePurgeReceipt
		if json.Unmarshal([]byte(body), &receipt) == nil {
			t.Fatal("accepted incomplete or duplicate proof")
		}
	}
}

func TestCleanupOwnershipAndSourceProofAreReadOnly(t *testing.T) {
	reads := 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("cleanup query mutated state")
		}
		reads++
		if r.URL.Path == "/v1/sessions/s/ownership" {
			io.WriteString(w, `{"session_id":"s","workspace_id":"w","profile_revision":"rev","tenant_id":"t","principal_id":"p","user_id":"u"}`)
			return
		}
		if r.URL.Path != "/v1/workspaces/w/purges/original" {
			t.Fatal("source query tied to compute session")
		}
		io.WriteString(w, `{"operation_id":"original","request_digest":"`+strings.Repeat("a", 64)+`","status":"purged","session_id":"expired","workspace_id":"w","profile_revision":"rev","tenant_id":"t","principal_id":"p","user_id":"u"}`)
	})
	if _, err := client.GetSessionOwnership(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LookupStoragePurge(context.Background(), "w", WorkspacePurgeRequest{OperationID: "original", RequestDigest: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatal("unexpected query count")
	}
	var owner SessionOwnership
	if json.Unmarshal([]byte(`{"session_id":"s","workspace_id":"w","profile_revision":"rev","tenant_id":"t","principal_id":"p","user_id":"u","stdout":"not granted"}`), &owner) == nil {
		t.Fatal("ownership expanded to logs")
	}
}
