package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CleanEntityID removes common entity type prefixes from entity IDs for cleaner display.
// For example: "sandbox/sb-ABC123" -> "sb-ABC123", "app_version/meet-vXYZ" -> "meet-vXYZ"
func CleanEntityID(id string) string {
	// Common entity prefixes to remove
	prefixes := []string{
		"sandbox/",
		"app_version/",
		"app/",
	}

	cleaned := id
	for _, prefix := range prefixes {
		if after, ok := strings.CutPrefix(cleaned, prefix); ok {
			cleaned = after
			break // Only remove the first matching prefix
		}
	}

	return cleaned
}

// DisplayAppVersion formats an app version string by removing prefixes and bolding the app name.
// For example: "app_version/meet-vXYZ123" -> "**meet**-vXYZ123" (where **meet** is bold)
func DisplayAppVersion(version string) string {
	if version == "" || version == "-" {
		return "-"
	}

	// First clean the entity ID prefix
	cleaned := CleanEntityID(version)

	// Find the hyphen that separates app name from version ID
	parts := strings.SplitN(cleaned, "-", 2)
	if len(parts) != 2 {
		// No hyphen found, return as-is
		return cleaned
	}

	// Bold the app name part
	appName := lipgloss.NewStyle().Bold(true).Render(parts[0])

	// Reconstruct with bold app name
	return appName + "-" + parts[1]
}
