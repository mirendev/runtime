package storage_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	DiskCreatedById          = entity.Id("dev.miren.storage/disk.created_by")
	DiskFilesystemId         = entity.Id("dev.miren.storage/disk.filesystem")
	DiskFilesystemExt4Id     = entity.Id("dev.miren.storage/filesystem.ext4")
	DiskFilesystemXfsId      = entity.Id("dev.miren.storage/filesystem.xfs")
	DiskFilesystemBtrfsId    = entity.Id("dev.miren.storage/filesystem.btrfs")
	DiskLsvdVolumeIdId       = entity.Id("dev.miren.storage/disk.lsvd_volume_id")
	DiskNameId               = entity.Id("dev.miren.storage/disk.name")
	DiskSizeGbId             = entity.Id("dev.miren.storage/disk.size_gb")
	DiskStatusId             = entity.Id("dev.miren.storage/disk.status")
	DiskStatusProvisioningId = entity.Id("dev.miren.storage/status.provisioning")
	DiskStatusProvisionedId  = entity.Id("dev.miren.storage/status.provisioned")
	DiskStatusAttachedId     = entity.Id("dev.miren.storage/status.attached")
	DiskStatusDetachedId     = entity.Id("dev.miren.storage/status.detached")
	DiskStatusDeletingId     = entity.Id("dev.miren.storage/status.deleting")
	DiskStatusErrorId        = entity.Id("dev.miren.storage/status.error")
)

type Disk struct {
	ID           entity.Id      `json:"id"`
	CreatedBy    entity.Id      `cbor:"created_by,omitempty" json:"created_by,omitempty"`
	Filesystem   DiskFilesystem `cbor:"filesystem,omitempty" json:"filesystem,omitempty"`
	LsvdVolumeId string         `cbor:"lsvd_volume_id,omitempty" json:"lsvd_volume_id,omitempty"`
	Name         string         `cbor:"name" json:"name"`
	SizeGb       int64          `cbor:"size_gb" json:"size_gb"`
	Status       DiskStatus     `cbor:"status,omitempty" json:"status,omitempty"`
}

type DiskFilesystem string

const (
	EXT4  DiskFilesystem = "filesystem.ext4"
	XFS   DiskFilesystem = "filesystem.xfs"
	BTRFS DiskFilesystem = "filesystem.btrfs"
)

var diskfilesystemFromId = map[entity.Id]DiskFilesystem{DiskFilesystemExt4Id: EXT4, DiskFilesystemXfsId: XFS, DiskFilesystemBtrfsId: BTRFS}
var diskfilesystemToId = map[DiskFilesystem]entity.Id{EXT4: DiskFilesystemExt4Id, XFS: DiskFilesystemXfsId, BTRFS: DiskFilesystemBtrfsId}

type DiskStatus string

const (
	PROVISIONING DiskStatus = "status.provisioning"
	PROVISIONED  DiskStatus = "status.provisioned"
	ATTACHED     DiskStatus = "status.attached"
	DETACHED     DiskStatus = "status.detached"
	DELETING     DiskStatus = "status.deleting"
	ERROR        DiskStatus = "status.error"
)

var diskstatusFromId = map[entity.Id]DiskStatus{DiskStatusProvisioningId: PROVISIONING, DiskStatusProvisionedId: PROVISIONED, DiskStatusAttachedId: ATTACHED, DiskStatusDetachedId: DETACHED, DiskStatusDeletingId: DELETING, DiskStatusErrorId: ERROR}
var diskstatusToId = map[DiskStatus]entity.Id{PROVISIONING: DiskStatusProvisioningId, PROVISIONED: DiskStatusProvisionedId, ATTACHED: DiskStatusAttachedId, DETACHED: DiskStatusDetachedId, DELETING: DiskStatusDeletingId, ERROR: DiskStatusErrorId}

