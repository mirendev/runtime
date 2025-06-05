package testutils

import (
	"path/filepath"
	"runtime"
)

// GetTestFilePath returns a path relative to the calling test file's directory
func GetTestFilePath(pathSegments ...string) string {
	_, testFile, _, _ := runtime.Caller(1)
	testDir := filepath.Dir(testFile)
	return filepath.Join(append([]string{testDir}, pathSegments...)...)
}
