package sandbox

import (
	"io"
	"strings"
	"testing"
)

func TestSSEStreamDecodesEventsIncrementally(t *testing.T) {
	stream := newSSEStream(io.NopCloser(strings.NewReader(
		"id: 7\nevent: stdout\ndata: {\"message\":\"hello\"}\n\n",
	)))
	defer stream.Close()
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "7" || event.Event != "stdout" {
		t.Fatalf("unexpected event: %+v", event)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := event.DecodeJSON(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Message != "hello" {
		t.Fatalf("message = %q", payload.Message)
	}
	if _, err := stream.Next(); !IsEndOfStream(err) {
		t.Fatalf("final error = %v, want EOF", err)
	}
}
