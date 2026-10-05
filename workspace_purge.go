package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

type WorkspacePurgeRequest WorkspacePreparationRequest

type SessionOwnership struct {
	SessionID       string `json:"session_id"`
	WorkspaceID     string `json:"workspace_id"`
	TenantID        string `json:"tenant_id"`
	PrincipalID     string `json:"principal_id"`
	UserID          string `json:"user_id"`
	ProfileRevision string `json:"profile_revision"`
}

func (r *SessionOwnership) UnmarshalJSON(data []byte) error {
	if err := lifecycleRequired(data, []string{"session_id", "workspace_id", "tenant_id", "principal_id", "user_id", "profile_revision"}); err != nil {
		return err
	}
	type plain SessionOwnership
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	if value.SessionID == "" || value.WorkspaceID == "" || value.TenantID == "" || value.PrincipalID == "" || value.UserID == "" || value.ProfileRevision == "" {
		return fmt.Errorf("missing original session ownership identity")
	}
	*r = SessionOwnership(value)
	return nil
}

func (c *Client) GetSessionOwnership(ctx context.Context, session string) (*SessionOwnership, error) {
	if err := validateLookupKey(session, 128); err != nil {
		return nil, err
	}
	var result SessionOwnership
	if err := c.request(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(session)+"/ownership", nil, &result); err != nil {
		return nil, err
	}
	if result.SessionID != session {
		return nil, fmt.Errorf("original session ownership mismatch")
	}
	return &result, nil
}

type WorkspacePurgeReceipt struct {
	OperationID     string `json:"operation_id"`
	RequestDigest   string `json:"request_digest"`
	Status          string `json:"status"`
	SessionID       string `json:"session_id"`
	WorkspaceID     string `json:"workspace_id"`
	ProfileRevision string `json:"profile_revision"`
	TenantID        string `json:"tenant_id"`
	PrincipalID     string `json:"principal_id"`
	UserID          string `json:"user_id"`
}

func (r *WorkspacePurgeReceipt) UnmarshalJSON(data []byte) error {
	fields := []string{"operation_id", "request_digest", "status", "session_id", "workspace_id", "profile_revision", "tenant_id", "principal_id", "user_id"}
	if err := lifecycleRequired(data, fields); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, field := range fields {
		value := bytes.TrimSpace(raw[field])
		if len(value) < 3 || value[0] != '"' {
			return fmt.Errorf("purge receipt identity must be nonempty string")
		}
	}
	type plain WorkspacePurgeReceipt
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	if value.Status != "unknown" && value.Status != "purged" {
		return fmt.Errorf("invalid purge status")
	}
	*r = WorkspacePurgeReceipt(value)
	return nil
}

// 原删除单发，未知结果只能查询同一持久身份。
func (c *Client) PurgeSessionWorkspace(ctx context.Context, session string, request WorkspacePurgeRequest) (*WorkspacePurgeReceipt, error) {
	return c.workspacePurge(ctx, session, request, false)
}

func (c *Client) LookupWorkspacePurge(ctx context.Context, session string, request WorkspacePurgeRequest) (*WorkspacePurgeReceipt, error) {
	return c.workspacePurge(ctx, session, request, true)
}

func (c *Client) LookupStoragePurge(ctx context.Context, workspace string, request WorkspacePurgeRequest) (*WorkspacePurgeReceipt, error) {
	if err := WorkspacePreparationRequest(request).Validate(); err != nil {
		return nil, err
	}
	if err := validateLookupKey(workspace, 128); err != nil {
		return nil, err
	}
	var result WorkspacePurgeReceipt
	err := c.request(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(workspace)+"/purges/"+url.PathEscape(request.OperationID)+"?request_digest="+url.QueryEscape(request.RequestDigest), nil, &result)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if result.WorkspaceID != workspace || result.OperationID != request.OperationID || result.RequestDigest != request.RequestDigest {
		return nil, fmt.Errorf("original storage purge identity mismatch")
	}
	return &result, nil
}

func (c *Client) workspacePurge(ctx context.Context, session string, request WorkspacePurgeRequest, lookup bool) (*WorkspacePurgeReceipt, error) {
	if err := WorkspacePreparationRequest(request).Validate(); err != nil {
		return nil, err
	}
	if err := validateLookupKey(session, 128); err != nil {
		return nil, err
	}
	method, path := http.MethodPost, "/v1/sessions/"+url.PathEscape(session)+"/workspace:purge"
	var payload any = request
	if lookup {
		method, path, payload = http.MethodGet, "/v1/sessions/"+url.PathEscape(session)+"/workspace/purges/"+url.PathEscape(request.OperationID)+"?request_digest="+url.QueryEscape(request.RequestDigest), nil
	}
	var result WorkspacePurgeReceipt
	if err := c.request(ctx, method, path, payload, &result); err != nil {
		var api *APIError
		if lookup && errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if result.SessionID != session || result.OperationID != request.OperationID || result.RequestDigest != request.RequestDigest {
		return nil, fmt.Errorf("original purge receipt identity mismatch")
	}
	return &result, nil
}
