// Package disk serves the DiskBackup RPC interface: backup and restore of disk
// images, driven by a client that need not be running on the server host.
//
// Two steps genuinely have to happen here rather than on the client. Moving the
// image bytes, because the image is a file in the server's data directory. And
// talking to miren.cloud, because that authenticates as the cluster using a key
// that lives only on the server and should never reach a laptop. Everything
// else — resolving which disk the operator means, deciding which restore point
// to use, writing a snapshot to a file — is orchestration the client does.
//
// See RFD-108.
package disk

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/diskresolve"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/snapshot"
)

// diskEntities is what the handlers need from the entity store: which disk the
// operator means, how to conjure one that does not exist yet, and which node
// stateful workloads run on.
type diskEntities interface {
	snapshot.DiskResolver
	snapshot.DiskCreator

	FindNodeId(ctx context.Context) (entity.Id, error)
}

// Server implements disk_v1alpha.DiskBackup.
type Server struct {
	log      *slog.Logger
	disks    diskEntities
	dataPath string

	// Recovering a deleted disk rebuilds entities from what the holding area
	// recorded, rather than from anything a caller supplied, so it writes to
	// the entity store directly instead of through the resolver.
	eac *entityserver_v1alpha.EntityAccessClient
	ec  *entityserver.Client

	// updates is nil on a cluster with no miren.cloud registration. The
	// streaming paths still work in that case; only the cloud ones refuse.
	updates diskio.CloudUpdatesClient

	// mntOps answers "is this image currently in use". It reads kernel state
	// directly, so it needs no handle on the runner's controllers.
	mntOps diskio.DiskMountOps

	// transfers serializes concurrent work on one transfer id, which is the
	// only shared mutable state these handlers have.
	transfers *transferLocks
}

// NewServer builds the disk backup service. updates may be nil, which means the
// cluster has no cloud to back up to.
func NewServer(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	ec *entityserver.Client,
	dataPath string,
	updates diskio.CloudUpdatesClient,
) *Server {
	log = log.With("module", "disk-backup")
	return &Server{
		log:       log,
		disks:     diskresolve.New(eac, ec),
		dataPath:  dataPath,
		eac:       eac,
		ec:        ec,
		updates:   updates,
		mntOps:    diskio.NewRealDiskMountOps(log),
		transfers: newTransferLocks(),
	}
}

// errCategory groups this service's errors in the RPC error envelope.
const errCategory = "disk-backup"

// refuse reports a request this server will not carry out, as opposed to one
// that failed partway. It travels as a validation failure so the client prints
// the message on its own rather than wrapping it in "remote error: generic
// unknown", which buries the sentence an operator needs to read.
func refuse(format string, args ...any) error {
	return cond.ValidationFailure(errCategory, fmt.Sprintf(format, args...))
}

// errNoCloud is what every cloud-dependent path reports. It names the command
// that would fix it, because "no cloud configured" on its own leaves an
// operator guessing whether that is a bug or a setup step they skipped.
func errNoCloud(what string) error {
	return refuse(
		"%s needs a miren.cloud registration, and this cluster has none — run `miren register` first, or back up to a local file instead",
		what,
	)
}

// liveImageDevice reports the loop device currently backing an image, or "" if
// there is none.
//
// This is the check that matters before rewriting an image, and it is not the
// same question as "does a lease exist". LoopAttach hands the kernel an open
// file descriptor, so a loop device is bound to the image's inode and not to
// its path. Renaming a fresh image over an attached one therefore changes
// nothing an operator can see: the old inode survives unlinked, the mounted
// filesystem keeps reading and writing it, and the restore reports success
// having accomplished nothing.
//
// A lease check does not cover this. Universal-mode volumes are mounted by the
// volume controller whether or not anything has leased them, and releasing the
// last lease deliberately leaves the mount up.
func (s *Server) liveImageDevice(imagePath string) (string, error) {
	dev, err := s.mntOps.FindLoopByBacking(imagePath)
	if err != nil {
		// Fail closed. Not knowing whether the image is in use is not the same
		// as knowing it is idle, and guessing wrong here loses data silently.
		return "", fmt.Errorf("checking whether %s is in use: %w", imagePath, err)
	}
	return dev, nil
}

