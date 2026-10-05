package sandbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCancelPreservesPhysicalStopAndOriginalIdentity(t *testing.T) {
	for _, proof := range []string{"false", "true"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/session/execs/original:cancel" {
				t.Errorf("unexpected cancel route %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"exec_id":"original","operation_id":"op","session_id":"session","status":"cancelled","stop_confirmed":` + proof + `}`))
		}))
		client, _ := NewClient(Config{BaseURL: server.URL, MaxAttempts: 5})
		record, err := client.CancelExec(context.Background(), "session", "original")
		server.Close()
		if err != nil || record.StopConfirmed != (proof == "true") {
			t.Fatalf("explicit stop proof lost: %+v %v", record, err)
		}
	}
	if err := validateExecReceipt(&ExecRecord{ExecID: "foreign", SessionID: "session", OperationID: "op", Status: "cancelled", StopConfirmed: true}, "session", "original", ""); err == nil {
		t.Fatal("foreign cancelled identity accepted")
	}
}

func TestExecHistoryAndJSONFailClosed(t *testing.T) {
	for _, body := range []string{`{"stop_confirmed":false,"stop_confirmed":true}`, `{"nested":{"id":"one","id":"two"}}`, `{"duration_ms":1.5}`} {
		var record ExecRecord
		if err := decodeJSONLimited(strings.NewReader(body), &record); err == nil {
			t.Fatal("unsafe DTO accepted", body)
		}
	}
	for _, limit := range []int{-1, 101} {
		if validateExecPageRequest("session", ExecListQuery{Limit: limit}) == nil {
			t.Fatal("invalid page limit accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error_code":"WORKSPACE_PATH_NOT_FOUND","message":"missing original"}`))
	}))
	defer server.Close()
	client, _ := NewClient(Config{BaseURL: server.URL})
	err := client.DeleteSession(context.Background(), "original")
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 404 || api.ErrorCode != "WORKSPACE_PATH_NOT_FOUND" {
		t.Fatalf("missing original delete was hidden: %v", err)
	}
}
