// Package imagerefs centralizes all Docker image references used throughout the project.
// This provides a single source of truth for image versions and makes updates easier.
package imagerefs

import "strings"

// Infrastructure images
const (
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
	AlpineDefault = "oci.miren.cloud/alpine:3.21"

	// Default Busybox image
	BusyboxDefault = "oci.miren.cloud/busybox:1.37-musl"
)

// GetPythonImage returns a Python image reference with the specified version
func GetPythonImage(version string) string {
	return "oci.miren.cloud/python:" + version + "-slim"
}

// GetRubyImage returns a Ruby image reference with the specified version
func GetRubyImage(version string) string {
	return "oci.miren.cloud/ruby:" + version + "-slim"
}

// GetGolangImage returns a Golang image reference with the specified version.
// The version is truncated to major.minor (e.g., "1.21.5" becomes "1.21").
func GetGolangImage(version string) string {
	// Truncate to major.minor only
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 2 {
		version = parts[0] + "." + parts[1]
	}
	return "oci.miren.cloud/golang:" + version + "-alpine"
}

// GetBunImage returns a Bun runtime image reference with the specified version
func GetBunImage(version string) string {
	return "oci.miren.cloud/bun:" + version
}

// GetNodeImage returns a Node.js image reference with the specified version
func GetNodeImage(version string) string {
	return "oci.miren.cloud/node:" + version + "-slim"
}

// GetRustImage returns a Rust image reference with the specified version
func GetRustImage(version string) string {
	return "oci.miren.cloud/rust:" + version
}
