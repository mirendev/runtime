package commands

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"miren.dev/runtime/dataset"
	"miren.dev/runtime/disk"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/rpc"
)

func DiskRun(ctx *Context, opts struct {
	DataDir string `long:"data" description:"Directory containing disk data"`
	Dir     string `short:"d" long:"dir" description:"Directory to maintain disk access info"`
	Mount   string `long:"mount" description:"Directory to mount the disk"`
	Name    string `short:"n" long:"name" description:"Name of the disk"`
	Bind    string `long:"bind" description:"Address to bind the disk server to" default:"0.0.0.0:8501"`

	DataSetURI string `long:"dataset" description:"Dataset URI"`

	Background bool `long:"background" description:"Run in background"`
}) error {
	// Handle background mode
	if opts.Background {
		ctx.Log.Info("Starting in background mode", "name", opts.Name)

		// Get the current executable path
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}

		// Prepare command arguments without the background flag
		args := []string{
			"disk",
			"run",
			"--data", opts.DataDir,
			"--dir", opts.Dir,
			"--name", opts.Name,
		}

		// Create command
		cmd := exec.Command(executable, args...)

		// Set process group ID to ensure complete detachment from parent
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Pgid:    0, // New process group
		}

		// Redirect stdout and stderr to log files
		logDir := filepath.Join(opts.Dir, "logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		logFile := filepath.Join(logDir, fmt.Sprintf("disk-%s.log", opts.Name))
		outFile, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		cmd.Stdout = outFile
		cmd.Stderr = outFile

		// Start the process in background
		if err := cmd.Start(); err != nil {
			outFile.Close()
			return fmt.Errorf("failed to start background process: %w", err)
		}

		// Close file handles in parent
		outFile.Close()

		ctx.Info("Started disk %s in background (PID: %d)", opts.Name, cmd.Process.Pid)

		// Write PID file
		pidFile := filepath.Join(opts.Dir, fmt.Sprintf("%s.pid", opts.Name))
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644); err != nil {
			ctx.Log.Warn("Failed to write PID file", "error", err)
		}

		return nil
	}

	var sa lsvd.SegmentAccess

	if opts.DataSetURI == "" {
		ctx.Log.Info("Using local filesystem to store data")
		sa = &lsvd.LocalFileAccess{Dir: opts.DataDir, Log: ctx.Log}
	} else {
		ctx.Log.Info("binding to", "addr", "0.0.0.0:0")
		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBindAddr("0.0.0.0:0"))
		if err != nil {
			return err
		}

		u, err := url.Parse(opts.DataSetURI)
		if err != nil {
			return err
		}

		client, err := cs.Connect(u.Host, strings.TrimPrefix(u.Path, "/"))
		if err != nil {
			return err
		}

		dc := &dataset.DataSetsClient{Client: client}
		sa = dataset.NewSegmentAccess(ctx.Log, dc, []string{"application/database"},
			dataset.WithHost(fmt.Sprintf("%s://%s", u.Scheme, u.Host)),
		)

		ctx.Log.Info("Using dataset to store data", "uri", opts.DataSetURI)
	}

	vi, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err != nil {
		ctx.Info("Error loading volume info on %s", opts.Name)
		return err
	}

	ctx.Log.Info("Starting volume", "name", opts.Name, "size", vi.Size.Short())

	runner, err := disk.NewRunner(sa, opts.Dir, ctx.Log)
	if err != nil {
		return err
	}

	return runner.Run(ctx, opts.Name, opts.Mount, opts.Bind)
}
