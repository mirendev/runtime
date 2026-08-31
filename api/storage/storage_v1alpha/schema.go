package storage_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

type DiskFilesystem string

const (
	DiskFilesystemExt4  DiskFilesystem = "ext4"
	DiskFilesystemXfs   DiskFilesystem = "xfs"
	DiskFilesystemBtrfs DiskFilesystem = "btrfs"
)
const (
	DiskFilesystemExt4MemberId  = entity.Id("dev.miren.storage/filesystem.ext4")
	DiskFilesystemXfsMemberId   = entity.Id("dev.miren.storage/filesystem.xfs")
	DiskFilesystemBtrfsMemberId = entity.Id("dev.miren.storage/filesystem.btrfs")
)

func initEnumMembers(sb *schema.SchemaBuilder) {
	sb.Singleton("dev.miren.storage/filesystem.ext4")
	sb.Singleton("dev.miren.storage/filesystem.xfs")
	sb.Singleton("dev.miren.storage/filesystem.btrfs")
}

const (
	DiskCreatedById          = entity.Id("dev.miren.storage/disk.created_by")
	DiskFilesystemId         = entity.Id("dev.miren.storage/disk.filesystem")
	DiskFilesystemExt4Id     = DiskFilesystemExt4MemberId
	DiskFilesystemXfsId      = DiskFilesystemXfsMemberId
	DiskFilesystemBtrfsId    = DiskFilesystemBtrfsMemberId
	DiskModeId               = entity.Id("dev.miren.storage/disk.mode")
	DiskModeUniversalId      = entity.Id("dev.miren.storage/mode.universal")
	DiskModeAcceleratorId    = entity.Id("dev.miren.storage/mode.accelerator")
	DiskNameId               = entity.Id("dev.miren.storage/disk.name")
	DiskRemoteOnlyId         = entity.Id("dev.miren.storage/disk.remote_only")
	DiskSizeGbId             = entity.Id("dev.miren.storage/disk.size_gb")
	DiskStatusId             = entity.Id("dev.miren.storage/disk.status")
	DiskStatusProvisioningId = entity.Id("dev.miren.storage/status.provisioning")
	DiskStatusProvisionedId  = entity.Id("dev.miren.storage/status.provisioned")
	DiskStatusAttachedId     = entity.Id("dev.miren.storage/status.attached")
	DiskStatusDetachedId     = entity.Id("dev.miren.storage/status.detached")
	DiskStatusDeletingId     = entity.Id("dev.miren.storage/status.deleting")
	DiskStatusErrorId        = entity.Id("dev.miren.storage/status.error")
	DiskStatusRestoringId    = entity.Id("dev.miren.storage/status.restoring")
	DiskVolumeIdId           = entity.Id("dev.miren.storage/disk.volume_id")
)

