package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// SSEEvent is one Server-Sent Event from a Job or Session exec log stream.
type SSEEvent struct {
	ID    string
	Event string
	Data  []byte
}

func (e SSEEvent) DecodeJSON(target any) error {
	return json.Unmarshal(e.Data, target)
}

// SSEStream incrementally decodes an SSE response. Call Close when finished.
type SSEStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

func newSSEStream(body io.ReadCloser) *SSEStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	return &SSEStream{body: body, scanner: scanner}
}

func (s *SSEStream) Next() (*SSEEvent, error) {
	event := &SSEEvent{Event: "message"}
	var data []string
	for s.scanner.Scan() {
		line := strings.TrimSuffix(s.scanner.Text(), "\r")
		if line == "" {
			if len(data) == 0 {
				continue
			}
			event.Data = []byte(strings.Join(data, "\n"))
			return event, nil
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			value := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(value, " ") {
				value = strings.TrimPrefix(value, " ")
			}
			data = append(data, value)
		}
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	if len(data) > 0 {
		event.Data = []byte(strings.Join(data, "\n"))
		return event, nil
	}
	return nil, io.EOF
}

func (s *SSEStream) Close() error {
	if s == nil || s.body == nil {
		return nil
	}
	err := s.body.Close()
	s.body = nil
	return err
}

func IsEndOfStream(err error) bool {
	return errors.Is(err, io.EOF)
}

func (c *Client) JobLogEvents(ctx context.Context, jobID string, cursor int) (*SSEStream, error) {
	body, err := c.JobLogs(ctx, jobID, cursor)
	if err != nil {
		return nil, err
	}
	return newSSEStream(body), nil
}

func (c *Client) ExecLogEvents(ctx context.Context, sessionID, execID string, cursor int) (*SSEStream, error) {
	body, err := c.StreamExecLogs(ctx, sessionID, execID, cursor)
	if err != nil {
		return nil, err
	}
	return newSSEStream(body), nil
}
