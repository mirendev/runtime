package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/elastic/go-seccomp-bpf/arch"
)

func validSyscallName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i, r := range name {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func addSyscallFilter(r *MonitorRequest, text string) error {
	if strings.HasPrefix(text, ":") {
		name := strings.TrimPrefix(text, ":")
		if !validSyscallName(name) {
			return fmt.Errorf("invalid syscall name %q", text)
		}
		r.SyscallNames = append(r.SyscallNames, name)
		return nil
	}
	id, err := strconv.ParseUint(text, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid syscall number %q; use :name for symbolic syscalls", text)
	}
	r.Syscalls = append(r.Syscalls, int(id))
	return nil
}

// Resolve only on the server, after authentication and before any collection or
// userspace filtering. The client architecture must not determine syscall IDs.
// ResolveSyscallNames resolves symbolic syscall filters for a server ABI.
func ResolveSyscallNames(r MonitorRequest, serverArch string) (MonitorRequest, error) {
	if r.Source == "script" {
		probes := make([]MonitorRequest, len(r.Probes))
		for i, probe := range r.Probes {
			resolved, err := ResolveSyscallNames(probe, serverArch)
			if err != nil {
				return MonitorRequest{}, err
			}
			probes[i] = resolved
		}
		r.Probes = probes
		return r, nil
	}
	if len(r.SyscallNames) == 0 {
		return r, nil
	}
	table, err := arch.GetInfo(serverArch)
	if err != nil {
		return MonitorRequest{}, fmt.Errorf("syscall names unavailable on server architecture %s: %w", serverArch, err)
	}
	ids := append([]int{}, r.Syscalls...)
	for _, name := range r.SyscallNames {
		id, ok := table.SyscallNames[name]
		if !ok {
			return MonitorRequest{}, fmt.Errorf("unknown syscall :%s on server architecture %s", name, serverArch)
		}
		ids = append(ids, id)
	}
	r.Syscalls, r.SyscallNames = ids, nil
	return r, nil
}
