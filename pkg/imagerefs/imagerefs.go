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

	// VictoriaLogs log storage server (docker.io/victoriametrics/victoria-logs).
	// v1.52.0 includes all-field LogsQL filters. Keep the multi-platform manifest
	// digest pinned; Miren snapshots data before changes that may migrate its
	// storage format. A pre-snapshot Miren binary still needs manual data restore.
	VictoriaLogs = "oci.miren.cloud/victoria-logs@sha256:47b820890d64c4575a2a0a46415dcd8a4fd59a0f1fcd6a377693d7aea639442e"

	// VictoriaMetrics metrics storage server (docker.io/victoriametrics/victoria-metrics)
	VictoriaMetrics = "oci.miren.cloud/victoria-metrics:v1.106.1"

	// vmagent Prometheus scraper and remote-write forwarder (docker.io/victoriametrics/vmagent)
	VMagent = "oci.miren.cloud/vmagent:v1.150.0"

	// Miren runtime server
	Miren = "oci.miren.cloud/miren:latest"
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

// hexpm tags pair an Elixir release with a specific OTP release and a dated
// Debian rebuild (1.19.6-erlang-28.5.0.7-debian-bookworm-20260918-slim), so
// there is no floating "1.19" tag to point at. ElixirReleases maps the
// versions people actually write onto tags known to exist in one rebuild, and
// is bumped deliberately, all at once. It must stay on bookworm so the
// release's ERTS links against the same glibc and OpenSSL as DebianSlim, where
// the release runs.
const elixirBuild = "debian-bookworm-20260918-slim"

// ElixirRelease is the build we use for one Elixir minor version.
type ElixirRelease struct {
	// Patch is the full Elixir version built for this minor.
	Patch string
	// OTP maps each supported OTP major to the full OTP version built.
	OTP map[string]string
	// DefaultOTP is the OTP major used when none is asked for.
	DefaultOTP string
}

// ElixirReleases is keyed by Elixir minor version ("1.19").
var ElixirReleases = map[string]ElixirRelease{
	"1.16": {Patch: "1.16.3", DefaultOTP: "26", OTP: map[string]string{"24": "24.3.4.17", "25": "25.3.2.21", "26": "26.2.5.21"}},
	"1.17": {Patch: "1.17.3", DefaultOTP: "27", OTP: map[string]string{"25": "25.3.2.21", "26": "26.2.5.21", "27": "27.3.4.18"}},
	"1.18": {Patch: "1.18.5", DefaultOTP: "27", OTP: map[string]string{"25": "25.3.2.21", "26": "26.2.5.21", "27": "27.3.4.18"}},
	"1.19": {Patch: "1.19.6", DefaultOTP: "28", OTP: map[string]string{"26": "26.2.5.21", "27": "27.3.4.18", "28": "28.5.0.7"}},
	"1.20": {Patch: "1.20.4", DefaultOTP: "28", OTP: map[string]string{"27": "27.3.4.18", "28": "28.5.0.7", "29": "29.1.1"}},
}

// ElixirDefaultVersion is the Elixir minor used when an app doesn't pick one.
const ElixirDefaultVersion = "1.19"

// ElixirDefaultTag is the full hexpm tag for ElixirDefaultVersion on its
// default OTP.
var ElixirDefaultTag = ElixirTag(ElixirDefaultVersion, ElixirReleases[ElixirDefaultVersion].DefaultOTP)

// ElixirTag returns the hexpm tag for an Elixir minor and OTP major from
// ElixirReleases, or "" when that pairing isn't in the table.
func ElixirTag(minor, otpMajor string) string {
	rel, ok := ElixirReleases[minor]
	if !ok {
		return ""
	}
	otp, ok := rel.OTP[otpMajor]
	if !ok {
		return ""
	}
	return rel.Patch + "-erlang-" + otp + "-" + elixirBuild
}

// GetElixirImage returns a hexpm/elixir builder image reference for the given
// full hexpm tag (e.g. ElixirDefaultTag). It keeps the hexpm/ namespace rather
// than a bare "elixir", which on Docker Hub is a different image with
// different tags.
func GetElixirImage(tag string) string {
	return "oci.miren.cloud/hexpm/elixir:" + tag
}

// GetRustImage returns a Rust image reference with the specified version
func GetRustImage(version string) string {
	return "oci.miren.cloud/rust:" + version
}
