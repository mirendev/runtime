package disk_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

type transferData struct {
	Done           *int64 `cbor:"0,keyasint,omitempty" json:"done,omitempty"`
	Total          *int64 `cbor:"1,keyasint,omitempty" json:"total,omitempty"`
	BytesPerSecond *int64 `cbor:"2,keyasint,omitempty" json:"bytes_per_second,omitempty"`
	EtaSeconds     *int64 `cbor:"3,keyasint,omitempty" json:"eta_seconds,omitempty"`
}

type Transfer struct {
	data transferData
}

func (v *Transfer) HasDone() bool {
	return v.data.Done != nil
}

func (v *Transfer) Done() int64 {
	if v.data.Done == nil {
		return 0
	}
	return *v.data.Done
}

func (v *Transfer) SetDone(done int64) {
	v.data.Done = &done
}

func (v *Transfer) HasTotal() bool {
	return v.data.Total != nil
}

func (v *Transfer) Total() int64 {
	if v.data.Total == nil {
		return 0
	}
	return *v.data.Total
}

func (v *Transfer) SetTotal(total int64) {
	v.data.Total = &total
}

func (v *Transfer) HasBytesPerSecond() bool {
	return v.data.BytesPerSecond != nil
}

func (v *Transfer) BytesPerSecond() int64 {
	if v.data.BytesPerSecond == nil {
		return 0
	}
	return *v.data.BytesPerSecond
}

func (v *Transfer) SetBytesPerSecond(bytes_per_second int64) {
	v.data.BytesPerSecond = &bytes_per_second
}

func (v *Transfer) HasEtaSeconds() bool {
	return v.data.EtaSeconds != nil
}

func (v *Transfer) EtaSeconds() int64 {
	if v.data.EtaSeconds == nil {
		return 0
	}
	return *v.data.EtaSeconds
}

func (v *Transfer) SetEtaSeconds(eta_seconds int64) {
	v.data.EtaSeconds = &eta_seconds
}

func (v *Transfer) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Transfer) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Transfer) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Transfer) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type ProgressUpdate interface {
	Which() string
	Message() string
	SetMessage(string)
	Transfer() *Transfer
	SetTransfer(*Transfer)
	Warning() string
	SetWarning(string)
	Error() string
	SetError(string)
}

type progressUpdate struct {
	U_Message  *string    `cbor:"1,keyasint,omitempty" json:"message,omitempty"`
	U_Transfer **Transfer `cbor:"2,keyasint,omitempty" json:"transfer,omitempty"`
	U_Warning  *string    `cbor:"3,keyasint,omitempty" json:"warning,omitempty"`
	U_Error    *string    `cbor:"4,keyasint,omitempty" json:"error,omitempty"`
}

func (v *progressUpdate) Which() string {
	if v.U_Message != nil {
		return "message"
	}
	if v.U_Transfer != nil {
		return "transfer"
	}
	if v.U_Warning != nil {
		return "warning"
	}
	if v.U_Error != nil {
		return "error"
	}
	return ""
}

func (v *progressUpdate) Message() string {
	if v.U_Message == nil {
		return ""
	}
	return *v.U_Message
}

func (v *progressUpdate) SetMessage(val string) {
	v.U_Transfer = nil
	v.U_Warning = nil
	v.U_Error = nil
	v.U_Message = &val
}

func (v *progressUpdate) Transfer() *Transfer {
	if v.U_Transfer == nil {
		return nil
	}
	return *v.U_Transfer
}

func (v *progressUpdate) SetTransfer(val *Transfer) {
	v.U_Message = nil
	v.U_Warning = nil
	v.U_Error = nil
	v.U_Transfer = &val
}

func (v *progressUpdate) Warning() string {
	if v.U_Warning == nil {
		return ""
	}
	return *v.U_Warning
}

func (v *progressUpdate) SetWarning(val string) {
	v.U_Message = nil
	v.U_Transfer = nil
	v.U_Error = nil
	v.U_Warning = &val
}

func (v *progressUpdate) Error() string {
	if v.U_Error == nil {
		return ""
	}
	return *v.U_Error
}

func (v *progressUpdate) SetError(val string) {
	v.U_Message = nil
	v.U_Transfer = nil
	v.U_Warning = nil
	v.U_Error = &val
}

type progressData struct {
	progressUpdate
}

type Progress struct {
	data progressData
}

func (v *Progress) Update() ProgressUpdate {
	return &v.data.progressUpdate
}

