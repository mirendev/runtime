//go:build linux

package query

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/elastic/go-seccomp-bpf/arch"
)

var fileSyscalls = []string{"fsync", "fdatasync", "read", "write", "pread64", "pwrite64", "readv", "writev", "preadv", "pwritev", "preadv2", "pwritev2", "ftruncate", "fallocate"}

const syscallPathRecordSize = 272
const syscallPathScratchSize = 528 // room for separately bounded offset + component length

type syscallPathBTFLayout struct {
	taskFiles, taskFS, filesFDT, fdtableFD, fdtableMax int16
	filePath, fsRoot, pathDentry, pathMnt              int16
	dentryParent, dentryName, qstrName, qstrLen        int16
	vfsmountRoot, mountMnt, mountParent, mountPoint    int16
}

func syscallPathLayout() (int16, int16, []int, syscallPathBTFLayout, error) {
	if strconv.IntSize != 64 {
		return 0, 0, nil, syscallPathBTFLayout{}, errors.New("syscall paths require a native 64-bit server/kernel ABI")
	}
	fields, err := readTracepointFormat("raw_syscalls", "sys_enter", []string{"id"})
	if err != nil {
		return 0, 0, nil, syscallPathBTFLayout{}, err
	}
	if fields[0].size != 8 {
		return 0, 0, nil, syscallPathBTFLayout{}, errors.New("syscall paths require a 64-bit syscall ABI")
	}
	var data []byte
	for _, root := range []string{"/sys/kernel/tracing", "/sys/kernel/debug/tracing"} {
		data, err = os.ReadFile(root + "/events/raw_syscalls/sys_enter/format")
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, 0, nil, syscallPathBTFLayout{}, err
	}
	args := int16(-1)
	for _, line := range strings.Split(string(data), "\n") {
		m := tracepointFormatLine.FindStringSubmatch(line)
		if m != nil && strings.HasSuffix(strings.TrimSpace(m[1]), " args[6]") {
			off, e := strconv.Atoi(m[2])
			if e != nil || off < 0 || off > 4095 || m[3] != "48" {
				return 0, 0, nil, syscallPathBTFLayout{}, errors.New("unsupported syscall args layout")
			}
			args = int16(off)
		}
	}
	if args < 0 {
		return 0, 0, nil, syscallPathBTFLayout{}, errors.New("sys_enter lacks 64-bit args[6]")
	}
	table, err := arch.GetInfo(runtime.GOARCH)
	if err != nil {
		return 0, 0, nil, syscallPathBTFLayout{}, err
	}
	var ids []int
	for _, name := range fileSyscalls {
		if id, ok := table.SyscallNames[name]; ok {
			ids = append(ids, id)
		}
	}
	l, err := loadSyscallPathBTFLayout()
	return fields[0].offset, args, ids, l, err
}

func loadSyscallPathBTFLayout() (syscallPathBTFLayout, error) {
	var l syscallPathBTFLayout
	if strconv.IntSize != 64 {
		return l, errors.New("syscall paths require a native 64-bit server/kernel ABI")
	}
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return l, fmt.Errorf("load kernel BTF for syscall paths: %w", err)
	}
	get := func(name string) (*btf.Struct, error) {
		var s *btf.Struct
		if e := spec.TypeByName(name, &s); e != nil {
			return nil, fmt.Errorf("kernel BTF lacks %s: %w", name, e)
		}
		return s, nil
	}
	task, err := get("task_struct")
	if err != nil {
		return l, err
	}
	files, err := get("files_struct")
	if err != nil {
		return l, err
	}
	fdt, err := get("fdtable")
	if err != nil {
		return l, err
	}
	file, err := get("file")
	if err != nil {
		return l, err
	}
	fs, err := get("fs_struct")
	if err != nil {
		return l, err
	}
	path, err := get("path")
	if err != nil {
		return l, err
	}
	dentry, err := get("dentry")
	if err != nil {
		return l, err
	}
	qstr, err := get("qstr")
	if err != nil {
		return l, err
	}
	vfsmount, err := get("vfsmount")
	if err != nil {
		return l, err
	}
	mount, err := get("mount")
	if err != nil {
		return l, err
	}
	set := func(dst *int16, s *btf.Struct, name string) error {
		off, _, e := memberOffset(s, name)
		*dst = off
		return e
	}
	for _, x := range []struct {
		d *int16
		s *btf.Struct
		n string
	}{
		{&l.taskFiles, task, "files"}, {&l.taskFS, task, "fs"}, {&l.filesFDT, files, "fdt"}, {&l.fdtableFD, fdt, "fd"}, {&l.fdtableMax, fdt, "max_fds"},
		{&l.filePath, file, "f_path"}, {&l.fsRoot, fs, "root"}, {&l.pathDentry, path, "dentry"}, {&l.pathMnt, path, "mnt"}, {&l.dentryParent, dentry, "d_parent"}, {&l.dentryName, dentry, "d_name"},
		{&l.qstrName, qstr, "name"}, {&l.qstrLen, qstr, "len"}, {&l.vfsmountRoot, vfsmount, "mnt_root"}, {&l.mountMnt, mount, "mnt"}, {&l.mountParent, mount, "mnt_parent"}, {&l.mountPoint, mount, "mnt_mountpoint"},
	} {
		if err := set(x.d, x.s, x.n); err != nil {
			return l, fmt.Errorf("unsupported syscall path layout: %w", err)
		}
	}
	return l, nil
}

