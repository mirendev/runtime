package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"miren.dev/runtime/pkg/containerdx"
)

func DebugCtr(ctx *Context, opts struct {
	Namespace string `short:"n" long:"namespace" description:"containerd namespace" default:"miren"`
	Socket    string `long:"socket" description:"path to containerd socket"`

	Rest struct {
		Args []string
	} `positional-args:"yes"`
}) error {
	socket := opts.Socket
	if socket == "" {
		socket = containerdx.DefaultSocket
	}

	ctrPath, err := findCtr()
	if err != nil {
		return err
	}

	args := []string{"ctr", "-a", socket, "-n", opts.Namespace}
	args = append(args, opts.Rest.Args...)

	env := os.Environ()

	return syscall.Exec(ctrPath, args, env)
}

// findCtr looks for ctr in the miren release directories and falls back to PATH.
// TODO: consolidate release path discovery with server.go into a shared helper
func findCtr() (string, error) {
	// Check system release path first
	systemPath := "/var/lib/miren/release/ctr"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath, nil
	}

	// Check user release path
	if homeDir, err := os.UserHomeDir(); err == nil {
		userPath := filepath.Join(homeDir, ".miren", "release", "ctr")
		if _, err := os.Stat(userPath); err == nil {
			return userPath, nil
		}
	}

	// Fall back to PATH (for dev environments where ctr might be in /usr/local/bin)
	if path, err := exec.LookPath("ctr"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("ctr not found in release directories or PATH")
}