type Disk struct {
	ID         entity.Id      `json:"id"`
	CreatedBy  entity.Id      `cbor:"created_by,omitempty" json:"created_by,omitempty"`
	Filesystem DiskFilesystem `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	Mode       DiskMode       `cbor:"mode,omitempty" json:"mode,omitempty"`
	Name       string         `cbor:"name" json:"name"`
	RemoteOnly bool           `cbor:"remote_only,omitempty" json:"remote_only,omitempty"`
	SizeGb     int64          `cbor:"size_gb" json:"size_gb"`
	Status     DiskStatus     `cbor:"status,omitempty" json:"status,omitempty"`
	VolumeId   string         `cbor:"volume_id,omitempty" json:"volume_id,omitempty"`
}

const (
	EXT4  DiskFilesystem = DiskFilesystemExt4
	XFS   DiskFilesystem = DiskFilesystemXfs
	BTRFS DiskFilesystem = DiskFilesystemBtrfs
)

var DiskFilesystemFromId = map[entity.Id]DiskFilesystem{DiskFilesystemExt4MemberId: DiskFilesystemExt4, DiskFilesystemXfsMemberId: DiskFilesystemXfs, DiskFilesystemBtrfsMemberId: DiskFilesystemBtrfs}
var DiskFilesystemToId = map[DiskFilesystem]entity.Id{DiskFilesystemExt4: DiskFilesystemExt4MemberId, DiskFilesystemXfs: DiskFilesystemXfsMemberId, DiskFilesystemBtrfs: DiskFilesystemBtrfsMemberId}

type DiskMode string

const (
	UNIVERSAL   DiskMode = "mode.universal"
	ACCELERATOR DiskMode = "mode.accelerator"
)

var diskmodeFromId = map[entity.Id]DiskMode{DiskModeUniversalId: UNIVERSAL, DiskModeAcceleratorId: ACCELERATOR}
var diskmodeToId = map[DiskMode]entity.Id{UNIVERSAL: DiskModeUniversalId, ACCELERATOR: DiskModeAcceleratorId}

type DiskStatus string

const (
	PROVISIONING DiskStatus = "status.provisioning"
	PROVISIONED  DiskStatus = "status.provisioned"
	ATTACHED     DiskStatus = "status.attached"
	DETACHED     DiskStatus = "status.detached"
	DELETING     DiskStatus = "status.deleting"
	ERROR        DiskStatus = "status.error"
	RESTORING    DiskStatus = "status.restoring"
)

var diskstatusFromId = map[entity.Id]DiskStatus{DiskStatusProvisioningId: PROVISIONING, DiskStatusProvisionedId: PROVISIONED, DiskStatusAttachedId: ATTACHED, DiskStatusDetachedId: DETACHED, DiskStatusDeletingId: DELETING, DiskStatusErrorId: ERROR, DiskStatusRestoringId: RESTORING}
var diskstatusToId = map[DiskStatus]entity.Id{PROVISIONING: DiskStatusProvisioningId, PROVISIONED: DiskStatusProvisionedId, ATTACHED: DiskStatusAttachedId, DETACHED: DiskStatusDetachedId, DELETING: DiskStatusDeletingId, ERROR: DiskStatusErrorId, RESTORING: DiskStatusRestoringId}

func (o *Disk) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskCreatedById); ok && a.Value.Kind() == entity.KindId {
		o.CreatedBy = a.Value.Id()
	}
	if a, ok := e.Get(DiskFilesystemId); ok && a.Value.Kind() == entity.KindId {
		o.Filesystem = DiskFilesystemFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskModeId); ok && a.Value.Kind() == entity.KindId {
		o.Mode = diskmodeFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DiskRemoteOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.RemoteOnly = a.Value.Bool()
	}
	if a, ok := e.Get(DiskSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
	if a, ok := e.Get(DiskStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = diskstatusFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeIdId); ok && a.Value.Kind() == entity.KindString {
		o.VolumeId = a.Value.String()
	}
}

func (o *Disk) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDisk)
}

func (o *Disk) ShortKind() string {
	return "disk"
}

func (o *Disk) Kind() entity.Id {
	return KindDisk
}

func (o *Disk) EntityId() entity.Id {
	return o.ID
}

func (o *Disk) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.CreatedBy) {
		attrs = append(attrs, entity.Ref(DiskCreatedById, o.CreatedBy))
	}
	if a, ok := DiskFilesystemToId[o.Filesystem]; ok {
		attrs = append(attrs, entity.Ref(DiskFilesystemId, a))
	}
	if a, ok := diskmodeToId[o.Mode]; ok {
		attrs = append(attrs, entity.Ref(DiskModeId, a))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DiskNameId, o.Name))
	}
	attrs = append(attrs, entity.Bool(DiskRemoteOnlyId, o.RemoteOnly))
	attrs = append(attrs, entity.Int64(DiskSizeGbId, o.SizeGb))
	if a, ok := diskstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(DiskStatusId, a))
	}
	if !entity.Empty(o.VolumeId) {
		attrs = append(attrs, entity.String(DiskVolumeIdId, o.VolumeId))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDisk))
	return
}

func (o *Disk) Empty() bool {
	if !entity.Empty(o.CreatedBy) {
		return false
	}
	if o.Filesystem != "" {
		return false
	}
	if o.Mode != "" {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.RemoteOnly) {
		return false
	}
	if !entity.Empty(o.SizeGb) {
		return false
	}
	if o.Status != "" {
		return false
	}
	if !entity.Empty(o.VolumeId) {
		return false
	}
	return true
}

func (o *Disk) InitSchema(sb *schema.SchemaBuilder) {
	initEnumMembers(sb)
	sb.Ref("created_by", "dev.miren.storage/disk.created_by", schema.Doc("Application that created this disk (for tracking purposes)"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Enum("filesystem", "dev.miren.storage/disk.filesystem", []any{DiskFilesystemExt4MemberId, DiskFilesystemXfsMemberId, DiskFilesystemBtrfsMemberId}, schema.Doc("Filesystem type for the disk"), schema.ElementType(entity.TypeRef))
	sb.Singleton("dev.miren.storage/mode.universal")
	sb.Singleton("dev.miren.storage/mode.accelerator")
	sb.Ref("mode", "dev.miren.storage/disk.mode", schema.Doc("Disk I/O mode"), schema.Indexed, schema.Choices(DiskModeUniversalId, DiskModeAcceleratorId))
	sb.String("name", "dev.miren.storage/disk.name", schema.Doc("Human-readable name for the disk"), schema.Required, schema.Indexed)
	sb.Bool("remote_only", "dev.miren.storage/disk.remote_only", schema.Doc("If true, disk is stored only remotely without local replica"))
	sb.Int64("size_gb", "dev.miren.storage/disk.size_gb", schema.Doc("Storage capacity in gigabytes"), schema.Required)
	sb.Singleton("dev.miren.storage/status.provisioning")
	sb.Singleton("dev.miren.storage/status.provisioned")
	sb.Singleton("dev.miren.storage/status.attached")
	sb.Singleton("dev.miren.storage/status.detached")
	sb.Singleton("dev.miren.storage/status.deleting")
	sb.Singleton("dev.miren.storage/status.error")
	sb.Singleton("dev.miren.storage/status.restoring")
	sb.Ref("status", "dev.miren.storage/disk.status", schema.Doc("Current state of the disk"), schema.Indexed, schema.Choices(DiskStatusProvisioningId, DiskStatusProvisionedId, DiskStatusAttachedId, DiskStatusDetachedId, DiskStatusDeletingId, DiskStatusErrorId, DiskStatusRestoringId))
	sb.String("volume_id", "dev.miren.storage/disk.volume_id", schema.Doc("Volume identifier for universal/accelerator mode disks"), schema.Indexed)
}

const (
	DiskLeaseAcquiredAtId     = entity.Id("dev.miren.storage/disk_lease.acquired_at")
	DiskLeaseAppIdId          = entity.Id("dev.miren.storage/disk_lease.app_id")
	DiskLeaseDiskIdId         = entity.Id("dev.miren.storage/disk_lease.disk_id")
	DiskLeaseErrorMessageId   = entity.Id("dev.miren.storage/disk_lease.error_message")
	DiskLeaseMountId          = entity.Id("dev.miren.storage/disk_lease.mount")
	DiskLeaseNodeIdId         = entity.Id("dev.miren.storage/disk_lease.node_id")
	DiskLeaseSandboxIdId      = entity.Id("dev.miren.storage/disk_lease.sandbox_id")
	DiskLeaseStatusId         = entity.Id("dev.miren.storage/disk_lease.status")
	DiskLeaseStatusPendingId  = entity.Id("dev.miren.storage/status.pending")
	DiskLeaseStatusBoundId    = entity.Id("dev.miren.storage/status.bound")
	DiskLeaseStatusFailedId   = entity.Id("dev.miren.storage/status.failed")
	DiskLeaseStatusReleasedId = entity.Id("dev.miren.storage/status.released")
)

type DiskLease struct {
	ID           entity.Id       `json:"id"`
	AcquiredAt   time.Time       `cbor:"acquired_at,omitempty" json:"acquired_at"`
	AppId        entity.Id       `cbor:"app_id,omitempty" json:"app_id,omitempty"`
	DiskId       entity.Id       `cbor:"disk_id" json:"disk_id"`
	ErrorMessage string          `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Mount        Mount           `cbor:"mount,omitempty" json:"mount"`
	NodeId       entity.Id       `cbor:"node_id" json:"node_id"`
	SandboxId    entity.Id       `cbor:"sandbox_id,omitempty" json:"sandbox_id,omitempty"`
	Status       DiskLeaseStatus `cbor:"status,omitempty" json:"status,omitempty"`
}

type DiskLeaseStatus string

const (
	PENDING  DiskLeaseStatus = "status.pending"
	BOUND    DiskLeaseStatus = "status.bound"
	FAILED   DiskLeaseStatus = "status.failed"
	RELEASED DiskLeaseStatus = "status.released"
)

var disk_leasestatusFromId = map[entity.Id]DiskLeaseStatus{DiskLeaseStatusPendingId: PENDING, DiskLeaseStatusBoundId: BOUND, DiskLeaseStatusFailedId: FAILED, DiskLeaseStatusReleasedId: RELEASED}
var disk_leasestatusToId = map[DiskLeaseStatus]entity.Id{PENDING: DiskLeaseStatusPendingId, BOUND: DiskLeaseStatusBoundId, FAILED: DiskLeaseStatusFailedId, RELEASED: DiskLeaseStatusReleasedId}

func (o *DiskLease) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskLeaseAcquiredAtId); ok && a.Value.Kind() == entity.KindTime {
		o.AcquiredAt = a.Value.Time()
	}
	if a, ok := e.Get(DiskLeaseAppIdId); ok && a.Value.Kind() == entity.KindId {
		o.AppId = a.Value.Id()
	}
	if a, ok := e.Get(DiskLeaseDiskIdId); ok && a.Value.Kind() == entity.KindId {
		o.DiskId = a.Value.Id()
	}
	if a, ok := e.Get(DiskLeaseErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(DiskLeaseMountId); ok && a.Value.Kind() == entity.KindComponent {
		o.Mount.Decode(a.Value.Component())
	}
	if a, ok := e.Get(DiskLeaseNodeIdId); ok && a.Value.Kind() == entity.KindId {
		o.NodeId = a.Value.Id()
	}
	if a, ok := e.Get(DiskLeaseSandboxIdId); ok && a.Value.Kind() == entity.KindId {
		o.SandboxId = a.Value.Id()
	}
	if a, ok := e.Get(DiskLeaseStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = disk_leasestatusFromId[a.Value.Id()]
	}
}

