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

type DiskLeaseStatus string

const (
	DiskLeaseStatusPending  DiskLeaseStatus = "pending"
	DiskLeaseStatusBound    DiskLeaseStatus = "bound"
	DiskLeaseStatusFailed   DiskLeaseStatus = "failed"
	DiskLeaseStatusReleased DiskLeaseStatus = "released"
)

type DiskMode string

const (
	DiskModeUniversal   DiskMode = "universal"
	DiskModeAccelerator DiskMode = "accelerator"
)

type DiskStatus string

const (
	DiskStatusProvisioning DiskStatus = "provisioning"
	DiskStatusProvisioned  DiskStatus = "provisioned"
	DiskStatusAttached     DiskStatus = "attached"
	DiskStatusDetached     DiskStatus = "detached"
	DiskStatusDeleting     DiskStatus = "deleting"
	DiskStatusError        DiskStatus = "error"
	DiskStatusRestoring    DiskStatus = "restoring"
)

type MountActualState string

const (
	MountActualStateDmPending    MountActualState = "dm_pending"
	MountActualStateDmAttaching  MountActualState = "dm_attaching"
	MountActualStateDmAttached   MountActualState = "dm_attached"
	MountActualStateDmMounting   MountActualState = "dm_mounting"
	MountActualStateDmMounted    MountActualState = "dm_mounted"
	MountActualStateDmUnmounting MountActualState = "dm_unmounting"
	MountActualStateDmDetaching  MountActualState = "dm_detaching"
	MountActualStateDmDetached   MountActualState = "dm_detached"
	MountActualStateDmError      MountActualState = "dm_error"
)

type MountDesiredState string

const (
	MountDesiredStateDmWantMounted   MountDesiredState = "dm_want_mounted"
	MountDesiredStateDmWantUnmounted MountDesiredState = "dm_want_unmounted"
)

type VolumeActualState string

const (
	VolumeActualStateDvPending  VolumeActualState = "dv_pending"
	VolumeActualStateDvCreating VolumeActualState = "dv_creating"
	VolumeActualStateDvReady    VolumeActualState = "dv_ready"
	VolumeActualStateDvDeleting VolumeActualState = "dv_deleting"
	VolumeActualStateDvDeleted  VolumeActualState = "dv_deleted"
	VolumeActualStateDvError    VolumeActualState = "dv_error"
)

type VolumeDesiredState string

const (
	VolumeDesiredStateDvPresent VolumeDesiredState = "dv_present"
	VolumeDesiredStateDvAbsent  VolumeDesiredState = "dv_absent"
)

type VolumeMode string

const (
	VolumeModeVmUniversal   VolumeMode = "vm_universal"
	VolumeModeVmAccelerator VolumeMode = "vm_accelerator"
)

