package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// diskMount describes a miren-provider disk bind mount whose backing directory
// may need its ownership adjusted so the container's run user can write to it.
type diskMount struct {
	// hostPath is the host path of the mounted disk filesystem root.
	hostPath string
	// owner is the disk's ownership policy: "" (derive from the run user),
	// "keep" (leave raw ownership untouched), or "uid"/"uid:gid" (pin a
	// specific numeric owner).
	owner string
}

// resolveDiskOwner decides the target uid/gid for a disk mount from its owner
// policy and the container's resolved run user. skip is true when ownership
// should be left untouched (an explicit "keep", or a root run user for which a
// chown would be a no-op).
func resolveDiskOwner(owner string, runUID, runGID uint32) (uid, gid uint32, skip bool, err error) {
	switch owner {
	case "keep":
		return 0, 0, true, nil
	case "":
		// Derive from the resolved run user. An image that runs as root needs
		// no chown, so nothing regresses for the common case.
		if runUID == 0 {
			return 0, 0, true, nil
		}
		return runUID, runGID, false, nil
	default:
		uid, gid, err = parseOwner(owner)
		if err != nil {
			return 0, 0, false, err
		}
		return uid, gid, false, nil
	}
}

// parseOwner parses an explicit "uid" or "uid:gid" owner override. When the gid
// is omitted it defaults to the uid, matching the common "one app user" case.
func parseOwner(owner string) (uid, gid uint32, err error) {
	uidStr, gidStr, hasGID := strings.Cut(owner, ":")

	u, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("owner uid %q is not numeric: %w", uidStr, err)
	}
	uid = uint32(u)

	if !hasGID {
		return uid, uid, nil
	}

	g, err := strconv.ParseUint(gidStr, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("owner gid %q is not numeric: %w", gidStr, err)
	}
	return uid, uint32(g), nil
}

// chownDiskRoot applies the OnRootMismatch policy: if the mount root already has
// the desired ownership, it returns immediately without walking the tree. Only
// when the root's owner differs does it recursively chown, so a correctly-owned
// disk pays nothing on every boot and a large disk is never blindly walked.
//
// The recursive pass (a first-provision or migration of a pre-existing disk) can
// take a while on a disk with many files; it runs once, and every later boot
// takes the fast path.
func chownDiskRoot(log *slog.Logger, path string, uid, gid uint32) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat disk mount: %w", err)
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("stat disk mount %s: unexpected stat type %T", path, fi.Sys())
	}

	if st.Uid == uid && st.Gid == gid {
		return nil
	}

	log.Info("chowning disk mount to run user (root ownership mismatch); "+
		"this may take a while on a disk with many files",
		"path", path,
		"from_uid", st.Uid, "from_gid", st.Gid,
		"to_uid", uid, "to_gid", gid)

	// Lchown so symlinks on the disk are retargeted, not their targets.
	return chownTreeRootLast(path, uid, gid, os.Lchown)
}

// chownTreeRootLast recursively chowns every entry under path, chowning the
// mount root itself LAST. The root's ownership is the sentinel chownDiskRoot's
// fast path keys on, so deferring it means an interrupted walk (e.g. an error
// partway through a large tree) leaves the root still mismatched and the whole
// pass is retried on the next boot, rather than the root being marked done over
// a half-owned tree.
func chownTreeRootLast(path string, uid, gid uint32, chown func(name string, uid, gid int) error) error {
	iuid, igid, err := ownerInts(uid, gid)
	if err != nil {
		return err
	}

	err = filepath.Walk(path, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == path {
			// Defer the root until every descendant has been chowned.
			return nil
		}
		return chown(p, iuid, igid)
	})
	if err != nil {
		return err
	}
	return chown(path, iuid, igid)
}

// ownerInts narrows a uid/gid to the int that os.Lchown expects. It rejects
// ids above math.MaxInt32, which can't fit a 32-bit int without wrapping to a
// negative value; real uids never come near that (4294967295 is the invalid-id
// sentinel), so the bound is safe as well as portable.
func ownerInts(uid, gid uint32) (int, int, error) {
	if uid > math.MaxInt32 {
		return 0, 0, fmt.Errorf("uid %d exceeds the maximum supported id", uid)
	}
	if gid > math.MaxInt32 {
		return 0, 0, fmt.Errorf("gid %d exceeds the maximum supported id", gid)
	}
	return int(uid), int(gid), nil
}

// withDiskOwnership returns a SpecOpt that makes each miren disk mount writable
// by the container's run user. It must run after oci.WithImageConfig (and any
// user override) so it reads the fully-resolved spec.Process.User; appending it
// last to the spec option list guarantees that.
func (c *SandboxController) withDiskOwnership(mounts []diskMount) oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		if len(mounts) == 0 {
			return nil
		}

		var runUID, runGID uint32
		if s.Process != nil {
			runUID = s.Process.User.UID
			runGID = s.Process.User.GID
		}

		for _, m := range mounts {
			uid, gid, skip, err := resolveDiskOwner(m.owner, runUID, runGID)
			if err != nil {
				return fmt.Errorf("disk mount %s: invalid owner %q: %w", m.hostPath, m.owner, err)
			}
			if skip {
				continue
			}
			if err := chownDiskRoot(c.Log, m.hostPath, uid, gid); err != nil {
				return fmt.Errorf("disk mount %s: %w", m.hostPath, err)
			}
		}
		return nil
	}
}
