package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"miren.dev/runtime/pkg/cloudapi"
)

const localFileLimit = 1 << 20
const localOutputLimit = 64 << 10

// The root stays open for the conversation: os.Root also confines opens when
// the working tree changes between validating a path and reading it.
type workbenchSource struct {
	root     *os.Root
	patterns []gitignore.Pattern
	prefix   []string
}

func (s *workbenchSource) tools() []cloudapi.ClientTool {
	return []cloudapi.ClientTool{
		{Name: "local_list", Description: "List source files and directories beside the CLI, relative to the local source root. Read-only; hidden, ignored, credential files and symlinks are excluded.", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`)},
		{Name: "local_read", Description: "Read numbered lines of a local source file beside the CLI (not the Cloud sandbox). Paths are relative to the local source root; at most 200 lines and 64 KiB per result.", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"lines":{"type":"integer","minimum":1,"maximum":200}},"required":["path"],"additionalProperties":false}`)},
		{Name: "local_search", Description: "Search local source beside the CLI for literal text. Returns relative paths and numbered matching lines; bounded and read-only. Hidden, ignored, credential files and symlinks are excluded.", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"query":{"type":"string","minLength":1}},"required":["query"],"additionalProperties":false}`)},
	}
}

func sourceExcluded(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		if part == "." {
			continue
		}
		if part == ".miren" && (i == len(parts)-1 || (i == len(parts)-2 && parts[i+1] == "app.toml")) {
			continue
		}
		if strings.HasPrefix(part, ".") && part != ".gitignore" {
			return true
		}
		switch strings.ToLower(part) {
		case "node_modules", "__pycache__", "credentials", "credentials.json", "clientconfig.yaml":
			return true
		}
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "secrets.") || lower == "secrets" {
			return true
		}
		for _, ext := range []string{".pem", ".key", ".p12", ".pfx"} {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
	}
	return false
}

func sourceIgnorePatterns(data []byte, domain []string) []gitignore.Pattern {
	var patterns []gitignore.Pattern
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, gitignore.ParsePattern(line, domain))
		}
	}
	return patterns
}

func sourceRepositoryIgnores(ctx context.Context, repo string) ([]gitignore.Pattern, error) {
	// Git resolves config includes, worktree common directories and path expansion.
	// These fixed metadata commands never execute model-provided commands or a shell.
	exclude, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve Git exclusions (Git must be installed): %w", err)
	}
	excludePath := strings.TrimSuffix(string(exclude), "\n")
	if excludePath == "" || strings.ContainsRune(excludePath, '\n') {
		return nil, fmt.Errorf("cannot resolve Git exclusions: unexpected metadata output")
	}
	global, err := exec.CommandContext(ctx, "git", "-C", repo, "config", "--null", "--path", "--get", "core.excludesFile").Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("cannot resolve Git global exclusions: %w", err)
		}
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			configHome = filepath.Join(home, ".config")
		}
		global = []byte(filepath.Join(configHome, "git", "ignore"))
	}
	var patterns []gitignore.Pattern
	for _, path := range []string{strings.TrimSuffix(string(global), "\x00"), excludePath} {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo, path)
		}
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot open Git exclusions: %w", err)
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > localFileLimit {
			f.Close()
			return nil, fmt.Errorf("cannot read bounded Git exclusions")
		}
		data, err := io.ReadAll(io.LimitReader(f, localFileLimit+1))
		f.Close()
		if err != nil || len(data) > localFileLimit {
			return nil, fmt.Errorf("cannot read bounded Git exclusions")
		}
		patterns = append(patterns, sourceIgnorePatterns(data, nil)...)
	}
	return patterns, nil
}

// Only ignore metadata is read outside the selected source root.
func (s *workbenchSource) inheritedIgnores(ctx context.Context) ([]gitignore.Pattern, []string, error) {
	dir, err := filepath.Abs(s.root.Name())
	if err != nil {
		return nil, nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil, err
	}
	for repo := dir; ; repo = filepath.Dir(repo) {
		if _, err := os.Lstat(filepath.Join(repo, ".git")); err == nil {
			patterns, err := sourceRepositoryIgnores(ctx, repo)
			if err != nil {
				return nil, nil, err
			}
			rel, err := filepath.Rel(repo, dir)
			if err != nil || rel == "." {
				return patterns, nil, err
			}
			prefix := strings.Split(rel, string(filepath.Separator))
			for i := range prefix {
				path := filepath.Join(repo, filepath.Join(prefix[:i]...), ".gitignore")
				info, err := os.Lstat(path)
				if os.IsNotExist(err) {
					if gitignore.NewMatcher(patterns).Match(prefix[:i+1], true) {
						return nil, nil, fmt.Errorf("source root is gitignored")
					}
					continue
				}
				if err != nil {
					return nil, nil, err
				}
				if !info.Mode().IsRegular() || info.Size() > localFileLimit {
					return nil, nil, fmt.Errorf("cannot read bounded ancestor gitignore file")
				}
				f, err := os.Open(path)
				if err != nil {
					return nil, nil, err
				}
				data, err := io.ReadAll(io.LimitReader(f, localFileLimit+1))
				f.Close()
				if err != nil || len(data) > localFileLimit {
					return nil, nil, fmt.Errorf("cannot read bounded ancestor gitignore file")
				}
				patterns = append(patterns, sourceIgnorePatterns(data, prefix[:i])...)
				if gitignore.NewMatcher(patterns).Match(prefix[:i+1], true) {
					return nil, nil, fmt.Errorf("source root is gitignored")
				}
			}
			return patterns, prefix, nil
		} else if !os.IsNotExist(err) {
			return nil, nil, err
		}
		if filepath.Dir(repo) == repo {
			return nil, nil, nil
		}
	}
}

func (s *workbenchSource) path(name string) (string, error) {
	if name == "" {
		name = "."
	}
	name = filepath.Clean(name)
	if !filepath.IsLocal(name) || sourceExcluded(name) {
		return "", fmt.Errorf("path is outside the source root or excluded")
	}
	parts := strings.Split(name, string(filepath.Separator))
	patterns, prefix := s.patterns, s.prefix
	if name == "." {
		return name, nil
	}
	fullPath := append(prefix, parts...)
	for i := range parts {
		parent := filepath.Join(parts[:i]...)
		if parent == "" {
			parent = "."
		}
		ignorePath := filepath.Join(parent, ".gitignore")
		if info, err := s.root.Lstat(ignorePath); err == nil && info.Mode().IsRegular() && info.Size() <= localFileLimit {
			f, err := s.root.Open(ignorePath)
			if err != nil {
				return "", err
			}
			data, err := io.ReadAll(io.LimitReader(f, localFileLimit+1))
			f.Close()
			if err != nil || len(data) > localFileLimit {
				return "", fmt.Errorf("cannot read bounded gitignore file")
			}
			patterns = append(patterns, sourceIgnorePatterns(data, fullPath[:len(prefix)+i])...)
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		component := filepath.Join(parts[:i+1]...)
		info, err := s.root.Lstat(component)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are excluded")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return "", fmt.Errorf("not a regular source file or directory")
		}
		if gitignore.NewMatcher(patterns).Match(fullPath[:len(prefix)+i+1], info.IsDir()) {
			return "", fmt.Errorf("path is gitignored")
		}
	}
	return name, nil
}

func (s *workbenchSource) read(name string) (string, error) {
	name, err := s.path(name)
	if err != nil {
		return "", err
	}
	f, err := s.root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular source file")
	}
	data, err := io.ReadAll(io.LimitReader(f, localFileLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > localFileLimit {
		return "", fmt.Errorf("source file exceeds 1 MiB")
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", fmt.Errorf("not a text source file")
	}
	return string(data), nil
}

func (s *workbenchSource) execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		Query     string `json:"query"`
		StartLine int    `json:"start_line"`
		Lines     int    `json:"lines"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	// Each concurrent request gets its own fresh metadata snapshot.
	snapshot := *s
	patterns, prefix, err := s.inheritedIgnores(ctx)
	if err != nil {
		return "", err
	}
	snapshot.patterns, snapshot.prefix = patterns, prefix
	s = &snapshot
	path, err := s.path(args.Path)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	appendLine := func(line string) bool {
		if out.Len()+len(line)+1 > localOutputLimit {
			return false
		}
		out.WriteString(line)
		out.WriteByte('\n')
		return true
	}
	switch name {
	case "local_list":
		f, err := s.root.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		entries, err := f.ReadDir(1000)
		if err != nil && err != io.EOF {
			return "", err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if _, err := s.path(filepath.Join(path, entry.Name())); err != nil {
				continue
			}
			line := entry.Name()
			if entry.IsDir() {
				line += "/"
			}
			if !appendLine(line) {
				break
			}
		}
		if len(entries) == 1000 {
			appendLine("[listing limited to 1000 entries]")
		}
	case "local_read":
		if args.StartLine == 0 {
			args.StartLine = 1
		}
		if args.Lines == 0 {
			args.Lines = 200
		}
		if args.StartLine < 1 || args.Lines < 1 || args.Lines > 200 {
			return "", fmt.Errorf("invalid line range")
		}
		data, err := s.read(path)
		if err != nil {
			return "", err
		}
		lineNumber, shown := 0, 0
		for line := range strings.SplitSeq(data, "\n") {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			lineNumber++
			if lineNumber < args.StartLine {
				continue
			}
			if shown == args.Lines || !appendLine(fmt.Sprintf("%d: %s", lineNumber, line)) {
				appendLine("[truncated; request a later start_line]")
				break
			}
			shown++
		}
	case "local_search":
		if args.Query == "" {
			return "", fmt.Errorf("query must not be empty")
		}
		visited, matches, bytesRead := 0, 0, 0
		limited := false
		err := fs.WalkDir(s.root.FS(), path, func(name string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			visited++
			if visited > 2000 || matches >= 100 || bytesRead >= 16<<20 {
				limited = true
				return fs.SkipAll
			}
			if walkErr != nil {
				return walkErr
			}
			if _, err := s.path(name); err != nil {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			data, err := s.read(name)
			if err != nil {
				return nil
			}
			bytesRead += len(data)
			lineNumber := 0
			for line := range strings.SplitSeq(data, "\n") {
				lineNumber++
				if strings.Contains(line, args.Query) {
					if matches == 100 || !appendLine(fmt.Sprintf("%s:%d: %s", filepath.ToSlash(name), lineNumber, line)) {
						limited = true
						return fs.SkipAll
					}
					matches++
				}
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if limited {
			appendLine("[search truncated; narrow path or query]")
		}
	default:
		return "", fmt.Errorf("unknown local tool %q", name)
	}
	return out.String(), nil
}
