// Package sqlitedisk replicates SQLite-provider disks from a runner to the
// coordinator's backup API. It supplies litestream with a ReplicaClient that
// speaks Miren RPC instead of S3,
// and a Manager that owns one replicated database per attached disk.
package sqlitedisk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/superfly/ltx"

	"github.com/benbjohnson/litestream"
	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

// ReplicaClientType identifies this backend in litestream's logs.
const ReplicaClientType = "miren"

// maxKeyLen matches the limit the coordinator enforces on backup keys.
const maxKeyLen = 255

// ReplicaClient stores a single database's LTX files on the coordinator.
type ReplicaClient struct {
	client *sqlitebackup_v1alpha.SqliteBackupClient
	key    string
	logger *slog.Logger
}

var _ litestream.ReplicaClient = (*ReplicaClient)(nil)

// NewReplicaClient returns a client that stores files under key.
func NewReplicaClient(log *slog.Logger, client *sqlitebackup_v1alpha.SqliteBackupClient, key string) *ReplicaClient {
	return &ReplicaClient{
		client: client,
		key:    key,
		logger: log.WithGroup(ReplicaClientType),
	}
}

func (c *ReplicaClient) Type() string { return ReplicaClientType }

// Init is a no-op: the RPC connection is established before the client is built.
func (c *ReplicaClient) Init(ctx context.Context) error { return nil }

func (c *ReplicaClient) SetLogger(logger *slog.Logger) {
	c.logger = logger.WithGroup(ReplicaClientType)
}

// LTXFiles lists the files stored at a level, starting at seek.
//
// useMetadata is ignored: the coordinator stamps each file's transaction
// timestamp onto its mtime at write time, so listed timestamps are always the
// accurate ones and never need a second metadata fetch.
func (c *ReplicaClient) LTXFiles(ctx context.Context, level int, seek ltx.TXID, useMetadata bool) (ltx.FileIterator, error) {
	res, err := c.client.ListLTXFiles(ctx, c.key, int32(level), uint64(seek))
	if err != nil {
		return nil, fmt.Errorf("list ltx files (key=%s level=%d): %w", c.key, level, err)
	}

	files := res.Files()
	infos := make([]*ltx.FileInfo, 0, len(files))
	for _, f := range files {
		infos = append(infos, &ltx.FileInfo{
			Level:             int(f.Level()),
			MinTXID:           ltx.TXID(f.MinTxid()),
			MaxTXID:           ltx.TXID(f.MaxTxid()),
			PreApplyChecksum:  ltx.Checksum(f.PreApplyChecksum()),
			PostApplyChecksum: ltx.Checksum(f.PostApplyChecksum()),
			Size:              f.Size(),
			CreatedAt:         standard.FromTimestamp(f.CreatedAt()).UTC(),
		})
	}

	return ltx.NewFileInfoSliceIterator(infos), nil
}

// OpenLTXFile streams one file back from the coordinator.
//
// The RPC call pushes bytes into a pipe and only returns once the transfer
// finishes, so it runs in the background while the caller reads. Peeking a
// single byte before returning forces a failure that happened before any data
// was sent — a missing file, most importantly — to surface here rather than on
// a later Read, because litestream branches on os.IsNotExist at the call site.
func (c *ReplicaClient) OpenLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, offset, size int64) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		_, err := c.client.OpenLTXFile(ctx, c.key, int32(level),
			uint64(minTXID), uint64(maxTXID), offset, size, stream.ServeWriter(ctx, pw))
		// A nil error closes the pipe cleanly, surfacing as io.EOF to the reader.
		_ = pw.CloseWithError(c.translateNotExist(err, level, minTXID, maxTXID))
	}()

	br := bufio.NewReader(pr)
	if _, err := br.Peek(1); err != nil && !errors.Is(err, io.EOF) {
		_ = pr.Close()
		return nil, err
	}

	return &pipeReadCloser{Reader: br, closer: pr}, nil
}