const (
	DiskCreatedById          = entity.Id("dev.miren.storage/disk.created_by")
	DiskFilesystemId         = entity.Id("dev.miren.storage/disk.filesystem")
	DiskFilesystemExt4Id     = entity.Id("dev.miren.storage/filesystem.ext4")
	DiskFilesystemXfsId      = entity.Id("dev.miren.storage/filesystem.xfs")
	DiskFilesystemBtrfsId    = entity.Id("dev.miren.storage/filesystem.btrfs")
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

var DiskFilesystemFromId = map[entity.Id]DiskFilesystem{DiskFilesystemExt4Id: DiskFilesystemExt4, DiskFilesystemXfsId: DiskFilesystemXfs, DiskFilesystemBtrfsId: DiskFilesystemBtrfs}
var DiskFilesystemToId = map[DiskFilesystem]entity.Id{DiskFilesystemExt4: DiskFilesystemExt4Id, DiskFilesystemXfs: DiskFilesystemXfsId, DiskFilesystemBtrfs: DiskFilesystemBtrfsId}

const (
	UNIVERSAL   DiskMode = DiskModeUniversal
	ACCELERATOR DiskMode = DiskModeAccelerator
)

var DiskModeFromId = map[entity.Id]DiskMode{DiskModeUniversalId: DiskModeUniversal, DiskModeAcceleratorId: DiskModeAccelerator}
var DiskModeToId = map[DiskMode]entity.Id{DiskModeUniversal: DiskModeUniversalId, DiskModeAccelerator: DiskModeAcceleratorId}

const (
	PROVISIONING DiskStatus = DiskStatusProvisioning
	PROVISIONED  DiskStatus = DiskStatusProvisioned
	ATTACHED     DiskStatus = DiskStatusAttached
	DETACHED     DiskStatus = DiskStatusDetached
	DELETING     DiskStatus = DiskStatusDeleting
	ERROR        DiskStatus = DiskStatusError
	RESTORING    DiskStatus = DiskStatusRestoring
)

var DiskStatusFromId = map[entity.Id]DiskStatus{DiskStatusProvisioningId: DiskStatusProvisioning, DiskStatusProvisionedId: DiskStatusProvisioned, DiskStatusAttachedId: DiskStatusAttached, DiskStatusDetachedId: DiskStatusDetached, DiskStatusDeletingId: DiskStatusDeleting, DiskStatusErrorId: DiskStatusError, DiskStatusRestoringId: DiskStatusRestoring}
var DiskStatusToId = map[DiskStatus]entity.Id{DiskStatusProvisioning: DiskStatusProvisioningId, DiskStatusProvisioned: DiskStatusProvisionedId, DiskStatusAttached: DiskStatusAttachedId, DiskStatusDetached: DiskStatusDetachedId, DiskStatusDeleting: DiskStatusDeletingId, DiskStatusError: DiskStatusErrorId, DiskStatusRestoring: DiskStatusRestoringId}

func (o *Disk) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskCreatedById); ok && a.Value.Kind() == entity.KindId {
		o.CreatedBy = a.Value.Id()
	}
	if a, ok := e.Get(DiskFilesystemId); ok && a.Value.Kind() == entity.KindId {
		o.Filesystem = DiskFilesystemFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskModeId); ok && a.Value.Kind() == entity.KindId {
		o.Mode = DiskModeFromId[a.Value.Id()]
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
		o.Status = DiskStatusFromId[a.Value.Id()]
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
	if a, ok := DiskModeToId[o.Mode]; ok {
		attrs = append(attrs, entity.Ref(DiskModeId, a))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DiskNameId, o.Name))
	}
	attrs = append(attrs, entity.Bool(DiskRemoteOnlyId, o.RemoteOnly))
	attrs = append(attrs, entity.Int64(DiskSizeGbId, o.SizeGb))
	if a, ok := DiskStatusToId[o.Status]; ok {
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
	sb.Ref("created_by", "dev.miren.storage/disk.created_by", schema.Doc("Application that created this disk (for tracking purposes)"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Singleton("dev.miren.storage/filesystem.ext4")
	sb.Singleton("dev.miren.storage/filesystem.xfs")
	sb.Singleton("dev.miren.storage/filesystem.btrfs")
	sb.Ref("filesystem", "dev.miren.storage/disk.filesystem", schema.Doc("Filesystem type for the disk"), schema.Choices(DiskFilesystemExt4Id, DiskFilesystemXfsId, DiskFilesystemBtrfsId))
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

const (
	PENDING  DiskLeaseStatus = DiskLeaseStatusPending
	BOUND    DiskLeaseStatus = DiskLeaseStatusBound
	FAILED   DiskLeaseStatus = DiskLeaseStatusFailed
	RELEASED DiskLeaseStatus = DiskLeaseStatusReleased
)

var DiskLeaseStatusFromId = map[entity.Id]DiskLeaseStatus{DiskLeaseStatusPendingId: DiskLeaseStatusPending, DiskLeaseStatusBoundId: DiskLeaseStatusBound, DiskLeaseStatusFailedId: DiskLeaseStatusFailed, DiskLeaseStatusReleasedId: DiskLeaseStatusReleased}
var DiskLeaseStatusToId = map[DiskLeaseStatus]entity.Id{DiskLeaseStatusPending: DiskLeaseStatusPendingId, DiskLeaseStatusBound: DiskLeaseStatusBoundId, DiskLeaseStatusFailed: DiskLeaseStatusFailedId, DiskLeaseStatusReleased: DiskLeaseStatusReleasedId}

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
		o.Status = DiskLeaseStatusFromId[a.Value.Id()]
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
	if a, ok := DiskLeaseStatusToId[o.Status]; ok {
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
	ID           entity.Id         `json:"id"`
	ActualState  MountActualState  `cbor:"actual_state,omitempty" json:"actual_state,omitempty"`
	DesiredState MountDesiredState `cbor:"desired_state,omitempty" json:"desired_state,omitempty"`
	DevicePath   string            `cbor:"device_path,omitempty" json:"device_path,omitempty"`
	DiskLeaseId  entity.Id         `cbor:"disk_lease_id,omitempty" json:"disk_lease_id,omitempty"`
	ErrorMessage string            `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	LoopDevice   string            `cbor:"loop_device,omitempty" json:"loop_device,omitempty"`
	MountPath    string            `cbor:"mount_path" json:"mount_path"`
	NodeId       entity.Id         `cbor:"node_id" json:"node_id"`
	ReadOnly     bool              `cbor:"read_only,omitempty" json:"read_only,omitempty"`
	VolumeId     entity.Id         `cbor:"volume_id" json:"volume_id"`
}

type DiskMountActualState = MountActualState

const (
	DM_PENDING    MountActualState = MountActualStateDmPending
	DM_ATTACHING  MountActualState = MountActualStateDmAttaching
	DM_ATTACHED   MountActualState = MountActualStateDmAttached
	DM_MOUNTING   MountActualState = MountActualStateDmMounting
	DM_MOUNTED    MountActualState = MountActualStateDmMounted
	DM_UNMOUNTING MountActualState = MountActualStateDmUnmounting
	DM_DETACHING  MountActualState = MountActualStateDmDetaching
	DM_DETACHED   MountActualState = MountActualStateDmDetached
	DM_ERROR      MountActualState = MountActualStateDmError
)

var DiskMountActualStateFromId = map[entity.Id]MountActualState{DiskMountActualStateDmPendingId: MountActualStateDmPending, DiskMountActualStateDmAttachingId: MountActualStateDmAttaching, DiskMountActualStateDmAttachedId: MountActualStateDmAttached, DiskMountActualStateDmMountingId: MountActualStateDmMounting, DiskMountActualStateDmMountedId: MountActualStateDmMounted, DiskMountActualStateDmUnmountingId: MountActualStateDmUnmounting, DiskMountActualStateDmDetachingId: MountActualStateDmDetaching, DiskMountActualStateDmDetachedId: MountActualStateDmDetached, DiskMountActualStateDmErrorId: MountActualStateDmError}
var DiskMountActualStateToId = map[MountActualState]entity.Id{MountActualStateDmPending: DiskMountActualStateDmPendingId, MountActualStateDmAttaching: DiskMountActualStateDmAttachingId, MountActualStateDmAttached: DiskMountActualStateDmAttachedId, MountActualStateDmMounting: DiskMountActualStateDmMountingId, MountActualStateDmMounted: DiskMountActualStateDmMountedId, MountActualStateDmUnmounting: DiskMountActualStateDmUnmountingId, MountActualStateDmDetaching: DiskMountActualStateDmDetachingId, MountActualStateDmDetached: DiskMountActualStateDmDetachedId, MountActualStateDmError: DiskMountActualStateDmErrorId}

type DiskMountDesiredState = MountDesiredState

const (
	DM_WANT_MOUNTED   MountDesiredState = MountDesiredStateDmWantMounted
	DM_WANT_UNMOUNTED MountDesiredState = MountDesiredStateDmWantUnmounted
)

var DiskMountDesiredStateFromId = map[entity.Id]MountDesiredState{DiskMountDesiredStateDmWantMountedId: MountDesiredStateDmWantMounted, DiskMountDesiredStateDmWantUnmountedId: MountDesiredStateDmWantUnmounted}
var DiskMountDesiredStateToId = map[MountDesiredState]entity.Id{MountDesiredStateDmWantMounted: DiskMountDesiredStateDmWantMountedId, MountDesiredStateDmWantUnmounted: DiskMountDesiredStateDmWantUnmountedId}

func (o *DiskMount) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskMountActualStateId); ok && a.Value.Kind() == entity.KindId {
		o.ActualState = DiskMountActualStateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskMountDesiredStateId); ok && a.Value.Kind() == entity.KindId {
		o.DesiredState = DiskMountDesiredStateFromId[a.Value.Id()]
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
	if a, ok := DiskMountActualStateToId[o.ActualState]; ok {
		attrs = append(attrs, entity.Ref(DiskMountActualStateId, a))
	}
	if a, ok := DiskMountDesiredStateToId[o.DesiredState]; ok {
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
	ID            entity.Id          `json:"id"`
	ActualState   VolumeActualState  `cbor:"actual_state,omitempty" json:"actual_state,omitempty"`
	CloudVolumeId string             `cbor:"cloud_volume_id,omitempty" json:"cloud_volume_id,omitempty"`
	DesiredState  VolumeDesiredState `cbor:"desired_state,omitempty" json:"desired_state,omitempty"`
	DiskId        entity.Id          `cbor:"disk_id" json:"disk_id"`
	ErrorMessage  string             `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Filesystem    DiskFilesystem     `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	ImagePath     string             `cbor:"image_path,omitempty" json:"image_path,omitempty"`
	MountId       string             `cbor:"mount_id,omitempty" json:"mount_id,omitempty"`
	Name          string             `cbor:"name,omitempty" json:"name,omitempty"`
	NodeId        entity.Id          `cbor:"node_id" json:"node_id"`
	SizeGb        int64              `cbor:"size_gb" json:"size_gb"`
	VolumeId      string             `cbor:"volume_id,omitempty" json:"volume_id,omitempty"`
	VolumeMode    VolumeMode         `cbor:"volume_mode,omitempty" json:"volume_mode,omitempty"`
}

type DiskVolumeActualState = VolumeActualState

const (
	DV_PENDING  VolumeActualState = VolumeActualStateDvPending
	DV_CREATING VolumeActualState = VolumeActualStateDvCreating
	DV_READY    VolumeActualState = VolumeActualStateDvReady
	DV_DELETING VolumeActualState = VolumeActualStateDvDeleting
	DV_DELETED  VolumeActualState = VolumeActualStateDvDeleted
	DV_ERROR    VolumeActualState = VolumeActualStateDvError
)

var DiskVolumeActualStateFromId = map[entity.Id]VolumeActualState{DiskVolumeActualStateDvPendingId: VolumeActualStateDvPending, DiskVolumeActualStateDvCreatingId: VolumeActualStateDvCreating, DiskVolumeActualStateDvReadyId: VolumeActualStateDvReady, DiskVolumeActualStateDvDeletingId: VolumeActualStateDvDeleting, DiskVolumeActualStateDvDeletedId: VolumeActualStateDvDeleted, DiskVolumeActualStateDvErrorId: VolumeActualStateDvError}
var DiskVolumeActualStateToId = map[VolumeActualState]entity.Id{VolumeActualStateDvPending: DiskVolumeActualStateDvPendingId, VolumeActualStateDvCreating: DiskVolumeActualStateDvCreatingId, VolumeActualStateDvReady: DiskVolumeActualStateDvReadyId, VolumeActualStateDvDeleting: DiskVolumeActualStateDvDeletingId, VolumeActualStateDvDeleted: DiskVolumeActualStateDvDeletedId, VolumeActualStateDvError: DiskVolumeActualStateDvErrorId}

type DiskVolumeDesiredState = VolumeDesiredState

const (
	DV_PRESENT VolumeDesiredState = VolumeDesiredStateDvPresent
	DV_ABSENT  VolumeDesiredState = VolumeDesiredStateDvAbsent
)

var DiskVolumeDesiredStateFromId = map[entity.Id]VolumeDesiredState{DiskVolumeDesiredStateDvPresentId: VolumeDesiredStateDvPresent, DiskVolumeDesiredStateDvAbsentId: VolumeDesiredStateDvAbsent}
var DiskVolumeDesiredStateToId = map[VolumeDesiredState]entity.Id{VolumeDesiredStateDvPresent: DiskVolumeDesiredStateDvPresentId, VolumeDesiredStateDvAbsent: DiskVolumeDesiredStateDvAbsentId}
var DiskVolumeFilesystemFromString = map[string]DiskFilesystem{"ext4": DiskFilesystemExt4, "xfs": DiskFilesystemXfs, "btrfs": DiskFilesystemBtrfs}
var DiskVolumeFilesystemToString = map[DiskFilesystem]string{DiskFilesystemExt4: "ext4", DiskFilesystemXfs: "xfs", DiskFilesystemBtrfs: "btrfs"}

type DiskVolumeVolumeMode = VolumeMode

const (
	VM_UNIVERSAL   VolumeMode = VolumeModeVmUniversal
	VM_ACCELERATOR VolumeMode = VolumeModeVmAccelerator
)

var DiskVolumeVolumeModeFromId = map[entity.Id]VolumeMode{DiskVolumeVolumeModeVmUniversalId: VolumeModeVmUniversal, DiskVolumeVolumeModeVmAcceleratorId: VolumeModeVmAccelerator}
var DiskVolumeVolumeModeToId = map[VolumeMode]entity.Id{VolumeModeVmUniversal: DiskVolumeVolumeModeVmUniversalId, VolumeModeVmAccelerator: DiskVolumeVolumeModeVmAcceleratorId}

func (o *DiskVolume) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskVolumeActualStateId); ok && a.Value.Kind() == entity.KindId {
		o.ActualState = DiskVolumeActualStateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeCloudVolumeIdId); ok && a.Value.Kind() == entity.KindString {
		o.CloudVolumeId = a.Value.String()
	}
	if a, ok := e.Get(DiskVolumeDesiredStateId); ok && a.Value.Kind() == entity.KindId {
		o.DesiredState = DiskVolumeDesiredStateFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskVolumeDiskIdId); ok && a.Value.Kind() == entity.KindId {
		o.DiskId = a.Value.Id()
	}
	if a, ok := e.Get(DiskVolumeErrorMessageId); ok && a.Value.Kind() == entity.KindString {
		o.ErrorMessage = a.Value.String()
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
		o.VolumeMode = DiskVolumeVolumeModeFromId[a.Value.Id()]
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
	if a, ok := DiskVolumeActualStateToId[o.ActualState]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeActualStateId, a))
	}
	if !entity.Empty(o.CloudVolumeId) {
		attrs = append(attrs, entity.String(DiskVolumeCloudVolumeIdId, o.CloudVolumeId))
	}
	if a, ok := DiskVolumeDesiredStateToId[o.DesiredState]; ok {
		attrs = append(attrs, entity.Ref(DiskVolumeDesiredStateId, a))
	}
	if !entity.Empty(o.DiskId) {
		attrs = append(attrs, entity.Ref(DiskVolumeDiskIdId, o.DiskId))
	}
	if !entity.Empty(o.ErrorMessage) {
		attrs = append(attrs, entity.String(DiskVolumeErrorMessageId, o.ErrorMessage))
	}
	if a, ok := DiskVolumeFilesystemToString[o.Filesystem]; ok {
		attrs = append(attrs, entity.String(DiskVolumeFilesystemId, a))
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
	if a, ok := DiskVolumeVolumeModeToId[o.VolumeMode]; ok {
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
	sb.String("filesystem", "dev.miren.storage/disk_volume.filesystem", schema.Doc("Filesystem type (ext4, xfs, btrfs)"), schema.EnumValues("ext4", "xfs", "btrfs"))
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
	schema.RegisterEncodedSchema("dev.miren.storage", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xacYێ\xe4&\x13~\x90\xff\xcf\xf9|\xf2j\xa5\xbc\x8fE\x9b\xb2M\xdb@\xaf\xc1\x8e'\x97\xd9(')/\x91\x1d\xcd*/\x98\xeb\x88\x02l\xecƘY\xe5fd\xe8\xef\xfb(\x8a\xa2\n\x98G*\b\x87W\x14\xa6\x82\xb3\x01D\xa1\xb4\x1cH\x03\xd01A\xd5\xe3\xfc\xbf\xbb_^\x98_\n\xcaT\xf7\x84\xdc\xe9\x1ea~\xb4\x02\xff\xd4Tr\xc2\xc4\xfd\x00u͠\xa7\xea\xb77\x17F\xe7\x8f\xe2\x1aE5\x00\xd1@\xcb\xcb\x03\x0eu\r\xda\xfa\xe1\x06\x17Fߦ\xe85\xebA=(\r\x9c\x82\x18\xf9\xfc\xf9=\xce\xf4\xe3d\xca\x00\x8cc\x05m3\x16*t\xe6O9\x91~\x04\xf5\xa6\x9ak5\x7fx/\xb9\x12\x8b\xb9V\x14f\xfd}\xcc\xc2\x00f p\xd1C\xad揓@\xc4\xf4h\x04\a~\x81A\xbdF}c\x8a\x15\xe0\xf8#\x88JR&\x9aj\x80\x1a=\x14YF\xf4\x10\x97\xd4\xce,6\x91\xd57\b3^\xa1\xf8\x15\xf5\xc7_l\x14l\x82A\x91>&f\x88ł\xe8HUA\x0f\x03\xd1r\x88\xcd\x19\xd1\x01f3\xe7\x9fXT\xe7~\xeaoRS\xc7\xe9\xac\x7f̜j\xa5\a&\x1a\xa4E\x8cB\xda\x00\\j(\xa5\xe8mLva\a:\xe6\"e\x8f\x12\xef\x1fH(\xf6#\x94\xcd\x05\xe9\x8do\x18jń\xc6\xe5z\uf229\x89\x1e\x95]\xb0\x88\x81\xeb\x829\xa0\x19\xa1v\xdf\xd1E{\v0\fr\x88\x99ji\x05\xfe\xde\x12\xadI\xd5Bt\xab9\xa0\x87\xb4\x14z\xd0L4\t\xac\x87\xb4\x14Nu=\x84\r`~1\u0091\xa9;\xf0\x82\xe9n\x83\x9c\x98bR\x00\x9d?=\xc4\a\xa8~\xf96C|vNa\xa2ل\xe5\xaf\x1b\x85Ѐ6\xf0\xcd\xceI\xd6\xff\xeb\xdc\x0e\x828\xb2\xa10 &ُ\x1cJFq\xa9\xd9\xda\f¹1[\x85I\xd1L/I\x7fkI\x7f\x1b\x18'\xc3Ci24521\xef/Y\xbe\xec\x81(\xb0\xb9~\xfe\x7f\xdc\x0e\x8byV\xca\xff2\xa5T\x90\xea\xd5\xc8\x06\xa0%\xd1v\x97\x85\x1d\x18ɚq@\xa1O\xd2B\xb7\x9b\xf7N\xed\xbe]\xe5@r$2\x022~:v\xe3\x1b!\xfd\xeb$\x1d\x97\xb6\xe4\xa0\x14il\x96\xe1ۮ`\x91\x1e\x139\xc7\xc9q9\n\xeb\r\xb0\x9f\x86\xce*\xc9oR\x80\xd0\xeb\x97[\xab{\xb5b\xaf\x96\xb9b\xafq\xb2\x1f\xc4\xd2\xf4(t!o\x9aIa\xb3M\xe3\x1b\xfb|\x1a\x89\x1c˾\x11\xdd\xda\x14\x8c_{^$4-o\x00B\xd74\xcc\xd6撄\x93\x81o}\x98\x11\x04B\xd2e\x835\xbe\x11\x06\xc1\x17I\xba\"\x82^\xe4\xec\x15\xaeA;<¤\xa38\xcc\xfb\x91\x8d\xb3\xe6}\x84gg\xffG\xb8\xc8QD\v\x95Kv\xf8{]\x13\xd6C4\x00\x1c\xcc\x02\x9a\x1b\b\x93\xb5b\xd9\xca'O\x8bh\a@KSy\xdfC69\xf6g?\x06\x84\x96-r\xf7\xd93\x19\x03\xd7\xd5g\xe9\x14\x88\xb1r\x92\x02\x9f\xb3\xa1\xfe\xc05\xff*\xa5T\x90J\x8f\xa4ǵtG\xb4\b\x1eW\x1e\xf1\xe5\x06o,\xed7=\xd1\x00\xf8\xbb\xa5\xbc\xb4'\x80H\x14\x87\xfc\xc2\x03\xaf\x94\xbb\xa9F'\xb0\xe78\xa8a\xf9\xe8\xc8`9hGy\xb9\x9c<\"\xb9vO\xf3X\xc3[N\x16\x19<\x8f\xed\xbc\xc1\xc6\xcc\f\x9e\xc7\xf6\xcb؆\xf8M\xae\xa1\x8eiG\xcfd.`Ny9\x8a\xc5\xdaoϩ+z\xb3\xa5~\x0f\xd6f3\x91n\xe7Q\xcf\x0e\"`k\xc4f2\xe1\x1a,Qvp;9*\xa3v#PPX\xf9\x83\x9d\x10!\x04;aK\xc0\xba\xbb\xed\x8a\xdf_$\xe5\xe5\x0fD\xe8%\xbc_D\xcc\nu\x8a\x1d\xe1\x95o;\x97\x00\x9d_\xe6J,\x94\xedMgo\xd3\xfd\x10\a\aƣ\xe3\x95w\xe9\xc4*(\x97\xd2ۅ\x1d\xfb\n|\xb2:k\xd9q%\x8eo\xbbr\xceKV\xea9祌I\xf6R\xdeJ;1;ɰc/uTĭ\x94\x8d\xab\xc5]נ\xbd\x17::LX\xa1\xd3\xc3D\xe4\x89\"\xa0\x9f\x9fy2D\x92w\x86\v\xa3\x19%\x13\x85b\xc7յdZYW3\x0f\xee\xb3\x0e\x94Y4\xffL\xe6\n+\x15\xa9\x9aG\xb9\xc2\xcd\xfb]\xca\xe6SK\xa7ܲ逆a\x96\xeb!\x87\x81\xc0+\x9dJ\xbc%\xe6\x14\xda\x05jXمvZ\v\xedT\xe2\xdbZV\xe1[\xb1\x9d\x1f8\x93籛$\xf7\xcb5n\xc8\xe2\xb2p\x94\xc0+\xcb\"\x1c\xa4\xc0\xefґR\xf5r\xa4\xe5v3\xc8}g\xb0\xbb1\xfa\"\x15:Ԍ\x94\xaa\b#\f\xbfw\xacU\x8cN%\xb9(\x10:z\x8aܖ\x18\x0fE?\x0f\x80\xac\xd86ڳ\x1cv[\x91\x02\x91Ո\x83\x15\x88<\xa2l\xbcuv\xbd>\xf1\xf6s\xea\xc5S\xaa^8\xbd\xff\xf2\xb9\xd8\r\xfb\x8c\xb7ڜ\xc2\xe6\fe\x9c4A\xf5\xbe\x06\xed}9:*\x05N\xc9\x162\xb7\x06\xed\xd2\xca|\f\xf5*'O\xa9'apZ\x13O\xf8\xc9\xe7\xd4dqw\x029\x8fh\xc9\v\xdbVg}S?z\xa2\r\x81x2\t;\xe2\x1b\xbe\x9f\xccIۿ\xafGvo Q\x84X1\xf12|l\x8f\\\x14v\xd4\xc3g\xf7>\xa1\xfb̛w\x17\xf8m\x0f\xecT+\a]\xda\x7f@\xd9\xf7\xc9\xd4\x7f\xa1\xb2/\xf1\b\t\xcf/\xe7W\xfe\xd0̜\xe3ο\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\x9d\x93W\xf8N\x1b\x00\x00"))
}
