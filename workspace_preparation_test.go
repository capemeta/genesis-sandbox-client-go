package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func preparationUnknown() string {
	return `{"operation_id":"prepare:1","request_digest":"` + strings.Repeat("a", 64) + `","status":"unknown","session_id":"s","workspace_id":"ws","sandbox_id":"","profile_revision":"rev","tenant_id":"tenant","principal_id":"principal","user_id":"user"}`
}

func TestPreparedProofRetainsOriginalFacts(t *testing.T) {
	body := strings.Replace(preparationUnknown(), `"status":"unknown"`, `"status":"prepared"`, 1)
	body = strings.Replace(body, `"sandbox_id":""`, `"sandbox_id":"container-original"`, 1)
	body = strings.TrimSuffix(body, "}") + `,"facts":{"session_id":"s","workspace_id":"ws","profile_revision":"rev","os":"linux","arch":"amd64","image_digest":"sha256:fixed","mechanisms":[],"resource_limits":{"memory_bytes":1},"network_mode":"none","view_state":"prepared","runtime_versions":{},"runtime_executables":{}}}`
	request := WorkspacePreparationRequest{OperationID: "prepare:1", RequestDigest: strings.Repeat("a", 64)}
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
	if result, err := client.LookupWorkspacePreparation(context.Background(), "s", request); err != nil || result.Status != "prepared" || result.Facts == nil {
		t.Fatal(result, err)
	}
	for _, field := range []string{`"network_mode":"none",`, `"mechanisms":[],`} {
		var result WorkspacePreparationReceipt
		if json.Unmarshal([]byte(strings.Replace(body, field, "", 1)), &result) == nil {
			t.Fatal("missing required prepared fact accepted")
		}
	}
}

func TestOriginalPreparationWireAndReadonly404(t *testing.T) {
	calls := 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"operation_id":"prepare:1","request_digest":"`+strings.Repeat("a", 64)+`"}` {
				t.Fatal(string(body))
			}
			io.WriteString(w, preparationUnknown())
		} else {
			if r.URL.Path != "/v1/sessions/s/workspace-view/preparations/prepare:1" || r.URL.Query().Get("request_digest") != strings.Repeat("a", 64) {
				t.Fatal(r.URL)
			}
			w.WriteHeader(404)
		}
	})
	request := WorkspacePreparationRequest{OperationID: "prepare:1", RequestDigest: strings.Repeat("a", 64)}
	if result, err := client.PrepareWorkspaceView(context.Background(), "s", request); err != nil || result.Status != "unknown" || result.Facts != nil {
		t.Fatal(result, err)
	}
	if result, err := client.LookupWorkspacePreparation(context.Background(), "s", request); err != nil || result != nil {
		t.Fatal(result, err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestPreparationMalformedReceiptAndUnknownFailureNeverReplay(t *testing.T) {
	request := WorkspacePreparationRequest{OperationID: "prepare:1", RequestDigest: strings.Repeat("a", 64)}
	for _, body := range []string{
		strings.Replace(preparationUnknown(), `"status":"unknown"`, `"status":"prepared"`, 1),
		strings.Replace(preparationUnknown(), `"user_id":"user"`, `"user_id":"user","host_path":"/secret"`, 1),
		strings.Replace(preparationUnknown(), `"session_id":"s"`, `"session_id":"other"`, 1),
		strings.Replace(preparationUnknown(), `"request_digest":"`+strings.Repeat("a", 64)+`"`, `"request_digest":"`+strings.Repeat("b", 64)+`"`, 1),
		strings.Replace(preparationUnknown(), `"sandbox_id":"",`, "", 1),
		strings.Replace(preparationUnknown(), `"status":"unknown"`, `"status":"unknown","facts":{}`, 1),
		strings.Replace(preparationUnknown(), `"status":"unknown"`, `"status":"unknown","facts":null`, 1),
		strings.Replace(preparationUnknown(), `"user_id":"user"`, `"user_id":""`, 1),
	} {
		client := controlClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err := client.PrepareWorkspaceView(context.Background(), "s", request); err == nil {
			t.Fatal("accepted malformed receipt", body)
		}
	}
	calls := 0
	client := controlClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(503)
		io.WriteString(w, `{"error_code":"UNKNOWN","message":"unknown"}`)
	})
	if _, err := client.PrepareWorkspaceView(context.Background(), "s", request); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
	var result WorkspacePreparationReceipt
	if json.Unmarshal([]byte(strings.Replace(preparationUnknown(), `"status":"unknown"`, `"status":"unknown","status":"unknown"`, 1)), &result) == nil {
		t.Fatal("duplicate key accepted")
	}
}