// WriteLTXFile uploads a file and returns the metadata the coordinator recorded.
func (c *ReplicaClient) WriteLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, rd io.Reader) (*ltx.FileInfo, error) {
	res, err := c.client.WriteLTXFile(ctx, c.key, int32(level), uint64(minTXID), uint64(maxTXID),
		stream.ServeReader(ctx, rd, stream.WithBulkBatching()))
	if err != nil {
		return nil, fmt.Errorf("write ltx file (key=%s level=%d txid=%s-%s): %w",
			c.key, level, minTXID, maxTXID, err)
	}

	f := res.File()
	if f == nil {
		return nil, fmt.Errorf("coordinator returned no metadata for written ltx file (key=%s)", c.key)
	}

	return &ltx.FileInfo{
		Level:     int(f.Level()),
		MinTXID:   ltx.TXID(f.MinTxid()),
		MaxTXID:   ltx.TXID(f.MaxTxid()),
		Size:      f.Size(),
		CreatedAt: standard.FromTimestamp(f.CreatedAt()).UTC(),
	}, nil
}

// DeleteLTXFiles removes the named files. Files already gone are not an error.
func (c *ReplicaClient) DeleteLTXFiles(ctx context.Context, a []*ltx.FileInfo) error {
	if len(a) == 0 {
		return nil
	}

	files := make([]*sqlitebackup_v1alpha.LTXFile, 0, len(a))
	for _, info := range a {
		var f sqlitebackup_v1alpha.LTXFile
		f.SetLevel(int32(info.Level))
		f.SetMinTxid(uint64(info.MinTXID))
		f.SetMaxTxid(uint64(info.MaxTXID))
		files = append(files, &f)
	}

	if _, err := c.client.DeleteLTXFiles(ctx, c.key, files); err != nil {
		return fmt.Errorf("delete ltx files (key=%s): %w", c.key, err)
	}
	return nil
}

// DeleteAll discards every file stored for this database.
func (c *ReplicaClient) DeleteAll(ctx context.Context) error {
	if _, err := c.client.DeleteAll(ctx, c.key); err != nil {
		return fmt.Errorf("delete all ltx files (key=%s): %w", c.key, err)
	}
	return nil
}

// translateNotExist converts the coordinator's typed not-found back into a
// value os.IsNotExist recognizes. It must be an *os.PathError: os.IsNotExist
// only unwraps a few concrete types, so a fmt.Errorf-wrapped os.ErrNotExist
// would silently fail the checks litestream makes.
func (c *ReplicaClient) translateNotExist(err error, level int, minTXID, maxTXID ltx.TXID) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, cond.ErrNotFound{}) || errors.Is(err, os.ErrNotExist) {
		return &os.PathError{
			Op:   "open",
			Path: fmt.Sprintf("%s/level-%d/%s", c.key, level, ltx.FormatFilename(minTXID, maxTXID)),
			Err:  os.ErrNotExist,
		}
	}
	return err
}

// pipeReadCloser reads through a buffer while closing the underlying pipe, so
// the peeked byte is not lost.
type pipeReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *pipeReadCloser) Close() error { return r.closer.Close() }

// BackupKey builds a coordinator backup key for one app's disk.
//
// The coordinator uses the key as a single directory name and rejects anything
// that is not [a-zA-Z0-9][a-zA-Z0-9._-]*, while app IDs contain a slash and
// volume names are user-supplied. Sanitizing alone could collide (two distinct
// inputs mapping to the same safe string), so a digest of the exact input is
// appended to keep distinct disks distinct.
func BackupKey(appID, volumeName string) string {
	sum := sha256.Sum256([]byte(appID + "\x00" + volumeName))
	digest := hex.EncodeToString(sum[:])[:12]

	prefix := sanitizeKeyPart(appID + "-" + volumeName)
	if prefix == "" {
		prefix = "disk"
	}

	// Leave room for the separator and digest.
	if max := maxKeyLen - len(digest) - 1; len(prefix) > max {
		prefix = prefix[:max]
	}

	return prefix + "-" + digest
}

func sanitizeKeyPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	// The first character must be alphanumeric.
	return strings.TrimLeft(b.String(), "._-")
}
