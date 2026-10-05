package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/capemeta/genesis-sandbox-client-go/internal/genapi"
)

const maxSafeInteger int64 = 9007199254740991

var opaqueStorageKey = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var sourceIdentityDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var filesystemUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// WorkspaceBindingRequest 仅表达已登记存储，不接受宿主路径或调用方自授所有者。
type WorkspaceBindingRequest struct {
	Mode           string `json:"mode"`
	StorageRef     string `json:"storage_ref,omitempty"`
	ResourceID     string `json:"resource_id,omitempty"`
	BindingVersion int64  `json:"binding_version,omitempty"`
}

func (binding WorkspaceBindingRequest) Validate() error {
	switch binding.Mode {
	case "isolated":
		if binding.StorageRef != "" || binding.ResourceID != "" || binding.BindingVersion != 0 {
			return fmt.Errorf("isolated binding cannot carry shared storage fields")
		}
	case "shared":
		if !opaqueStorageKey.MatchString(binding.StorageRef) || !opaqueStorageKey.MatchString(binding.ResourceID) || binding.BindingVersion < 1 || binding.BindingVersion > maxSafeInteger || int64(int(binding.BindingVersion)) != binding.BindingVersion {
			return fmt.Errorf("invalid opaque shared storage binding")
		}
	default:
		return fmt.Errorf("workspace binding mode must be isolated or shared")
	}
	return nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

func (binding *WorkspaceBindingRequest) UnmarshalJSON(data []byte) error {
	type plain WorkspaceBindingRequest
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	validated := WorkspaceBindingRequest(value)
	if err := validated.Validate(); err != nil {
		return err
	}
	*binding = validated
	return nil
}

func (binding WorkspaceBindingRequest) MarshalJSON() ([]byte, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	type plain WorkspaceBindingRequest
	return json.Marshal(plain(binding))
}

func validateWorkspaceBindingRequest(request CreateWorkspaceRequest) error {
	if request.WorkspaceBinding == nil {
		return nil
	}
	if err := request.WorkspaceBinding.Validate(); err != nil {
		return err
	}
	if request.WorkspaceBinding.Mode == "shared" && (request.WorkspaceID != request.WorkspaceBinding.ResourceID || request.RetentionMode != "explicit_delete" || request.TTLSeconds != 0) {
		return fmt.Errorf("shared workspace requires matching resource ID and explicit_delete retention without TTL")
	}
	return nil
}

func toGenWorkspaceBinding(binding *WorkspaceBindingRequest) *genapi.WorkspaceBindingRequest {
	if binding == nil {
		return nil
	}
	result := &genapi.WorkspaceBindingRequest{}
	if binding.Mode == "isolated" {
		_ = result.FromWorkspaceBindingRequest0(genapi.WorkspaceBindingRequest0{Mode: "isolated"})
	} else {
		_ = result.FromWorkspaceBindingRequest1(genapi.WorkspaceBindingRequest1{Mode: "shared", StorageRef: binding.StorageRef, ResourceId: binding.ResourceID, BindingVersion: int(binding.BindingVersion)})
	}
	return result
}

type WorkspaceViewFacts struct {
	SessionID          *string           `json:"session_id,omitempty"`
	WorkspaceID        *string           `json:"workspace_id,omitempty"`
	ProfileRevision    *string           `json:"profile_revision,omitempty"`
	OS                 string            `json:"os"`
	Arch               string            `json:"arch"`
	ImageDigest        string            `json:"image_digest"`
	Mechanisms         []string          `json:"mechanisms"`
	ResourceLimits     map[string]int64  `json:"resource_limits"`
	RuntimeVersions    map[string]string `json:"runtime_versions"`
	RuntimeExecutables map[string]string `json:"runtime_executables"`
	NetworkMode        string            `json:"network_mode"`
	ViewState          string            `json:"view_state"`
	ReadonlyRegions    *[]string         `json:"readonly_regions,omitempty"`
}

func fromGenWorkspaceViewFacts(value *genapi.WorkspaceViewFacts) *WorkspaceViewFacts {
	if value == nil {
		return nil
	}
	return &WorkspaceViewFacts{SessionID: value.SessionId, WorkspaceID: value.WorkspaceId, ProfileRevision: value.ProfileRevision, OS: value.Os, Arch: value.Arch, ImageDigest: value.ImageDigest, Mechanisms: value.Mechanisms, ResourceLimits: value.ResourceLimits, RuntimeVersions: value.RuntimeVersions, RuntimeExecutables: value.RuntimeExecutables, NetworkMode: value.NetworkMode, ViewState: string(value.ViewState), ReadonlyRegions: value.ReadonlyRegions}
}

type SharedStorageIdentity struct {
	SourceIdentity  string `json:"source_identity"`
	DeviceID        int64  `json:"device_id"`
	RootInode       int64  `json:"root_inode"`
	BackingDeviceID int64  `json:"backing_device_id"`
	BackingInode    int64  `json:"backing_inode"`
	Filesystem      string `json:"filesystem"`
	FilesystemUUID  string `json:"filesystem_uuid"`
	CapacityBytes   int64  `json:"capacity_bytes"`
	InodeCapacity   int64  `json:"inode_capacity"`
}

func (identity *SharedStorageIdentity) UnmarshalJSON(data []byte) error {
	type plain SharedStorageIdentity
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"source_identity", "device_id", "root_inode", "backing_device_id", "backing_inode", "filesystem", "filesystem_uuid", "capacity_bytes", "inode_capacity"} {
		if item, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return fmt.Errorf("storage identity missing %s", key)
		}
	}
	if !sourceIdentityDigest.MatchString(value.SourceIdentity) || !filesystemUUID.MatchString(value.FilesystemUUID) {
		return fmt.Errorf("invalid storage filesystem identity")
	}
	*identity = SharedStorageIdentity(value)
	return nil
}

