package containerdx

import "strings"

// NormalizeImageReference converts short image references to fully qualified names.
// Examples:
//   - "postgres:15" -> "docker.io/library/postgres:15"
//   - "myuser/myimage:tag" -> "docker.io/myuser/myimage:tag"
//   - "gcr.io/project/image:tag" -> "gcr.io/project/image:tag" (unchanged)
//   - "localhost:5000/image:tag" -> "localhost:5000/image:tag" (unchanged)
func NormalizeImageReference(image string) string {
	// If the image already contains a registry (has '/' before any ':' or no ':' at all),
	// assume it's fully qualified
	if strings.Contains(image, "/") {
		// Check if the registry part (before first '/') looks like a domain/host
		firstSlash := strings.Index(image, "/")
		registryPart := image[:firstSlash]

		// If it contains a '.', ':', or is 'localhost', it's likely a registry
		if strings.Contains(registryPart, ".") ||
		   strings.Contains(registryPart, ":") ||
		   registryPart == "localhost" {
			return image
		}

		// Otherwise it's like "user/repo:tag", prepend docker.io
		return "docker.io/" + image
	}

	// No '/', so it's a short reference like "postgres:15" or "postgres"
	return "docker.io/library/" + image
}
