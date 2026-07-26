package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenAndCloseCanBeRetriedAfterFailure(t *testing.T) {
	var creates atomic.Int32
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			if creates.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error_code":"UNAVAILABLE","message":"retry open","details":{"retryable":true}}`))
				return
			}
			_, _ = w.Write([]byte(`{"session_id":"sess-1","workspace_id":"ws-1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sessions/sess-1":
			if deletes.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error_code":"UNAVAILABLE","message":"retry close","details":{"retryable":true}}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	sb, err := New(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.Open(context.Background()); err == nil {
		t.Fatal("first Open() error = nil")
	}
	if err := sb.Open(context.Background()); err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if err := sb.Close(); err == nil {
		t.Fatal("first Close() error = nil")
	}
	if err := sb.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
