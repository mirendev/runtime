package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info contains git repository information
type Info struct {
	SHA             string
	Branch          string
	IsDirty         bool
	WorkingTreeHash string
	CommitMessage   string
	CommitAuthor    string
	CommitEmail     string
	CommitTimestamp string
	RemoteURL       string
}

// GetInfo retrieves git information for the given directory
func GetInfo(dir string) (*Info, error) {
	// Convert to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if it's a git repository
	if !isGitRepo(absDir) {
		return nil, fmt.Errorf("not a git repository")
	}

	info := &Info{}

	// Get current commit SHA
	sha, err := runGitCommand(absDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to get commit SHA: %w", err)
	}
	info.SHA = strings.TrimSpace(sha)

	// Get current branch
	branch, err := runGitCommand(absDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		// Try to get branch from symbolic ref
		branch, _ = runGitCommand(absDir, "symbolic-ref", "--short", "HEAD")
	}
	info.Branch = strings.TrimSpace(branch)

	// Check if working tree is dirty
	status, _ := runGitCommand(absDir, "status", "--porcelain")
	info.IsDirty = len(strings.TrimSpace(status)) > 0

	// If dirty, get a hash of the working tree state
	if info.IsDirty {
		// This creates a hash of all tracked files including uncommitted changes
		hash, _ := runGitCommand(absDir, "hash-object", "--stdin")
		if hash == "" {
			// Fallback: use status output as a simple indicator
			info.WorkingTreeHash = "dirty"
		} else {
			info.WorkingTreeHash = strings.TrimSpace(hash)[:8]
		}
	}

	// Get commit message
	msg, err := runGitCommand(absDir, "log", "-1", "--pretty=%B")
	if err == nil {
		info.CommitMessage = strings.TrimSpace(msg)
	}

	// Get commit author
	author, err := runGitCommand(absDir, "log", "-1", "--pretty=%an")
	if err == nil {
		info.CommitAuthor = strings.TrimSpace(author)
	}

	// Get commit email
	email, err := runGitCommand(absDir, "log", "-1", "--pretty=%ae")
	if err == nil {
		info.CommitEmail = strings.TrimSpace(email)
	}

	// Get commit timestamp (ISO 8601 format)
	timestamp, err := runGitCommand(absDir, "log", "-1", "--pretty=%cI")
	if err == nil {
		info.CommitTimestamp = strings.TrimSpace(timestamp)
	}

	// Get remote URL (origin)
	remote, _ := runGitCommand(absDir, "config", "--get", "remote.origin.url")
	info.RemoteURL = strings.TrimSpace(remote)

	return info, nil
}

// isGitRepo checks if the directory is inside a git repository
func isGitRepo(dir string) bool {
	_, err := runGitCommand(dir, "rev-parse", "--git-dir")
	return err == nil
}

// runGitCommand executes a git command in the specified directory
func runGitCommand(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// GetShortSHA returns the first 8 characters of the SHA
func (i *Info) GetShortSHA() string {
	if len(i.SHA) >= 8 {
		return i.SHA[:8]
	}
	return i.SHA
}
