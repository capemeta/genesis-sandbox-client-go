package sandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeartbeatStopsOnUnknownRenewalAndPreservesOriginalIdentity(t *testing.T) {
	for _, failure := range []string{"lost-response", "unavailable", "bad-json", "foreign-receipt"} {
		t.Run(failure, func(t *testing.T) {
			var renews, creates, reconnects atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/sessions":
					creates.Add(1)
					io.WriteString(w, `{"session_id":"original","workspace_id":"workspace-original"}`)
				case "/v1/sessions/original:renew":
					renews.Add(1)
					switch failure {
					case "lost-response":
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						connection.Close()
					case "unavailable":
						w.WriteHeader(503)
						io.WriteString(w, `{"error_code":"RUNTIME_UNAVAILABLE","message":"unknown renewal","request_id":"req-original"}`)
					case "bad-json":
						io.WriteString(w, `{`)
					default:
						io.WriteString(w, `{"session_id":"foreign","workspace_id":"foreign-workspace"}`)
					}
				case "/v1/sessions/original":
					io.WriteString(w, `{"status":"deleted"}`)
				default:
					reconnects.Add(1)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			client, err := NewClient(Config{BaseURL: server.URL, Token: "test", MaxAttempts: 4, RetryBaseDelay: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			sb, err := New(client, WithHeartbeatInterval(5*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			if err := sb.Open(context.Background()); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for sb.HeartbeatError() == nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var stopped *HeartbeatRenewalError
			if !errors.As(sb.HeartbeatError(), &stopped) {
				t.Fatalf("missing exposed heartbeat error: %v", sb.HeartbeatError())
			}
			if stopped.SessionID != "original" || stopped.WorkspaceID != "workspace-original" || stopped.Cause == nil {
				t.Fatal(stopped)
			}
			if failure == "unavailable" {
				var api *APIError
				if !errors.As(stopped, &api) || api.StatusCode != 503 || api.ErrorCode != "RUNTIME_UNAVAILABLE" || api.RequestID != "req-original" {
					t.Fatal(stopped)
				}
			}
			if err := sb.Open(context.Background()); err != nil {
				t.Fatal(err)
			}
			time.Sleep(40 * time.Millisecond)
			if renews.Load() != 1 || creates.Load() != 1 || reconnects.Load() != 0 {
				t.Fatal(renews.Load(), creates.Load(), reconnects.Load())
			}
			if sb.SessionID() != "original" || sb.WorkspaceID() != "workspace-original" || !sb.IsOpen() {
				t.Fatal("original identity lost")
			}
			if err := sb.CloseContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if sb.HeartbeatError() != stopped {
				t.Fatal("close discarded the unknown renewal evidence")
			}
		})
	}
}

func TestConfirmedHeartbeatMayContinueUntilFirstFailure(t *testing.T) {
	var renews atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sessions/s:renew" && renews.Add(1) > 1 {
			w.WriteHeader(503)
			io.WriteString(w, `{"message":"unknown"}`)
			return
		}
		io.WriteString(w, `{"session_id":"s","workspace_id":"w"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sb, err := New(client, WithHeartbeatInterval(5*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for sb.HeartbeatError() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if sb.HeartbeatError() == nil || renews.Load() != 2 {
		t.Fatal(sb.HeartbeatError(), renews.Load())
	}
	if err := sb.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