func (v *Progress) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Progress) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Progress) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Progress) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type restorePointData struct {
	Id             *string             `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	CreatedAt      *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"created_at,omitempty"`
	SizeBytes      *int64              `cbor:"2,keyasint,omitempty" json:"size_bytes,omitempty"`
	ImageSizeBytes *int64              `cbor:"3,keyasint,omitempty" json:"image_size_bytes,omitempty"`
	Name           *string             `cbor:"4,keyasint,omitempty" json:"name,omitempty"`
	Mode           *string             `cbor:"5,keyasint,omitempty" json:"mode,omitempty"`
}

type RestorePoint struct {
	data restorePointData
}

func (v *RestorePoint) HasId() bool {
	return v.data.Id != nil
}

func (v *RestorePoint) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RestorePoint) SetId(id string) {
	v.data.Id = &id
}

func (v *RestorePoint) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *RestorePoint) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *RestorePoint) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *RestorePoint) HasSizeBytes() bool {
	return v.data.SizeBytes != nil
}

func (v *RestorePoint) SizeBytes() int64 {
	if v.data.SizeBytes == nil {
		return 0
	}
	return *v.data.SizeBytes
}

func (v *RestorePoint) SetSizeBytes(size_bytes int64) {
	v.data.SizeBytes = &size_bytes
}

func (v *RestorePoint) HasImageSizeBytes() bool {
	return v.data.ImageSizeBytes != nil
}

func (v *RestorePoint) ImageSizeBytes() int64 {
	if v.data.ImageSizeBytes == nil {
		return 0
	}
	return *v.data.ImageSizeBytes
}

func (v *RestorePoint) SetImageSizeBytes(image_size_bytes int64) {
	v.data.ImageSizeBytes = &image_size_bytes
}

func (v *RestorePoint) HasName() bool {
	return v.data.Name != nil
}

func (v *RestorePoint) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RestorePoint) SetName(name string) {
	v.data.Name = &name
}

func (v *RestorePoint) HasMode() bool {
	return v.data.Mode != nil
}

func (v *RestorePoint) Mode() string {
	if v.data.Mode == nil {
		return ""
	}
	return *v.data.Mode
}

func (v *RestorePoint) SetMode(mode string) {
	v.data.Mode = &mode
}

func (v *RestorePoint) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RestorePoint) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RestorePoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RestorePoint) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type backupResultData struct {
	ImageSizeBytes      *int64  `cbor:"0,keyasint,omitempty" json:"image_size_bytes,omitempty"`
	CompressedSizeBytes *int64  `cbor:"1,keyasint,omitempty" json:"compressed_size_bytes,omitempty"`
	Checksum            *string `cbor:"2,keyasint,omitempty" json:"checksum,omitempty"`
	RestorePointId      *string `cbor:"3,keyasint,omitempty" json:"restore_point_id,omitempty"`
}

type BackupResult struct {
	data backupResultData
}

func (v *BackupResult) HasImageSizeBytes() bool {
	return v.data.ImageSizeBytes != nil
}

func (v *BackupResult) ImageSizeBytes() int64 {
	if v.data.ImageSizeBytes == nil {
		return 0
	}
	return *v.data.ImageSizeBytes
}

func (v *BackupResult) SetImageSizeBytes(image_size_bytes int64) {
	v.data.ImageSizeBytes = &image_size_bytes
}

func (v *BackupResult) HasCompressedSizeBytes() bool {
	return v.data.CompressedSizeBytes != nil
}

func (v *BackupResult) CompressedSizeBytes() int64 {
	if v.data.CompressedSizeBytes == nil {
		return 0
	}
	return *v.data.CompressedSizeBytes
}

func (v *BackupResult) SetCompressedSizeBytes(compressed_size_bytes int64) {
	v.data.CompressedSizeBytes = &compressed_size_bytes
}

func (v *BackupResult) HasChecksum() bool {
	return v.data.Checksum != nil
}

func (v *BackupResult) Checksum() string {
	if v.data.Checksum == nil {
		return ""
	}
	return *v.data.Checksum
}

func (v *BackupResult) SetChecksum(checksum string) {
	v.data.Checksum = &checksum
}

func (v *BackupResult) HasRestorePointId() bool {
	return v.data.RestorePointId != nil
}

func (v *BackupResult) RestorePointId() string {
	if v.data.RestorePointId == nil {
		return ""
	}
	return *v.data.RestorePointId
}