func (o *DiskLease) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDiskLease)
}

func (o *DiskLease) ShortKind() string {
	return "disk_lease"
}

func (o *DiskLease) Kind() entity.Id {
	return KindDiskLease
}

func (o *DiskLease) EntityId() entity.Id {
	return o.ID
}

func (o *DiskLease) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AcquiredAt) {
		attrs = append(attrs, entity.Time(DiskLeaseAcquiredAtId, o.AcquiredAt))
	}
	if !entity.Empty(o.AppId) {
		attrs = append(attrs, entity.Ref(DiskLeaseAppIdId, o.AppId))
	}
	if !entity.Empty(o.DiskId) {
		attrs = append(attrs, entity.Ref(DiskLeaseDiskIdId, o.DiskId))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(DiskLeaseErrorMessageId, o.ErrorMessage))
	}
	if !o.Mount.Empty() {
		attrs = append(attrs, entity.Component(DiskLeaseMountId, o.Mount.Encode()))
	}
	if !entity.Empty(o.NodeId) {
		attrs = append(attrs, entity.Ref(DiskLeaseNodeIdId, o.NodeId))
	}
	if !entity.Empty(o.SandboxId) {
		attrs = append(attrs, entity.Ref(DiskLeaseSandboxIdId, o.SandboxId))
	}
	if a, ok := disk_leasestatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(DiskLeaseStatusId, a))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDiskLease))
	return
}

func (o *DiskLease) Empty() bool {
	if !entity.Empty(o.AcquiredAt) {
		return false
	}
	if !entity.Empty(o.AppId) {
		return false
	}
	if !entity.Empty(o.DiskId) {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if !o.Mount.Empty() {
		return false
	}
	if !entity.Empty(o.NodeId) {
		return false
	}
	if !entity.Empty(o.SandboxId) {
		return false
	}
	if o.Status != "" {
		return false
	}
	return true
}

func (o *DiskLease) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("acquired_at", "dev.miren.storage/disk_lease.acquired_at", schema.Doc("When the lease was acquired"))
	sb.Ref("app_id", "dev.miren.storage/disk_lease.app_id", schema.Doc("Reference to the application (for debugging)"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("disk_id", "dev.miren.storage/disk_lease.disk_id", schema.Doc("Reference to the leased disk"), schema.Required, schema.Indexed)
	sb.String("error_message", "dev.miren.storage/disk_lease.error_message", schema.Doc("Error details if lease binding failed"))
	sb.Component("mount", "dev.miren.storage/disk_lease.mount", schema.Doc("Mount configuration for the disk"))
	(&Mount{}).InitSchema(sb.Builder("disk_lease.mount"))
	sb.Ref("node_id", "dev.miren.storage/disk_lease.node_id", schema.Doc("Node where the disk is mounted"), schema.Required)
	sb.Ref("sandbox_id", "dev.miren.storage/disk_lease.sandbox_id", schema.Doc("Reference to the sandbox using the disk"), schema.Indexed)
	sb.Singleton("dev.miren.storage/status.pending")
	sb.Singleton("dev.miren.storage/status.bound")
	sb.Singleton("dev.miren.storage/status.failed")
	sb.Singleton("dev.miren.storage/status.released")
	sb.Ref("status", "dev.miren.storage/disk_lease.status", schema.Doc("Current state of the lease"), schema.Indexed, schema.Choices(DiskLeaseStatusPendingId, DiskLeaseStatusBoundId, DiskLeaseStatusFailedId, DiskLeaseStatusReleasedId))
}

const (
	MountOptionsId  = entity.Id("dev.miren.storage/mount.options")
	MountPathId     = entity.Id("dev.miren.storage/mount.path")
	MountReadOnlyId = entity.Id("dev.miren.storage/mount.read_only")
)

type Mount struct {
	Options  string `cbor:"options,omitempty" json:"options,omitempty"`
	Path     string `cbor:"path" json:"path"`
	ReadOnly bool   `cbor:"read_only,omitempty" json:"read_only,omitempty"`
}

func (o *Mount) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(MountOptionsId); ok && a.Value.Kind() == entity.KindString {
		o.Options = a.Value.String()
	}
	if a, ok := e.Get(MountPathId); ok && a.Value.Kind() == entity.KindString {
		o.Path = a.Value.String()
	}
	if a, ok := e.Get(MountReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
}

func (o *Mount) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.Options) {
		attrs = append(attrs, entity.String(MountOptionsId, o.Options))
	}
	if !entity.Empty(o.Path) {
		attrs = append(attrs, entity.String(MountPathId, o.Path))
	}
	attrs = append(attrs, entity.Bool(MountReadOnlyId, o.ReadOnly))
	return
}

func (o *Mount) Empty() bool {
	if !entity.Empty(o.Options) {
		return false
	}
	if !entity.Empty(o.Path) {
		return false
	}
	if !entity.Empty(o.ReadOnly) {
		return false
	}
	return true
}

func (o *Mount) InitSchema(sb *schema.SchemaBuilder) {
	sb.String("options", "dev.miren.storage/mount.options", schema.Doc("Mount options (e.g., \"rw,noatime\")"))
	sb.String("path", "dev.miren.storage/mount.path", schema.Doc("Mount path in the container"), schema.Required)
	sb.Bool("read_only", "dev.miren.storage/mount.read_only", schema.Doc("Whether the mount is read-only"))
}

const (
	DiskMountActualStateId                 = entity.Id("dev.miren.storage/disk_mount.actual_state")
	DiskMountActualStateDmPendingId        = entity.Id("dev.miren.storage/actual_state.dm_pending")
	DiskMountActualStateDmAttachingId      = entity.Id("dev.miren.storage/actual_state.dm_attaching")
	DiskMountActualStateDmAttachedId       = entity.Id("dev.miren.storage/actual_state.dm_attached")
	DiskMountActualStateDmMountingId       = entity.Id("dev.miren.storage/actual_state.dm_mounting")
	DiskMountActualStateDmMountedId        = entity.Id("dev.miren.storage/actual_state.dm_mounted")
	DiskMountActualStateDmUnmountingId     = entity.Id("dev.miren.storage/actual_state.dm_unmounting")
	DiskMountActualStateDmDetachingId      = entity.Id("dev.miren.storage/actual_state.dm_detaching")
	DiskMountActualStateDmDetachedId       = entity.Id("dev.miren.storage/actual_state.dm_detached")
	DiskMountActualStateDmErrorId          = entity.Id("dev.miren.storage/actual_state.dm_error")
	DiskMountDesiredStateId                = entity.Id("dev.miren.storage/disk_mount.desired_state")
	DiskMountDesiredStateDmWantMountedId   = entity.Id("dev.miren.storage/desired_state.dm_want_mounted")
	DiskMountDesiredStateDmWantUnmountedId = entity.Id("dev.miren.storage/desired_state.dm_want_unmounted")
	DiskMountDevicePathId                  = entity.Id("dev.miren.storage/disk_mount.device_path")
	DiskMountDiskLeaseIdId                 = entity.Id("dev.miren.storage/disk_mount.disk_lease_id")
	DiskMountErrorMessageId                = entity.Id("dev.miren.storage/disk_mount.error_message")
	DiskMountLoopDeviceId                  = entity.Id("dev.miren.storage/disk_mount.loop_device")
	DiskMountMountPathId                   = entity.Id("dev.miren.storage/disk_mount.mount_path")
	DiskMountNodeIdId                      = entity.Id("dev.miren.storage/disk_mount.node_id")
	DiskMountReadOnlyId                    = entity.Id("dev.miren.storage/disk_mount.read_only")
	DiskMountVolumeIdId                    = entity.Id("dev.miren.storage/disk_mount.volume_id")
)

