package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWorkspaceLifecycleCanonicalVectors(t *testing.T) {
	digest, err := WorkspaceLifecycleRequestDigest(WorkspaceLifecycleRequest{OperationID: "original", State: "active"})
	if err != nil || digest != "600867eb32528c3e0e110f9de2ec17e7e8c96b73428a54545934953a05b49442" {
		t.Fatal(digest, err)
	}
	terminal, _ := time.Parse(time.RFC3339Nano, "2026-10-01T08:30:12.123456+08:00")
	digest, err = WorkspaceLifecycleRequestDigest(WorkspaceLifecycleRequest{OperationID: "terminal", ExpectedRevision: 7, State: "terminal", TerminalAt: &terminal, RetentionSeconds: 86400})
	if err != nil || digest != "abb14a40cc3f77bc822599e2a4b089677e01c3b66ff178513f3e71e84adba74a" {
		t.Fatal(digest, err)
	}
}
func TestWorkspaceLifecycleSingleMutationAndOriginalLookup(t *testing.T) {
	posts, gets := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			w.WriteHeader(503)
			return
		}
		gets++
		json.NewEncoder(w).Encode(map[string]any{"state": "unknown", "receipt": nil})
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, MaxAttempts: 3, RetryBaseDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.ControlWorkspaceLifecycle(context.Background(), "w", WorkspaceLifecycleRequest{OperationID: "original", State: "active"}); err == nil {
		t.Fatal("missing failure")
	}
	result, err := client.LookupWorkspaceLifecycle(context.Background(), "w", "original")
	if err != nil || result.State != "unknown" || posts != 1 || gets != 1 {
		t.Fatal(result, err, posts, gets)
	}
}

func TestWorkspaceLifecycleRejectsForgedTerminalReceipt(t *testing.T) {
	terminal := time.Date(2026, 10, 1, 0, 0, 0, 123456000, time.UTC)
	request := WorkspaceLifecycleRequest{OperationID: "terminal", State: "terminal", TerminalAt: &terminal, RetentionSeconds: 86400}
	digest, err := WorkspaceLifecycleRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"terminal", "deadline", "precision"} {
		t.Run(field, func(t *testing.T) {
			end, finish := terminal.Add(24*time.Hour), terminal
			if field == "terminal" {
				finish = finish.Add(time.Second)
			}
			if field == "deadline" {
				end = end.Add(time.Second)
			}
			if field == "precision" {
				end = end.Add(time.Nanosecond)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(WorkspaceLifecycleReceipt{OperationID: "terminal", WorkspaceID: "w", Revision: 1, State: "terminal", TerminalAt: &finish, Deadline: end, RequestDigest: digest})
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ControlWorkspaceLifecycle(context.Background(), "w", request); err == nil {
				t.Fatal("forged receipt accepted")
			}
		})
	}
}
