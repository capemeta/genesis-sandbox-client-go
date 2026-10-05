package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type WorkspaceLifecycleRequest struct {
	OperationID      string     `json:"operation_id"`
	ExpectedRevision int64      `json:"expected_revision"`
	State            string     `json:"state"`
	HoldSeconds      int64      `json:"hold_seconds"`
	TerminalAt       *time.Time `json:"terminal_at"`
	RetentionSeconds int64      `json:"retention_seconds"`
}

func lifecycleRevision(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

type WorkspaceLifecycleReceipt struct {
	OperationID   string     `json:"operation_id"`
	WorkspaceID   string     `json:"workspace_id"`
	Revision      int64      `json:"revision"`
	State         string     `json:"state"`
	Deadline      time.Time  `json:"deadline"`
	TerminalAt    *time.Time `json:"terminal_at,omitempty"`
	RequestDigest string     `json:"request_digest"`
}
type WorkspaceLifecycleLookup struct {
	State   string                     `json:"state"`
	Receipt *WorkspaceLifecycleReceipt `json:"receipt"`
}

func (r WorkspaceLifecycleRequest) Validate() error {
	if !opaqueStorageKey.MatchString(r.OperationID) || r.OperationID == "." || r.OperationID == ".." || r.ExpectedRevision < 0 || r.ExpectedRevision >= maxSafeInteger || r.HoldSeconds < 0 || r.HoldSeconds > 86400 || r.RetentionSeconds < 0 || r.RetentionSeconds > 30*86400 {
		return fmt.Errorf("invalid lifecycle request identity or budget")
	}
	switch r.State {
	case "active", "paused":
		if r.TerminalAt != nil || r.RetentionSeconds != 0 {
			return fmt.Errorf("nonterminal lifecycle carries terminal retention")
		}
	case "terminal":
		if r.HoldSeconds != 0 || r.RetentionSeconds == 0 || r.TerminalAt == nil || r.TerminalAt.Nanosecond()%1000 != 0 || r.TerminalAt.UTC().Year() < 1 || r.TerminalAt.UTC().Year() > 9999 {
			return fmt.Errorf("terminal lifecycle requires microsecond terminal time and retention")
		}
	default:
		return fmt.Errorf("invalid lifecycle state")
	}
	return nil
}

func WorkspaceLifecycleRequestDigest(r WorkspaceLifecycleRequest) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	var terminal any
	if r.TerminalAt != nil {
		terminal = r.TerminalAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	// 所有六字段固定出现；map编码排序键，ASCII身份与RFC3339时间不受HTML转义影响。
	data, err := json.Marshal(map[string]any{"expected_revision": r.ExpectedRevision, "hold_seconds": r.HoldSeconds, "operation_id": r.OperationID, "retention_seconds": r.RetentionSeconds, "state": r.State, "terminal_at": terminal})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (r *WorkspaceLifecycleReceipt) UnmarshalJSON(data []byte) error {
	if err := lifecycleRequired(data, []string{"operation_id", "workspace_id", "revision", "state", "deadline", "request_digest"}); err != nil {
		return err
	}
	type plain WorkspaceLifecycleReceipt
	var v plain
	if err := strictJSON(data, &v); err != nil {
		return err
	}
	*r = WorkspaceLifecycleReceipt(v)
	return nil
}
func (r *WorkspaceLifecycleLookup) UnmarshalJSON(data []byte) error {
	if err := lifecycleRequired(data, []string{"state", "receipt"}); err != nil {
		return err
	}
	type plain WorkspaceLifecycleLookup
	var v plain
	if err := strictJSON(data, &v); err != nil {
		return err
	}
	*r = WorkspaceLifecycleLookup(v)
	return nil
}
func lifecycleRequired(data []byte, fields []string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("invalid lifecycle JSON object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return fmt.Errorf("duplicate lifecycle field")
		}
		seen[name] = true
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return err
		}
	}
	for _, name := range fields {
		if !seen[name] {
			return fmt.Errorf("missing lifecycle field %s", name)
		}
	}
	return nil
}
func (r WorkspaceLifecycleReceipt) validate(workspace, operation string) error {
	if r.WorkspaceID != workspace || r.OperationID != operation || r.Revision < 1 || r.Revision > maxSafeInteger || r.Deadline.IsZero() || !sourceIdentityDigest.MatchString(r.RequestDigest) {
		return fmt.Errorf("invalid lifecycle receipt identity")
	}
	if (r.State == "terminal") != (r.TerminalAt != nil) || r.State != "active" && r.State != "paused" && r.State != "terminal" {
		return fmt.Errorf("invalid lifecycle receipt state")
	}
	if r.Deadline.Nanosecond()%1000 != 0 || r.Deadline.UTC().Year() < 1 || r.Deadline.UTC().Year() > 9999 {
		return fmt.Errorf("invalid lifecycle deadline precision or calendar")
	}
	if r.TerminalAt != nil && (r.TerminalAt.Nanosecond()%1000 != 0 || r.TerminalAt.UTC().Year() < 1 || r.TerminalAt.UTC().Year() > 9999 || r.TerminalAt.After(r.Deadline)) {
		return fmt.Errorf("invalid lifecycle terminal time")
	}
	return nil
}
func (c *Client) ControlWorkspaceLifecycle(ctx context.Context, workspaceID string, r WorkspaceLifecycleRequest) (*WorkspaceLifecycleReceipt, error) {
	digest, err := WorkspaceLifecycleRequestDigest(r)
	if err != nil {
		return nil, err
	}
	var receipt WorkspaceLifecycleReceipt
	if err = c.request(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(workspaceID)+"/lifecycle", r, &receipt); err != nil {
		return nil, err
	}
	if err = receipt.validate(workspaceID, r.OperationID); err != nil {
		return nil, err
	}
	if receipt.RequestDigest != digest || receipt.Revision != r.ExpectedRevision+1 || receipt.State != r.State {
		return nil, fmt.Errorf("lifecycle receipt differs from original intent")
	}
	if r.State == "terminal" && (!receipt.TerminalAt.Equal(*r.TerminalAt) || !receipt.Deadline.Equal(r.TerminalAt.Add(time.Duration(r.RetentionSeconds)*time.Second))) {
		return nil, fmt.Errorf("lifecycle receipt differs from parent terminal time or retention")
	}
	return &receipt, nil
}
func (c *Client) LookupWorkspaceLifecycle(ctx context.Context, workspaceID, operationID string) (*WorkspaceLifecycleLookup, error) {
	if !opaqueStorageKey.MatchString(operationID) || operationID == "." || operationID == ".." {
		return nil, fmt.Errorf("invalid lifecycle operation ID")
	}
	var result WorkspaceLifecycleLookup
	if err := c.request(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(workspaceID)+"/lifecycle:lookup?operation_id="+url.QueryEscape(operationID), nil, &result); err != nil {
		return nil, err
	}
	if result.State == "found" {
		if result.Receipt == nil {
			return nil, fmt.Errorf("missing original receipt")
		}
		if err := result.Receipt.validate(workspaceID, operationID); err != nil {
			return nil, err
		}
	} else if result.Receipt != nil || result.State != "unknown" && result.State != "history_expired" {
		return nil, fmt.Errorf("invalid lifecycle lookup state")
	}
	return &result, nil
}