// The map value is fd/marker, 256 bytes of leaf-first length-prefixed
// components, and status/used. Dynamic helper destinations are map memory,
// never variable stack offsets.
func appendSyscallPath(i asm.Instructions, scratchFD int, dst, args int16, ids []int, l syscallPathBTFLayout) asm.Instructions {
	i = append(i, asm.Mov.Imm(asm.R0, 0))
	for off := int16(0); off < syscallPathRecordSize; off += 8 {
		i = append(i, asm.StoreMem(asm.RFP, dst+off, asm.R0, asm.DWord))
	}
	for _, id := range ids {
		i = append(i, asm.JEq.Imm32(asm.R8, int32(id), "path_file"))
	}
	i = append(i, asm.Ja.Label("path_done"), asm.Mov.Imm(asm.R0, 0).WithSymbol("path_file"),
		asm.StoreMem(asm.RFP, -480, asm.R0, asm.Word), asm.LoadMapPtr(asm.R1, scratchFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -480), asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, "path_done"), asm.Mov.Reg(asm.R9, asm.R0))
	i = appendSyscallPathWalk(i, args, l)
	i = appendCopyPathRecord(i, dst, asm.R9)
	return append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol("path_done"))
}

func appendSyscallPathWalk(i asm.Instructions, args int16, l syscallPathBTFLayout) asm.Instructions {
	// Clear all fixed slots and mark this as a file syscall.
	i = append(i, asm.Mov.Imm(asm.R0, 0))
	for off := int16(0); off < syscallPathScratchSize; off += 8 {
		i = append(i, asm.StoreMem(asm.R9, off, asm.R0, asm.DWord))
	}
	i = append(i, asm.StoreImm(asm.R9, 4, 1, asm.Word), asm.Mov.Reg(asm.R1, asm.RFP), asm.Add.Imm(asm.R1, -464), asm.Mov.Imm(asm.R2, 4), asm.Mov.Reg(asm.R3, asm.R6), asm.Add.Imm(asm.R3, int32(args)), asm.FnProbeReadKernel.Call(), asm.JNE.Imm(asm.R0, 0, "path_probe_fail"), asm.LoadMem(asm.R0, asm.RFP, -464, asm.Word), asm.StoreMem(asm.R9, 0, asm.R0, asm.Word), asm.JGT.Imm(asm.R0, 2147483647, "path_bad_fd"))
	// task -> files -> fdt, validate fd, then fd[] -> file -> f_path.
	i = append(i, asm.FnGetCurrentTask.Call(), asm.Mov.Reg(asm.R7, asm.R0))
	readPtr := func(dst int16, base asm.Register, off int16) {
		i = append(i, probeRead(dst, base, off, 8, "path_probe_fail")...)
	}
	readPtr(-464, asm.R7, l.taskFiles)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord))
	readPtr(-464, asm.R7, l.filesFDT)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord))
	i = append(i, probeRead(-456, asm.R7, l.fdtableMax, 4, "path_probe_fail")...)
	readPtr(-464, asm.R7, l.fdtableFD)
	i = append(i, asm.LoadMem(asm.R0, asm.R9, 0, asm.Word), asm.LoadMem(asm.R1, asm.RFP, -456, asm.Word), asm.JGE.Reg(asm.R0, asm.R1, "path_closed"), asm.LSh.Imm(asm.R0, 3), asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord), asm.Add.Reg(asm.R7, asm.R0))
	readPtr(-464, asm.R7, 0)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord), asm.JEq.Imm(asm.R7, 0, "path_closed"))
	readPtr(-456, asm.R7, l.filePath+l.pathDentry)
	readPtr(-448, asm.R7, l.filePath+l.pathMnt)
	// Capture the process root pair.
	i = append(i, asm.FnGetCurrentTask.Call(), asm.Mov.Reg(asm.R7, asm.R0))
	readPtr(-464, asm.R7, l.taskFS)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord))
	readPtr(-440, asm.R7, l.fsRoot+l.pathDentry)
	readPtr(-432, asm.R7, l.fsRoot+l.pathMnt)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -456, asm.DWord), asm.LoadMem(asm.R8, asm.RFP, -448, asm.DWord), asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, -480, asm.R0, asm.Word))
	for step := 0; step < 32; step++ {
		label := fmt.Sprintf("path_step_%d", step)
		next := fmt.Sprintf("path_step_%d", step+1)
		i = append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol(label), asm.LoadMem(asm.R1, asm.RFP, -440, asm.DWord), asm.JNE.Reg(asm.R7, asm.R1, "path_not_root"+label), asm.LoadMem(asm.R1, asm.RFP, -432, asm.DWord), asm.JEq.Reg(asm.R8, asm.R1, "path_success"), asm.Mov.Imm(asm.R0, 0).WithSymbol("path_not_root"+label))
		readPtr(-464, asm.R8, l.vfsmountRoot)
		i = append(i, asm.LoadMem(asm.R0, asm.RFP, -464, asm.DWord), asm.JNE.Reg(asm.R7, asm.R0, "path_component"+label))
		// container_of(vfsmount), then parent mount and mountpoint.
		i = append(i, asm.Mov.Reg(asm.R0, asm.R8), asm.Sub.Imm(asm.R0, int32(l.mountMnt)), asm.StoreMem(asm.RFP, -424, asm.R0, asm.DWord), asm.Mov.Reg(asm.R7, asm.R0))
		readPtr(-464, asm.R7, l.mountParent)
		readPtr(-456, asm.R7, l.mountPoint)
		i = append(i, asm.LoadMem(asm.R0, asm.RFP, -464, asm.DWord), asm.LoadMem(asm.R1, asm.RFP, -424, asm.DWord), asm.JEq.Reg(asm.R0, asm.R1, "path_incomplete"), asm.Add.Imm(asm.R0, int32(l.mountMnt)), asm.Mov.Reg(asm.R8, asm.R0), asm.LoadMem(asm.R7, asm.RFP, -456, asm.DWord), asm.Ja.Label(next))
		// Read parent, qstr length/name; append [length][name].
		componentStart := len(i)
		readPtr(-464, asm.R7, l.dentryParent)
		i[componentStart] = i[componentStart].WithSymbol("path_component" + label)
		i = append(i, asm.LoadMem(asm.R0, asm.RFP, -464, asm.DWord), asm.JEq.Reg(asm.R0, asm.R7, "path_incomplete"))
		i = append(i, probeRead(-456, asm.R7, l.dentryName+l.qstrLen, 4, "path_probe_fail")...)
		readPtr(-448, asm.R7, l.dentryName+l.qstrName)
		i = append(i, asm.LoadMem(asm.R1, asm.RFP, -456, asm.Word), asm.JEq.Imm(asm.R1, 0, "path_incomplete"), asm.JGT.Imm(asm.R1, 255, "path_bounds"), asm.LoadMem(asm.R0, asm.RFP, -480, asm.Word), asm.Add.Reg(asm.R0, asm.R1), asm.Add.Imm(asm.R0, 1), asm.JGT.Imm(asm.R0, 256, "path_bounds"), asm.LoadMem(asm.R2, asm.RFP, -480, asm.Word), asm.JGT.Imm(asm.R2, 255, "path_bounds"), asm.Mov.Reg(asm.R3, asm.R9), asm.Add.Reg(asm.R3, asm.R2), asm.StoreMem(asm.R3, 8, asm.R1, asm.Byte), asm.Add.Imm(asm.R3, 9), asm.Mov.Reg(asm.R2, asm.R1), asm.Mov.Reg(asm.R1, asm.R3), asm.LoadMem(asm.R3, asm.RFP, -448, asm.DWord), asm.FnProbeReadKernel.Call(), asm.JNE.Imm(asm.R0, 0, "path_probe_fail"), asm.LoadMem(asm.R0, asm.RFP, -480, asm.Word), asm.LoadMem(asm.R1, asm.RFP, -456, asm.Word), asm.Add.Reg(asm.R0, asm.R1), asm.Add.Imm(asm.R0, 1), asm.StoreMem(asm.RFP, -480, asm.R0, asm.Word), asm.LoadMem(asm.R7, asm.RFP, -464, asm.DWord), asm.Ja.Label(next))
	}
	i = append(i, asm.Ja.Label("path_incomplete").WithSymbol("path_step_32"), asm.Mov.Imm(asm.R0, 0).WithSymbol("path_success"), asm.LoadMem(asm.R1, asm.RFP, -480, asm.Word), asm.StoreMem(asm.R9, 524, asm.R1, asm.Word), asm.Ja.Label("path_finish"), asm.Mov.Imm(asm.R0, 1).WithSymbol("path_bad_fd"), asm.Ja.Label("path_error"), asm.Mov.Imm(asm.R0, 2).WithSymbol("path_closed"), asm.Ja.Label("path_error"), asm.Mov.Imm(asm.R0, 3).WithSymbol("path_probe_fail"), asm.Ja.Label("path_error"), asm.Mov.Imm(asm.R0, 4).WithSymbol("path_bounds"), asm.Ja.Label("path_error"), asm.Mov.Imm(asm.R0, 5).WithSymbol("path_incomplete"), asm.StoreMem(asm.R9, 520, asm.R0, asm.Word).WithSymbol("path_error"), asm.Mov.Imm(asm.R0, 0).WithSymbol("path_finish"), asm.LoadMem(asm.R8, asm.RFP, -472, asm.DWord))
	return i
}

