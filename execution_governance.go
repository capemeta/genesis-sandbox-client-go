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
	"reflect"
	"regexp"
	"strings"

	"github.com/capemeta/genesis-sandbox-client-go/internal/genapi"
)

type PlatformCallContext = genapi.PlatformCallContext
type PlatformLeaseRef = genapi.PlatformLeaseRef
type PlatformWorkspaceRef = genapi.PlatformWorkspaceRef
type TrustedExecutionGovernance = genapi.TrustedExecutionGovernance
type MaintenanceProofQuery = genapi.MaintenanceProofQuery
type MaintenanceSourceProof = genapi.MaintenanceSourceProof
type MaintenanceCall = genapi.MaintenanceCall
type MaintenanceLeaf = genapi.MaintenanceLeaf

var governedOutputPath = regexp.MustCompile(`^output/[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validateGovernanceContext(c PlatformCallContext, execution bool) error {
	for _, value := range []string{c.TenantId, c.UserId, c.RunId, c.TraceId, c.AuthorizationScope} {
		if !genapi.GovernanceIdentityValid(value) {
			return fmt.Errorf("invalid Platform context identity")
		}
	}
	if string(c.SubjectKind) != "enterprise" {
		return fmt.Errorf("enterprise Platform context required")
	}
	if c.DecisionReference != nil && !genapi.GovernanceIdentityValid(*c.DecisionReference) {
		return fmt.Errorf("invalid Platform decision identity")
	}
	if execution || c.InvocationId != nil || c.ExecutionId != nil || c.AttemptId != nil {
		for _, value := range []*string{c.InvocationId, c.ExecutionId, c.AttemptId} {
			if value == nil || !genapi.GovernanceIdentityValid(*value) {
				return fmt.Errorf("incomplete original Platform execution identity")
			}
		}
	}
	return nil
}

func (c *Client) QueryWorkspaceMaintenanceProof(ctx context.Context, workspaceID string, query MaintenanceProofQuery) (*MaintenanceSourceProof, error) {
	if err := validateGovernanceContext(query.Context, false); err != nil {
		return nil, err
	}
	if query.WorkspaceId != workspaceID || !genapi.GovernanceIdentityValid(workspaceID) || !genapi.GovernanceIdentityValid(query.OperationId) || !sourceIdentityDigest.MatchString(query.SourceIdentity) {
		return nil, fmt.Errorf("invalid original source query identity")
	}
	raw, _ := json.Marshal(map[string]any{"context": query.Context, "operation_id": query.OperationId, "workspace_id": workspaceID, "source_identity": query.SourceIdentity})
	var tree any
	_ = json.Unmarshal(raw, &tree)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(tree)
	digest := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
	if query.ClaimDigest != hex.EncodeToString(digest[:]) {
		return nil, fmt.Errorf("original source query digest mismatch")
	}
	var result MaintenanceSourceProof
	if err := c.request(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(workspaceID)+"/maintenance-proof:query", query, &result); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(result.Context, query.Context) || result.OperationId != query.OperationId || result.ClaimDigest != query.ClaimDigest || result.SourceIdentity != query.SourceIdentity || result.RootDevice < 0 || result.RootInode < 1 || result.Calls == nil || result.Leaves == nil || len(result.Calls) > 10000 || len(result.Leaves) > 100000 {
		return nil, fmt.Errorf("original source proof scope or budget mismatch")
	}
	calls := map[string]MaintenanceCall{}
	uids := map[int64]bool{}
	for _, call := range result.Calls {
		if err := validateGovernanceContext(call.Context, true); err != nil {
			return nil, err
		}
		if err := genapi.ValidateGovernanceLease(call.LeaseRef, call.Context.AuthorizationScope); err != nil {
			return nil, err
		}
		if !genapi.GovernanceIdentityValid(call.CallId) || !genapi.GovernanceIdentityValid(call.OperationId) || !sourceIdentityDigest.MatchString(call.RequestDigest) || call.Context.TenantId != query.Context.TenantId || call.Context.UserId != query.Context.UserId || call.ExecutionId != *call.Context.ExecutionId || !bool(call.StopConfirmed) || call.Uid < 1 || call.Gid < 1 || call.RootInode < 1 || call.RootDevice != result.RootDevice || calls[call.CallId].CallId != "" || uids[call.Uid] || !governedOutputPath.MatchString(call.OutputDirectory) {
			return nil, fmt.Errorf("original call proof identity mismatch")
		}
		calls[call.CallId], uids[call.Uid] = call, true
	}
	paths := map[string]bool{}
	outputRoots := map[string]bool{}
	for _, leaf := range result.Leaves {
		parts := strings.Split(leaf.Path, "/")
		if len(parts) == 0 || parts[0] != "work" && parts[0] != "output" || paths[leaf.Path] || leaf.Device != result.RootDevice || leaf.Inode < 1 || leaf.Uid < 0 || leaf.Gid < 0 || leaf.Mode < 0 || leaf.Mode > 07777 {
			return nil, fmt.Errorf("invalid source proof leaf")
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				return nil, fmt.Errorf("invalid source proof leaf path")
			}
		}
		paths[leaf.Path] = true
		if leaf.CallId == nil {
			if leaf.Uid != 0 && leaf.Uid != 65532 || strings.HasPrefix(leaf.Path, "output/") && leaf.Uid == 0 && leaf.Gid >= 100001 {
				return nil, fmt.Errorf("foreign leaf original call absent")
			}
			continue
		}
		owner, exists := calls[*leaf.CallId]
		if !exists {
			return nil, fmt.Errorf("source leaf original call absent")
		}
		if leaf.Path == owner.OutputDirectory {
			if !leaf.Directory || leaf.Uid != 0 || leaf.Gid != owner.Gid || leaf.Device != owner.RootDevice || leaf.Inode != owner.RootInode || leaf.Mode != 0770 {
				return nil, fmt.Errorf("original output root changed")
			}
			outputRoots[owner.CallId] = true
		} else if leaf.Uid != owner.Uid || leaf.Gid != owner.Gid && leaf.Gid != 65532 || strings.HasPrefix(leaf.Path, "work/") && !owner.AuthorizedWorkWrite || strings.HasPrefix(leaf.Path, "output/") && !strings.HasPrefix(leaf.Path, owner.OutputDirectory+"/") {
			return nil, fmt.Errorf("source leaf original call ownership changed")
		}
	}
	if !paths["work"] || !paths["output"] {
		return nil, fmt.Errorf("source public root proof absent")
	}
	for callID := range calls {
		if !outputRoots[callID] {
			return nil, fmt.Errorf("original call output root proof absent")
		}
	}
	return &result, nil
}
