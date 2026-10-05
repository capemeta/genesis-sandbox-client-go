package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

var preparationOperationKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type WorkspacePreparationRequest struct {
	OperationID   string `json:"operation_id"`
	RequestDigest string `json:"request_digest"`
}

type WorkspacePreparationReceipt struct {
	OperationID     string              `json:"operation_id"`
	RequestDigest   string              `json:"request_digest"`
	Status          string              `json:"status"`
	SessionID       string              `json:"session_id"`
	WorkspaceID     string              `json:"workspace_id"`
	SandboxID       string              `json:"sandbox_id"`
	ProfileRevision string              `json:"profile_revision"`
	TenantID        string              `json:"tenant_id"`
	PrincipalID     string              `json:"principal_id"`
	UserID          string              `json:"user_id"`
	Facts           *WorkspaceViewFacts `json:"facts,omitempty"`
}

func (r WorkspacePreparationRequest) Validate() error {
	if !preparationOperationKey.MatchString(r.OperationID) || !sourceIdentityDigest.MatchString(r.RequestDigest) {
		return fmt.Errorf("invalid original preparation identity or digest")
	}
	return nil
}

func (r *WorkspacePreparationReceipt) UnmarshalJSON(data []byte) error {
	fields := []string{"operation_id", "request_digest", "status", "session_id", "workspace_id", "sandbox_id", "profile_revision", "tenant_id", "principal_id", "user_id"}
	if err := lifecycleRequired(data, fields); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, field := range fields {
		value := bytes.TrimSpace(raw[field])
		if len(value) == 0 || value[0] != '"' {
			return fmt.Errorf("preparation identity must be string")
		}
	}
	if facts := bytes.TrimSpace(raw["facts"]); len(facts) > 0 && string(facts) != "null" {
		if err := lifecycleRequired(facts, []string{"os", "arch", "image_digest", "mechanisms", "resource_limits", "network_mode", "view_state", "runtime_versions", "runtime_executables"}); err != nil {
			return err
		}
	}
	type plain WorkspacePreparationReceipt
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	if value.Status == "unknown" {
		if _, present := raw["facts"]; present {
			return fmt.Errorf("unknown preparation must omit facts")
		}
	}
	*r = WorkspacePreparationReceipt(value)
	return nil
}

func (r WorkspacePreparationReceipt) validate(session string, request WorkspacePreparationRequest) error {
	if r.SessionID != session || r.OperationID != request.OperationID || r.RequestDigest != request.RequestDigest || r.WorkspaceID == "" || r.ProfileRevision == "" || r.TenantID == "" || r.PrincipalID == "" || r.UserID == "" {
		return fmt.Errorf("original preparation receipt identity mismatch")
	}
	switch r.Status {
	case "unknown":
		if r.Facts != nil {
			return fmt.Errorf("unknown preparation cannot claim completed facts")
		}
	case "prepared":
		f := r.Facts
		if r.SandboxID == "" || f == nil || f.SessionID == nil || *f.SessionID != session || f.WorkspaceID == nil || *f.WorkspaceID != r.WorkspaceID || f.ProfileRevision == nil || *f.ProfileRevision != r.ProfileRevision || f.ViewState != "prepared" || f.OS == "" || f.Arch == "" || f.ImageDigest == "" || f.NetworkMode == "" || f.Mechanisms == nil || f.ResourceLimits == nil || f.RuntimeVersions == nil || f.RuntimeExecutables == nil {
			return fmt.Errorf("prepared receipt requires original bound complete facts")
		}
		for _, value := range f.ResourceLimits {
			if value < 1 || value > maxSafeInteger {
				return fmt.Errorf("invalid preparation resource limit")
			}
		}
	default:
		return fmt.Errorf("invalid preparation receipt status")
	}
	return nil
}

// PrepareWorkspaceView 单次发送；已有unknown只能显式查询，不能自动重放。
func (c *Client) PrepareWorkspaceView(ctx context.Context, sessionID string, request WorkspacePreparationRequest) (*WorkspacePreparationReceipt, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := validateLookupKey(sessionID, 128); err != nil {
		return nil, err
	}
	var result WorkspacePreparationReceipt
	if err := c.request(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/workspace-view?action=prepare", request, &result); err != nil {
		return nil, err
	}
	if err := result.validate(sessionID, request); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) LookupWorkspacePreparation(ctx context.Context, sessionID string, request WorkspacePreparationRequest) (*WorkspacePreparationReceipt, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := validateLookupKey(sessionID, 128); err != nil {
		return nil, err
	}
	var result WorkspacePreparationReceipt
	err := c.request(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/workspace-view/preparations/"+url.PathEscape(request.OperationID)+"?request_digest="+url.QueryEscape(request.RequestDigest), nil, &result)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if err := result.validate(sessionID, request); err != nil {
		return nil, err
	}
	return &result, nil
}