type DiskMount struct {
	ID           entity.Id             `json:"id"`
	ActualState  DiskMountActualState  `cbor:"actual_state,omitempty" json:"actual_state,omitempty"`
	DesiredState DiskMountDesiredState `cbor:"desired_state,omitempty" json:"desired_state,omitempty"`
	DevicePath   string                `cbor:"device_path,omitempty" json:"device_path,omitempty"`
	DiskLeaseId  entity.Id             `cbor:"disk_lease_id,omitempty" json:"disk_lease_id,omitempty"`
	ErrorMessage string                `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	LoopDevice   string                `cbor:"loop_device,omitempty" json:"loop_device,omitempty"`
	MountPath    string                `cbor:"mount_path" json:"mount_path"`
	NodeId       entity.Id             `cbor:"node_id" json:"node_id"`
	ReadOnly     bool                  `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	VolumeId     entity.Id             `cbor:"volume_id" json:"volume_id"`
}

type DiskMountActualState string

const (
	DM_PENDING    DiskMountActualState = "actual_state.dm_pending"
	DM_ATTACHING  DiskMountActualState = "actual_state.dm_attaching"
	DM_ATTACHED   DiskMountActualState = "actual_state.dm_attached"
	DM_MOUNTING   DiskMountActualState = "actual_state.dm_mounting"
	DM_MOUNTED    DiskMountActualState = "actual_state.dm_mounted"
	DM_UNMOUNTING DiskMountActualState = "actual_state.dm_unmounting"
	DM_DETACHING  DiskMountActualState = "actual_state.dm_detaching"
	DM_DETACHED   DiskMountActualState = "actual_state.dm_detached"
	DM_ERROR      DiskMountActualState = "actual_state.dm_error"
)

var disk_mountactual_stateFromId = map[entity.Id]DiskMountActualState{DiskMountActualStateDmPendingId: DM_PENDING, DiskMountActualStateDmAttachingId: DM_ATTACHING, DiskMountActualStateDmAttachedId: DM_ATTACHED, DiskMountActualStateDmMountingId: DM_MOUNTING, DiskMountActualStateDmMountedId: DM_MOUNTED, DiskMountActualStateDmUnmountingId: DM_UNMOUNTING, DiskMountActualStateDmDetachingId: DM_DETACHING, DiskMountActualStateDmDetachedId: DM_DETACHED, DiskMountActualStateDmErrorId: DM_ERROR}
var disk_mountactual_stateToId = map[DiskMountActualState]entity.Id{DM_PENDING: DiskMountActualStateDmPendingId, DM_ATTACHING: DiskMountActualStateDmAttachingId, DM_ATTACHED: DiskMountActualStateDmAttachedId, DM_MOUNTING: DiskMountActualStateDmMountingId, DM_MOUNTED: DiskMountActualStateDmMountedId, DM_UNMOUNTING: DiskMountActualStateDmUnmountingId, DM_DETACHING: DiskMountActualStateDmDetachingId, DM_DETACHED: DiskMountActualStateDmDetachedId, DM_ERROR: DiskMountActualStateDmErrorId}

type DiskMountDesiredState string

const (
	DM_WANT_MOUNTED   DiskMountDesiredState = "desired_state.dm_want_mounted"
	DM_WANT_UNMOUNTED DiskMountDesiredState = "desired_state.dm_want_unmounted"
)

var disk_mountdesired_stateFromId = map[entity.Id]DiskMountDesiredState{DiskMountDesiredStateDmWantMountedId: DM_WANT_MOUNTED, DiskMountDesiredStateDmWantUnmountedId: DM_WANT_UNMOUNTED}
var disk_mountdesired_stateToId = map[DiskMountDesiredState]entity.Id{DM_WANT_MOUNTED: DiskMountDesiredStateDmWantMountedId, DM_WANT_UNMOUNTED: DiskMountDesiredStateDmWantUnmountedId}

func (o *DiskMount) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskMountActualStateId); ok && a.Value.Kind() == entity.KindId {
		o.ActualState = disk_mountactual_stateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskMountDesiredStateId); ok && a.Value.Kind() == entity.KindId {
		o.DesiredState = disk_mountdesired_stateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskMountDevicePathId); ok && a.Value.Kind() == entity.KindString {
		o.DevicePath = a.Value.String()
	}
	if a, ok := e.Get(DiskMountDiskLeaseIdId); ok && a.Value.Kind() == entity.KindId {
		o.DiskLeaseId = a.Value.Id()
	}
	if a, ok := e.Get(DiskMountErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(DiskMountLoopDeviceId); ok && a.Value.Kind() == entity.KindString {
		o.LoopDevice = a.Value.String()
	}
	if a, ok := e.Get(DiskMountMountPathId); ok && a.Value.Kind() == entity.KindString {
		o.MountPath = a.Value.String()
	}
	if a, ok := e.Get(DiskMountNodeIdId); ok && a.Value.Kind() == entity.KindId {
		o.NodeId = a.Value.Id()
	}
	if a, ok := e.Get(DiskMountReadOnlyId); ok && a.Value.Kind() == entity.KindBool {
		o.ReadOnly = a.Value.Bool()
	}
	if a, ok := e.Get(DiskMountVolumeIdId); ok && a.Value.Kind() == entity.KindId {
		o.VolumeId = a.Value.Id()
	}
}

func (o *DiskMount) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDiskMount)
}

func (o *DiskMount) ShortKind() string {
	return "disk_mount"
}

func (o *DiskMount) Kind() entity.Id {
	return KindDiskMount
}

func (o *DiskMount) EntityId() entity.Id {
	return o.ID
}

