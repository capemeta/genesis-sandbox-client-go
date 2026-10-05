package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) InspectSharedStorage(ctx context.Context, storageRef, resourceID string) (*SharedStorageResource, error) {
	if !opaqueStorageKey.MatchString(storageRef) || !opaqueStorageKey.MatchString(resourceID) {
		return nil, fmt.Errorf("invalid opaque storage reference")
	}
	var result SharedStorageResource
	if err := c.request(ctx, http.MethodGet, "/v1/storage-resources/"+storageRef+"/"+resourceID, nil, &result); err != nil {
		return nil, err
	}
	if result.StorageRef != storageRef || result.ResourceID != resourceID {
		return nil, fmt.Errorf("invalid storage resource evidence")
	}
	return &result, nil
}

func (c *Client) GetWorkspaceView(ctx context.Context, sessionID string) (*WorkspaceViewFacts, error) {
	return c.workspaceView(ctx, sessionID, "")
}

func (c *Client) SealWorkspaceView(ctx context.Context, sessionID string) (*WorkspaceViewFacts, error) {
	return c.workspaceView(ctx, sessionID, "seal")
}

func (c *Client) workspaceView(ctx context.Context, sessionID, action string) (*WorkspaceViewFacts, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/workspace-view"
	method := http.MethodGet
	if action != "" {
		method = http.MethodPost
		path += "?action=" + action
	}
	var result WorkspaceViewFacts
	if err := c.request(ctx, method, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetSessionHistory(ctx context.Context, sessionID string) (*Session, error) {
	var result Session
	if err := c.request(ctx, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/history", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func validateLookupKey(value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" || len(value) > maxBytes || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("invalid operation lookup key")
	}
	return nil
}

type SetExecutableRequest struct {
	Executable         bool  `json:"executable"`
	ExpectedExecutable *bool `json:"expected_executable,omitempty"`
}

// 文件路径缺失必须保留服务端 404，不套用资源删除的幂等成功语义。
func (c *Client) removeWorkspacePath(ctx context.Context, path string) error {
	response, err := c.rawRequestWithHeaders(ctx, http.MethodDelete, path, "", nil, nil)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusNotFound {
		return decodeAPIError(response)
	}
	return response.Body.Close()
}

func (c *Client) SetSessionFileExecutable(ctx context.Context, sessionID, path, ifMatch string, request SetExecutableRequest) (*WorkspaceFileInfo, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(ifMatch, "\r\n") {
		return nil, fmt.Errorf("invalid file metadata precondition")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if ifMatch != "" {
		headers.Set("If-Match", ifMatch)
	}
	response, err := c.rawRequestWithHeaders(ctx, http.MethodPatch, "/v1/sessions/"+url.PathEscape(sessionID)+"/files:executable?path="+url.QueryEscape(path), "application/json", bytes.NewReader(body), headers)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result WorkspaceFileInfo
	if err := decodeJSONLimited(response.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
