package etcd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"go.etcd.io/etcd/client/v3/snapshot"
	"go.uber.org/zap"
	"miren.dev/runtime/pkg/slogout"
)

const (
	// restoreContainerName is the one-shot etcdutl container a restore runs.
	restoreContainerName = ContainerName + "-restore"
	// restoreStagingDir, under the component data path, is where etcdutl
	// builds the new data directory before it is swapped into place.
	restoreStagingDir = "etcd-restore"
	// replacedDirPrefix names the data directory a restore moved aside, kept
	// so a bad restore can be undone by hand. Only the latest is kept.
	replacedDirPrefix = "etcd.replaced-"

	restoreTimeout = 10 * time.Minute
	// snapshotTimeout bounds a snapshot that would otherwise wait on a
	// stalled stream forever: the client's dial timeout does not cover the
	// RPC, and an executor stuck in backing_up blocks every later operation.
	// The backend is quota-bounded and the copy is over loopback, so this is
	// generous.
	snapshotTimeout = 5 * time.Minute
)

// Snapshot writes a consistent copy of the etcd backend to path, in the
// format etcdctl snapshot save produces: the database followed by its sha256,
// which etcdutl snapshot restore verifies. It dials endpoint the way the
// maintenance loop does, so it works from any process that can read the
// server's certificates.
func Snapshot(ctx context.Context, log *slog.Logger, endpoint string, tls *TLSConfig, path string) error {
	cfg, err := clientConfig(endpoint, tls)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	start := time.Now()
	version, err := snapshot.SaveWithVersion(ctx, zap.NewNop(), cfg, path)
	if err != nil {
		return fmt.Errorf("snapshot etcd at %s: %w", endpoint, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	log.Info("etcd snapshot saved", "path", path, "bytes", info.Size(), "etcd_version", version, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// VerifySnapshot checks that path is a complete etcd snapshot: the database
// is page-aligned and the trailing sha256 matches it. It is what restore
// checks too, but running it first means a bad file is found before etcd is
// stopped for it.
func VerifySnapshot(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	// The database is a whole number of 512-byte sectors (bbolt pages are
	// larger still), so a valid file is that plus exactly one sha256.
	size := info.Size()
	if size%512 != sha256.Size {
		return fmt.Errorf("%s is not an etcd snapshot: %d bytes, no checksum trailer", path, size)
	}
	h := sha256.New()
	if _, err := io.CopyN(h, f, size-sha256.Size); err != nil {
		return err
	}
	trailer := make([]byte, sha256.Size)
	if _, err := io.ReadFull(f, trailer); err != nil {
		return err
	}
	if !bytes.Equal(h.Sum(nil), trailer) {
		return fmt.Errorf("%s is corrupt: sha256 does not match its trailer", path)
	}
	return nil
}

// RestoreOptions names the snapshot to restore and the member identity to
// build it for.
type RestoreOptions struct {
	// Snapshot is the host path of the file to restore.
	Snapshot string
	// Config must carry the same Name and PeerPort the container is started
	// with; a data directory built for a different member name or peer URL
	// makes etcd refuse to start.
	Config EtcdConfig
	// Label distinguishes the data directory moved aside by this restore,
	// typically the operation id that asked for it.
	Label string
}

// Restore replaces the etcd data directory under the component's data path
// with the contents of a snapshot, using etcdutl from the same image the
// server runs, so the data directory is built by the version that will read
// it. It is meant for a server that is booting after the previous process
// died or was restarted. A graceful stop took that process's etcd down with
// it, but an unclean death (KillMode=process) leaves the container running;
// if so its task is stopped here, and the container is left in place for
// Start to reuse with a new task. Nothing else may be using etcd.
//
// The new data directory is built in a staging directory and swapped in only
// once etcdutl has succeeded, so a failed restore leaves the live data
// untouched. The previous data directory is kept as etcd.replaced-<label>.
func (e *EtcdComponent) Restore(ctx context.Context, opts RestoreOptions) error {
	if err := VerifySnapshot(opts.Snapshot); err != nil {
		return err
	}
	config := opts.Config
	config.applyDefaults()
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(ctx, e.Namespace), restoreTimeout)
	defer cancel()

	if err := e.stopSurvivingTask(ctx); err != nil {
		return err
	}

	staging := filepath.Join(e.DataPath, restoreStagingDir)
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear restore staging dir: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	start := time.Now()
	if err := e.runRestore(ctx, opts.Snapshot, staging, config); err != nil {
		return err
	}
	if err := e.swapInRestored(filepath.Join(staging, "etcd"), opts.Label); err != nil {
		return err
	}
	e.Log.Info("etcd data restored from snapshot", "snapshot", opts.Snapshot, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// stopSurvivingTask stops the etcd task a previous server process left
// running. The container stays so Start can reuse it.
func (e *EtcdComponent) stopSurvivingTask(ctx context.Context) error {
	container, err := e.CC.LoadContainer(ctx, ContainerName)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load etcd container: %w", err)
	}
	task, err := container.Task(ctx, nil)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load etcd task: %w", err)
	}
	e.Log.Info("stopping etcd before restoring its data", "container_id", container.ID())
	if err := e.StopTask(ctx, task); err != nil {
		return fmt.Errorf("stop etcd for restore: %w", err)
	}
	return nil
}

// restoreArgs is the etcdutl command line. The member identity flags mirror
// etcdArgs; the paths are the mounts runRestore sets up.
func restoreArgs(config EtcdConfig) []string {
	args := []string{
		"/usr/local/bin/etcdutl", "snapshot", "restore", "/snapshot.db",
		"--data-dir", "/restore/etcd",
		"--name", config.Name,
		"--initial-cluster", config.Name + "=" + config.advertisePeerURL(),
		"--initial-advertise-peer-urls", config.advertisePeerURL(),
	}
	if config.InitialToken != "" {
		args = append(args, "--initial-cluster-token", config.InitialToken)
	}
	return args
}

// runRestore runs etcdutl once in a throwaway container and waits for it.
func (e *EtcdComponent) runRestore(ctx context.Context, snapshotPath, staging string, config EtcdConfig) error {
	image, err := e.CC.Pull(ctx, etcdImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("pull etcd image for restore: %w", err)
	}
	// A restore interrupted last time may have left its container behind.
	if stale, err := e.CC.LoadContainer(ctx, restoreContainerName); err == nil {
		if err := e.CleanupExistingContainer(ctx, stale); err != nil {
			return fmt.Errorf("remove stale restore container: %w", err)
		}
	}

	mounts := []specs.Mount{
		{Destination: "/snapshot.db", Type: "bind", Source: snapshotPath, Options: []string{"rbind", "ro"}},
		{Destination: "/restore", Type: "bind", Source: staging, Options: []string{"rbind", "rw"}},
	}
	container, err := e.CC.NewContainer(ctx, restoreContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(restoreContainerName+"-snapshot", image),
		containerd.WithNewSpec(
			oci.WithImageConfig(image),
			oci.WithProcessArgs(restoreArgs(config)...),
			oci.WithMounts(mounts),
		),
	)
	if err != nil {
		return fmt.Errorf("create restore container: %w", err)
	}
	defer func() {
		if err := e.CleanupExistingContainer(ctx, container); err != nil {
			e.Log.Warn("could not remove restore container", "error", err)
		}
	}()

	// etcdutl's own output is the most useful thing in the log if this fails.
	task, err := container.NewTask(ctx, slogout.WithLogger(e.Log, "etcdutl", slogout.WithMaxLevel(slog.LevelInfo)))
	if err != nil {
		return fmt.Errorf("create restore task: %w", err)
	}
	exited, err := task.Wait(ctx)
	if err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		return fmt.Errorf("wait on restore task: %w", err)
	}
	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		return fmt.Errorf("start restore task: %w", err)
	}
	var status containerd.ExitStatus
	select {
	case status = <-exited:
	case <-ctx.Done():
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		return fmt.Errorf("restore: %w", ctx.Err())
	}
	if _, err := task.Delete(ctx); err != nil {
		e.Log.Warn("could not delete restore task", "error", err)
	}
	if code, _, err := status.Result(); err != nil {
		return fmt.Errorf("restore task: %w", err)
	} else if code != 0 {
		return fmt.Errorf("etcdutl snapshot restore exited %d", code)
	}
	return nil
}

// swapInRestored moves the live data directory aside and puts the restored
// one in its place. The container state file travels with it: it describes
// the container spec, which has not changed, and without it Start would
// recreate the container for nothing.
//
// If the second rename fails the live directory is put back, so there is
// always a data directory to boot from, and older kept directories are
// pruned only once the new one is installed, so a failed swap never costs
// the one copy that would still be there to undo it.
func (e *EtcdComponent) swapInRestored(restored, label string) error {
	if _, err := os.Stat(filepath.Join(restored, "member")); err != nil {
		return fmt.Errorf("etcdutl produced no member directory: %w", err)
	}
	live := filepath.Join(e.DataPath, "etcd")
	replaced := ""
	if _, err := os.Stat(live); err == nil {
		if state, err := os.ReadFile(e.stateFilePath()); err == nil {
			if err := os.WriteFile(filepath.Join(restored, etcdStateFile), state, 0o600); err != nil {
				return err
			}
		}
		replaced = filepath.Join(e.DataPath, replacedDirPrefix+label)
		if err := os.RemoveAll(replaced); err != nil {
			return fmt.Errorf("clear previous kept etcd data for %s: %w", label, err)
		}
		if err := os.Rename(live, replaced); err != nil {
			return fmt.Errorf("move aside current etcd data: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(restored, live); err != nil {
		if replaced != "" {
			if back := os.Rename(replaced, live); back != nil {
				e.Log.Error("could not put the previous etcd data back after a failed restore; it is intact", "path", replaced, "error", back)
			}
		}
		return fmt.Errorf("move restored etcd data into place: %w", err)
	}
	if replaced != "" {
		e.Log.Info("previous etcd data kept", "path", replaced)
	}
	if err := e.pruneReplacedDirs(replaced); err != nil {
		e.Log.Warn("could not prune older kept etcd data", "error", err)
	}
	return nil
}

// pruneReplacedDirs removes every kept data directory except keep.
func (e *EtcdComponent) pruneReplacedDirs(keep string) error {
	entries, err := os.ReadDir(e.DataPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), replacedDirPrefix) && filepath.Join(e.DataPath, entry.Name()) != keep {
			if err := os.RemoveAll(filepath.Join(e.DataPath, entry.Name())); err != nil {
				return fmt.Errorf("remove old replaced etcd data: %w", err)
			}
		}
	}
	return nil
}
