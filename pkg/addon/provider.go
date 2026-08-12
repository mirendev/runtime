package addon

import (
	"context"
	"strings"

	"miren.dev/runtime/api/addon/addon_v1alpha"
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
	// InApp means the addon has no server of its own: its backing resource is
	// attached to the app's own sandbox. An embedded database like SQLite is
	// in-process, so there is no host to connect to — the app opens a file the
	// addon arranged to have mounted. Such an addon contributes Disks rather
	// than standing up a sandbox pool, and has no container image.
	InApp LocalityMode = "in_app"
)

// AddonProvider defines the interface that addon implementations must satisfy.
type AddonProvider interface {
	// LocalityMode returns where this addon's backing resources run.
	LocalityMode() LocalityMode

	// Provision creates the backing resources for an addon and returns the
	// environment variables and entity attributes to store. It takes the
	// association so the work can be named after it, which is what lets a
	// later pass continue an attempt a crash interrupted rather than start
	// over on top of what it already built.
	Provision(ctx context.Context, assoc AddonAssociation, app App, variant Variant) (*ProvisionResult, error)

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

// Disk is a storage attachment an addon contributes to the app's own sandbox.
//
// Only InApp addons use this. An OnCluster addon owns its storage privately —
// it declares volumes on the sandbox pool it creates — whereas an InApp addon
// has no sandbox of its own and must reach into the app's.
type Disk struct {
	// Name identifies the disk within a service's spec.
	Name string

	// Provider selects the volume provider (see controllers/sandbox/volume.go).
	Provider string

	// MountPath is where the disk appears inside the container.
	MountPath string

	// DbFile names a database within the mounted directory, for providers that
	// manage one. Empty for providers that do not.
	DbFile string

	// Services names the services that receive this disk. Empty means every
	// service in the app, matching how addon env vars reach every service.
	Services []string

	// RequiresSingleWriter marks storage that only one process may write, such
	// as a SQLite database. Services receiving such a disk must run a single
	// fixed instance; the controller refuses to attach it otherwise.
	//
	// The provider declares this rather than the controller inferring it,
	// because whether concurrent writers are safe is a property of what the
	// addon put on the disk, not of the disk itself.
	RequiresSingleWriter bool
}

// ProvisionResult is returned by a provider after successful provisioning.
type ProvisionResult struct {
	EnvVars []Variable
	Attrs   []entity.Attr

	// Disks are attached to the app's own sandbox. Only InApp providers set
	// this; the controller writes them into the app's config the same way it
	// writes EnvVars, so they are removed again on deprovision.
	Disks []Disk
}

// AddonAssociation is the provider's view of an association: enough to name the
// work after it, plus the entity so a teardown or rotation can read what an
// earlier run recorded.
type AddonAssociation struct {
	ID      entity.Id
	App     entity.Id
	Addon   entity.Id
	Variant string

	// Read-only in practice, despite the type. Providers report writes by
	// returning Attrs on a ProvisionResult, and a resumed saga could not write
	// through this anyway: it arrives as a detached copy via JSON.
	Entity *entity.Entity
}

// AssociationFrom builds the provider view from a decoded association and the
// entity it came out of.
//
// NOTE for future cleanup: this type is a subset of
// addon_v1alpha.AddonAssociation plus that entity. Passing the decoded
// association through instead would delete the type, this function, and every
// conversion at once.
func AssociationFrom(assoc *addon_v1alpha.AddonAssociation, ent *entity.Entity) AddonAssociation {
	return AddonAssociation{
		ID:      assoc.ID,
		App:     assoc.App,
		Addon:   assoc.Addon,
		Variant: assoc.Variant,
		Entity:  ent,
	}
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

// ProvisionExecutionID and DeprovisionExecutionID name an association's saga
// after the association itself. That is what lets a later reconcile pass
// continue an attempt a crash interrupted, instead of starting a second one
// alongside whatever the first already built.
//
// Two passes must not drive the same execution at once. The executor guards
// against that within a single instance, but each operation here builds its
// own, so what actually serializes association work is the reconcile
// controller: it processes one event per entity at a time and queues the rest.
// Anything that drives a provision from outside that loop would need its own
// answer.
func ProvisionExecutionID(assocID entity.Id) string {
	return "provision-" + assocID.String()
}

func DeprovisionExecutionID(assocID entity.Id) string {
	return "deprovision-" + assocID.String()
}
