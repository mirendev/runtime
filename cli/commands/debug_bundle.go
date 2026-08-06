package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// Default timeout for quick commands like df, free, ps
	cmdTimeoutShort = 30 * time.Second
	// Timeout for longer operations like log gathering
	cmdTimeoutLong = 60 * time.Second
)

// runWithTimeout executes a command with the given timeout.
// Returns the combined stdout/stderr output and any error.
func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %s", timeout)
	}

	return output, err
}

func DebugBundle(ctx *Context, opts struct {
	OutputFile      string `short:"o" long:"output" description:"Output file path" default:"miren-debug.tar.gz"`
	Since           string `short:"s" long:"since" description:"Include logs since this time" default:"1 day ago"`
	Namespace       string `long:"namespace" description:"containerd namespace" default:"miren"`
	Socket          string `long:"socket" description:"path to containerd socket"`
	DockerContainer string `short:"d" long:"docker-container" description:"Docker container name to get logs from" default:"miren"`
}) error {
	ctx.Info("Gathering system debug information...")

	// Create output file and stream directly to disk to minimize memory usage
	// on potentially distressed systems
	outFile, err := os.Create(opts.OutputFile)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	// Track success to determine if we should clean up on exit
	success := false
	defer func() {
		// Only close if not already closed (nil check prevents double-close)
		if tw != nil {
			tw.Close()
		}
		if gw != nil {
			gw.Close()
		}
		if outFile != nil {
			outFile.Close()
		}
		if !success {
			os.Remove(opts.OutputFile)
		}
	}()

	// Gather system info
	ctx.Info("Collecting system information...")
	infoData := gatherSystemInfo()
	if err := writeToArchive(tw, "miren-debug/info.txt", infoData); err != nil {
		return fmt.Errorf("writing info.txt: %w", err)
	}

	// Gather processes
	ctx.Info("Collecting process information...")
	processData, err := gatherProcesses()
	if err != nil {
		ctx.Warn("Failed to gather processes: %v", err)
		processData = fmt.Appendf(nil, "Error gathering processes: %v\n", err)
	}
	if err := writeToArchive(tw, "miren-debug/processes.txt", processData); err != nil {
		return fmt.Errorf("writing processes.txt: %w", err)
	}

	// Gather Docker container processes (if Docker container exists and is running)
	containerProcData := gatherContainerProcesses(opts.DockerContainer)
	if len(containerProcData) > 0 {
		if err := writeToArchive(tw, "miren-debug/docker-processes.txt", containerProcData); err != nil {
			return fmt.Errorf("writing docker-processes.txt: %w", err)
		}
	}

	// Gather Docker inspect output
	containerInspectData := gatherContainerInspect(opts.DockerContainer)
	if len(containerInspectData) > 0 {
		if err := writeToArchive(tw, "miren-debug/docker-inspect.json", containerInspectData); err != nil {
			return fmt.Errorf("writing docker-inspect.json: %w", err)
		}
	}

	// Gather containers
	ctx.Info("Collecting container information...")
	containerData, err := gatherContainers(ctx, opts.Socket, opts.Namespace)
	if err != nil {
		ctx.Warn("Failed to gather containers: %v", err)
		containerData = fmt.Appendf(nil, "Error gathering containers: %v\n", err)
	}
	if err := writeToArchive(tw, "miren-debug/containers.txt", containerData); err != nil {
		return fmt.Errorf("writing containers.txt: %w", err)
	}

	// Gather server logs
	ctx.Info("Collecting server logs...")
	logData, err := gatherServerLogs(opts.Since, opts.DockerContainer)
	if err != nil {
		ctx.Warn("Failed to gather server logs: %v", err)
		logData = fmt.Appendf(nil, "Error gathering server logs: %v\n", err)
	}
	if err := writeToArchive(tw, "miren-debug/server-logs.txt", logData); err != nil {
		return fmt.Errorf("writing server-logs.txt: %w", err)
	}

	// Close archive writers in correct order and check for errors.
	// Set to nil after close to prevent double-close in defer.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing tar writer: %w", err)
	}
	tw = nil

	if err := gw.Close(); err != nil {
		return fmt.Errorf("closing gzip writer: %w", err)
	}
	gw = nil

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("closing output file: %w", err)
	}
	outFile = nil

	success = true
	ctx.Completed("Debug information written to %s", opts.OutputFile)
	return nil
}

