package addon

import (
	"context"
	"strings"

	"miren.dev/runtime/pkg/entity"
)

// ConfigImage is the well-known key used in variant config maps to pass
// the resolved container image to provider saga actions.
const ConfigImage = "_image"

// LocalityMode describes where an addon's backing resources run.
type LocalityMode string

const (
	// OnCluster means the addon runs within the Miren cluster.
	OnCluster LocalityMode = "on_cluster"
	// Remote means the addon connects to an external service.
	Remote LocalityMode = "remote"
)

// AddonProvider defines the interface that addon implementations must satisfy.
type AddonProvider interface {
	// LocalityMode returns where this addon's backing resources run.
	LocalityMode() LocalityMode

	// Provision creates the backing resources for an addon and returns the
	// environment variables and entity attributes to store.
	Provision(ctx context.Context, app App, variant Variant) (*ProvisionResult, error)

	// AdjustEnvVars is called when provisioned env vars collide with existing
	// app env vars. The provider can rename or adjust variables.
	AdjustEnvVars(ctx context.Context, result *ProvisionResult, assoc AddonAssociation, collisions []string) ([]Variable, error)

	// Deprovision tears down the backing resources for an addon.
	Deprovision(ctx context.Context, assoc AddonAssociation) error
}

// CredentialRotator is an optional capability for providers that support
// rotating a backing server credential in place. Rotation applies a freshly
// generated secret to the live engine and updates the stored value; the
// returned EnvVars are the updated variables the controller propagates to
// consuming apps.
//
// Not every provider implements this — the controller type-asserts for it and
// reports "rotation not supported" when a provider does not.
type CredentialRotator interface {
	// RotateCredential rotates the named credential on the server backing assoc
	// to newSecret. credential selects which secret to rotate (provider-defined;
	// empty means the provider's default/only credential).
	//
	// newSecret is supplied (and durably recorded) by the controller rather than
	// generated per call, so the operation is idempotent: re-invoking after a
	// crash converges on the same target instead of minting a new secret. Every
	// implementation must be safe to call repeatedly with the same newSecret.
	RotateCredential(ctx context.Context, assoc AddonAssociation, credential, newSecret string) (*RotationResult, error)
}

// RotationResult is returned by a CredentialRotator after a successful rotation.
type RotationResult struct {
	// EnvVars are the updated variables consuming apps must pick up. It is empty
	// when consumers don't embed the rotated secret (e.g. a shared-Postgres
	// superuser password that apps never receive), in which case no redeploy is
	// needed.
	EnvVars []Variable
}

// App identifies the application an addon is being attached to.
type App struct {
	ID   entity.Id
	Name string
}

// Variant describes the variant selected for provisioning.
type Variant struct {
	Name   string
	Config map[string]string
}

// Variable represents an environment variable contributed by an addon.
type Variable struct {
	Key       string
	Value     string
	Sensitive bool
}

// ProvisionResult is returned by a provider after successful provisioning.
type ProvisionResult struct {
	EnvVars []Variable
	Attrs   []entity.Attr
}

// AddonAssociation holds the state needed for deprovisioning.
type AddonAssociation struct {
	ID      entity.Id
	App     entity.Id
	Addon   entity.Id
	Variant string
	Entity  *entity.Entity
}

// AddonDefinition describes an addon's metadata and available variants.
type AddonDefinition struct {
	Name           string
	DisplayName    string
	Description    string
	DefaultVariant string
	Variants       []VariantDefinition
	BaseImage      string // container image without tag (e.g., "oci.miren.cloud/postgres")
	DefaultVersion string // default tag when no version is specified (e.g., "17")
}

// ResolveImage returns the container image for the given version.
// If version is empty, the default version is used.
// If version contains ":", it is used as the full image reference.
// Otherwise, it is appended as a tag to the base image.
func ResolveImage(baseImage, defaultVersion, requestedVersion string) string {
	if requestedVersion == "" {
		requestedVersion = defaultVersion
	}
	if strings.Contains(requestedVersion, ":") {
		return requestedVersion
	}
	return baseImage + ":" + requestedVersion
}

// VariantDefinition describes a single variant within an addon.
type VariantDefinition struct {
	Name        string
	Description string
	Details     map[string]string // display key-value pairs shown to users
	Config      map[string]string // provider-internal configuration
}

// ImageChecker validates that a container image is accessible in its registry.
type ImageChecker interface {
	CheckImage(ctx context.Context, image string) error
}

// NameFromRef extracts the addon name from an entity reference like "addon/postgresql".
func NameFromRef(ref entity.Id) string {
	s := string(ref)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}
