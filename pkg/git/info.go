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

// GetInfo retrieves version-control information for the given directory.
// Git is consulted first; a jj workspace without a colocated .git (the shape
// `jj workspace add` produces) is read through jj instead.
func GetInfo(dir string) (*Info, error) {
	// Convert to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if isGitRepo(absDir) {
		return gitInfo(absDir)
	}
	if isJJWorkspace(absDir) {
		return jjInfo(absDir)
	}
	return nil, fmt.Errorf("not a git or jj repository")
}

func gitInfo(absDir string) (*Info, error) {
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
		// Get a combined view of the dirty state:
		// 1. Status output showing which files are modified
		// 2. Actual diff of changes
		statusOutput, _ := runGitCommand(absDir, "status", "--porcelain", "-z")
		diffOutput, _ := runGitCommand(absDir, "diff", "HEAD", "--no-ext-diff")

		// Combine both outputs to create a unique fingerprint of the dirty state
		combinedState := fmt.Sprintf("status:\n%s\ndiff:\n%s", statusOutput, diffOutput)

		// Create a hash of the combined state
		cmd := exec.Command("git", "hash-object", "--stdin")
		cmd.Dir = absDir
		cmd.Stdin = strings.NewReader(combinedState)
		output, err := cmd.Output()

		if err != nil || len(output) == 0 {
			// Fallback: use a simple hash of the status
			info.WorkingTreeHash = "dirty"
		} else {
			// Use first 8 characters of the hash
			info.WorkingTreeHash = strings.TrimSpace(string(output))[:8]
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

// jjInfo reports the same shape git would for a colocated jj repo, where jj
// pins git HEAD to @- and the working-copy commit @ plays the role of the
// working tree: the commit fields describe @-, and the deploy is dirty when @
// has changes on top of it. The parent's id is stable once pushed, whereas @'s
// id changes on every snapshot, so this keeps recorded SHAs resolvable.
func jjInfo(absDir string) (*Info, error) {
	// jj's template language turns \0 into a NUL byte, which no field can contain.
	const sep = `\0`
	parent, err := runJJCommand(absDir, "log", "-r", "@-", "-n", "1", "--no-graph", "--color=never", "-T",
		`commit_id ++ "`+sep+`" ++ local_bookmarks.map(|b| b.name()).join(",") ++ "`+sep+`" ++ description ++ "`+sep+`" ++ author.name() ++ "`+sep+`" ++ author.email() ++ "`+sep+`" ++ committer.timestamp().format("%+") ++ "`+sep+`"`)
	if err != nil {
		return nil, fmt.Errorf("failed to read jj parent commit: %w", err)
	}
	fields := strings.Split(parent, "\x00")
	if len(fields) < 6 {
		return nil, fmt.Errorf("unexpected jj log output: %q", parent)
	}
	info := &Info{
		SHA:             strings.TrimSpace(fields[0]),
		Branch:          strings.TrimSpace(fields[1]),
		CommitMessage:   strings.TrimSpace(fields[2]),
		CommitAuthor:    strings.TrimSpace(fields[3]),
		CommitEmail:     strings.TrimSpace(fields[4]),
		CommitTimestamp: strings.TrimSpace(fields[5]),
	}
	if info.SHA == "" {
		return nil, fmt.Errorf("jj reported no parent commit for the working copy")
	}

	// `empty` is false as soon as @ carries any change over @-, which is jj's
	// equivalent of a dirty working tree. @'s commit id already hashes that
	// tree, so it doubles as the working-tree fingerprint.
	wc, err := runJJCommand(absDir, "log", "-r", "@", "--no-graph", "--color=never", "-T",
		`empty ++ "`+sep+`" ++ commit_id ++ "`+sep+`"`)
	if err != nil {
		return nil, fmt.Errorf("failed to read jj working copy: %w", err)
	}
	wcFields := strings.Split(wc, "\x00")
	// jj's `empty` is true when @ has no changes over @-, so "false" is dirty.
	if len(wcFields) >= 2 && strings.TrimSpace(wcFields[0]) == "false" {
		info.IsDirty = true
		info.WorkingTreeHash = strings.TrimSpace(wcFields[1])
		if len(info.WorkingTreeHash) > 8 {
			info.WorkingTreeHash = info.WorkingTreeHash[:8]
		}
	}

	remotes, _ := runJJCommand(absDir, "git", "remote", "list")
	for _, line := range strings.Split(remotes, "\n") {
		name, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == "origin" {
			info.RemoteURL = strings.TrimSpace(url)
			break
		}
	}

	return info, nil
}

// isGitRepo checks if the directory is inside a git repository
func isGitRepo(dir string) bool {
	_, err := runGitCommand(dir, "rev-parse", "--git-dir")
	return err == nil
}

// isJJWorkspace checks if the directory is inside a jj workspace. A missing
// jj binary simply reports false, which the caller surfaces as "no provenance".
func isJJWorkspace(dir string) bool {
	_, err := runJJCommand(dir, "workspace", "root")
	return err == nil
}

// runGitCommand executes a git command in the specified directory
func runGitCommand(dir string, args ...string) (string, error) {
	return runCommand("git", dir, args...)
}

// runJJCommand executes a jj command in the specified directory
func runJJCommand(dir string, args ...string) (string, error) {
	return runCommand("jj", dir, args...)
}

func runCommand(name, dir string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s command failed: %w, stderr: %s", name, err, stderr.String())
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
