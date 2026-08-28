package commands

import "fmt"

func formatSource(kind, value string) string {
	switch kind {
	case "image":
		if value == "" {
			return "image"
		}
		return fmt.Sprintf("image %s", value)
	case "dockerfile":
		return "dockerfile"
	case "stack":
		if value == "" {
			return "auto-detected stack"
		}
		return fmt.Sprintf("%s (auto-detected)", value)
	case "":
		return ""
	default:
		if value == "" {
			return kind
		}
		return fmt.Sprintf("%s  %s", kind, value)
	}
}

// analysisSource keeps a new CLI useful against an older server. Stack was the
// only source-shaped field before source_kind/source_value were added.
func analysisSource(stack, kind, value string) (string, string) {
	if kind != "" {
		return kind, value
	}

	switch stack {
	case "image", "dockerfile", "unknown", "":
		return stack, ""
	default:
		return "stack", stack
	}
}
