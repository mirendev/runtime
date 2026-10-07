//go:build linux

package query

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

// The first bio represents the request's blkcg association: the block layer
// rejects ordinary merges across associations. This is not current's cgroup.
func requestCgroupOffsets(spec *btf.Spec) ([]int16, error) {
	var rq *btf.Struct
	if err := spec.TypeByName("request", &rq); err != nil {
		return nil, err
	}
	var offsets []int16
	s := rq
	for _, step := range []struct{ field, target string }{
		{"bio", "bio"}, {"bi_blkg", "blkcg_gq"}, {"blkcg", "blkcg"},
	} {
		off, typ, err := memberOffset(s, step.field)
		if err != nil {
			return nil, err
		}
		offsets = append(offsets, off)
		s, err = pointerStruct(typ, step.target)
		if err != nil {
			return nil, err
		}
	}
	cssOffset, cssType, err := memberOffset(s, "css")
	if err != nil {
		return nil, err
	}
	css, ok := btf.UnderlyingType(cssType).(*btf.Struct)
	if !ok {
		return nil, fs.ErrInvalid
	}
	off, typ, err := memberOffset(css, "cgroup")
	if err != nil || int32(cssOffset)+int32(off) > 4095 {
		return nil, fs.ErrInvalid
	}
	offsets = append(offsets, cssOffset+off)
	s, err = pointerStruct(typ, "cgroup")
	if err != nil {
		return nil, err
	}
	off, typ, err = memberOffset(s, "kn")
	if err != nil {
		return nil, err
	}
	offsets = append(offsets, off)
	s, err = pointerStruct(typ, "kernfs_node")
	if err != nil {
		return nil, err
	}
	off, typ, err = memberOffset(s, "id")
	if err != nil {
		return nil, err
	}
	if size, err := btf.Sizeof(typ); err != nil || size != 8 {
		return nil, fs.ErrInvalid
	}
	return append(offsets, off), nil
}

// Ownership is optional: absent configuration, no bio (e.g. synthetic flush),
// or failed reads must never drop a disk event or substitute task ownership.
func appendRequestCgroup(i asm.Instructions, offsets []int16) asm.Instructions {
	i = append(i, asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, -24, asm.R0, asm.DWord))
	if len(offsets) == 0 {
		return i
	}
	i = append(i, asm.Mov.Reg(asm.R7, asm.R6))
	for _, off := range offsets {
		i = append(i, asm.JEq.Imm(asm.R7, 0, "io_cgroup_done"))
		i = append(i, probeRead(-104, asm.R7, off, 8, "io_cgroup_done")...)
		i = append(i, asm.LoadMem(asm.R7, asm.RFP, -104, asm.DWord))
	}
	return append(i, asm.StoreMem(asm.RFP, -24, asm.R7, asm.DWord), asm.Mov.Imm(asm.R0, 0).WithSymbol("io_cgroup_done"))
}

type diskCgroupPaths struct {
	mount string
	paths map[uint64]string
	next  time.Time
}

func (d *diskCgroupPaths) enrich(event *DiskEvent) {
	if event.IOCgroupID == nil {
		event.IOCgroupError = "request block-cgroup association unavailable"
		return
	}
	if !time.Now().Before(d.next) {
		d.next = time.Now().Add(time.Second)
		d.paths = make(map[uint64]string)
		var stat unix.Statfs_t
		if unix.Statfs(d.mount, &stat) == nil && stat.Type == unix.CGROUP2_SUPER_MAGIC {
			// A bounded, non-symlink-following directory walk. Invisible or
			// removed groups keep their kernel ID with an unresolved path.
			visited := 0
			filepath.WalkDir(d.mount, func(path string, entry fs.DirEntry, err error) error {
				visited++
				if visited > 65536 {
					return fs.SkipAll
				}
				if err != nil || !entry.IsDir() {
					return nil
				}
				info, err := os.Lstat(path)
				if err != nil {
					return nil
				}
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					rel, err := filepath.Rel(d.mount, path)
					if err == nil {
						name := "/"
						if rel != "." {
							name += filepath.ToSlash(rel)
						}
						d.paths[st.Ino] = name
					}
				}
				return nil
			})
		}
	}
	event.IOCgroupPath = d.paths[*event.IOCgroupID]
	if event.IOCgroupPath == "" {
		event.IOCgroupError = "charged cgroup not visible in server cgroup-v2 mount"
	}
}
