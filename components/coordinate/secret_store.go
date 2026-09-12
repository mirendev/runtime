package coordinate

import (
	"context"
	"errors"
	"fmt"

	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/secret/secret_v1alpha"
	keyrotationctrl "miren.dev/runtime/controllers/keyrotation"
	"miren.dev/runtime/pkg/secret"
	secretcluster "miren.dev/runtime/pkg/secret/cluster"
	"miren.dev/runtime/pkg/secret/keyring"
	secretsrv "miren.dev/runtime/servers/secret"
)

// NewSecretStore constructs the cluster-owned secret backend and rotation
// controller on top of an already-created foundation.
func NewSecretStore(foundation *Foundation) *SecretStore {
	return &SecretStore{Foundation: foundation}
}

// SecretStore owns the cluster keyring, secret backend, rotation loop, and
// runner-facing secret endpoint.
type SecretStore struct {
	*Foundation
	registry    *secret.Registry
	keyRotation *keyrotationctrl.Controller
}

func (c *SecretStore) Stop() {
	if c.keyRotation != nil {
		c.keyRotation.Stop()
	}
}

// Start opens the cluster secret backend, starts rotation, and exposes secret
// resolution to distributed runners.
func (c *SecretStore) Start(ctx context.Context) error {
	if c.state == nil || c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}

	ec := aes.NewClient(c.Log, c.eac)
	registry := c.Secrets
	if registry == nil {
		registry = secret.NewRegistry()
	}
	secretKeyring, err := keyring.Ensure(c.Log, c.DataPath)
	if err != nil {
		return fmt.Errorf("opening secret keyring: %w", err)
	}
	backend := secretcluster.NewBackend(c.Log, ec, secretKeyring)
	if err := registry.Register(backend); err != nil {
		return fmt.Errorf("registering in-cluster secret backend: %w", err)
	}

	rotationConfig := keyrotationctrl.DefaultConfig()
	if c.SecretKeyRotationPeriod >= 0 {
		rotationConfig.MaxKeyAge = c.SecretKeyRotationPeriod
	}
	rotation := &keyrotationctrl.Controller{
		Log:      c.Log.With("module", "key-rotation"),
		EC:       ec,
		Backend:  backend,
		DataPath: c.DataPath,
		Config:   rotationConfig,
	}
	rotation.Start(ctx)

	c.registry = registry
	c.keyRotation = rotation
	server := c.state.Server()
	server.ExposeValue("dev.miren.runtime/secrets", secret_v1alpha.AdaptSecrets(secretsrv.NewServer(c.Log, registry, rotation)))
	return nil
}

func (c *SecretStore) Registry() *secret.Registry { return c.registry }
