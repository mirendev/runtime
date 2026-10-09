//go:build linux

package query

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func readCgroups(ctx context.Context, path string) ([]CgroupInfo, error) {
	const mount = "/sys/fs/cgroup"
	var stat unix.Statfs_t
	if err := unix.Statfs(mount, &stat); err != nil {
		return nil, fmt.Errorf("inspect cgroup v2 mount: %w", err)
	}
	if stat.Type != unix.CGROUP2_SUPER_MAGIC {
		return nil, errors.New("cgroups requires a cgroup v2 mount at /sys/fs/cgroup")
	}
	return cgroupSnapshot(ctx, mount, path)
}

// Walk only real directories; filters select visible paths, never filesystem
// paths to open. Each group's reads are anchored to its directory handle.
func cgroupSnapshot(ctx context.Context, mount, pattern string) ([]CgroupInfo, error) {
	result := make([]CgroupInfo, 0)
	err := filepath.WalkDir(mount, func(dir string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if dir != mount && errors.Is(err, fs.ErrNotExist) {
				return nil // Group disappeared during enumeration.
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(mount, dir)
		if err != nil {
			return err
		}
		path := "/"
		if rel != "." {
			path += filepath.ToSlash(rel)
		}
		// Exact selections (including inventory-driven paths) need only their
		// ancestors, not a walk through every app and descendant in the mount.
		if pattern != "" && !strings.Contains(pattern, "*") && path != "/" && path != pattern && !strings.HasPrefix(pattern, path+"/") {
			return filepath.SkipDir
		}
		if !processNameMatches(pattern, path) {
			return nil
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		defer root.Close()
		before, err := root.Stat(".")
		if err != nil {
			return err
		}
		stat := before.Sys().(*syscall.Stat_t)
		info := CgroupInfo{Path: path, ID: fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)}
		readErr := readCgroupMetrics(root, &info)
		after, err := os.Stat(dir)
		if errors.Is(err, fs.ErrNotExist) || err == nil && !os.SameFile(before, after) {
			return nil // Removed/recreated group: do not mix its lifetimes.
		}
		if err != nil {
			return err
		}
		if readErr != nil {
			return fmt.Errorf("read cgroup %s: %w", path, readErr)
		}
		if len(result) >= maxAggregateGroups {
			return errors.New("cgroup snapshot exceeds 4096 groups; narrow the path filter")
		}
		result = append(result, info)
		if pattern == path {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect cgroups: %w", err)
	}
	return result, nil
}

func readCgroupMetrics(root *os.Root, info *CgroupInfo) error {
	// Controller files can be absent (disabled controller or hierarchy root).
	read := func(name string) (string, error) {
		data, err := root.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		text := strings.TrimSpace(string(data))
		if err == nil && text == "" {
			return "", fmt.Errorf("empty %s", name)
		}
		return text, err
	}
	counters := func(name string) (map[string]uint64, error) {
		text, err := read(name)
		if err != nil {
			return nil, err
		}
		values := make(map[string]uint64)
		for _, line := range strings.Split(text, "\n") {
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 2 {
				return nil, fmt.Errorf("invalid %s row %q", name, line)
			}
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid %s value: %w", name, err)
			}
			values[fields[0]] = value
		}
		return values, nil
	}
	cpu, err := counters("cpu.stat")
	if err != nil {
		return err
	}
	if usage, ok := cpu["usage_usec"]; ok {
		seconds := float64(usage) / 1e6
		info.CPUSeconds = &seconds
	}
	max, err := read("cpu.max")
	if err != nil {
		return err
	}
	if max != "" {
		fields := strings.Fields(max)
		if len(fields) != 2 {
			return errors.New("invalid cpu.max: expected quota and period")
		}
		period, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || period == 0 {
			return errors.New("invalid cpu.max period")
		}
		if fields[0] != "max" {
			quota, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil || quota == 0 {
				return errors.New("invalid cpu.max quota")
			}
			cores := float64(quota) / float64(period)
			info.CPULimitCores = &cores
		}
	}
	for _, field := range []struct {
		file string
		out  **uint64
	}{
		{"memory.current", &info.MemoryBytes}, {"memory.max", &info.MemoryLimitBytes}, {"pids.current", &info.PIDsCurrent},
	} {
		text, err := read(field.file)
		if err != nil {
			return err
		}
		if text == "" || field.file == "memory.max" && text == "max" {
			continue
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", field.file, err)
		}
		*field.out = &value
	}
	memory, err := counters("memory.stat")
	if err != nil {
		return err
	}
	if value, ok := memory["anon"]; ok {
		info.MemoryAnonBytes = &value
	}
	if value, ok := memory["file"]; ok {
		info.MemoryFileBytes = &value
	}
	io, err := readCgroupIO(root)
	if err != nil {
		return err
	}
	info.IO = io
	return nil
}

func readCgroupIO(root *os.Root) (*CgroupIO, error) {
	data, err := root.ReadFile("io.stat")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	io := &CgroupIO{Devices: make([]CgroupIODevice, 0)}
	seenDevices := make(map[string]struct{})
	known := []struct {
		key string
		out **uint64
	}{
		{"rbytes", &io.ReadBytes}, {"wbytes", &io.WriteBytes}, {"dbytes", &io.DiscardBytes},
		{"rios", &io.ReadIOs}, {"wios", &io.WriteIOs}, {"dios", &io.DiscardIOs},
	}
	totals := make(map[string]uint64, len(known))
	complete := make(map[string]bool, len(known))
	for _, counter := range known {
		complete[counter.key] = true
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return nil, fmt.Errorf("invalid io.stat row %q", line)
		}
		parts := strings.Split(fields[0], ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid io.stat device %q", fields[0])
		}
		major, majorErr := strconv.ParseUint(parts[0], 10, 32)
		minor, minorErr := strconv.ParseUint(parts[1], 10, 32)
		if majorErr != nil || minorErr != nil {
			return nil, fmt.Errorf("invalid io.stat device %q", fields[0])
		}
		device := fmt.Sprintf("%d:%d", major, minor)
		if _, exists := seenDevices[device]; exists {
			return nil, fmt.Errorf("duplicate io.stat device %q", device)
		}
		seenDevices[device] = struct{}{}
		if len(seenDevices) > 4096 {
			return nil, errors.New("io.stat exceeds 4096 devices")
		}

		counters := make(map[string]uint64, len(fields)-1)
		for _, field := range fields[1:] {
			pair := strings.Split(field, "=")
			if len(pair) != 2 || pair[0] == "" || pair[1] == "" {
				return nil, fmt.Errorf("invalid io.stat counter %q", field)
			}
			if _, exists := counters[pair[0]]; exists {
				return nil, fmt.Errorf("duplicate io.stat counter %q for device %s", pair[0], device)
			}
			value, err := strconv.ParseUint(pair[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid io.stat counter %q: %w", field, err)
			}
			counters[pair[0]] = value
		}
		for _, counter := range known {
			value, exists := counters[counter.key]
			if !exists {
				complete[counter.key] = false
				continue
			}
			if ^uint64(0)-totals[counter.key] < value {
				return nil, fmt.Errorf("io.stat %s total overflows uint64", counter.key)
			}
			totals[counter.key] += value
		}
		io.Devices = append(io.Devices, CgroupIODevice{Device: device, Counters: counters})
	}
	for _, counter := range known {
		if complete[counter.key] {
			value := totals[counter.key]
			*counter.out = &value
		}
	}
	return io, nil
}