func (o *DiskMount) Encode() (attrs []entity.Attr) {
	if a, ok := disk_mountactual_stateToId[o.ActualState]; ok {
		attrs = append(attrs, entity.Ref(DiskMountActualStateId, a))
	}
	if a, ok := disk_mountdesired_stateToId[o.DesiredState]; ok {
		attrs = append(attrs, entity.Ref(DiskMountDesiredStateId, a))
	}
	if !entity.Empty(o.DevicePath) {
		attrs = append(attrs, entity.String(DiskMountDevicePathId, o.DevicePath))
	}
	if !entity.Empty(o.DiskLeaseId) {
		attrs = append(attrs, entity.Ref(DiskMountDiskLeaseIdId, o.DiskLeaseId))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(DiskMountErrorMessageId, o.ErrorMessage))
	}
	if !entity.Empty(o.LoopDevice) {
		attrs = append(attrs, entity.String(DiskMountLoopDeviceId, o.LoopDevice))
	}
	if !entity.Empty(o.MountPath) {
		attrs = append(attrs, entity.String(DiskMountMountPathId, o.MountPath))
	}
	if !entity.Empty(o.NodeId) {
		attrs = append(attrs, entity.Ref(DiskMountNodeIdId, o.NodeId))
	}
	attrs = append(attrs, entity.Bool(DiskMountReadOnlyId, o.ReadOnly))
	if !entity.Empty(o.VolumeId) {
		attrs = append(attrs, entity.Ref(DiskMountVolumeIdId, o.VolumeId))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDiskMount))
	return
}

