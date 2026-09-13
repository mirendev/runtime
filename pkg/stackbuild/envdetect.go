package stackbuild

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// EnvVarRequirement represents a detected environment variable requirement
type EnvVarRequirement struct {
	Name         string // e.g., "DATABASE_URL"
	Source       string // "gem", "code", "config", "rails_core"
	Confidence   string // "required", "recommended", "optional"
	Reason       string // e.g., "pg gem detected in Gemfile"
	CanGenerate  bool   // true if Miren can auto-generate a value (e.g., SECRET_KEY_BASE)
	ReadFromFile string // if set, read value from this file path (relative to app dir)
	DefaultValue string // if set, this is the default value to use (non-secret)
}

// ignoredEnvVars are environment variables that are commonly used but don't need explicit configuration
// or are detected via dedicated logic (SECRET_KEY_BASE, RAILS_MASTER_KEY are detected via rails_core)
var ignoredEnvVars = map[string]bool{
	"PATH":                  true,
	"HOME":                  true,
	"USER":                  true,
	"SHELL":                 true,
	"LANG":                  true,
	"TZ":                    true,
	"PWD":                   true,
	"TERM":                  true,
	"RACK_ENV":              true, // Handled via RAILS_ENV in Rails apps
	"NODE_ENV":              true,
	"BUNDLE_PATH":           true,
	"BUNDLE_WITHOUT":        true,
	"GEM_HOME":              true,
	"GEM_PATH":              true,
	"SECRET_KEY_BASE_DUMMY": true,
	"SECRET_KEY_BASE":       true, // Detected via rails_core
	"RAILS_MASTER_KEY":      true, // Detected via rails_core
	"RAILS_ENV":             true, // Detected via rails_core with default
	"PORT":                  true,
}

// parseEnvSampleFile parses a .env.sample or .env.example file and returns variable names
func parseEnvSampleFile(dir string, filename string) []string {
	path := filepath.Join(dir, filename)
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var found []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=value or KEY= format
		idx := strings.Index(line, "=")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			// Validate it looks like an env var name
			if isValidEnvVarName(key) && !seen[key] && !ignoredEnvVars[key] {
				seen[key] = true
				found = append(found, key)
			}
		}
	}

	return found
}

// isValidEnvVarName checks if a string is a valid environment variable name
// Valid names start with uppercase letter or underscore, followed by uppercase letters, digits, or underscores
func isValidEnvVarName(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		isUppercase := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		isUnderscore := c == '_'

		if i == 0 {
			if !isUppercase && !isUnderscore {
				return false
			}
		} else {
			if !isUppercase && !isDigit && !isUnderscore {
				return false
			}
		}
	}
	return true
}

// hasEnvVar checks if a slice of EnvVarRequirement contains a variable with the given name
func hasEnvVar(vars []EnvVarRequirement, name string) bool {
	for _, v := range vars {
		if v.Name == name {
			return true
		}
	}
	return false
}

// detectedEnvVar holds an env var found in source code with its optionality
type detectedEnvVar struct {
	name     string
	optional bool
}

// vcsDirs are version-control metadata directories excluded from every
// build context and source scan. The client-side upload already drops them
// (see pkg/tarx), but contexts can also arrive by other routes.
var vcsDirs = []string{".git", ".jj"}

// contextExcludes returns vcsDirs plus any stack-specific patterns, for
// llb.ExcludePatterns on the local build context.
func contextExcludes(extra ...string) []string {
	return append(append([]string{}, vcsDirs...), extra...)
}

// skipDirs is the set of directory names to skip when scanning source files
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	".jj":          true,
	"tmp":          true,
	"log":          true,
	"logs":         true,
	"target":       true, // Rust build directory
	"dist":         true,
	"build":        true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
}

// scanTimeout is the maximum time to spend scanning source files for env vars
const scanTimeout = 10 * time.Second

// errScanTimeout is returned when the scan timeout is exceeded
var errScanTimeout = errors.New("scan timeout exceeded")

// scanSourceFilesForEnvVars walks source files in a directory and extracts env var usage
// using the provided regex patterns. Extensions should include the dot (e.g., ".py", ".go").
// The scan will stop after scanTimeout (10 seconds) to prevent excessive time on large codebases.
func scanSourceFilesForEnvVars(dir string, extensions []string, patterns []*regexp.Regexp, optionalPatterns []*regexp.Regexp) []detectedEnvVar {
	var found []detectedEnvVar
	seen := make(map[string]bool)

	extSet := make(map[string]bool)
	for _, ext := range extensions {
		extSet[ext] = true
	}

	startTime := time.Now()

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Check timeout before processing each file
		if time.Since(startTime) > scanTimeout {
			return errScanTimeout
		}

		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if !extSet[ext] {
			return nil
		}

		vars := scanFileForEnvVars(path, patterns, optionalPatterns)
		for _, v := range vars {
			if !seen[v.name] && !ignoredEnvVars[v.name] {
				seen[v.name] = true
				found = append(found, v)
			}
		}

		return nil
	})

	return found
}

// scanFileForEnvVars extracts env var names from a single file using the provided patterns
func scanFileForEnvVars(path string, patterns []*regexp.Regexp, optionalPatterns []*regexp.Regexp) []detectedEnvVar {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var found []detectedEnvVar
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pattern := range patterns {
			matches := pattern.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) > 1 {
					varName := match[1]
					if !seen[varName] {
						seen[varName] = true
						optional := isOptionalEnvUsageGeneric(line, varName, optionalPatterns)
						found = append(found, detectedEnvVar{
							name:     varName,
							optional: optional,
						})
					}
				}
			}
		}
	}

	return found
}

// isOptionalEnvUsageGeneric checks if the line indicates the env var has a default/fallback
func isOptionalEnvUsageGeneric(line, varName string, optionalPatterns []*regexp.Regexp) bool {
	for _, pattern := range optionalPatterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 1 && match[1] == varName {
			return true
		}
	}
	return false
}

// elevateToRequired checks if an env var name appears in the source code usage set
// and should be elevated from recommended to required
func elevateToRequired(name string, sourceVars []detectedEnvVar) bool {
	for _, v := range sourceVars {
		if v.name == name && !v.optional {
			return true
		}
	}
	return false
}
