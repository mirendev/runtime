// Package imagerefs centralizes all Docker image references used throughout the project.
// This provides a single source of truth for image versions and makes updates easier.
package imagerefs

// Infrastructure images
const (
	// ClickHouse database server
	ClickHouse = "oci.miren.cloud/clickhouse:v2"

	// etcd distributed key-value store
	Etcd = "oci.miren.cloud/etcd:v1"

	// Kubernetes pause container for sandboxes
	Pause = "oci.miren.cloud/pause:v1"

	// BuildKit daemon for building containers
	BuildKit = "oci.miren.cloud/buildkit:v1"

	// Minio object storage server
	Minio = "oci.miren.cloud/minio:v1"

	// VictoriaLogs log storage server
	VictoriaLogs = "oci.miren.cloud/victoria-logs:v1"

	// VictoriaMetrics metrics storage server
	VictoriaMetrics = "oci.miren.cloud/victoria-metrics:v1"

	// Miren runtime server
	Miren = "oci.miren.cloud/miren:latest"
)

// Base images for language stacks
const (
	// Default Alpine Linux base image
	AlpineDefault = "alpine:3.21"
)

// GetPythonImage returns a Python image reference with the specified version
func GetPythonImage(version string) string {
	return "python:" + version + "-slim"
}

// GetRubyImage returns a Ruby image reference with the specified version
func GetRubyImage(version string) string {
	return "ruby:" + version + "-slim"
}

// GetGolangImage returns a Golang image reference with the specified version
func GetGolangImage(version string) string {
	return "golang:" + version + "-alpine"
}

// GetBunImage returns a Bun runtime image reference with the specified version
func GetBunImage(version string) string {
	return "oven/bun:" + version
}

// GetNodeImage returns a Node.js image reference with the specified version
func GetNodeImage(version string) string {
	return "node:" + version + "-alpine"
}
