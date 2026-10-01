package sandbox

import (
	"time"

	"github.com/capemeta/genesis-sandbox-client-go/internal/genapi"
)

func optString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optInt(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func optMap(value map[string]string) *map[string]string {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return &cloned
}

func optStringSlice(value []string) *[]string {
	if len(value) == 0 {
		return nil
	}
	cloned := append([]string(nil), value...)
	return &cloned
}

func toGenEnvironmentSelector(selector *EnvironmentSelector) *genapi.EnvironmentSelector {
	if selector == nil {
		return nil
	}

	result := &genapi.EnvironmentSelector{}
	if selector.Profile != nil {
		result.Profile = &genapi.ProfileReference{
			Name: selector.Profile.Name,
		}
		if selector.Profile.Revision != "" {
			result.Profile.Revision = optString(selector.Profile.Revision)
		}
	}
	if selector.Hints != nil {
		result.Hints = &genapi.EnvHints{}
		if len(selector.Hints.Capabilities) > 0 {
			result.Hints.Capabilities = optStringSlice(selector.Hints.Capabilities)
		}
		if selector.Hints.Strict {
			strict := true
			result.Hints.Strict = &strict
		}
		if selector.Hints.Description != "" {
			result.Hints.Description = optString(selector.Hints.Description)
		}
	}
	return result
}

func toGenResolveEnvironmentRequest(req ResolveEnvironmentRequest) genapi.ResolveEnvironmentRequest {
	result := genapi.ResolveEnvironmentRequest{
		Environment: *toGenEnvironmentSelector(&req.Environment),
	}
	if req.TTLSeconds > 0 {
		result.TtlSeconds = optInt(req.TTLSeconds)
	}
	return result
}

func toGenLeaseRequest(req LeaseRequest) genapi.LeaseRequest {
	result := genapi.LeaseRequest{}
	if req.WorkspaceID != "" {
		result.WorkspaceId = optString(req.WorkspaceID)
	}
	if req.ResolutionID != "" {
		result.ResolutionId = optString(req.ResolutionID)
	}
	if len(req.Metadata) > 0 {
		result.Metadata = optMap(req.Metadata)
	}
	result.Environment = toGenEnvironmentSelector(req.Environment)
	return result
}

func toGenSubmitJobRequest(req SubmitJobRequest) genapi.SubmitJobRequest {
	result := genapi.SubmitJobRequest{
		Environment:    toGenEnvironmentSelector(req.Environment),
		ResolutionId:   optString(req.ResolutionID),
		Code:           optString(req.Code),
		Command:        optStringSlice(req.Command),
		WorkspaceId:    optString(req.WorkspaceID),
		SessionId:      optString(req.SessionID),
		IdempotencyKey: optString(req.IdempotencyKey),
		CallbackUrl:    optString(req.CallbackURL),
		Env:            optMap(req.Env),
		Metadata:       optMap(req.Metadata),
	}
	if req.Language != "" {
		language := genapi.SubmitJobRequestLanguage(req.Language)
		result.Language = &language
	}
	if req.TimeoutSeconds > 0 {
		result.TimeoutSeconds = optInt(req.TimeoutSeconds)
	}
	if req.QueueWaitTimeoutSeconds > 0 {
		result.QueueWaitTimeoutSeconds = optInt(req.QueueWaitTimeoutSeconds)
	}
	if len(req.InputArtifactIDs) > 0 {
		result.InputArtifactIds = optStringSlice(req.InputArtifactIDs)
	}
	return result
}

func toGenExecSessionRequest(req ExecSessionRequest) genapi.ExecSessionRequest {
	result := genapi.ExecSessionRequest{
		Code:       optString(req.Code),
		Command:    optStringSlice(req.Command),
		WorkingDir: optString(req.WorkingDir),
		Env:        optMap(req.Env),
	}
	if req.Language != "" {
		language := genapi.ExecSessionRequestLanguage(req.Language)
		result.Language = &language
	}
	if req.TimeoutSeconds > 0 {
		result.TimeoutSeconds = optInt(req.TimeoutSeconds)
	}
	return result
}

func toGenAsyncExecRequest(req ExecSessionRequest) genapi.AsyncExecRequest {
	result := genapi.AsyncExecRequest{
		Code:        optString(req.Code),
		Command:     optStringSlice(req.Command),
		WorkingDir:  optString(req.WorkingDir),
		Env:         optMap(req.Env),
		CallbackUrl: optString(req.CallbackURL),
	}
	if req.Language != "" {
		language := genapi.AsyncExecRequestLanguage(req.Language)
		result.Language = &language
	}
	if req.TimeoutSeconds > 0 {
		result.TimeoutSeconds = optInt(req.TimeoutSeconds)
	}
	return result
}

func toGenCreateWorkspaceRequest(req CreateWorkspaceRequest) genapi.CreateWorkspaceRequest {
	result := genapi.CreateWorkspaceRequest{
		WorkspaceId: optString(req.WorkspaceID),
		Metadata:    optMap(req.Metadata),
	}
	if req.RetentionMode != "" {
		mode := genapi.CreateWorkspaceRequestRetentionMode(req.RetentionMode)
		result.RetentionMode = &mode
	}
	if req.TTLSeconds > 0 {
		result.TtlSeconds = optInt(req.TTLSeconds)
	}
	if req.QuotaMB > 0 {
		result.QuotaMb = optInt(req.QuotaMB)
	}
	return result
}

func toGenCreateSessionRequest(req CreateSessionRequest) genapi.CreateSessionRequest {
	result := genapi.CreateSessionRequest{
		Environment:    toGenEnvironmentSelector(req.Environment),
		ResolutionId:   optString(req.ResolutionID),
		WorkspaceId:    optString(req.WorkspaceID),
		IdempotencyKey: optString(req.IdempotencyKey),
		Metadata:       optMap(req.Metadata),
	}
	if req.StatePolicy != "" {
		statePolicy := genapi.CreateSessionRequestStatePolicy(req.StatePolicy)
		result.StatePolicy = &statePolicy
	}
	if req.TTLSeconds > 0 {
		result.TtlSeconds = optInt(req.TTLSeconds)
	}
	if req.WorkspaceRetention != "" {
		retention := genapi.CreateSessionRequestWorkspaceRetention(req.WorkspaceRetention)
		result.WorkspaceRetention = &retention
	}
	if req.WorkspaceTTLSeconds > 0 {
		result.WorkspaceTtlSeconds = optInt(req.WorkspaceTTLSeconds)
	}
	return result
}

func toGenSessionContext(patch SessionContext) genapi.SessionContext {
	result := genapi.SessionContext{
		Cwd: optString(patch.Cwd),
		Env: optMap(patch.Env),
	}
	return result
}

func toGenBuildDependencyRequest(req BuildDependencyRequest) genapi.BuildDependencyRequest {
	result := genapi.BuildDependencyRequest{
		ResolutionId: optString(req.ResolutionID),
		Manifest:     optString(req.Manifest),
		Lockfile:     optString(req.Lockfile),
		Packages:     optStringSlice(req.Packages),
	}
	if req.Language != "" {
		language := genapi.BuildDependencyRequestLanguage(req.Language)
		result.Language = &language
	}
	if req.Environment != nil && req.Environment.Profile != nil {
		result.Environment = &genapi.ExactEnvironmentSelector{}
		result.Environment.Profile.Name = req.Environment.Profile.Name
		result.Environment.Profile.Revision = req.Environment.Profile.Revision
	}
	return result
}

func toGenStartGUIRequest(req StartGUIRequest) genapi.StartGUIRequest {
	result := genapi.StartGUIRequest{
		Resolution: optString(req.Resolution),
		Metadata:   optMap(req.Metadata),
	}
	if req.Kind != "" {
		kind := genapi.StartGUIRequestKind(req.Kind)
		result.Kind = &kind
	}
	if req.TTLSeconds > 0 {
		result.TtlSeconds = optInt(req.TTLSeconds)
	}
	return result
}

func errorDetailsMap(details genapi.ErrorResponse_Details) map[string]any {
	result := make(map[string]any, len(details.AdditionalProperties)+4)
	result["retryable"] = details.Retryable
	if details.Field != nil {
		result["field"] = *details.Field
	}
	if details.ResourceType != nil {
		result["resource_type"] = *details.ResourceType
	}
	if details.RetryAfterSeconds != nil {
		result["retry_after_seconds"] = *details.RetryAfterSeconds
	}
	for key, value := range details.AdditionalProperties {
		result[key] = value
	}
	return result
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func derefStringEnum[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func cloneStringMapPtr(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	return cloneStringMap(*value)
}

func cloneStringSlicePtr(value *[]string) []string {
	if value == nil || len(*value) == 0 {
		return nil
	}
	return append([]string(nil), (*value)...)
}

func cloneAnyMapPtr(value *map[string]any) map[string]any {
	if value == nil || len(*value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(*value))
	for key, item := range *value {
		cloned[key] = item
	}
	return cloned
}

func fromGenEffectiveEnvironment(value *genapi.EffectiveEnvironment) *EffectiveEnvironment {
	if value == nil {
		return nil
	}
	return &EffectiveEnvironment{
		ProfileName:     derefString(value.ProfileName),
		ProfileRevision: derefString(value.ProfileRevision),
		SelectionMode:   derefStringEnum(value.SelectionMode),
		SelectionReason: cloneStringSlicePtr(value.SelectionReason),
		Capabilities:    cloneStringSlicePtr(value.Capabilities),
		Degraded:        derefBool(value.Degraded),
	}
}

func fromGenCatalogCardLimits(value *genapi.CatalogCardLimits) *ProfileLimits {
	if value == nil {
		return nil
	}
	return &ProfileLimits{
		MaxExecTimeoutSeconds: derefInt(value.MaxExecTimeoutSeconds),
		MaxSessionTTLSeconds:  derefInt(value.MaxSessionTtlSeconds),
		WorkspaceQuotaMB:      derefInt(value.WorkspaceQuotaMb),
		MaxLogBytes:           derefInt64(value.MaxLogBytes),
		MaxConcurrentExecs:    derefInt(value.MaxConcurrentExecs),
	}
}

func fromGenCatalogCardFeatures(value *genapi.CatalogCardFeatures) *ProfileFeatures {
	if value == nil {
		return nil
	}
	return &ProfileFeatures{
		Viewer:        derefBool(value.Viewer),
		SuspendResume: derefBool(value.SuspendResume),
	}
}

func fromGenCatalogCard(card genapi.CatalogCard) CatalogCard {
	return CatalogCard{
		ProfileName:     card.Name,
		ProfileRevision: card.ProfileRevision,
		DisplayName:     derefString(card.DisplayName),
		Description:     derefString(card.Description),
		Capabilities:    cloneStringSlicePtr(card.Capabilities),
		Tags:            cloneStringSlicePtr(card.Tags),
		UseWhen:         cloneStringSlicePtr(card.UseWhen),
		AvoidWhen:       cloneStringSlicePtr(card.AvoidWhen),
		Limits:          fromGenCatalogCardLimits(card.Limits),
		Features:        fromGenCatalogCardFeatures(card.Features),
	}
}

func fromGenCatalogResponse(catalog genapi.EnvironmentCatalog) *CatalogResponse {
	result := &CatalogResponse{
		CapabilityVocabulary: cloneStringSlicePtr(catalog.CapabilityVocabulary),
		DefaultProfile:       derefString(catalog.DefaultProfile),
		NextOffset:           derefInt(catalog.NextOffset),
		Total:                derefInt(catalog.Total),
	}
	if catalog.Items != nil {
		result.Items = make([]CatalogCard, 0, len(*catalog.Items))
		for _, item := range *catalog.Items {
			result.Items = append(result.Items, fromGenCatalogCard(item))
		}
	}
	return result
}

func fromGenEnvironmentResolution(value genapi.EnvironmentResolution) *EnvironmentResolution {
	return &EnvironmentResolution{
		ResolutionID:    derefString(value.ResolutionId),
		ProfileName:     derefString(value.ProfileName),
		ProfileRevision: derefString(value.ProfileRevision),
		SelectionMode:   derefStringEnum(value.SelectionMode),
		SelectionReason: cloneStringSlicePtr(value.SelectionReason),
		Capabilities:    cloneStringSlicePtr(value.Capabilities),
		ExpiresAt:       derefTime(value.ExpiresAt).Format(time.RFC3339),
	}
}

func fromGenArtifact(value genapi.Artifact) Artifact {
	return Artifact{
		ArtifactID:  derefString(value.ArtifactId),
		TenantID:    derefString(value.TenantId),
		WorkspaceID: derefString(value.WorkspaceId),
		JobID:       derefString(value.JobId),
		Name:        derefString(value.Name),
		Size:        derefInt64(value.Size),
		SHA256:      derefString(value.Sha256),
		MIME:        derefString(value.Mime),
		CreatedAt:   derefTime(value.CreatedAt),
	}
}

func fromGenArtifacts(values *[]genapi.Artifact) []Artifact {
	if values == nil || len(*values) == 0 {
		return nil
	}
	items := make([]Artifact, 0, len(*values))
	for _, item := range *values {
		items = append(items, fromGenArtifact(item))
	}
	return items
}

func fromGenJobResult(value genapi.JobResult) *JobResult {
	result := &JobResult{
		JobID:                derefString(value.JobId),
		TenantID:             derefString(value.TenantId),
		WorkspaceID:          derefString(value.WorkspaceId),
		SandboxID:            derefString(value.SandboxId),
		TaskType:             derefString(value.TaskType),
		Operation:            derefString(value.Operation),
		Status:               derefString(value.Status),
		ExitCode:             derefInt(value.ExitCode),
		Stdout:               derefString(value.Stdout),
		Stderr:               derefString(value.Stderr),
		StdoutTruncated:      derefBool(value.StdoutTruncated),
		StderrTruncated:      derefBool(value.StderrTruncated),
		ErrorCode:            derefString(value.ErrorCode),
		Error:                derefString(value.Error),
		DurationMS:           derefInt64(value.DurationMs),
		StartedAt:            value.StartedAt,
		FinishedAt:           value.FinishedAt,
		OutputArtifacts:      fromGenArtifacts(value.OutputArtifacts),
		LogsURL:              derefString(value.LogsUrl),
		EffectiveEnvironment: fromGenEffectiveEnvironment(value.EffectiveEnvironment),
	}
	result.ErrorMessage = result.Error
	return result
}

func fromGenJobList(value genapi.JobList) *JobList {
	result := &JobList{
		NextOffset: derefInt(value.NextOffset),
		Total:      derefInt(value.Total),
	}
	if value.Items != nil {
		result.Items = make([]JobResult, 0, len(*value.Items))
		for _, item := range *value.Items {
			result.Items = append(result.Items, *fromGenJobResult(item))
		}
	}
	return result
}

func fromGenExecRecord(value genapi.ExecRecord) *ExecRecord {
	return &ExecRecord{
		ExecID:               derefString(value.ExecId),
		SessionID:            derefString(value.SessionId),
		Status:               derefStringEnum(value.Status),
		ExitCode:             derefInt(value.ExitCode),
		Stdout:               derefString(value.Stdout),
		Stderr:               derefString(value.Stderr),
		ErrorCode:            derefString(value.ErrorCode),
		StdoutTruncated:      derefBool(value.StdoutTruncated),
		StderrTruncated:      derefBool(value.StderrTruncated),
		WorkingDir:           derefString(value.WorkingDir),
		EffectiveEnvironment: fromGenEffectiveEnvironment(value.EffectiveEnvironment),
		LogsURL:              derefString(value.LogsUrl),
		DurationMS:           derefInt64(value.DurationMs),
		CreatedAt:            derefTime(value.CreatedAt),
		StartedAt:            value.StartedAt,
		FinishedAt:           value.FinishedAt,
	}
}

func fromGenExecRecordList(value genapi.ExecRecordList) *ExecRecordList {
	result := &ExecRecordList{
		NextCursor: derefString(value.NextCursor),
		Total:      derefInt(value.Total),
	}
	if value.Items != nil {
		result.Items = make([]ExecRecord, 0, len(*value.Items))
		for _, item := range *value.Items {
			result.Items = append(result.Items, *fromGenExecRecord(item))
		}
	}
	return result
}

func fromGenSandboxLease(value genapi.SandboxLease) *SandboxLease {
	return &SandboxLease{
		SandboxID:            derefString(value.SandboxId),
		LeaseID:              derefString(value.LeaseId),
		TenantID:             derefString(value.TenantId),
		WorkspaceID:          derefString(value.WorkspaceId),
		RuntimeProfile:       derefString(value.RuntimeProfile),
		Status:               derefString(value.Status),
		CreatedAt:            derefTime(value.CreatedAt),
		ExpiresAt:            derefTime(value.ExpiresAt),
		EffectivePolicy:      cloneAnyMapPtr(value.EffectivePolicy),
		Metadata:             cloneStringMapPtr(value.Metadata),
		ResourceVersion:      derefInt64(value.ResourceVersion),
		EffectiveEnvironment: fromGenEffectiveEnvironment(value.EffectiveEnvironment),
		ProfileRevision:      derefString(value.ProfileRevision),
	}
}

func fromGenSandboxLeases(values []genapi.SandboxLease) []SandboxLease {
	if len(values) == 0 {
		return nil
	}
	items := make([]SandboxLease, 0, len(values))
	for _, item := range values {
		items = append(items, *fromGenSandboxLease(item))
	}
	return items
}

func fromGenExecSessionResult(value genapi.ExecSessionResult) *ExecSessionResult {
	return &ExecSessionResult{
		ExitCode:        derefInt(value.ExitCode),
		Stdout:          derefString(value.Stdout),
		Stderr:          derefString(value.Stderr),
		StdoutTruncated: derefBool(value.StdoutTruncated),
		StderrTruncated: derefBool(value.StderrTruncated),
		Environment:     derefStringEnum(value.Environment),
		SessionID:       derefString(value.SessionId),
		WorkspaceID:     derefString(value.WorkspaceId),
		SandboxID:       derefString(value.SandboxId),
		Cwd:             derefString(value.Cwd),
	}
}

func fromGenWorkspace(value genapi.Workspace) *Workspace {
	return &Workspace{
		WorkspaceID:   derefString(value.WorkspaceId),
		TenantID:      derefString(value.TenantId),
		UserID:        derefString(value.UserId),
		CreatedAt:     derefTime(value.CreatedAt),
		Metadata:      cloneStringMapPtr(value.Metadata),
		RetentionMode: derefStringEnum(value.RetentionMode),
		ExpiresAt:     value.ExpiresAt,
		QuotaMB:       derefInt(value.QuotaMb),
	}
}

func fromGenSession(value genapi.Session) *Session {
	return &Session{
		SessionID:            derefString(value.SessionId),
		TenantID:             derefString(value.TenantId),
		UserID:               derefString(value.UserId),
		WorkspaceID:          derefString(value.WorkspaceId),
		RuntimeProfile:       derefString(value.RuntimeProfile),
		ProfileRevision:      derefString(value.ProfileRevision),
		StatePolicy:          derefString(value.StatePolicy),
		ActiveSandboxID:      derefString(value.ActiveSandboxId),
		Status:               derefString(value.Status),
		CreatedAt:            derefTime(value.CreatedAt),
		ExpiresAt:            derefTime(value.ExpiresAt),
		Metadata:             cloneStringMapPtr(value.Metadata),
		ResourceVersion:      derefInt64(value.ResourceVersion),
		EffectiveEnvironment: fromGenEffectiveEnvironment(value.EffectiveEnvironment),
	}
}

func fromGenDependencyBuild(value genapi.DependencyBuild) *DependencyBuild {
	return &DependencyBuild{
		Fingerprint:     value.Fingerprint,
		TenantID:        derefString(value.TenantId),
		WorkspaceID:     derefString(value.WorkspaceId),
		ProfileName:     value.ProfileName,
		ProfileRevision: value.ProfileRevision,
		Language:        value.Language,
		Status:          string(value.Status),
		CachePath:       derefString(value.CachePath),
		ReadOnly:        value.ReadOnly,
		Error:           derefString(value.Error),
		CreatedAt:       value.CreatedAt,
		UpdatedAt:       value.UpdatedAt,
	}
}

func fromGenViewerDescriptor(value genapi.ViewerDescriptor) *ViewerDescriptor {
	return &ViewerDescriptor{
		SandboxID:      derefString(value.SandboxId),
		RuntimeProfile: derefString(value.RuntimeProfile),
		Status:         derefStringEnum(value.Status),
		Kind:           derefStringEnum(value.Kind),
		Ready:          derefBool(value.Ready),
		PageURL:        derefString(value.PageUrl),
		WebSocketURL:   derefString(value.WebsocketUrl),
		ProxyURL:       derefString(value.ProxyUrl),
		AccessToken:    derefString(value.AccessToken),
		TokenHeader:    derefString(value.TokenHeader),
		ExpiresAt:      derefTime(value.ExpiresAt),
		RenewOnAccess:  derefBool(value.RenewOnAccess),
	}
}

func fromGenWorkspaceFileInfo(value genapi.WorkspaceFileInfo) WorkspaceFileInfo {
	return WorkspaceFileInfo{
		Path:        derefString(value.Path),
		SandboxPath: derefString(value.SandboxPath),
		Environment: derefStringEnum(value.Environment),
		Name:        derefString(value.Name),
		Kind:        derefStringEnum(value.Kind),
		Size:        derefInt64(value.Size),
		SHA256:      derefString(value.Sha256),
		MIME:        derefString(value.Mime),
		ModTime:     derefTime(value.ModTime),
	}
}

func fromGenWorkspaceListResult(value genapi.WorkspaceListResult) *WorkspaceListResult {
	result := &WorkspaceListResult{
		Path:      derefString(value.Path),
		Truncated: derefBool(value.Truncated),
		Limit:     derefInt(value.Limit),
	}
	if value.Entries != nil {
		result.Entries = make([]WorkspaceFileInfo, 0, len(*value.Entries))
		for _, item := range *value.Entries {
			result.Entries = append(result.Entries, fromGenWorkspaceFileInfo(item))
		}
	}
	return result
}

func fromGenSessionContext(value genapi.SessionContext) *SessionContext {
	return &SessionContext{
		Cwd: derefString(value.Cwd),
		Env: cloneStringMapPtr(value.Env),
	}
}

func fromGenAuditEvent(value genapi.AuditEvent) AuditEvent {
	return AuditEvent{
		EventID:      derefString(value.EventId),
		TenantID:     derefString(value.TenantId),
		UserID:       derefString(value.UserId),
		PrincipalID:  derefString(value.PrincipalId),
		KeyID:        derefString(value.KeyId),
		Action:       derefString(value.Action),
		ResourceType: derefString(value.ResourceType),
		ResourceID:   derefString(value.ResourceId),
		Status:       derefString(value.Status),
		Message:      derefString(value.Message),
		TraceID:      derefString(value.TraceId),
		CreatedAt:    derefTime(value.CreatedAt),
	}
}

func fromGenAuditEventList(value genapi.AuditEventList) *AuditEventList {
	result := &AuditEventList{
		NextOffset: derefInt(value.NextOffset),
		Total:      derefInt(value.Total),
	}
	if value.Items != nil {
		result.Items = make([]AuditEvent, 0, len(*value.Items))
		for _, item := range *value.Items {
			result.Items = append(result.Items, fromGenAuditEvent(item))
		}
	}
	return result
}