func (v *BackupResult) SetRestorePointId(restore_point_id string) {
	v.data.RestorePointId = &restore_point_id
}

func (v *BackupResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BackupResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BackupResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BackupResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type restoreResultData struct {
	DiskId         *string `cbor:"0,keyasint,omitempty" json:"disk_id,omitempty"`
	ImageSizeBytes *int64  `cbor:"1,keyasint,omitempty" json:"image_size_bytes,omitempty"`
	Created        *bool   `cbor:"2,keyasint,omitempty" json:"created,omitempty"`
}

type RestoreResult struct {
	data restoreResultData
}

func (v *RestoreResult) HasDiskId() bool {
	return v.data.DiskId != nil
}

func (v *RestoreResult) DiskId() string {
	if v.data.DiskId == nil {
		return ""
	}
	return *v.data.DiskId
}

func (v *RestoreResult) SetDiskId(disk_id string) {
	v.data.DiskId = &disk_id
}

func (v *RestoreResult) HasImageSizeBytes() bool {
	return v.data.ImageSizeBytes != nil
}

func (v *RestoreResult) ImageSizeBytes() int64 {
	if v.data.ImageSizeBytes == nil {
		return 0
	}
	return *v.data.ImageSizeBytes
}

func (v *RestoreResult) SetImageSizeBytes(image_size_bytes int64) {
	v.data.ImageSizeBytes = &image_size_bytes
}

func (v *RestoreResult) HasCreated() bool {
	return v.data.Created != nil
}

func (v *RestoreResult) Created() bool {
	if v.data.Created == nil {
		return false
	}
	return *v.data.Created
}

func (v *RestoreResult) SetCreated(created bool) {
	v.data.Created = &created
}

func (v *RestoreResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RestoreResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RestoreResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RestoreResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupBackupArgsData struct {
	Disk     *string         `cbor:"0,keyasint,omitempty" json:"disk,omitempty"`
	ToCloud  *bool           `cbor:"1,keyasint,omitempty" json:"to_cloud,omitempty"`
	Pin      *string         `cbor:"2,keyasint,omitempty" json:"pin,omitempty"`
	Data     *rpc.Capability `cbor:"3,keyasint,omitempty" json:"data,omitempty"`
	Progress *rpc.Capability `cbor:"4,keyasint,omitempty" json:"progress,omitempty"`
}

type DiskBackupBackupArgs struct {
	call rpc.Call
	data diskBackupBackupArgsData
}

func (v *DiskBackupBackupArgs) HasDisk() bool {
	return v.data.Disk != nil
}

func (v *DiskBackupBackupArgs) Disk() string {
	if v.data.Disk == nil {
		return ""
	}
	return *v.data.Disk
}

func (v *DiskBackupBackupArgs) HasToCloud() bool {
	return v.data.ToCloud != nil
}

func (v *DiskBackupBackupArgs) ToCloud() bool {
	if v.data.ToCloud == nil {
		return false
	}
	return *v.data.ToCloud
}

func (v *DiskBackupBackupArgs) HasPin() bool {
	return v.data.Pin != nil
}

func (v *DiskBackupBackupArgs) Pin() string {
	if v.data.Pin == nil {
		return ""
	}
	return *v.data.Pin
}

func (v *DiskBackupBackupArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *DiskBackupBackupArgs) Data() *stream.SendStreamClient[[]byte] {
	if v.data.Data == nil {
		return nil
	}
	return &stream.SendStreamClient[[]byte]{Client: v.call.NewClient(v.data.Data)}
}

func (v *DiskBackupBackupArgs) HasProgress() bool {
	return v.data.Progress != nil
}

func (v *DiskBackupBackupArgs) Progress() *stream.SendStreamClient[*Progress] {
	if v.data.Progress == nil {
		return nil
	}
	return &stream.SendStreamClient[*Progress]{Client: v.call.NewClient(v.data.Progress)}
}

func (v *DiskBackupBackupArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupBackupArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupBackupArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupBackupArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupBackupResultsData struct {
	Result **BackupResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type DiskBackupBackupResults struct {
	call rpc.Call
	data diskBackupBackupResultsData
}

func (v *DiskBackupBackupResults) SetResult(result **BackupResult) {
	v.data.Result = result
}

func (v *DiskBackupBackupResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupBackupResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupBackupResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupBackupResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupListBackupsArgsData struct {
	Disk *string `cbor:"0,keyasint,omitempty" json:"disk,omitempty"`
}

type DiskBackupListBackupsArgs struct {
	call rpc.Call
	data diskBackupListBackupsArgsData
}

func (v *DiskBackupListBackupsArgs) HasDisk() bool {
	return v.data.Disk != nil
}

func (v *DiskBackupListBackupsArgs) Disk() string {
	if v.data.Disk == nil {
		return ""
	}
	return *v.data.Disk
}

func (v *DiskBackupListBackupsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupListBackupsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupListBackupsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupListBackupsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupListBackupsResultsData struct {
	Points *[]*RestorePoint `cbor:"0,keyasint,omitempty" json:"points,omitempty"`
}

type DiskBackupListBackupsResults struct {
	call rpc.Call
	data diskBackupListBackupsResultsData
}

func (v *DiskBackupListBackupsResults) SetPoints(points []*RestorePoint) {
	x := slices.Clone(points)
	v.data.Points = &x
}

func (v *DiskBackupListBackupsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupListBackupsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupListBackupsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupListBackupsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupRestoreArgsData struct {
	Disk         *string         `cbor:"0,keyasint,omitempty" json:"disk,omitempty"`
	RestorePoint *string         `cbor:"1,keyasint,omitempty" json:"restore_point,omitempty"`
	Data         *rpc.Capability `cbor:"2,keyasint,omitempty" json:"data,omitempty"`
	Force        *bool           `cbor:"3,keyasint,omitempty" json:"force,omitempty"`
	Progress     *rpc.Capability `cbor:"4,keyasint,omitempty" json:"progress,omitempty"`
}

type DiskBackupRestoreArgs struct {
	call rpc.Call
	data diskBackupRestoreArgsData
}

func (v *DiskBackupRestoreArgs) HasDisk() bool {
	return v.data.Disk != nil
}

func (v *DiskBackupRestoreArgs) Disk() string {
	if v.data.Disk == nil {
		return ""
	}
	return *v.data.Disk
}

func (v *DiskBackupRestoreArgs) HasRestorePoint() bool {
	return v.data.RestorePoint != nil
}

func (v *DiskBackupRestoreArgs) RestorePoint() string {
	if v.data.RestorePoint == nil {
		return ""
	}
	return *v.data.RestorePoint
}

func (v *DiskBackupRestoreArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *DiskBackupRestoreArgs) Data() *stream.RecvStreamClient[[]byte] {
	if v.data.Data == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Data)}
}

func (v *DiskBackupRestoreArgs) HasForce() bool {
	return v.data.Force != nil
}

func (v *DiskBackupRestoreArgs) Force() bool {
	if v.data.Force == nil {
		return false
	}
	return *v.data.Force
}

func (v *DiskBackupRestoreArgs) HasProgress() bool {
	return v.data.Progress != nil
}

func (v *DiskBackupRestoreArgs) Progress() *stream.SendStreamClient[*Progress] {
	if v.data.Progress == nil {
		return nil
	}
	return &stream.SendStreamClient[*Progress]{Client: v.call.NewClient(v.data.Progress)}
}

func (v *DiskBackupRestoreArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupRestoreArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupRestoreArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupRestoreArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskBackupRestoreResultsData struct {
	Result **RestoreResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type DiskBackupRestoreResults struct {
	call rpc.Call
	data diskBackupRestoreResultsData
}

func (v *DiskBackupRestoreResults) SetResult(result **RestoreResult) {
	v.data.Result = result
}

func (v *DiskBackupRestoreResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskBackupRestoreResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskBackupRestoreResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskBackupRestoreResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DiskBackupBackup struct {
	rpc.Call
	args    DiskBackupBackupArgs
	results DiskBackupBackupResults
}

func (t *DiskBackupBackup) Args() *DiskBackupBackupArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DiskBackupBackup) Results() *DiskBackupBackupResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DiskBackupListBackups struct {
	rpc.Call
	args    DiskBackupListBackupsArgs
	results DiskBackupListBackupsResults
}

func (t *DiskBackupListBackups) Args() *DiskBackupListBackupsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DiskBackupListBackups) Results() *DiskBackupListBackupsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DiskBackupRestore struct {
	rpc.Call
	args    DiskBackupRestoreArgs
	results DiskBackupRestoreResults
}

func (t *DiskBackupRestore) Args() *DiskBackupRestoreArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DiskBackupRestore) Results() *DiskBackupRestoreResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DiskBackup interface {
	Backup(ctx context.Context, state *DiskBackupBackup) error
	ListBackups(ctx context.Context, state *DiskBackupListBackups) error
	Restore(ctx context.Context, state *DiskBackupRestore) error
}

type reexportDiskBackup struct {
	client rpc.Client
}

func (reexportDiskBackup) Backup(ctx context.Context, state *DiskBackupBackup) error {
	panic("not implemented")
}

func (reexportDiskBackup) ListBackups(ctx context.Context, state *DiskBackupListBackups) error {
	panic("not implemented")
}

func (reexportDiskBackup) Restore(ctx context.Context, state *DiskBackupRestore) error {
	panic("not implemented")
}

func (t reexportDiskBackup) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDiskBackup(t DiskBackup) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "backup",
			InterfaceName: "DiskBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"disk", "to_cloud", "pin", "data", "progress"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Backup(ctx, &DiskBackupBackup{Call: call})
			},
		},
		{
			Name:          "listBackups",
			InterfaceName: "DiskBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"disk"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListBackups(ctx, &DiskBackupListBackups{Call: call})
			},
		},
		{
			Name:          "restore",
			InterfaceName: "DiskBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"disk", "restore_point", "data", "force", "progress"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Restore(ctx, &DiskBackupRestore{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DiskBackupClient struct {
	rpc.Client
}

func NewDiskBackupClient(client rpc.Client) *DiskBackupClient {
	return &DiskBackupClient{Client: client}
}

func (c DiskBackupClient) Export() DiskBackup {
	return reexportDiskBackup{client: c.Client}
}

type DiskBackupClientBackupResults struct {
	client rpc.Client
	data   diskBackupBackupResultsData
}

func (v *DiskBackupClientBackupResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *DiskBackupClientBackupResults) Result() *BackupResult {
	if v.data.Result == nil {
		return nil
	}
	return *v.data.Result
}

func (v DiskBackupClient) Backup(ctx context.Context, disk string, to_cloud bool, pin string, data stream.SendStream[[]byte], progress stream.SendStream[*Progress]) (*DiskBackupClientBackupResults, error) {
	args := DiskBackupBackupArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Disk = &disk
	args.data.ToCloud = &to_cloud
	args.data.Pin = &pin
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[[]byte](data), data)
		args.data.Data = c
		caps[oid] = ic
	}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*Progress](progress), progress)
		args.data.Progress = c
		caps[oid] = ic
	}

	var ret diskBackupBackupResultsData

	err := v.CallWithCaps(ctx, "backup", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &DiskBackupClientBackupResults{client: v.Client, data: ret}, nil
}

