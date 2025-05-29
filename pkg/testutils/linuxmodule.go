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
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}
	return false
}
