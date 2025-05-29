package testutils

import (
	"os"
	"strings"
)

// IsModuleLoaded does a naive check to see if the given linux kernel module is available
func IsModuleLoaded(moduleName string) bool {
	content, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	return strings.Contains(string(content), moduleName)
}