func writeToArchive(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func gatherSystemInfo() []byte {
	var buf bytes.Buffer

	hostname, _ := os.Hostname()
	now := time.Now()

	fmt.Fprintf(&buf, "Miren System Debug Information\n")
	fmt.Fprintf(&buf, "Generated: %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&buf, "Hostname: %s\n", hostname)

	// Add disk usage (df)
	fmt.Fprintf(&buf, "\n%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(&buf, "DISK USAGE (df -h)\n")
	fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))
	dfOutput, err := runWithTimeout(cmdTimeoutShort, "df", "-h")
	if err != nil {
		fmt.Fprintf(&buf, "Error running df: %v\n", err)
	} else {
		buf.Write(dfOutput)
	}

	// Add memory usage (free)
	fmt.Fprintf(&buf, "\n%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(&buf, "MEMORY USAGE (free -h)\n")
	fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))
	freeOutput, err := runWithTimeout(cmdTimeoutShort, "free", "-h")
	if err != nil {
		fmt.Fprintf(&buf, "Error running free: %v\n", err)
	} else {
		buf.Write(freeOutput)
	}

	return buf.Bytes()
}

func gatherProcesses() ([]byte, error) {
	// Use ps command which has a more efficient CPU percent calculator than
	// iterating processes via /proc and calling CPUPercent() on each.
	// Format: PID, COMM (name), STATE, PPID, %CPU, RSS (in KB), ARGS (cmdline)
	output, err := runWithTimeout(cmdTimeoutShort, "ps", "-eo", "pid,comm,state,ppid,pcpu,rss,args", "--sort=pid")
	if err != nil {
		return nil, fmt.Errorf("running ps: %w", err)
	}
	return output, nil
}

// formatAge formats a duration as a human-readable age string like "2h ago" or "3d ago"
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

func gatherServerLogs(since, dockerContainer string) ([]byte, error) {
	var buf bytes.Buffer

	// Try journalctl first
	fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(&buf, "JOURNALCTL LOGS (miren service)\n")
	fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))

	output, err := runWithTimeout(cmdTimeoutLong, "journalctl", "-u", "miren", "--no-pager", "--since", since)
	if err != nil {
		fmt.Fprintf(&buf, "journalctl error: %v\n\nOutput:\n%s\n", err, string(output))
	} else {
		buf.Write(output)
	}

	// Check for miren running in Docker
	containerLogs := gatherContainerLogs(since, dockerContainer)
	if len(containerLogs) > 0 {
		buf.Write(containerLogs)
	}

	return buf.Bytes(), nil
}

type containerInfo struct {
	id     string
	name   string
	status string
	// rt is the container runtime (docker or podman) that this container was
	// found under, so later logs/exec/inspect calls drive the right engine.
	rt containerRuntime
}

// findRuntimeContainers finds miren server containers matching containerName
// across every available container runtime (Docker and/or Podman), tagging each
// with the runtime it was found under. On a host with both, a container hosted
// by either engine is picked up.
func findRuntimeContainers(containerName string) []containerInfo {
	var containers []containerInfo

	for _, rt := range availableContainerRuntimes() {
		// Find containers matching the specified name (running or recently stopped)
		output, err := runWithTimeout(cmdTimeoutShort, rt.bin, "ps", "-a", "--filter", "name="+containerName, "--format", "{{.ID}}\t{{.Names}}\t{{.Status}}")
		if err != nil {
			continue
		}

		for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}

			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 2 {
				continue
			}

			info := containerInfo{
				id:   parts[0],
				name: parts[1],
				rt:   rt,
			}
			if len(parts) >= 3 {
				info.status = parts[2]
			}
			containers = append(containers, info)
		}
	}

	return containers
}

