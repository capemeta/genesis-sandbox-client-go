package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutputDirectoryOriginalRequestEncoding(t *testing.T) {
	for _, path := range []string{"output/call-1", "output/../work", "output/a/b", "/workspace/output/a"} {
		request := ExecSessionRequest{OperationID: "original", OutputDirectory: path, SubprocessPolicy: "deny"}
		for _, dto := range []any{toGenExecSessionRequest(request), toGenAsyncExecRequest(request)} {
			body, err := json.Marshal(dto)
			if path != "output/call-1" {
				if err == nil {
					t.Fatalf("invalid path encoded: %s", body)
				}
			} else if err != nil || !strings.Contains(string(body), `"output_directory":"output/call-1"`) || !strings.Contains(string(body), `"subprocess_policy":"deny"`) {
				t.Fatalf("original output identity lost: %s %v", body, err)
			}
		}
	}
}