func (o *DiskMount) Empty() bool {
	if o.ActualState != "" {
		return false
	}
	if o.DesiredState != "" {
		return false
	}
	if !entity.Empty(o.DevicePath) {
		return false
	}
	if !entity.Empty(o.DiskLeaseId) {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if !entity.Empty(o.LoopDevice) {
		return false
	}
	if !entity.Empty(o.MountPath) {
		return false
	}
	if !entity.Empty(o.NodeId) {
		return false
	}
	if !entity.Empty(o.ReadOnly) {
		return false
	}
	if !entity.Empty(o.VolumeId) {
		return false
	}
	return true
}

func (o *DiskMount) InitSchema(sb *schema.SchemaBuilder) {
	sb.Singleton("dev.miren.storage/actual_state.dm_pending")
	sb.Singleton("dev.miren.storage/actual_state.dm_attaching")
	sb.Singleton("dev.miren.storage/actual_state.dm_attached")
	sb.Singleton("dev.miren.storage/actual_state.dm_mounting")
	sb.Singleton("dev.miren.storage/actual_state.dm_mounted")
	sb.Singleton("dev.miren.storage/actual_state.dm_unmounting")
	sb.Singleton("dev.miren.storage/actual_state.dm_detaching")
	sb.Singleton("dev.miren.storage/actual_state.dm_detached")
	sb.Singleton("dev.miren.storage/actual_state.dm_error")
	sb.Ref("actual_state", "dev.miren.storage/disk_mount.actual_state", schema.Doc("Current state of the mount"), schema.Indexed, schema.Choices(DiskMountActualStateDmPendingId, DiskMountActualStateDmAttachingId, DiskMountActualStateDmAttachedId, DiskMountActualStateDmMountingId, DiskMountActualStateDmMountedId, DiskMountActualStateDmUnmountingId, DiskMountActualStateDmDetachingId, DiskMountActualStateDmDetachedId, DiskMountActualStateDmErrorId))
	sb.Singleton("dev.miren.storage/desired_state.dm_want_mounted")
	sb.Singleton("dev.miren.storage/desired_state.dm_want_unmounted")
	sb.Ref("desired_state", "dev.miren.storage/disk_mount.desired_state", schema.Doc("What state should this mount be in"), schema.Indexed, schema.Choices(DiskMountDesiredStateDmWantMountedId, DiskMountDesiredStateDmWantUnmountedId))
	sb.String("device_path", "dev.miren.storage/disk_mount.device_path", schema.Doc("Full path to the device node (e.g., /dev/loopN)"))
	sb.Ref("disk_lease_id", "dev.miren.storage/disk_mount.disk_lease_id", schema.Doc("Reference to the parent DiskLease entity"), schema.Indexed)
	sb.String("error_message", "dev.miren.storage/disk_mount.error_message", schema.Doc("Error details if actual_state is error"))
	sb.String("loop_device", "dev.miren.storage/disk_mount.loop_device", schema.Doc("Loop device name for universal mode"))
	sb.String("mount_path", "dev.miren.storage/disk_mount.mount_path", schema.Doc("Path where the volume should be mounted"), schema.Required)
	sb.Ref("node_id", "dev.miren.storage/disk_mount.node_id", schema.Doc("Node where this mount exists"), schema.Required, schema.Indexed)
	sb.Bool("read_only", "dev.miren.storage/disk_mount.read_only", schema.Doc("Whether the mount is read-only"))
	sb.Ref("volume_id", "dev.miren.storage/disk_mount.volume_id", schema.Doc("Reference to the disk_volume entity"), schema.Required, schema.Indexed)
}

const (
	DiskVolumeActualStateId             = entity.Id("dev.miren.storage/disk_volume.actual_state")
	DiskVolumeActualStateDvPendingId    = entity.Id("dev.miren.storage/actual_state.dv_pending")
	DiskVolumeActualStateDvCreatingId   = entity.Id("dev.miren.storage/actual_state.dv_creating")
	DiskVolumeActualStateDvReadyId      = entity.Id("dev.miren.storage/actual_state.dv_ready")
	DiskVolumeActualStateDvDeletingId   = entity.Id("dev.miren.storage/actual_state.dv_deleting")
	DiskVolumeActualStateDvDeletedId    = entity.Id("dev.miren.storage/actual_state.dv_deleted")
	DiskVolumeActualStateDvErrorId      = entity.Id("dev.miren.storage/actual_state.dv_error")
	DiskVolumeCloudVolumeIdId           = entity.Id("dev.miren.storage/disk_volume.cloud_volume_id")
	DiskVolumeDesiredStateId            = entity.Id("dev.miren.storage/disk_volume.desired_state")
	DiskVolumeDesiredStateDvPresentId   = entity.Id("dev.miren.storage/desired_state.dv_present")
	DiskVolumeDesiredStateDvAbsentId    = entity.Id("dev.miren.storage/desired_state.dv_absent")
	DiskVolumeDiskIdId                  = entity.Id("dev.miren.storage/disk_volume.disk_id")
	DiskVolumeErrorMessageId            = entity.Id("dev.miren.storage/disk_volume.error_message")
	DiskVolumeFilesystemId              = entity.Id("dev.miren.storage/disk_volume.filesystem")
	DiskVolumeImagePathId               = entity.Id("dev.miren.storage/disk_volume.image_path")
	DiskVolumeMountIdId                 = entity.Id("dev.miren.storage/disk_volume.mount_id")
	DiskVolumeNameId                    = entity.Id("dev.miren.storage/disk_volume.name")
	DiskVolumeNodeIdId                  = entity.Id("dev.miren.storage/disk_volume.node_id")
	DiskVolumeSizeGbId                  = entity.Id("dev.miren.storage/disk_volume.size_gb")
	DiskVolumeVolumeIdId                = entity.Id("dev.miren.storage/disk_volume.volume_id")
	DiskVolumeVolumeModeId              = entity.Id("dev.miren.storage/disk_volume.volume_mode")
	DiskVolumeVolumeModeVmUniversalId   = entity.Id("dev.miren.storage/volume_mode.vm_universal")
	DiskVolumeVolumeModeVmAcceleratorId = entity.Id("dev.miren.storage/volume_mode.vm_accelerator")
)

type DiskVolume struct {
	ID            entity.Id              `json:"id"`
	ActualState   DiskVolumeActualState  `cbor:"actual_state,omitempty" json:"actual_state,omitempty"`
	CloudVolumeId string                 `cbor:"cloud_volume_id,omitempty" json:"cloud_volume_id,omitempty"`
	DesiredState  DiskVolumeDesiredState `cbor:"desired_state,omitempty" json:"desired_state,omitempty"`
	DiskId        entity.Id              `cbor:"disk_id" json:"disk_id"`
	ErrorMessage  string                 `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Filesystem    DiskFilesystem         `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	ImagePath     string                 `cbor:"image_path,omitempty" json:"image_path,omitempty"`
	MountId       string                 `cbor:"mount_id,omitempty" json:"mount_id,omitempty"`
	Name          string                 `cbor:"name,omitempty" json:"name,omitempty"`
	NodeId        entity.Id              `cbor:"node_id" json:"node_id"`
	SizeGb        int64                  `cbor:"size_gb" json:"size_gb"`
	VolumeId      string                 `cbor:"volume_id,omitempty" json:"volume_id,omitempty"`
	VolumeMode    DiskVolumeVolumeMode   `cbor:"volume_mode,omitempty" json:"volume_mode,omitempty"`
}

type DiskVolumeActualState string

const (
	DV_PENDING  DiskVolumeActualState = "actual_state.dv_pending"
	DV_CREATING DiskVolumeActualState = "actual_state.dv_creating"
	DV_READY    DiskVolumeActualState = "actual_state.dv_ready"
	DV_DELETING DiskVolumeActualState = "actual_state.dv_deleting"
	DV_DELETED  DiskVolumeActualState = "actual_state.dv_deleted"
	DV_ERROR    DiskVolumeActualState = "actual_state.dv_error"
)

var disk_volumeactual_stateFromId = map[entity.Id]DiskVolumeActualState{DiskVolumeActualStateDvPendingId: DV_PENDING, DiskVolumeActualStateDvCreatingId: DV_CREATING, DiskVolumeActualStateDvReadyId: DV_READY, DiskVolumeActualStateDvDeletingId: DV_DELETING, DiskVolumeActualStateDvDeletedId: DV_DELETED, DiskVolumeActualStateDvErrorId: DV_ERROR}
var disk_volumeactual_stateToId = map[DiskVolumeActualState]entity.Id{DV_PENDING: DiskVolumeActualStateDvPendingId, DV_CREATING: DiskVolumeActualStateDvCreatingId, DV_READY: DiskVolumeActualStateDvReadyId, DV_DELETING: DiskVolumeActualStateDvDeletingId, DV_DELETED: DiskVolumeActualStateDvDeletedId, DV_ERROR: DiskVolumeActualStateDvErrorId}

type DiskVolumeDesiredState string

const (
	DV_PRESENT DiskVolumeDesiredState = "desired_state.dv_present"
	DV_ABSENT  DiskVolumeDesiredState = "desired_state.dv_absent"
)

var disk_volumedesired_stateFromId = map[entity.Id]DiskVolumeDesiredState{DiskVolumeDesiredStateDvPresentId: DV_PRESENT, DiskVolumeDesiredStateDvAbsentId: DV_ABSENT}
var disk_volumedesired_stateToId = map[DiskVolumeDesiredState]entity.Id{DV_PRESENT: DiskVolumeDesiredStateDvPresentId, DV_ABSENT: DiskVolumeDesiredStateDvAbsentId}
var DiskVolumeFilesystemFromId = map[entity.Id]DiskFilesystem{DiskFilesystemExt4MemberId: DiskFilesystemExt4, DiskFilesystemXfsMemberId: DiskFilesystemXfs, DiskFilesystemBtrfsMemberId: DiskFilesystemBtrfs}
var DiskVolumeFilesystemToId = map[DiskFilesystem]entity.Id{DiskFilesystemExt4: DiskFilesystemExt4MemberId, DiskFilesystemXfs: DiskFilesystemXfsMemberId, DiskFilesystemBtrfs: DiskFilesystemBtrfsMemberId}
var DiskVolumeFilesystemFromString = map[string]DiskFilesystem{"ext4": DiskFilesystemExt4, "xfs": DiskFilesystemXfs, "btrfs": DiskFilesystemBtrfs}

type DiskVolumeVolumeMode string

const (
	VM_UNIVERSAL   DiskVolumeVolumeMode = "volume_mode.vm_universal"
	VM_ACCELERATOR DiskVolumeVolumeMode = "volume_mode.vm_accelerator"
)

var disk_volumevolume_modeFromId = map[entity.Id]DiskVolumeVolumeMode{DiskVolumeVolumeModeVmUniversalId: VM_UNIVERSAL, DiskVolumeVolumeModeVmAcceleratorId: VM_ACCELERATOR}
var disk_volumevolume_modeToId = map[DiskVolumeVolumeMode]entity.Id{VM_UNIVERSAL: DiskVolumeVolumeModeVmUniversalId, VM_ACCELERATOR: DiskVolumeVolumeModeVmAcceleratorId}

func (o *DiskVolume) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskVolumeActualStateId); ok && a.Value.Kind() == entity.KindId {
		o.ActualState = disk_volumeactual_stateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeCloudVolumeIdId); ok && a.Value.Kind() == entity.KindString {
		o.CloudVolumeId = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeDesiredStateId); ok && a.Value.Kind() == entity.KindId {
		o.DesiredState = disk_volumedesired_stateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeDiskIdId); ok && a.Value.Kind() == entity.KindId {
		o.DiskId = a.Value.Id()
	}
	if a, ok := e.Get(DiskVolumeErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeFilesystemId); ok && a.Value.Kind() == entity.KindId {
		o.Filesystem = DiskVolumeFilesystemFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeFilesystemId); ok && a.Value.Kind() == entity.KindString {
		o.Filesystem = DiskVolumeFilesystemFromString[a.Value.String()]
	}
	if a, ok := e.Get(DiskVolumeImagePathId); ok && a.Value.Kind() == entity.KindString {
		o.ImagePath = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeMountIdId); ok && a.Value.Kind() == entity.KindString {
		o.MountId = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeNodeIdId); ok && a.Value.Kind() == entity.KindId {
		o.NodeId = a.Value.Id()
	}
	if a, ok := e.Get(DiskVolumeSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
	if a, ok := e.Get(DiskVolumeVolumeIdId); ok && a.Value.Kind() == entity.KindString {
		o.VolumeId = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeVolumeModeId); ok && a.Value.Kind() == entity.KindId {
		o.VolumeMode = disk_volumevolume_modeFromId[a.Value.Id()]
	}
}

func (o *DiskVolume) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindDiskVolume)
}

func (o *DiskVolume) ShortKind() string {
	return "disk_volume"
}

func (o *DiskVolume) Kind() entity.Id {
	return KindDiskVolume
}

func (o *DiskVolume) EntityId() entity.Id {
	return o.ID
}