func appendCopyPathRecord(i asm.Instructions, dst int16, scratch asm.Register) asm.Instructions {
	for off := int16(0); off < syscallPathRecordSize; off += 8 {
		source := off
		if off == 264 {
			source = 520
		}
		i = append(i, asm.LoadMem(asm.R0, scratch, source, asm.DWord), asm.StoreMem(asm.RFP, dst+off, asm.R0, asm.DWord))
	}
	return i
}

func resolveSyscallFile(raw []byte, event *Event) {
	if len(raw) != syscallPathRecordSize || binary.NativeEndian.Uint32(raw[4:8]) != 1 {
		return
	}
	f := &SyscallFile{FD: int32(binary.NativeEndian.Uint32(raw[:4]))}
	event.File = f
	status := binary.NativeEndian.Uint32(raw[264:268])
	if status != 0 {
		messages := []string{"", "invalid file descriptor", "file descriptor is closed", "kernel path read failed", "path exceeds capture bounds", "path could not be resolved to process root"}
		if int(status) >= len(messages) {
			f.Error = "invalid captured path status"
		} else {
			f.Error = messages[status]
		}
		return
	}
	used := int(binary.NativeEndian.Uint32(raw[268:]))
	if used < 0 || used > 256 {
		f.Error = "invalid captured path"
		return
	}
	var comps []string
	for p := 0; p < used; {
		n := int(raw[8+p])
		p++
		if n == 0 || p+n > used {
			f.Error = "invalid captured path"
			return
		}
		comps = append(comps, string(raw[8+p:8+p+n]))
		p += n
	}
	for a, b := 0, len(comps)-1; a < b; a, b = a+1, b-1 {
		comps[a], comps[b] = comps[b], comps[a]
	}
	f.Path = "/" + strings.Join(comps, "/")
}
