package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
		socket = defaultContainerdSocket()
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

// findCtr looks for ctr in the miren release directory and falls back to PATH.
func findCtr() (string, error) {
	// Check release directory first
	if releasePath := FindReleasePath(); releasePath != "" {
		ctrPath := filepath.Join(releasePath, "ctr")
		if _, err := os.Stat(ctrPath); err == nil {
			return ctrPath, nil
		}
	}

	// Fall back to PATH (for dev environments where ctr might be in /usr/local/bin)
	if path, err := exec.LookPath("ctr"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("ctr not found in release directory or PATH")
}

// defaultContainerdSocket returns the path to the miren containerd socket.
// It checks for the socket in the default data path, falling back to the
// standard system containerd socket if not found.
func defaultContainerdSocket() string {
	// Check miren's containerd socket (in default data path)
	mirenSocket := "/var/lib/miren/containerd/containerd.sock"
	if _, err := os.Stat(mirenSocket); err == nil {
		return mirenSocket
	}

	// Fall back to system containerd socket
	return "/run/containerd/containerd.sock"
}
