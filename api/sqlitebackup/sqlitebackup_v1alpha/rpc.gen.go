package sqlitebackup_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

type lTXFileData struct {
	Level             *int32              `cbor:"0,keyasint,omitempty" json:"level,omitempty"`
	MinTxid           *uint64             `cbor:"1,keyasint,omitempty" json:"min_txid,omitempty"`
	MaxTxid           *uint64             `cbor:"2,keyasint,omitempty" json:"max_txid,omitempty"`
	PreApplyChecksum  *uint64             `cbor:"3,keyasint,omitempty" json:"pre_apply_checksum,omitempty"`
	PostApplyChecksum *uint64             `cbor:"4,keyasint,omitempty" json:"post_apply_checksum,omitempty"`
	Size              *int64              `cbor:"5,keyasint,omitempty" json:"size,omitempty"`
	CreatedAt         *standard.Timestamp `cbor:"6,keyasint,omitempty" json:"created_at,omitempty"`
}

type LTXFile struct {
	data lTXFileData
}

func (v *LTXFile) HasLevel() bool {
	return v.data.Level != nil
}

func (v *LTXFile) Level() int32 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v *LTXFile) SetLevel(level int32) {
	v.data.Level = &level
}

func (v *LTXFile) HasMinTxid() bool {
	return v.data.MinTxid != nil
}

func (v *LTXFile) MinTxid() uint64 {
	if v.data.MinTxid == nil {
		return 0
	}
	return *v.data.MinTxid
}

func (v *LTXFile) SetMinTxid(min_txid uint64) {
	v.data.MinTxid = &min_txid
}

func (v *LTXFile) HasMaxTxid() bool {
	return v.data.MaxTxid != nil
}

func (v *LTXFile) MaxTxid() uint64 {
	if v.data.MaxTxid == nil {
		return 0
	}
	return *v.data.MaxTxid
}

func (v *LTXFile) SetMaxTxid(max_txid uint64) {
	v.data.MaxTxid = &max_txid
}

func (v *LTXFile) HasPreApplyChecksum() bool {
	return v.data.PreApplyChecksum != nil
}

func (v *LTXFile) PreApplyChecksum() uint64 {
	if v.data.PreApplyChecksum == nil {
		return 0
	}
	return *v.data.PreApplyChecksum
}

func (v *LTXFile) SetPreApplyChecksum(pre_apply_checksum uint64) {
	v.data.PreApplyChecksum = &pre_apply_checksum
}

func (v *LTXFile) HasPostApplyChecksum() bool {
	return v.data.PostApplyChecksum != nil
}

func (v *LTXFile) PostApplyChecksum() uint64 {
	if v.data.PostApplyChecksum == nil {
		return 0
	}
	return *v.data.PostApplyChecksum
}

func (v *LTXFile) SetPostApplyChecksum(post_apply_checksum uint64) {
	v.data.PostApplyChecksum = &post_apply_checksum
}

func (v *LTXFile) HasSize() bool {
	return v.data.Size != nil
}

func (v *LTXFile) Size() int64 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *LTXFile) SetSize(size int64) {
	v.data.Size = &size
}

func (v *LTXFile) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *LTXFile) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *LTXFile) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *LTXFile) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LTXFile) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LTXFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LTXFile) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupListLTXFilesArgsData struct {
	Key      *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Level    *int32  `cbor:"1,keyasint,omitempty" json:"level,omitempty"`
	SeekTxid *uint64 `cbor:"2,keyasint,omitempty" json:"seek_txid,omitempty"`
}

type SqliteBackupListLTXFilesArgs struct {
	call rpc.Call
	data sqliteBackupListLTXFilesArgsData
}

func (v *SqliteBackupListLTXFilesArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *SqliteBackupListLTXFilesArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *SqliteBackupListLTXFilesArgs) HasLevel() bool {
	return v.data.Level != nil
}

func (v *SqliteBackupListLTXFilesArgs) Level() int32 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v *SqliteBackupListLTXFilesArgs) HasSeekTxid() bool {
	return v.data.SeekTxid != nil
}

func (v *SqliteBackupListLTXFilesArgs) SeekTxid() uint64 {
	if v.data.SeekTxid == nil {
		return 0
	}
	return *v.data.SeekTxid
}

func (v *SqliteBackupListLTXFilesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupListLTXFilesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupListLTXFilesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupListLTXFilesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupListLTXFilesResultsData struct {
	Files *[]*LTXFile `cbor:"0,keyasint,omitempty" json:"files,omitempty"`
}

type SqliteBackupListLTXFilesResults struct {
	call rpc.Call
	data sqliteBackupListLTXFilesResultsData
}

func (v *SqliteBackupListLTXFilesResults) SetFiles(files []*LTXFile) {
	x := slices.Clone(files)
	v.data.Files = &x
}

func (v *SqliteBackupListLTXFilesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupListLTXFilesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupListLTXFilesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupListLTXFilesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupOpenLTXFileArgsData struct {
	Key     *string         `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Level   *int32          `cbor:"1,keyasint,omitempty" json:"level,omitempty"`
	MinTxid *uint64         `cbor:"2,keyasint,omitempty" json:"min_txid,omitempty"`
	MaxTxid *uint64         `cbor:"3,keyasint,omitempty" json:"max_txid,omitempty"`
	Offset  *int64          `cbor:"4,keyasint,omitempty" json:"offset,omitempty"`
	Size    *int64          `cbor:"5,keyasint,omitempty" json:"size,omitempty"`
	Data    *rpc.Capability `cbor:"6,keyasint,omitempty" json:"data,omitempty"`
}

type SqliteBackupOpenLTXFileArgs struct {
	call rpc.Call
	data sqliteBackupOpenLTXFileArgsData
}

func (v *SqliteBackupOpenLTXFileArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *SqliteBackupOpenLTXFileArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *SqliteBackupOpenLTXFileArgs) HasLevel() bool {
	return v.data.Level != nil
}

func (v *SqliteBackupOpenLTXFileArgs) Level() int32 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v *SqliteBackupOpenLTXFileArgs) HasMinTxid() bool {
	return v.data.MinTxid != nil
}

func (v *SqliteBackupOpenLTXFileArgs) MinTxid() uint64 {
	if v.data.MinTxid == nil {
		return 0
	}
	return *v.data.MinTxid
}

func (v *SqliteBackupOpenLTXFileArgs) HasMaxTxid() bool {
	return v.data.MaxTxid != nil
}

func (v *SqliteBackupOpenLTXFileArgs) MaxTxid() uint64 {
	if v.data.MaxTxid == nil {
		return 0
	}
	return *v.data.MaxTxid
}

func (v *SqliteBackupOpenLTXFileArgs) HasOffset() bool {
	return v.data.Offset != nil
}

func (v *SqliteBackupOpenLTXFileArgs) Offset() int64 {
	if v.data.Offset == nil {
		return 0
	}
	return *v.data.Offset
}

func (v *SqliteBackupOpenLTXFileArgs) HasSize() bool {
	return v.data.Size != nil
}

func (v *SqliteBackupOpenLTXFileArgs) Size() int64 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *SqliteBackupOpenLTXFileArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *SqliteBackupOpenLTXFileArgs) Data() *stream.SendStreamClient[[]byte] {
	if v.data.Data == nil {
		return nil
	}
	return &stream.SendStreamClient[[]byte]{Client: v.call.NewClient(v.data.Data)}
}

func (v *SqliteBackupOpenLTXFileArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupOpenLTXFileArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupOpenLTXFileArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupOpenLTXFileArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupOpenLTXFileResultsData struct{}

type SqliteBackupOpenLTXFileResults struct {
	call rpc.Call
	data sqliteBackupOpenLTXFileResultsData
}

func (v *SqliteBackupOpenLTXFileResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupOpenLTXFileResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupOpenLTXFileResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupOpenLTXFileResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupWriteLTXFileArgsData struct {
	Key     *string         `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Level   *int32          `cbor:"1,keyasint,omitempty" json:"level,omitempty"`
	MinTxid *uint64         `cbor:"2,keyasint,omitempty" json:"min_txid,omitempty"`
	MaxTxid *uint64         `cbor:"3,keyasint,omitempty" json:"max_txid,omitempty"`
	Data    *rpc.Capability `cbor:"4,keyasint,omitempty" json:"data,omitempty"`
}

type SqliteBackupWriteLTXFileArgs struct {
	call rpc.Call
	data sqliteBackupWriteLTXFileArgsData
}

func (v *SqliteBackupWriteLTXFileArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *SqliteBackupWriteLTXFileArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *SqliteBackupWriteLTXFileArgs) HasLevel() bool {
	return v.data.Level != nil
}

func (v *SqliteBackupWriteLTXFileArgs) Level() int32 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v *SqliteBackupWriteLTXFileArgs) HasMinTxid() bool {
	return v.data.MinTxid != nil
}

func (v *SqliteBackupWriteLTXFileArgs) MinTxid() uint64 {
	if v.data.MinTxid == nil {
		return 0
	}
	return *v.data.MinTxid
}

func (v *SqliteBackupWriteLTXFileArgs) HasMaxTxid() bool {
	return v.data.MaxTxid != nil
}

func (v *SqliteBackupWriteLTXFileArgs) MaxTxid() uint64 {
	if v.data.MaxTxid == nil {
		return 0
	}
	return *v.data.MaxTxid
}

func (v *SqliteBackupWriteLTXFileArgs) HasData() bool {
	return v.data.Data != nil
}

func (v *SqliteBackupWriteLTXFileArgs) Data() *stream.RecvStreamClient[[]byte] {
	if v.data.Data == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Data)}
}

func (v *SqliteBackupWriteLTXFileArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupWriteLTXFileArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupWriteLTXFileArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupWriteLTXFileArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupWriteLTXFileResultsData struct {
	File **LTXFile `cbor:"0,keyasint,omitempty" json:"file,omitempty"`
}

type SqliteBackupWriteLTXFileResults struct {
	call rpc.Call
	data sqliteBackupWriteLTXFileResultsData
}

func (v *SqliteBackupWriteLTXFileResults) SetFile(file **LTXFile) {
	v.data.File = file
}

func (v *SqliteBackupWriteLTXFileResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupWriteLTXFileResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupWriteLTXFileResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupWriteLTXFileResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupDeleteLTXFilesArgsData struct {
	Key   *string     `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Files *[]*LTXFile `cbor:"1,keyasint,omitempty" json:"files,omitempty"`
}

type SqliteBackupDeleteLTXFilesArgs struct {
	call rpc.Call
	data sqliteBackupDeleteLTXFilesArgsData
}

func (v *SqliteBackupDeleteLTXFilesArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *SqliteBackupDeleteLTXFilesArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *SqliteBackupDeleteLTXFilesArgs) HasFiles() bool {
	return v.data.Files != nil
}

func (v *SqliteBackupDeleteLTXFilesArgs) Files() []*LTXFile {
	if v.data.Files == nil {
		return nil
	}
	return *v.data.Files
}

func (v *SqliteBackupDeleteLTXFilesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupDeleteLTXFilesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupDeleteLTXFilesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupDeleteLTXFilesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupDeleteLTXFilesResultsData struct{}

type SqliteBackupDeleteLTXFilesResults struct {
	call rpc.Call
	data sqliteBackupDeleteLTXFilesResultsData
}

func (v *SqliteBackupDeleteLTXFilesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupDeleteLTXFilesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupDeleteLTXFilesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupDeleteLTXFilesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupDeleteAllArgsData struct {
	Key *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
}

type SqliteBackupDeleteAllArgs struct {
	call rpc.Call
	data sqliteBackupDeleteAllArgsData
}

func (v *SqliteBackupDeleteAllArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *SqliteBackupDeleteAllArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *SqliteBackupDeleteAllArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupDeleteAllArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupDeleteAllArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupDeleteAllArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type sqliteBackupDeleteAllResultsData struct{}

type SqliteBackupDeleteAllResults struct {
	call rpc.Call
	data sqliteBackupDeleteAllResultsData
}

func (v *SqliteBackupDeleteAllResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SqliteBackupDeleteAllResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SqliteBackupDeleteAllResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SqliteBackupDeleteAllResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SqliteBackupListLTXFiles struct {
	rpc.Call
	args    SqliteBackupListLTXFilesArgs
	results SqliteBackupListLTXFilesResults
}

func (t *SqliteBackupListLTXFiles) Args() *SqliteBackupListLTXFilesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SqliteBackupListLTXFiles) Results() *SqliteBackupListLTXFilesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SqliteBackupOpenLTXFile struct {
	rpc.Call
	args    SqliteBackupOpenLTXFileArgs
	results SqliteBackupOpenLTXFileResults
}

func (t *SqliteBackupOpenLTXFile) Args() *SqliteBackupOpenLTXFileArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SqliteBackupOpenLTXFile) Results() *SqliteBackupOpenLTXFileResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SqliteBackupWriteLTXFile struct {
	rpc.Call
	args    SqliteBackupWriteLTXFileArgs
	results SqliteBackupWriteLTXFileResults
}

func (t *SqliteBackupWriteLTXFile) Args() *SqliteBackupWriteLTXFileArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SqliteBackupWriteLTXFile) Results() *SqliteBackupWriteLTXFileResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SqliteBackupDeleteLTXFiles struct {
	rpc.Call
	args    SqliteBackupDeleteLTXFilesArgs
	results SqliteBackupDeleteLTXFilesResults
}

func (t *SqliteBackupDeleteLTXFiles) Args() *SqliteBackupDeleteLTXFilesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SqliteBackupDeleteLTXFiles) Results() *SqliteBackupDeleteLTXFilesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SqliteBackupDeleteAll struct {
	rpc.Call
	args    SqliteBackupDeleteAllArgs
	results SqliteBackupDeleteAllResults
}

func (t *SqliteBackupDeleteAll) Args() *SqliteBackupDeleteAllArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SqliteBackupDeleteAll) Results() *SqliteBackupDeleteAllResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SqliteBackup interface {
	ListLTXFiles(ctx context.Context, state *SqliteBackupListLTXFiles) error
	OpenLTXFile(ctx context.Context, state *SqliteBackupOpenLTXFile) error
	WriteLTXFile(ctx context.Context, state *SqliteBackupWriteLTXFile) error
	DeleteLTXFiles(ctx context.Context, state *SqliteBackupDeleteLTXFiles) error
	DeleteAll(ctx context.Context, state *SqliteBackupDeleteAll) error
}

type reexportSqliteBackup struct {
	client rpc.Client
}

func (reexportSqliteBackup) ListLTXFiles(ctx context.Context, state *SqliteBackupListLTXFiles) error {
	panic("not implemented")
}

func (reexportSqliteBackup) OpenLTXFile(ctx context.Context, state *SqliteBackupOpenLTXFile) error {
	panic("not implemented")
}

func (reexportSqliteBackup) WriteLTXFile(ctx context.Context, state *SqliteBackupWriteLTXFile) error {
	panic("not implemented")
}

func (reexportSqliteBackup) DeleteLTXFiles(ctx context.Context, state *SqliteBackupDeleteLTXFiles) error {
	panic("not implemented")
}

func (reexportSqliteBackup) DeleteAll(ctx context.Context, state *SqliteBackupDeleteAll) error {
	panic("not implemented")
}

func (t reexportSqliteBackup) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSqliteBackup(t SqliteBackup) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "listLTXFiles",
			InterfaceName: "SqliteBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"key", "level", "seek_txid"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListLTXFiles(ctx, &SqliteBackupListLTXFiles{Call: call})
			},
		},
		{
			Name:          "openLTXFile",
			InterfaceName: "SqliteBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"key", "level", "min_txid", "max_txid", "offset", "size", "data"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.OpenLTXFile(ctx, &SqliteBackupOpenLTXFile{Call: call})
			},
		},
		{
			Name:          "writeLTXFile",
			InterfaceName: "SqliteBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"key", "level", "min_txid", "max_txid", "data"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.WriteLTXFile(ctx, &SqliteBackupWriteLTXFile{Call: call})
			},
		},
		{
			Name:          "deleteLTXFiles",
			InterfaceName: "SqliteBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"key", "files"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeleteLTXFiles(ctx, &SqliteBackupDeleteLTXFiles{Call: call})
			},
		},
		{
			Name:          "deleteAll",
			InterfaceName: "SqliteBackup",
			Index:         0,
			Public:        false,
			Params:        []string{"key"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeleteAll(ctx, &SqliteBackupDeleteAll{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SqliteBackupClient struct {
	rpc.Client
}

func NewSqliteBackupClient(client rpc.Client) *SqliteBackupClient {
	return &SqliteBackupClient{Client: client}
}

func (c SqliteBackupClient) Export() SqliteBackup {
	return reexportSqliteBackup{client: c.Client}
}

type SqliteBackupClientListLTXFilesResults struct {
	client rpc.Client
	data   sqliteBackupListLTXFilesResultsData
}

func (v *SqliteBackupClientListLTXFilesResults) HasFiles() bool {
	return v.data.Files != nil
}

func (v *SqliteBackupClientListLTXFilesResults) Files() []*LTXFile {
	if v.data.Files == nil {
		return nil
	}
	return *v.data.Files
}

func (v SqliteBackupClient) ListLTXFiles(ctx context.Context, key string, level int32, seek_txid uint64) (*SqliteBackupClientListLTXFilesResults, error) {
	args := SqliteBackupListLTXFilesArgs{}
	args.data.Key = &key
	args.data.Level = &level
	args.data.SeekTxid = &seek_txid

	var ret sqliteBackupListLTXFilesResultsData

	err := v.Call(ctx, "listLTXFiles", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SqliteBackupClientListLTXFilesResults{client: v.Client, data: ret}, nil
}

type SqliteBackupClientOpenLTXFileResults struct {
	client rpc.Client
	data   sqliteBackupOpenLTXFileResultsData
}

func (v SqliteBackupClient) OpenLTXFile(ctx context.Context, key string, level int32, min_txid uint64, max_txid uint64, offset int64, size int64, data stream.SendStream[[]byte]) (*SqliteBackupClientOpenLTXFileResults, error) {
	args := SqliteBackupOpenLTXFileArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Key = &key
	args.data.Level = &level
	args.data.MinTxid = &min_txid
	args.data.MaxTxid = &max_txid
	args.data.Offset = &offset
	args.data.Size = &size
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[[]byte](data), data)
		args.data.Data = c
		caps[oid] = ic
	}

	var ret sqliteBackupOpenLTXFileResultsData

	err := v.CallWithCaps(ctx, "openLTXFile", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &SqliteBackupClientOpenLTXFileResults{client: v.Client, data: ret}, nil
}

type SqliteBackupClientWriteLTXFileResults struct {
	client rpc.Client
	data   sqliteBackupWriteLTXFileResultsData
}

func (v *SqliteBackupClientWriteLTXFileResults) HasFile() bool {
	return v.data.File != nil
}

func (v *SqliteBackupClientWriteLTXFileResults) File() *LTXFile {
	if v.data.File == nil {
		return nil
	}
	return *v.data.File
}

func (v SqliteBackupClient) WriteLTXFile(ctx context.Context, key string, level int32, min_txid uint64, max_txid uint64, data stream.RecvStream[[]byte]) (*SqliteBackupClientWriteLTXFileResults, error) {
	args := SqliteBackupWriteLTXFileArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Key = &key
	args.data.Level = &level
	args.data.MinTxid = &min_txid
	args.data.MaxTxid = &max_txid
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](data), data)
		args.data.Data = c
		caps[oid] = ic
	}

	var ret sqliteBackupWriteLTXFileResultsData

	err := v.CallWithCaps(ctx, "writeLTXFile", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &SqliteBackupClientWriteLTXFileResults{client: v.Client, data: ret}, nil
}

type SqliteBackupClientDeleteLTXFilesResults struct {
	client rpc.Client
	data   sqliteBackupDeleteLTXFilesResultsData
}

func (v SqliteBackupClient) DeleteLTXFiles(ctx context.Context, key string, files []*LTXFile) (*SqliteBackupClientDeleteLTXFilesResults, error) {
	args := SqliteBackupDeleteLTXFilesArgs{}
	args.data.Key = &key
	x := slices.Clone(files)
	args.data.Files = &x

	var ret sqliteBackupDeleteLTXFilesResultsData

	err := v.Call(ctx, "deleteLTXFiles", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SqliteBackupClientDeleteLTXFilesResults{client: v.Client, data: ret}, nil
}

type SqliteBackupClientDeleteAllResults struct {
	client rpc.Client
	data   sqliteBackupDeleteAllResultsData
}

func (v SqliteBackupClient) DeleteAll(ctx context.Context, key string) (*SqliteBackupClientDeleteAllResults, error) {
	args := SqliteBackupDeleteAllArgs{}
	args.data.Key = &key

	var ret sqliteBackupDeleteAllResultsData

	err := v.Call(ctx, "deleteAll", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SqliteBackupClientDeleteAllResults{client: v.Client, data: ret}, nil
}