func (o *Disk) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(DiskCreatedById); ok && a.Value.Kind() == entity.KindId {
		o.CreatedBy = a.Value.Id()
	}
	if a, ok := e.Get(DiskFilesystemId); ok && a.Value.Kind() == entity.KindId {
		o.Filesystem = diskfilesystemFromId[a.Value.Id()]
	}
	if a, ok := e.Get(DiskLsvdVolumeIdId); ok && a.Value.Kind() == entity.KindString {
		o.LsvdVolumeId = a.Value.String()
	}
	if a, ok := e.Get(DiskNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(DiskSizeGbId); ok && a.Value.Kind() == entity.KindInt64 {
		o.SizeGb = a.Value.Int64()
	}
	if a, ok := e.Get(DiskStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = diskstatusFromId[a.Value.Id()]
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
	if a, ok := diskfilesystemToId[o.Filesystem]; ok {
		attrs = append(attrs, entity.Ref(DiskFilesystemId, a))
	}
	if !entity.Empty(o.LsvdVolumeId) {
		attrs = append(attrs, entity.String(DiskLsvdVolumeIdId, o.LsvdVolumeId))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(DiskNameId, o.Name))
	}
	if !entity.Empty(o.SizeGb) {
		attrs = append(attrs, entity.Int64(DiskSizeGbId, o.SizeGb))
	}
	if a, ok := diskstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(DiskStatusId, a))
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
	if !entity.Empty(o.LsvdVolumeId) {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.SizeGb) {
		return false
	}
	if o.Status != "" {
		return false
	}
	return true
}

func (o *Disk) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("created_by", "dev.miren.storage/disk.created_by", schema.Doc("Application that created this disk (for tracking purposes)"), schema.Indexed)
	sb.Singleton("dev.miren.storage/filesystem.ext4")
	sb.Singleton("dev.miren.storage/filesystem.xfs")
	sb.Singleton("dev.miren.storage/filesystem.btrfs")
	sb.Ref("filesystem", "dev.miren.storage/disk.filesystem", schema.Doc("Filesystem type for the disk"), schema.Choices(DiskFilesystemExt4Id, DiskFilesystemXfsId, DiskFilesystemBtrfsId))
	sb.String("lsvd_volume_id", "dev.miren.storage/disk.lsvd_volume_id", schema.Doc("LSVD backend volume identifier"), schema.Indexed)
	sb.String("name", "dev.miren.storage/disk.name", schema.Doc("Human-readable name for the disk"), schema.Required, schema.Indexed)
	sb.Int64("size_gb", "dev.miren.storage/disk.size_gb", schema.Doc("Storage capacity in gigabytes"), schema.Required)
	sb.Singleton("dev.miren.storage/status.provisioning")
	sb.Singleton("dev.miren.storage/status.provisioned")
	sb.Singleton("dev.miren.storage/status.attached")
	sb.Singleton("dev.miren.storage/status.detached")
	sb.Singleton("dev.miren.storage/status.deleting")
	sb.Singleton("dev.miren.storage/status.error")
	sb.Ref("status", "dev.miren.storage/disk.status", schema.Doc("Current state of the disk"), schema.Indexed, schema.Choices(DiskStatusProvisioningId, DiskStatusProvisionedId, DiskStatusAttachedId, DiskStatusDetachedId, DiskStatusDeletingId, DiskStatusErrorId))
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
	AcquiredAt   time.Time       `cbor:"acquired_at,omitempty" json:"acquired_at,omitempty"`
	AppId        entity.Id       `cbor:"app_id,omitempty" json:"app_id,omitempty"`
	DiskId       entity.Id       `cbor:"disk_id" json:"disk_id"`
	ErrorMessage string          `cbor:"error_message,omitempty" json:"error_message,omitempty"`
	Mount        Mount           `cbor:"mount,omitempty" json:"mount,omitempty"`
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
	sb.Ref("app_id", "dev.miren.storage/disk_lease.app_id", schema.Doc("Reference to the application (for debugging)"), schema.Indexed)
	sb.Ref("disk_id", "dev.miren.storage/disk_lease.disk_id", schema.Doc("Reference to the leased disk"), schema.Required, schema.Indexed)
	sb.String("error_message", "dev.miren.storage/disk_lease.error_message", schema.Doc("Error details if lease binding failed"))
	sb.Component("mount", "dev.miren.storage/disk_lease.mount", schema.Doc("Mount configuration for the disk"))
	(&Mount{}).InitSchema(sb.Builder("mount"))
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

