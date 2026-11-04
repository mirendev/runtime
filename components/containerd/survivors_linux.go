package containerd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// findSurvivors returns the containerd processes running from configPath. A
// server process that dies without running its stop hooks (a panic, SIGKILL)
// leaves its daemon running, reparented to init. That daemon keeps the socket
// and the metadata store's lock, so every daemon started after it blocks
// forever opening the store while the old one keeps answering on the socket.
// Both kinds turn up here.
//
// Candidates are matched on the config file's identity, not its path: a
// containerd in a container can use the same path for an unrelated file.
// Callers must hold the base directory's owner lock, or a live server's daemon
// would count as a survivor too.
func findSurvivors(socketPath, configPath string) ([]*os.Process, error) {
	ours, err := os.Stat(configPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var procs []*os.Process
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !isContainerd(pid, socketPath, ours) {
			continue
		}
		// The handle is a pidfd, so signals sent through it cannot reach
		// an unrelated process that later reuses the pid. Check again once
		// it is open in case that already happened.
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if !isContainerd(pid, socketPath, ours) {
			proc.Release()
			continue
		}
		procs = append(procs, proc)
	}
	return procs, nil
}

func isContainerd(pid int, socketPath string, config os.FileInfo) bool {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	comm, err := os.ReadFile(filepath.Join(dir, "comm"))
	if err != nil || strings.TrimSpace(string(comm)) != "containerd" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if flagValue(args, "--address") != socketPath {
		return false
	}
	theirs := flagValue(args, "--config")
	if theirs == "" {
		return false
	}
	// Resolve the path the way the process sees it, through its own root
	// and working directory.
	if filepath.IsAbs(theirs) {
		theirs = filepath.Join(dir, "root", theirs)
	} else {
		theirs = filepath.Join(dir, "cwd", theirs)
	}
	info, err := os.Stat(theirs)
	return err == nil && os.SameFile(info, config)
}

func flagValue(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// stopSurvivors sends SIGTERM to every process and waits up to timeout for
// them all to exit, then SIGKILLs any that remain. Shims are separate
// processes, so the containers they run outlive their daemon.
func stopSurvivors(procs []*os.Process, timeout time.Duration) error {
	defer func() {
		for _, proc := range procs {
			proc.Release()
		}
	}()
	for _, proc := range procs {
		if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("signalling containerd %d: %w", proc.Pid, err)
		}
	}
	if waitGone(procs, timeout) {
		return nil
	}
	for _, proc := range procs {
		if err := proc.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("killing containerd %d: %w", proc.Pid, err)
		}
	}
	if !waitGone(procs, 5*time.Second) {
		return fmt.Errorf("containerd processes still running after SIGKILL")
	}
	return nil
}

func waitGone(procs []*os.Process, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !slices.ContainsFunc(procs, alive) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func alive(proc *os.Process) bool {
	return proc.Signal(syscall.Signal(0)) == nil
}

// socketOwner returns the PID of the process listening on socketPath.
func socketOwner(socketPath string) (int, error) {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	return int(cred.Pid), nil
}