func gatherContainerLogs(since, containerName string) []byte {
	containers := findRuntimeContainers(containerName)
	if len(containers) == 0 {
		return nil
	}

	// Convert human-readable "since" to Docker format (Go duration or timestamp)
	dockerSince := convertToDockerSince(since)

	var buf bytes.Buffer
	for _, container := range containers {
		fmt.Fprintf(&buf, "\n%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(&buf, "%s LOGS: %s (%s)\n", strings.ToUpper(container.rt.bin), container.name, container.status)
		fmt.Fprintf(&buf, "Container ID: %s\n", container.id)
		fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))

		// Get logs with --since flag
		logOutput, err := runWithTimeout(cmdTimeoutLong, container.rt.bin, "logs", "--since", dockerSince, container.id)
		if err != nil {
			fmt.Fprintf(&buf, "Error getting logs: %v\n%s\n", err, logOutput)
		} else if len(logOutput) == 0 {
			fmt.Fprintf(&buf, "(no logs in the specified time range)\n")
		} else {
			buf.Write(logOutput)
		}
	}

	return buf.Bytes()
}

// convertToDockerSince converts human-readable time strings to Docker's --since format.
// Docker accepts Go duration (e.g., "24h", "30m") or RFC3339 timestamps.
func convertToDockerSince(since string) string {
	since = strings.TrimSpace(strings.ToLower(since))

	// Common conversions from journalctl-style to Docker-style
	conversions := map[string]string{
		"1 day ago":      "24h",
		"1 hour ago":     "1h",
		"2 hours ago":    "2h",
		"6 hours ago":    "6h",
		"12 hours ago":   "12h",
		"1 week ago":     "168h",
		"30 minutes ago": "30m",
	}

	if dockerFormat, ok := conversions[since]; ok {
		return dockerFormat
	}

	// Try to parse patterns like "X days ago", "X hours ago", etc.
	var num int
	var unit string
	if _, err := fmt.Sscanf(since, "%d %s ago", &num, &unit); err == nil {
		switch {
		case strings.HasPrefix(unit, "day"):
			return fmt.Sprintf("%dh", num*24)
		case strings.HasPrefix(unit, "hour"):
			return fmt.Sprintf("%dh", num)
		case strings.HasPrefix(unit, "minute"):
			return fmt.Sprintf("%dm", num)
		case strings.HasPrefix(unit, "week"):
			return fmt.Sprintf("%dh", num*24*7)
		}
	}

	// If already in Go duration format, return as-is
	if _, err := time.ParseDuration(since); err == nil {
		return since
	}

	// Default fallback: 24 hours
	return "24h"
}