type DiskBackupClientListBackupsResults struct {
	client rpc.Client
	data   diskBackupListBackupsResultsData
}

func (v *DiskBackupClientListBackupsResults) HasPoints() bool {
	return v.data.Points != nil
}

func (v *DiskBackupClientListBackupsResults) Points() []*RestorePoint {
	if v.data.Points == nil {
		return nil
	}
	return *v.data.Points
}

func (v DiskBackupClient) ListBackups(ctx context.Context, disk string) (*DiskBackupClientListBackupsResults, error) {
	args := DiskBackupListBackupsArgs{}
	args.data.Disk = &disk

	var ret diskBackupListBackupsResultsData

	err := v.Call(ctx, "listBackups", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DiskBackupClientListBackupsResults{client: v.Client, data: ret}, nil
}

type DiskBackupClientRestoreResults struct {
	client rpc.Client
	data   diskBackupRestoreResultsData
}

func (v *DiskBackupClientRestoreResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *DiskBackupClientRestoreResults) Result() *RestoreResult {
	if v.data.Result == nil {
		return nil
	}
	return *v.data.Result
}

func (v DiskBackupClient) Restore(ctx context.Context, disk string, restore_point string, data stream.RecvStream[[]byte], force bool, progress stream.SendStream[*Progress]) (*DiskBackupClientRestoreResults, error) {
	args := DiskBackupRestoreArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Disk = &disk
	args.data.RestorePoint = &restore_point
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](data), data)
		args.data.Data = c
		caps[oid] = ic
	}
	args.data.Force = &force
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*Progress](progress), progress)
		args.data.Progress = c
		caps[oid] = ic
	}

	var ret diskBackupRestoreResultsData

	err := v.CallWithCaps(ctx, "restore", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &DiskBackupClientRestoreResults{client: v.Client, data: ret}, nil
}
