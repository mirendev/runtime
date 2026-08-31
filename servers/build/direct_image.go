package build

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/imageutil"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"

	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	maxManifestIndexNesting    = 3
	directImageRegistryTimeout = 60 * time.Second
)

type imageMetadataResolver interface {
	Resolve(ctx context.Context, ref string, platform ocispecs.Platform) (digest.Digest, []byte, error)
}

type registryImageMetadataResolver struct {
	resolver remotes.Resolver
}

func (r registryImageMetadataResolver) Resolve(ctx context.Context, ref string, platform ocispecs.Platform) (digest.Digest, []byte, error) {
	buffer := contentutil.NewBuffer()
	topLevelDigest, config, err := imageutil.Config(ctx, ref, r.resolver, buffer, nil, &platform)
	if err != nil {
		return "", nil, err
	}

	manifestDigest, err := selectedManifestDigest(ctx, buffer, topLevelDigest)
	if err != nil {
		return "", nil, fmt.Errorf("finding selected manifest: %w", err)
	}
	return manifestDigest, config, nil
}

// selectedManifestDigest follows the content imageutil.Config fetched for its
// platform selection. LimitManifests leaves only the chosen branch of an index
// in the buffer, so this recovers the exact manifest whose config was returned
// rather than pinning the (potentially multi-platform) index above it.
func selectedManifestDigest(ctx context.Context, provider content.Provider, dgst digest.Digest) (digest.Digest, error) {
	return selectedManifestDigestAtDepth(ctx, provider, dgst, 0)
}

func selectedManifestDigestAtDepth(ctx context.Context, provider content.Provider, dgst digest.Digest, depth int) (digest.Digest, error) {
	desc := ocispecs.Descriptor{Digest: dgst}
	ra, err := provider.ReaderAt(ctx, desc)
	if err != nil {
		return "", err
	}
	desc.Size = ra.Size()
	mediaType, err := imageutil.DetectManifestMediaType(ra)
	ra.Close()
	if err != nil {
		return "", err
	}
	desc.MediaType = mediaType

	if images.IsManifestType(desc.MediaType) {
		return desc.Digest, nil
	}
	if !images.IsIndexType(desc.MediaType) {
		return "", fmt.Errorf("unexpected media type %q for %s", desc.MediaType, desc.Digest)
	}
	if depth >= maxManifestIndexNesting {
		return "", fmt.Errorf("manifest index nesting exceeds maximum depth %d", maxManifestIndexNesting)
	}

	data, err := content.ReadBlob(ctx, provider, desc)
	if err != nil {
		return "", err
	}
	var index ocispecs.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return "", fmt.Errorf("decoding image index: %w", err)
	}
	var selected []ocispecs.Descriptor
	for _, child := range index.Manifests {
		ra, err := provider.ReaderAt(ctx, child)
		if err != nil {
			continue
		}
		ra.Close()
		selected = append(selected, child)
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("expected one selected child in fetched index %s, found %d", desc.Digest, len(selected))
	}
	return selectedManifestDigestAtDepth(ctx, provider, selected[0].Digest, depth+1)
}

func (b *Builder) resolveDirectImage(ctx context.Context, input string) (string, *BuildResult, error) {
	source := containerdx.NormalizeImageReference(input)
	// Miren does not model a separate target sandbox platform yet. Match source
	// builds, which also resolve and emit images for the server's native platform.
	pl := platforms.Normalize(platforms.DefaultSpec())
	manifestDigest, config, err := b.directImageMetadataResolver().Resolve(ctx, source, pl)
	if err != nil {
		return "", nil, fmt.Errorf("resolving image %s: %w", source, err)
	}

	res := &BuildResult{ManifestDigest: manifestDigest.String()}
	if err := applyImageConfig(res, config); err != nil {
		return "", nil, fmt.Errorf("reading config for image %s: %w", source, err)
	}
	if res.WorkingDir == "" {
		res.WorkingDir = "/"
	}

	named, err := reference.ParseNormalizedNamed(source)
	if err != nil {
		return "", nil, fmt.Errorf("parsing resolved image name %s: %w", source, err)
	}
	pinned, err := reference.WithDigest(reference.TrimNamed(named), manifestDigest)
	if err != nil {
		return "", nil, fmt.Errorf("pinning image %s: %w", source, err)
	}
	return pinned.String(), res, nil
}

func (b *Builder) directImageMetadataResolver() imageMetadataResolver {
	if b.imageMetadataResolver != nil {
		return b.imageMetadataResolver
	}
	return registryImageMetadataResolver{
		resolver: docker.NewResolver(docker.ResolverOptions{
			Hosts: b.directImageRegistryHosts,
		}),
	}
}

func (b *Builder) directImageRegistryHosts(host string) ([]docker.RegistryHost, error) {
	if host != ocireg.Host && host != "cluster.local" {
		return []docker.RegistryHost{containerdx.DefaultRegistryHost(host)}, nil
	}

	registryHost, err := b.clusterRegistryHost()
	if err != nil {
		return nil, err
	}
	return []docker.RegistryHost{registryHost}, nil
}

func (b *Builder) clusterRegistryHost() (docker.RegistryHost, error) {
	if b.Resolver == nil {
		return docker.RegistryHost{}, fmt.Errorf("network resolver is not configured for %s", ocireg.Host)
	}
	addr, err := b.Resolver.LookupHost("cluster.local")
	if err != nil {
		return docker.RegistryHost{}, fmt.Errorf("resolving cluster registry: %w", err)
	}
	if !addr.IsValid() {
		return docker.RegistryHost{}, fmt.Errorf("cluster registry address is unavailable")
	}

	registryHost := docker.RegistryHost{
		Client:       &http.Client{Timeout: directImageRegistryTimeout},
		Host:         addr.String() + ":5000",
		Scheme:       "http",
		Path:         "/v2",
		Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve,
	}
	if b.WorkloadIssuer == nil {
		return registryHost, nil
	}

	token, err := b.WorkloadIssuer.IssueSystemWorkloadToken(
		workloadidentity.SystemWorkloadBuildKit,
		workloadidentity.TokenOptions{Audience: []string{ocireg.Audience}},
	)
	if err != nil {
		return docker.RegistryHost{}, fmt.Errorf("issuing registry token: %w", err)
	}
	registryHost.Header = http.Header{"Authorization": []string{"Bearer " + token}}
	return registryHost, nil
}
