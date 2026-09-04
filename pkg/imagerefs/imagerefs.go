// Package imagerefs centralizes all Docker image references used throughout the project.
// This provides a single source of truth for image versions and makes updates easier.
package imagerefs

// Infrastructure images. These reference exact upstream version tags served
// through the oci.miren.cloud pull-through cache (RFD-87): the proxy aliases
// each name to its real upstream registry (gcr.io, registry.k8s.io, ghcr.io,
// quay.io, Docker Hub). They were previously pinned to opaque "v1" tags from
// the hand-maintained mirror; that mirror is retired, though the old v1 tags
// stay frozen in the registry so already-deployed clusters keep resolving them
// through the proxy's legacy-tag bypass.
const (
	// etcd distributed key-value store (gcr.io/etcd-development/etcd)
	Etcd = "oci.miren.cloud/etcd:v3.5.15"

	// Kubernetes pause container for sandboxes (registry.k8s.io/pause)
	Pause = "oci.miren.cloud/pause:3.8"

	// BuildKit daemon for building containers (ghcr.io/mirendev/buildkit). Our
	// fork only publishes a rolling "latest", so we pin its digest for
	// reproducible builds; bump this when rolling out a new BuildKit.
	BuildKit = "oci.miren.cloud/buildkit@sha256:1263587b78162302359fec3485c153d44872114b8e944ef94be053cc2218679f"

	// Minio object storage server (quay.io/minio/minio)
	Minio = "oci.miren.cloud/minio:RELEASE.2025-04-03T14-56-28Z"

	// VictoriaLogs log storage server (docker.io/victoriametrics/victoria-logs)
	VictoriaLogs = "oci.miren.cloud/victoria-logs:v1.0.0-victorialogs"

	// VictoriaMetrics metrics storage server (docker.io/victoriametrics/victoria-metrics)
	VictoriaMetrics = "oci.miren.cloud/victoria-metrics:v1.106.1"

	// vmagent Prometheus scraper and remote-write forwarder (docker.io/victoriametrics/vmagent)
	VMagent = "oci.miren.cloud/vmagent:v1.150.0"

	// Miren runtime server
	Miren = "oci.miren.cloud/miren:latest"

	// LbdBuilder carries the toolchain that compiles the lbd kernel module
	// against a node's running kernel (docker/Dockerfile.lbd-builder). It
	// holds no module source -- miren embeds that and mounts it in -- so the
	// tag only moves when the toolchain itself needs to.
	LbdBuilder = "oci.miren.cloud/lbd-builder:v1"
)

// Base images for language stacks
const (
	// Default Alpine Linux base image
	AlpineDefault = "oci.miren.cloud/alpine:3.21"

	// Default Busybox image
	BusyboxDefault = "oci.miren.cloud/busybox:1.37-musl"

	// GoRuntimeStatic is the runtime base for pure-Go (cgo-disabled) builds:
	// a fully static binary lands here, yielding the canonical tiny Go image.
	// distroless/static ships ca-certificates, tzdata, and a nonroot user but
	// no shell or libc (docker.io/... proxied to gcr.io/distroless).
	GoRuntimeStatic = "oci.miren.cloud/distroless/static-debian12:nonroot"

	// DebianSlim is the runtime base for builds that need glibc at runtime
	// (cgo) or that carry a runtime filesystem the static image can't hold
	// (e.g. JS-augmented assets). It has a shell, apt, and glibc.
	DebianSlim = "oci.miren.cloud/debian:bookworm-slim"
)

// GetPythonImage returns a Python image reference with the specified version
func GetPythonImage(version string) string {
	return "oci.miren.cloud/python:" + version + "-slim"
}

// GetRubyImage returns a Ruby image reference with the specified version
func GetRubyImage(version string) string {
	return "oci.miren.cloud/ruby:" + version + "-slim"
}

// GetGolangImage returns a Golang builder image reference with the specified
// version. The Go stack builds on the glibc/bookworm variant (not alpine/musl)
// so cgo works out of the box: bookworm ships a C toolchain, and glibc is the
// lingua franca of the prebuilt-binary world (MIR-1248). The build then copies
// the resulting binary onto a minimal runtime base (see GoRuntimeStatic /
// DebianSlim) so the heavyweight toolchain never reaches the final image.
func GetGolangImage(version string) string {
	return "oci.miren.cloud/golang:" + version + "-bookworm"
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
