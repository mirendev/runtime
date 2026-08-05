// Package remote resolves secret references over RPC, for nodes that hold no
// key material.
//
// A distributed runner can reach the entity store but not the cluster keyring,
// so it cannot decrypt a stored secret itself. It asks the coordinator instead:
// the bytes are decrypted where the keyring lives and travel back over the
// authenticated RPC connection the runner already holds, to be held in memory
// only for as long as it takes to hand them to a container.
package remote

import (
	"context"
	"fmt"
	"time"

	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/pkg/secret"
)

// resolveTimeout bounds a single resolve so a hung coordinator cannot stall
// sandbox creation indefinitely. Failing is better than hanging: the sandbox
// reports a clear error and the scheduler can retry.
const resolveTimeout = 15 * time.Second

// Resolver resolves references through the coordinator's secrets service.
type Resolver struct {
	client *secret_v1alpha.SecretsClient
}

var _ secret.Resolver = (*Resolver)(nil)

// NewResolver wraps a secrets client as a Resolver.
func NewResolver(client *secret_v1alpha.SecretsClient) *Resolver {
	return &Resolver{client: client}
}

// ResolveRef fetches a reference's value from the coordinator.
//
// The error deliberately names the backend and reference but not the upstream
// error's full text, which could quote surrounding context back into a log on a
// node that has no business holding it.
func (r *Resolver) ResolveRef(ctx context.Context, backend, ref string) (secret.SecretValue, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	res, err := r.client.Resolve(ctx, backend, ref)
	if err != nil {
		return secret.SecretValue{}, fmt.Errorf("resolving %s:%s via coordinator: %w", backend, ref, err)
	}

	return secret.SecretValue{
		Ref:   res.Ref(),
		Bytes: res.Value(),
	}, nil
}
