package serverlifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
)

// launchTimeout bounds the systemd-run round trip on its own, since the
// launch is deliberately cut loose from the caller's context.
const launchTimeout = 30 * time.Second

// LaunchChecker is an optional Launcher capability: after Launch reported an
// error, it says whether the executor is running anyway. systemd-run can fail
// after the unit was submitted, and an executor that is running owns the
// record, so the caller must not mark it failed on top of it.
type LaunchChecker interface {
	Launched(ctx context.Context, opID string) bool
}

// ErrInvalidID is returned for an operation id that is not a ULID. The id is
// a file name and the ledger's sort key, so the format is not negotiable.
var ErrInvalidID = errors.New("lifecycle operation id must be a ULID")

// ValidateID checks that id is a ULID as NewID would mint one.
func ValidateID(id string) error {
	if _, err := ulid.ParseStrict(id); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return nil
}

// Start records op and launches its executor. It is closed over the id: a
// second Start with an id already on disk returns that record untouched, so
// a caller that lost the first reply (the reply may well be lost, since the
// operation restarts the server answering it) can retry without starting a
// second operation. Two such calls arriving together are settled under the
// store's lock, so exactly one creates and the other gets the record. A
// record that exists with a different action or target is a caller bug and
// is refused rather than silently returned. The bool reports whether this
// call is the one that started it.
//
// The launch runs on a context cut loose from the caller's. The caller is an
// RPC or an uplink session about to be torn down by the very restart it
// asked for, and a launch cancelled midway is the worst outcome: the unit
// may be running while the caller believes it is not.
func Start(ctx context.Context, store *Store, launcher Launcher, op *Operation) (*Operation, bool, error) {
	if err := ValidateID(op.ID); err != nil {
		return nil, false, err
	}
	existing, created, err := store.CreateOrGet(op)
	if err != nil {
		return nil, false, err
	}
	if !created {
		if existing.Action != op.Action || existing.TargetVersion != op.TargetVersion {
			return nil, false, fmt.Errorf("operation %s already exists as %s %s", op.ID, existing.Action, existing.TargetVersion)
		}
		return existing, false, nil
	}

	launchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), launchTimeout)
	defer cancel()
	if err := launcher.Launch(launchCtx, op.ID); err != nil {
		// systemd-run can report an error after the unit was submitted. If
		// the executor is running it owns the record from here; failing it
		// underneath would free the slot for a second operation to race it.
		// The check gets its own bound: the launch context may be what
		// expired, and a check that cannot run would read as "not running".
		if checker, ok := launcher.(LaunchChecker); ok {
			checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), launchTimeout)
			running := checker.Launched(checkCtx, op.ID)
			cancelCheck()
			if running {
				return op, true, nil
			}
		}
		op.Phase = PhaseFailed
		op.Error = "could not start executor: " + err.Error()
		if updateErr := store.Update(op); updateErr != nil {
			// A pending record with no executor would hold the busy slot
			// for good. Better no record than that one.
			if removeErr := store.Remove(op.ID); removeErr != nil {
				return nil, false, fmt.Errorf("%w; and could not record the failure (%v) or remove the record (%v)", err, updateErr, removeErr)
			}
			return nil, false, fmt.Errorf("%w; could not record the failure: %v", err, updateErr)
		}
		return nil, false, err
	}
	return op, true, nil
}

// ExecutorBinary is the binary a launcher should run the executor from: this
// process's own, resolved through any symlink so the transient unit captures
// the real file. Running our own build rather than the installed server's
// matters when they differ: an older server may predate the executor entirely.
func ExecutorBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
