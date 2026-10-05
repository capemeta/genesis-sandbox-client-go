package genapi

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

var governanceDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func GovernanceIdentityValid(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 || c == '\u2028' || c == '\u2029' {
			return false
		}
	}
	return true
}

func (g TrustedExecutionGovernance) Validate() error {
	for _, value := range []string{g.Context.TenantId, g.Context.UserId, g.Context.RunId, g.Context.TraceId, g.Context.AuthorizationScope, g.Context.InvocationId, g.Context.ExecutionId, g.Context.AttemptId, g.WorkspaceId, g.PlatformOperationId, g.LeaseRef.ProviderId, g.LeaseRef.LeaseId, g.LeaseRef.Workspace.WorkspaceKey, g.LeaseRef.Workspace.ProviderId, g.LeaseRef.Workspace.ResourceId, g.LeaseRef.Workspace.Scope} {
		if !GovernanceIdentityValid(value) {
			return fmt.Errorf("invalid original governance identity")
		}
	}
	if g.Context.DecisionReference != nil && !GovernanceIdentityValid(*g.Context.DecisionReference) {
		return fmt.Errorf("invalid original governance decision")
	}
	_, offset := g.LeaseRef.ExpiresAt.Zone()
	if string(g.Context.SubjectKind) != "enterprise" || !bool(g.AuthorizedWorkWrite) || !governanceDigest.MatchString(g.RequestDigest) || !governanceDigest.MatchString(g.SourceIdentity) || g.LeaseRef.ProviderId != g.LeaseRef.Workspace.ProviderId || g.LeaseRef.Generation < 1 || g.LeaseRef.Workspace.Generation < 1 || g.LeaseRef.ExpiresAt.IsZero() || offset != 0 {
		return fmt.Errorf("unsupported or incomplete original governance binding")
	}
	return ValidateGovernanceLease(g.LeaseRef, g.Context.AuthorizationScope)
}

// 原 lease 的引用与 scope 属于执行主体，不能从当前服务凭据补推。
func ValidateGovernanceLease(lease PlatformLeaseRef, scope string) error {
	for _, value := range []string{lease.ProviderId, lease.LeaseId, lease.Workspace.WorkspaceKey,
		lease.Workspace.ProviderId, lease.Workspace.ResourceId, lease.Workspace.Scope} {
		if !GovernanceIdentityValid(value) {
			return fmt.Errorf("invalid original governance lease identity")
		}
	}
	_, offset := lease.ExpiresAt.Zone()
	if lease.ProviderId != lease.Workspace.ProviderId || lease.Workspace.Scope != scope ||
		lease.Generation < 1 || lease.Workspace.Generation < 1 || lease.ExpiresAt.IsZero() || offset != 0 {
		return fmt.Errorf("invalid original governance lease binding")
	}
	return nil
}
