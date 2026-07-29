package runner

import (
	"context"
	"fmt"
	"time"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/workloadidentity"
)

// remoteTokenTimeout bounds a single token-minting RPC to the coordinator.
const remoteTokenTimeout = 30 * time.Second

// remoteIssuer satisfies workloadidentity.TokenIssuer by proxying token minting
// to the coordinator over RPC. Distributed runners do not hold the cluster
// signing key, so they cannot mint tokens locally and instead ask the
// coordinator, which holds the key.
//
// The issuer URL is captured once at startup. It is the cluster's OIDC issuer
// anchor, derived from the coordinator's configuration at boot, so it is stable
// for a coordinator's process lifetime; a change (e.g. the cluster gaining a DNS
// hostname via re-registration) restarts the coordinator and is picked up when
// the runner reconnects/restarts.
type remoteIssuer struct {
	ctx       context.Context
	client    *runner_v1alpha.RunnerRegistrationClient
	issuerURL string
}

var _ workloadidentity.TokenIssuer = (*remoteIssuer)(nil)

func newRemoteIssuer(ctx context.Context, client *runner_v1alpha.RunnerRegistrationClient, issuerURL string) *remoteIssuer {
	return &remoteIssuer{
		ctx:       ctx,
		client:    client,
		issuerURL: issuerURL,
	}
}

func (r *remoteIssuer) IssuerURL() string {
	return r.issuerURL
}

func (r *remoteIssuer) IssueToken(app, sandboxID string) (string, error) {
	return r.IssueTokenWithOptions(app, sandboxID, workloadidentity.TokenOptions{})
}

// IssueTokenWithOptions mints a token via the coordinator. The app argument is
// ignored: the coordinator derives the app identity from the sandbox itself so
// a runner cannot forge it.
func (r *remoteIssuer) IssueTokenWithOptions(_, sandboxID string, opts workloadidentity.TokenOptions) (string, error) {
	ctx, cancel := context.WithTimeout(r.ctx, remoteTokenTimeout)
	defer cancel()

	var ttlSeconds int64
	if opts.TTL > 0 {
		ttlSeconds = int64(opts.TTL / time.Second)
	}

	res, err := r.client.IssueWorkloadToken(ctx, sandboxID, opts.Audience, ttlSeconds)
	if err != nil {
		return "", fmt.Errorf("requesting workload token from coordinator: %w", err)
	}
	if res.Error() != "" {
		return "", fmt.Errorf("coordinator refused workload token: %s", res.Error())
	}
	return res.Token(), nil
}

// IssueSystemWorkloadToken mints an identity for a system workload running on
// this runner. The coordinator constrains which workloads a runner may ask for.
func (r *remoteIssuer) IssueSystemWorkloadToken(workload workloadidentity.SystemWorkload, opts workloadidentity.TokenOptions) (string, error) {
	ctx, cancel := context.WithTimeout(r.ctx, remoteTokenTimeout)
	defer cancel()

	var ttlSeconds int64
	if opts.TTL > 0 {
		ttlSeconds = int64(opts.TTL / time.Second)
	}

	res, err := r.client.IssueSystemWorkloadToken(ctx, string(workload), opts.Audience, ttlSeconds)
	if err != nil {
		return "", fmt.Errorf("requesting system workload token from coordinator: %w", err)
	}
	if res.Error() != "" {
		return "", fmt.Errorf("coordinator refused system workload token: %s", res.Error())
	}
	return res.Token(), nil
}