var (
	KindDisk      = entity.Id("dev.miren.storage/kind.disk")
	KindDiskLease = entity.Id("dev.miren.storage/kind.disk_lease")
	Schema        = entity.Id("dev.miren.storage/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.storage", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Disk{}).InitSchema(sb)
		(&DiskLease{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.storage", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cVK\x8e\xdc \x10=H\xfe?%\xca\xc2Q\xa4\xdc\xc7\xc2M\x81k\x9a\x8f\a\xb0\xe5\xce6R\x94sdFsĬ#\n\xbb\x9b\xd84\xe9\x8dUe\xde{@\xfd\xecGn\x98\x86{\x0eS\xa3сi|\xb0\x8eI\x80#\x1a\xee\x7f\xcf\xcfv+_\xe2J\xc3\xd1\x1f\x9f\x88;\xed\x11q1\t\xfc\x11\xdcj\x86f\xbf\x81\x10\b\x8a\xfb\x9f\x0f\x1d\xf2\xf9MY\xa398`\x01x\u06ddh\xab\xbb\xcc\x0f\xa7\x01:\xe4\x8f5\xba@\x05\xfe\xe4\x03\xe8D\xcf\xfcH\xe7`F}\x8c\x8fvbj\x04\xffp\x98\x85\x9f_\xef\xd5.\xc4f\x16\x9e\xc3\x1c\xbe\x956\xcd`\x11\x02]p\xc2\xcfo\xab@\xc2P\x10>\\\xb9\x85\xf2\x13o'\xabF\r-r\xba\x89ټ\x8b\xb7\x11>84\x92\xa4\nY#\xa9\xc8\xe5\x97ǖ\xf6\xf2\n\xcd\xe3wheG$\xb9:\x91|@\x13(\x03/\xae1\x03\v\xa3'\xa2X\xecb\xe4\x9f\x00\x9c\xb3\xaet\x82Dkh\xbdg!\xb0C\x0fŔ/\xc0\x15\xd2sP\x10\xd0\xc8\nv\x85\xf4\x1c\xfe\xab\xbbB\x8e\x83\xb3\x13z\xb4\x06\xf8\xfc\xfe*<C\xa9\xb3\x1dOSH\xf3\x96\x82F\xca\t\\4\xe5\xf4\x95\xa9\xa1gjp\xa8\x99;\xb5\xb1\xadx\x8cm\xe9\xa8\xe7\xd6l\x150\x0f\xa9A\xe7\xe7\xe5\xe4$̍}\xfa\x8b\n\xe4SM\xa9a\x87\xfb\x11\x1d\xf0\x96\x05\xda\xf8\x98\xbf\xa0\xb4\a\xd4@B\xef\xeaBðV\xbaX\xec\xa5݉\\\bzF&sa\xcb\xd5\xc9韫t\xaa\xb3V\x83\xf7L\xa66\xd1\xff\xbeʚ\x86J\xbf\xd0ޙ\x9c\xb6\xa3IрdF:\x1e\xac\x1e\xac\x01\x13.֒\xab\xbdZ\xb3U\xbb1c?貯\xf6\xa7#\x91\xc6\x0e\x01\xadI\xad)Wg;\x10\n\x95\x93\xd8\x03\v}\x9a!dmy\x85\xd2L<\a\x8c\xb7֨4\xcf\xf1\xe2Ryt֪j\xe1\xa7\x18\xdeP\x04\xc6\xf2\U000f052b\x93\x17\xc1\xc7*\xdd3\xc3;;\xaf\nw\x99\x9f\x7fw\xeaU|\xeb\xec{\x84Ύ\xa68}\x97\xc1@\xebB0TP\xcc\xe8\x02K\x009\x80\xe1q\xd0\x14\xbec\xeb\xa0I\x88\xde\x01\x9d\xb46\xf5VH5-w\x97[oqG\xdf[\x17\xda\xf47\x91\xe6V\xed\x97\"S\xbaa\xbe\xfd\x05\x00\x00\xff\xff\x01\x00\x00\xff\xff\x13\xd7P\xb8\xbd\b\x00\x00"))
}
