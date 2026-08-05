package addon

import (
	"strconv"
	"strings"
)

// SanitizeIdentifier converts a name into a safe database identifier by
// keeping only lowercase alphanumeric characters and underscores, converting
// uppercase to lowercase and hyphens to underscores, and truncating to maxLen.
func SanitizeIdentifier(name string, maxLen int) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32) // lowercase
		} else if c == '-' {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		result = []byte("app")
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = append([]byte{'a'}, result...)
	}
	if maxLen > 0 && len(result) > maxLen {
		result = result[:maxLen]
	}
	return string(result)
}

// ParseStorageGb converts a Kubernetes-style size string (e.g. "1Gi", "50Gi")
// to an int64 value in gigabytes. Returns 1 if the string cannot be parsed.
func ParseStorageGb(s string) int64 {
	s = strings.TrimSpace(s)
	if before, ok := strings.CutSuffix(s, "Gi"); ok {
		n, err := strconv.ParseInt(before, 10, 64)
		if err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// IsSharedVariant returns true if the variant is a shared-server variant.
func IsSharedVariant(variantName string) bool {
	return variantName == "shared"
}