func (o *DiskVolume) Encode() (attrs []entity.Attr) {
	if a, ok := disk_volumeactual_stateToId[o.ActualState]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeActualStateId, a))
	}
	if !entity.Empty(o.CloudVolumeId) {
		attrs = append(attrs, entity.String(DiskVolumeCloudVolumeIdId, o.CloudVolumeId))
	}
	if a, ok := disk_volumedesired_stateToId[o.DesiredState]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeDesiredStateId, a))
	}
	if !entity.Empty(o.DiskId) {
		attrs = append(attrs, entity.Ref(DiskVolumeDiskIdId, o.DiskId))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(DiskVolumeErrorMessageId, o.ErrorMessage))
	}
	if a, ok := DiskVolumeFilesystemToId[o.Filesystem]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeFilesystemId, a))
	}
	if !entity.Empty(o.ImagePath) {
		attrs = append(attrs, entity.String(DiskVolumeImagePathId, o.ImagePath))
	}
	if !entity.Empty(o.MountId) {
		attrs = append(attrs, entity.String(DiskVolumeMountIdId, o.MountId))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DiskVolumeNameId, o.Name))
	}
	if !entity.Empty(o.NodeId) {
		attrs = append(attrs, entity.Ref(DiskVolumeNodeIdId, o.NodeId))
	}
	attrs = append(attrs, entity.Int64(DiskVolumeSizeGbId, o.SizeGb))
	if !entity.Empty(o.VolumeId) {
		attrs = append(attrs, entity.String(DiskVolumeVolumeIdId, o.VolumeId))
	}
	if a, ok := disk_volumevolume_modeToId[o.VolumeMode]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeVolumeModeId, a))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindDiskVolume))
	return
}

func (o *DiskVolume) Empty() bool {
	if o.ActualState != "" {
		return false
	}
	if !entity.Empty(o.CloudVolumeId) {
		return false
	}
	if o.DesiredState != "" {
		return false
	}
	if !entity.Empty(o.DiskId) {
		return false
	}
	if !entity.Empty(o.ErrorMessage) {
		return false
	}
	if o.Filesystem != "" {
		return false
	}
	if !entity.Empty(o.ImagePath) {
		return false
	}
	if !entity.Empty(o.MountId) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.NodeId) {
		return false
	}
	if !entity.Empty(o.SizeGb) {
		return false
	}
	if !entity.Empty(o.VolumeId) {
		return false
	}
	if o.VolumeMode != "" {
		return false
	}
	return true
}

func (o *DiskVolume) InitSchema(sb *schema.SchemaBuilder) {
	initEnumMembers(sb)
	sb.Singleton("dev.miren.storage/actual_state.dv_pending")
	sb.Singleton("dev.miren.storage/actual_state.dv_creating")
	sb.Singleton("dev.miren.storage/actual_state.dv_ready")
	sb.Singleton("dev.miren.storage/actual_state.dv_deleting")
	sb.Singleton("dev.miren.storage/actual_state.dv_deleted")
	sb.Singleton("dev.miren.storage/actual_state.dv_error")
	sb.Ref("actual_state", "dev.miren.storage/disk_volume.actual_state", schema.Doc("Current state of the volume"), schema.Indexed, schema.Choices(DiskVolumeActualStateDvPendingId, DiskVolumeActualStateDvCreatingId, DiskVolumeActualStateDvReadyId, DiskVolumeActualStateDvDeletingId, DiskVolumeActualStateDvDeletedId, DiskVolumeActualStateDvErrorId))
	sb.String("cloud_volume_id", "dev.miren.storage/disk_volume.cloud_volume_id", schema.Doc("Identifier for this volume in miren.cloud, assigned when the disk controller registers it there (empty until then)"))
	sb.Singleton("dev.miren.storage/desired_state.dv_present")
	sb.Singleton("dev.miren.storage/desired_state.dv_absent")
	sb.Ref("desired_state", "dev.miren.storage/disk_volume.desired_state", schema.Doc("What state should this volume be in"), schema.Indexed, schema.Choices(DiskVolumeDesiredStateDvPresentId, DiskVolumeDesiredStateDvAbsentId))
	sb.Ref("disk_id", "dev.miren.storage/disk_volume.disk_id", schema.Doc("Reference to the parent Disk entity"), schema.Required, schema.Indexed)
	sb.String("error_message", "dev.miren.storage/disk_volume.error_message", schema.Doc("Error details if actual_state is error"))
	sb.Enum("filesystem", "dev.miren.storage/disk_volume.filesystem", []any{DiskFilesystemExt4MemberId, DiskFilesystemXfsMemberId, DiskFilesystemBtrfsMemberId}, schema.Doc("Filesystem type (ext4, xfs, btrfs)"), schema.ElementType(entity.TypeRef))
	sb.String("image_path", "dev.miren.storage/disk_volume.image_path", schema.Doc("Path to backing image file"))
	sb.String("mount_id", "dev.miren.storage/disk_volume.mount_id", schema.Doc("Override for the mount point directory name (defaults to entity suffix if empty)"))
	sb.String("name", "dev.miren.storage/disk_volume.name", schema.Doc("Human-readable name for the volume (from parent disk)"))
	sb.Ref("node_id", "dev.miren.storage/disk_volume.node_id", schema.Doc("Node where this volume should be provisioned"), schema.Required, schema.Indexed)
	sb.Int64("size_gb", "dev.miren.storage/disk_volume.size_gb", schema.Doc("Volume size in gigabytes"), schema.Required)
	sb.String("volume_id", "dev.miren.storage/disk_volume.volume_id", schema.Doc("Volume identifier (generated during creation)"), schema.Indexed)
	sb.Singleton("dev.miren.storage/volume_mode.vm_universal")
	sb.Singleton("dev.miren.storage/volume_mode.vm_accelerator")
	sb.Ref("volume_mode", "dev.miren.storage/disk_volume.volume_mode", schema.Doc("Disk I/O mode"), schema.Choices(DiskVolumeVolumeModeVmUniversalId, DiskVolumeVolumeModeVmAcceleratorId))
}