func gatherContainerProcesses(containerName string) []byte {
	containers := findRuntimeContainers(containerName)
	if len(containers) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, container := range containers {
		// Only running containers can have exec run on them
		if !strings.HasPrefix(container.status, "Up") {
			continue
		}

		fmt.Fprintf(&buf, "\n%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(&buf, "%s CONTAINER: %s\n", strings.ToUpper(container.rt.bin), container.name)
		fmt.Fprintf(&buf, "Container ID: %s\n", container.id)
		fmt.Fprintf(&buf, "%s\n", strings.Repeat("=", 80))

		// Run ps inside the container
		fmt.Fprintf(&buf, "\n--- PROCESSES (ps aux) ---\n")
		psOutput, err := runWithTimeout(cmdTimeoutShort, container.rt.bin, "exec", container.id, "ps", "aux")
		if err != nil {
			// Try alternative: ps without aux (some minimal containers don't have full ps)
			psOutput, err = runWithTimeout(cmdTimeoutShort, container.rt.bin, "exec", container.id, "ps", "-ef")
			if err != nil {
				fmt.Fprintf(&buf, "Error running ps: %v\n", err)
			} else {
				buf.Write(psOutput)
			}
		} else {
			buf.Write(psOutput)
		}

		// Run df inside the container
		fmt.Fprintf(&buf, "\n--- DISK USAGE (df -h) ---\n")
		dfOutput, err := runWithTimeout(cmdTimeoutShort, container.rt.bin, "exec", container.id, "df", "-h")
		if err != nil {
			fmt.Fprintf(&buf, "Error running df: %v\n", err)
		} else {
			buf.Write(dfOutput)
		}

		// Run free inside the container
		fmt.Fprintf(&buf, "\n--- MEMORY USAGE (free -h) ---\n")
		freeOutput, err := runWithTimeout(cmdTimeoutShort, container.rt.bin, "exec", container.id, "free", "-h")
		if err != nil {
			// Try without -h flag for systems that don't support it
			freeOutput, err = runWithTimeout(cmdTimeoutShort, container.rt.bin, "exec", container.id, "free")
			if err != nil {
				fmt.Fprintf(&buf, "Error running free: %v\n", err)
			} else {
				buf.Write(freeOutput)
			}
		} else {
			buf.Write(freeOutput)
		}

		// Run nerdctl to list miren containers
		fmt.Fprintf(&buf, "\n--- MIREN CONTAINERS (nerdctl) ---\n")
		nerdctlOutput, err := runWithTimeout(cmdTimeoutShort,
			container.rt.bin, "exec", container.id,
			"/var/lib/miren/release/nerdctl",
			"-a", "/var/lib/miren/containerd/containerd.sock",
			"--namespace", "miren",
			"ps", "-a", "--no-trunc")
		if err != nil {
			fmt.Fprintf(&buf, "Error running nerdctl: %v\n%s\n", err, nerdctlOutput)
		} else {
			buf.Write(nerdctlOutput)
		}
	}

	if buf.Len() == 0 {
		return nil
	}

	return buf.Bytes()
}

func gatherContainerInspect(containerName string) []byte {
	containers := findRuntimeContainers(containerName)
	if len(containers) == 0 {
		return nil
	}

	// Group container IDs by the runtime they were found under, then inspect
	// each group with its own engine (a host may run both docker and podman).
	idsByRuntime := make(map[string][]string)
	var order []string
	for _, container := range containers {
		if _, ok := idsByRuntime[container.rt.bin]; !ok {
			order = append(order, container.rt.bin)
		}
		idsByRuntime[container.rt.bin] = append(idsByRuntime[container.rt.bin], container.id)
	}

	// Collect every container's inspect object into one slice so the result is a
	// single valid JSON array even when more than one runtime matched (each
	// `inspect` returns its own top-level array, and concatenating them would
	// produce invalid "[...][...]" JSON).
	var all []map[string]any
	for _, bin := range order {
		args := append([]string{"inspect"}, idsByRuntime[bin]...)
		output, err := runWithTimeout(cmdTimeoutShort, bin, args...)
		if err != nil {
			continue
		}
		var parsed []map[string]any
		if err := json.Unmarshal(output, &parsed); err != nil {
			// Skip unparseable output rather than leak secrets or corrupt the JSON.
			continue
		}
		all = append(all, parsed...)
	}

	if len(all) == 0 {
		return nil
	}

	// Redact secrets (env vars commonly hold credentials) before encoding once.
	for i := range all {
		redactEnvVars(all[i])
	}

	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return []byte("[error: could not encode container inspect output]\n")
	}
	return out
}

// redactEnvVars walks a JSON object tree and replaces environment variable
// values with [REDACTED]. It targets the "Env" arrays that docker inspect
// includes in both container Config and process Config sections.
func redactEnvVars(obj map[string]any) {
	for key, val := range obj {
		if key == "Env" {
			if envList, ok := val.([]any); ok {
				for j, entry := range envList {
					if s, ok := entry.(string); ok {
						if eqIdx := strings.Index(s, "="); eqIdx >= 0 {
							envList[j] = s[:eqIdx+1] + "[REDACTED]"
						}
					}
				}
			}
			continue
		}
		// Recurse into nested objects
		if nested, ok := val.(map[string]any); ok {
			redactEnvVars(nested)
		}
		// Recurse into arrays of objects
		if arr, ok := val.([]any); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]any); ok {
					redactEnvVars(nested)
				}
			}
		}
	}
}
