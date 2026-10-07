//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestResolveSyscallFile(t *testing.T) {
	raw := make([]byte, syscallPathRecordSize)
	binary.NativeEndian.PutUint32(raw[:4], 17)
	binary.NativeEndian.PutUint32(raw[4:], 1)
	copy(raw[8:], []byte{3, 't', 'x', 't', 6, 'n', 'e', 's', 't', 'e', 'd'})
	binary.NativeEndian.PutUint32(raw[268:], 11)
	e := Event{Syscall: unix.SYS_FSYNC}
	resolveSyscallFile(raw, &e)
	if e.File == nil || e.File.FD != 17 || e.File.Path != "/nested/txt" || e.File.Error != "" {
		t.Fatalf("file: %+v", e.File)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.File == nil || decoded.File.Path != "/nested/txt" {
		t.Fatalf("wire: %s, %v", data, err)
	}
	binary.NativeEndian.PutUint32(raw[:4], ^uint32(0))
	binary.NativeEndian.PutUint32(raw[264:], 1)
	resolveSyscallFile(raw, &e)
	if e.File.FD != -1 || e.File.Error == "" || e.File.Path != "" {
		t.Fatalf("invalid FD: %+v", e.File)
	}
	e.File = nil
	binary.NativeEndian.PutUint32(raw[4:], 0)
	resolveSyscallFile(raw, &e)
	if e.File != nil {
		t.Fatal("non-file syscall acquired a file")
	}
}

func TestSyscallPathsLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root tracepoints")
	}
	for _, phase := range []string{"entry", "completion"} {
		t.Run(phase, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "fsync-target")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.Write([]byte("path probe")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := make(chan Event, 128)
			done := make(chan error, 1)
			go func() {
				done <- syscallEvents(ctx, MonitorRequest{Source: "syscalls", PID: uint32(os.Getpid()), Phase: phase, Paths: true, Syscalls: []int{unix.SYS_FSYNC, unix.SYS_GETPID}, Stacks: &StackCapture{Kernel: true}}, func(e Event) error {
					select {
					case got <- e:
					default:
					}
					return nil
				})
			}()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			tid := uint32(unix.Gettid())
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			found := false
			for !found {
				select {
				case <-ticker.C:
					unix.Getpid()
					if err := unix.Fsync(int(f.Fd())); err != nil {
						t.Fatal(err)
					}
				case e := <-got:
					if e.Syscall == unix.SYS_FSYNC && e.TID == tid {
						if e.File == nil || e.File.Path != f.Name() || e.File.FD != int32(f.Fd()) || e.File.Error != "" || e.KernelStack == nil {
							t.Fatalf("bad event: %+v file=%+v", e, e.File)
						}
						if phase == "completion" && (e.DurationNS == nil || e.ReturnValue == nil || *e.ReturnValue != 0) {
							t.Fatalf("bad completion: %+v", e)
						}
						found = true
					}
					if e.Syscall == unix.SYS_GETPID && e.File != nil {
						t.Fatal("getpid has file")
					}
				case err := <-done:
					t.Fatalf("collector stopped: %v", err)
				case <-deadline.C:
					t.Fatal("no fsync event")
				}
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSyscallPathsRetainedAfterClose(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root tracepoints")
	}
	for _, name := range []string{"closed_reused", "mount_boundary", "capture_bounds"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if name == "mount_boundary" {
				var err error
				dir, err = os.MkdirTemp("/dev/shm", "portal-path-")
				if err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(dir)
			}
			if name == "capture_bounds" {
				dir = filepath.Join(dir, strings.Repeat("a", 100), strings.Repeat("b", 100), strings.Repeat("c", 100))
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "target")
			fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			blocked, release := make(chan struct{}), make(chan struct{})
			got, done := make(chan Event, 1), make(chan error, 1)
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			tid := uint32(unix.Gettid())
			go func() {
				done <- syscallEvents(ctx, MonitorRequest{Source: "syscalls", PID: uint32(os.Getpid()), Phase: "completion", Paths: true, Syscalls: []int{unix.SYS_GETPID, unix.SYS_FSYNC}}, func(e Event) error {
					if e.TID != tid {
						return nil
					}
					if e.Syscall == unix.SYS_GETPID {
						select {
						case <-blocked:
						default:
							close(blocked)
						}
						select {
						case <-release:
						case <-ctx.Done():
						}
					} else if e.Syscall == unix.SYS_FSYNC {
						got <- e
					}
					return nil
				})
			}()
			// Hold the reader behind a known captured event, so it cannot
			// resolve fsync's descriptor before the close/unlink/reuse below.
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
		ready:
			for {
				unix.Getpid()
				select {
				case <-blocked:
					break ready
				case err := <-done:
					t.Fatalf("collector stopped: %v", err)
				case <-ctx.Done():
					t.Fatal("collector never attached")
				case <-ticker.C:
				}
			}
			if err := unix.Fsync(fd); err != nil {
				t.Fatal(err)
			}
			if err := unix.Close(fd); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			replacement, err := unix.Open(filepath.Join(dir, "replacement"), unix.O_CREAT|unix.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(replacement)
			if replacement != fd {
				if err := unix.Dup3(replacement, fd, 0); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case e := <-got:
				if e.File == nil || e.File.FD != int32(fd) {
					t.Fatalf("missing original descriptor: %+v", e)
				}
				if name == "capture_bounds" {
					if e.File.Path != "" || e.File.Error != "path exceeds capture bounds" {
						t.Fatalf("truncation disguised as path: %+v", e.File)
					}
				} else if e.File.Path != path || e.File.Error != "" {
					t.Fatalf("entry path not retained after reuse: %+v, want %s", e.File, path)
				}
			case err := <-done:
				t.Fatalf("collector stopped: %v", err)
			case <-ctx.Done():
				t.Fatal("no fsync completion")
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