var (
	KindDisk       = entity.Id("dev.miren.storage/kind.disk")
	KindDiskLease  = entity.Id("dev.miren.storage/kind.disk_lease")
	KindDiskMount  = entity.Id("dev.miren.storage/kind.disk_mount")
	KindDiskVolume = entity.Id("dev.miren.storage/kind.disk_volume")
	Schema         = entity.Id("dev.miren.storage/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.storage", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Disk{}).InitSchema(sb)
		(&DiskLease{}).InitSchema(sb)
		(&DiskMount{}).InitSchema(sb)
		(&DiskVolume{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.storage", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xecX\xcb\xd2\xdc&\x13]\xf9)\xfe?\xf7\xfbM.W\xe5}T\x8chi\x18\t\x18\x03R4\xd9\xd9ޤR\x95\x97\x88\xa7\x9c\xf8\x05\xb3Nр\xc4h\x18\xc4\xe7u6S\x80\xce94MO7p\xa5\x82pxIa\xaa8S *m\xa4\"\x1d@\xcf\x04\xd5\xd7\xf9\x7fw_\x9e\xdb/\x15e\xba\x7f\x87\xdc\xe9\x1ea?:\x81\x7fZ*9a\xe2~\x82\xb6e0P\xfd\xdb\xdb\x03\xa3\xf3gi\x8d\xaaQ@\f\xd0\xfap\xc1\xa9NQ\xdf\\\xcep`\xf4\xef\x1c\xbde\x03\xe8\x8b6\xc0)\x88\x91\xcf_\xdf\xe3\xec8.\xa6\x8e\xc08WԷs\xa1Bo\x7f\xea\x89\f#\xe8\xb7\xcd\xdc\xea\xf9\xd3{ɕXͭ\xa60\x9b\x9fS\x16F0\v\x81\x83Q\xad\x9e?\xcf\x02\x113\xa0\x11\x1c\xf8\x01\x94~\x83\xfa\xd6\x14'\xc0\xf1#\x88FR&\xbaFAkpd\x80\x8e4\x97\x9b\x0fW\xeb\xba\xc4\xfe\xa2븤\x80~\xa0\xd8Jz\xe0O6\n6\x81\xd2dH\xf9\xc1\x12\xab\x05ѓ\xa6\x81\x01\x141R\xa5V\x89\xe8\b\xf36g\x1d\x1a\xb6\xfeX\xebZm\x14\x13\x1d\xd2\x12\xf2HS\xc0\xa5\x81Z\x8a\xc1\xc5S\x1f\x0f\xe0\x12\x0fR\x0e(\xf1\xf1\x03\t\xcd~\x85\xba; \xbd\v\x1dKm\x980\xe8я\x1e1\r1\xa3Fb\xeb\xdbI\xaf\xfe\x05\xa0\x94T)\v\x1c\xad\xc2\xefGb\fi\x8e\x90\x8c~\x0f\f\x90#\x85\x01\f\x13]\x06\x1b G\n\xbb\xba\x01\xc2\x14\xd8/V8\xe1r\x0f^0\xfdYɉi&\x05\xd0\xf9ˇ\xf8\b5,m;\xc5W\xfb\x94\x10\x00\x89h\xc4=\x98\xe40r\xa8\x19\xc5m`k7\x8a\xa0\xce\xc6+\x93\xa2\x9b^\x90\xe1|$\xc3Y1Nԥ\xb6\t\x8dZ\x99\x94g\x96\xa4X\x0f@4\xb8\xd48\xff?m\x87\xc3<)C~\x9bS\xaaH\xf3rd\nhM\x8c\v\xecx\x00\xa3\xcc0\x0e(\xf4E^\xe8|\x0e\xdei}\xdb'Z$'v-\"cӳ\xbbЉ\xe9\xdfg\xe9\x18\xd65\a\xadI\xe7\xfe\xd8\xfcv(ڤk\xe6o\xee\xe5\xb8\x1c\x85\xf3\x06\xb8\xa6\xa5\xb3F\xf2\xb3\x14 \xcc\xda\xf2{u\xafVm\xd5\nw\xec\r.\xf6\x93T\x8e\x1b\x85\xa9\xe4\xd90)\\&\xe8Bg\x9b\xc2\x12\x91\xe3\xd8gb\x8e.\xebak\xcbK\x84\xa6\xe3) t\xcd|l\xed.y/\x1b\xf8·\x05A $]\xfe`]\xe8\xc4A\xf0M\x96\xae\x89\xa0\a9\a\x85S\xd4\xf7\"\xd7\xfd(.M\xb5W8\xc8Q$\x93\xbd\xcf,\xf8\xbdm\t\x1b \xb9\xa3\x1e\xe6\x00\xdd\x19\x84-\xae\xa9\xf4\x132\x95C\x1c\x15\xa0\xa5\xb9$\x1b \xd9m9\xad\xab\xceg%ܾ\x9d\xac\xf4\x94\x18\xff\x1d\xb7ỜRE\x1a3\x92\xa1\xb6\xebq\xff\xe7\xe1f$\xb9%\uf3d4\u05ee\x00&\x02%\xe6W\x01x\xa2ܛ\x9e4h\xcb\xf1P\xcb\n\xfbU\xc0\xf2О\xf2z)\xbc\x89t\xb6\xa5\x05\xac\xe5-\x85\xb5\x80\x17\xb0}0ؚY\xc0\v\xd8a\x99\xdb\x12\x7f(5\xd43\xdd\xec\x85\xcc\x05\xcc)\xafG\xb1X\xfb\xe3>uE_s\xe5\xc1E\x13\x05\x8d\x15m\r'~;\x94>\xa3J\xca\xeb_\x880K\x88<O\xcc\x12\xebT\x1b\xc2\xcb\xd0\xf7\xd6\x02\x9d_\x94J,\x94l\r\x0f\xeb\x9bX\x03\xf5\x92\xdf\xfbx`\x9b\xe6w\\\xb5$\x85\x90G\xf9\xedPIQvRO)\xca\x05\x8b\x1c\xa4<\xd7nan\x91\xf1\xc0V\xeaQ\xa5pR\xf8\xbb\xba\xeb\x14\xf5\xb7B\x8f*\x96\x13ڭX\x89kcD\xdf/\xac\x05\"ك違\x14\x01\x14J\x9d\x89\xd6\"\xe0d}\x15xpO\xf1\xa0\xc22\xf0G\xf6\x8f\xeb\xa4>\xa8\x0e\xbc;ҩ\xb4\x0ex\xa0eX\xef_J\x18\b<ѩ\xc6{OI\xe5X\xa0\x96U\\9\xa6\xb5rL5>_\x14e\xf2\x15ۇ\x89\vy\x01\x8bA\xf7S~c\x9aA\x8e\xb4\xbe\x8d=\xb9\x1dܞ\xba\x13%!\xd6\xfc\xc04\xcd\xe8T\x93\x83\x06a\x92\x87\x8a\xdb\xec\x1a\xa0\xb8\x13\n\x90\x95\x8a\xc1-\xcbc\xd15\x89\xdb\xe4\xcd2\xf6\xee2;nxJ\xde|\x9f˛^\uffe7,\x05\xad\x8a\x9f\xb2\"\xd3_\xbd~\xd6\x04\xfb^\xbd~FW#^\xbd~\xe6\x1a\xc9W\xb0\x92\xba\xe5\xfd\xcf8\xe9\xa2\xe2|\x8a\xfa\xdbj\xf3(\xd3{%W\xa7|h\x1d\x97^\xe1\x1bVP\xd9y\x01ۉ\xeeݒ\xb7\xc3Ͼ\x82ek\xb7\x17(y\x88\xc9\xde0nu\x96\xd7\xca>\x1eHg\x9aa\xb2\a\xcf\xf0n\x99H\x1b\x91D\x15c\xc5\xc4\xeb\xf8\x113q\xc0\xddP#t\xb6\x80\xf7ђ\xb6\xc0^\x1f\xa52\xb5{\x8ew\xcfO\xb97\xf9\xe2\v!B\xe2\x93\xc3\xfe\xf516\xb3\xe4\xa0\xf1/\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff/\x8c*.\\\x18\x00\x00"))
}