// progressSink adapts the client's progress stream to something the handlers
// can call without checking for nil or caring about send failures. A client
// that has stopped listening should not fail an in-flight backup.
type progressSink struct {
	send func(*disk_v1alpha.Progress)
}

func (p progressSink) Message(format string, args ...any) {
	up := new(disk_v1alpha.Progress)
	up.Update().SetMessage(fmt.Sprintf(format, args...))
	p.send(up)
}

// Warn sends a warning to the client. Callers log the underlying condition
// themselves, once, rather than having this emit a server log line per line of
// operator-facing text.
func (p progressSink) Warn(format string, args ...any) {
	up := new(disk_v1alpha.Progress)
	up.Update().SetWarning(fmt.Sprintf(format, args...))
	p.send(up)
}

func (p progressSink) Transfer(done, total, perSecond, etaSeconds int64) {
	t := new(disk_v1alpha.Transfer)
	t.SetDone(done)
	t.SetTotal(total)
	t.SetBytesPerSecond(perSecond)
	t.SetEtaSeconds(etaSeconds)

	up := new(disk_v1alpha.Progress)
	up.Update().SetTransfer(t)
	p.send(up)
}

// newProgress wraps a client's progress stream, tolerating a caller that did
// not supply one.
func (s *Server) newProgress(ctx context.Context, out *stream.SendStreamClient[*disk_v1alpha.Progress]) progressSink {
	return progressSink{
		send: func(up *disk_v1alpha.Progress) {
			if out == nil {
				return
			}
			if _, err := out.Send(ctx, up); err != nil {
				s.log.Debug("dropping progress event", "error", err)
			}
		},
	}
}

// prepareBackup resolves a disk to an image on this host.
func (s *Server) prepareBackup(ctx context.Context, name string) (*snapshot.BackupTarget, error) {
	if name == "" {
		return nil, refuse("disk name is required")
	}
	return snapshot.PrepareBackup(ctx, s.disks, name, s.dataPath)
}

// restorePointFromUpdate converts a cloud update into the restore point a
// client picks from.
//
// The cloud's UpdateInfo carries no timestamp, so it is recovered from the
// ordering key, whose format is fixed per update kind: loop_image keys are Unix
// nanoseconds in 16 hex digits.
func restorePointFromUpdate(u diskio.UpdateInfo) *disk_v1alpha.RestorePoint {
	rp := new(disk_v1alpha.RestorePoint)
	rp.SetId(u.UpdateID)
	rp.SetSizeBytes(u.Size)
	rp.SetName(u.SnapshotName)
	rp.SetMode(modeForKind(u.Kind))
	if ts := timestamp(orderingKeyTime(u)); ts != nil {
		rp.SetCreatedAt(ts)
	}
	return rp
}

func modeForKind(kind string) string {
	switch diskio.UpdateKind(kind) {
	case diskio.KindLoopImage:
		return "universal"
	case diskio.KindLBDLog:
		return "accelerator"
	default:
		return kind
	}
}

func orderingKeyTime(u diskio.UpdateInfo) time.Time {
	if diskio.UpdateKind(u.Kind) != diskio.KindLoopImage {
		return time.Time{}
	}
	var nanos int64
	if _, err := fmt.Sscanf(u.OrderingKey, "%016x", &nanos); err != nil {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func timestamp(t time.Time) *standard.Timestamp {
	if t.IsZero() {
		return nil
	}
	ts := new(standard.Timestamp)
	ts.SetSeconds(t.Unix())
	ts.SetNanoseconds(int32(t.Nanosecond()))
	return ts
}

// stagingDir is where a snapshot is compressed before it is uploaded or
// streamed. It sits beside the image so the write stays on the same filesystem
// as the data it came from, which is the one sized for it.
func (s *Server) stagingDir(imagePath string) string {
	return filepath.Dir(imagePath)
}