type SharedStorageResource struct {
	StorageRef        string                 `json:"storage_ref"`
	ResourceID        string                 `json:"resource_id"`
	WorkspaceID       string                 `json:"workspace_id"`
	SourceIdentity    string                 `json:"source_identity"`
	ProvisionerID     string                 `json:"provisioner_id"`
	OwnerTenantID     string                 `json:"owner_tenant_id"`
	OwnerUserID       string                 `json:"owner_user_id"`
	OwnerRunID        string                 `json:"owner_run_id"`
	BindingVersion    int64                  `json:"binding_version"`
	BudgetBytes       int64                  `json:"budget_bytes"`
	QuotaMB           int64                  `json:"quota_mb"`
	Persistent        bool                   `json:"persistent"`
	HardQuota         bool                   `json:"hard_quota"`
	HardQuotaVerified bool                   `json:"hard_quota_verified"`
	SharedAttachment  bool                   `json:"shared_attachment"`
	Identity          *SharedStorageIdentity `json:"identity,omitempty"`
}

func (resource *SharedStorageResource) UnmarshalJSON(data []byte) error {
	type plain SharedStorageResource
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"storage_ref", "resource_id", "workspace_id", "source_identity", "provisioner_id", "owner_tenant_id", "owner_user_id", "owner_run_id", "binding_version", "budget_bytes", "quota_mb", "persistent", "hard_quota", "hard_quota_verified", "shared_attachment"} {
		if item, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return fmt.Errorf("storage evidence missing %s", key)
		}
	}
	if !opaqueStorageKey.MatchString(value.StorageRef) || !opaqueStorageKey.MatchString(value.ResourceID) || value.WorkspaceID != value.ResourceID || !sourceIdentityDigest.MatchString(value.SourceIdentity) || value.ProvisionerID == "" || value.OwnerTenantID == "" || value.OwnerUserID == "" || value.OwnerRunID == "" || value.BindingVersion < 1 || value.BindingVersion > maxSafeInteger || value.BudgetBytes < 1 || value.BudgetBytes > maxSafeInteger || value.QuotaMB < 32 || value.QuotaMB > maxSafeInteger/1048576 {
		return fmt.Errorf("invalid shared storage evidence")
	}
	if value.Identity != nil && (value.Identity.SourceIdentity != value.SourceIdentity || value.Identity.Filesystem != "ext4" || value.Identity.CapacityBytes <= 0 || value.Identity.CapacityBytes > value.BudgetBytes || value.Identity.InodeCapacity < 1 || value.Identity.InodeCapacity > 65536 || value.Identity.RootInode < 1 || value.Identity.BackingInode < 1 || value.Identity.DeviceID < 0 || value.Identity.BackingDeviceID < 0) {
		return fmt.Errorf("invalid storage identity evidence")
	}
	*resource = SharedStorageResource(value)
	return nil
}
