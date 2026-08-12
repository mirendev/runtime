package coordinate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// publishRetries and publishRetryDelay bound the startup attempt. Publication
// is not on the critical path for the cluster's own services — they verify
// tokens in-process — so a few quick tries is the right trade against blocking
// startup on cloud being reachable. The periodic loop keeps trying after that.
const (
	publishRetries    = 3
	publishRetryDelay = 2 * time.Second
)

// publishSigningKeys sends the public half of the workload identity key set to
// miren.cloud, which serves it as OIDC discovery on this cluster's behalf.
//
// Only public material crosses the wire. The signing key was generated here and
// stays here, which is what keeps a compromise of cloud from being able to mint
// an identity — cloud can serve keys, not sign with them.
//
// Returns false when there was nothing to do (not anchored at cloud, or the key
// set is unchanged since the last publish).
func (c *Coordinator) publishSigningKeys(ctx context.Context) (bool, error) {
	if !c.anchoredAtCloud() {
		return false, nil
	}

	fingerprint := c.WorkloadIssuer.KeySetFingerprint()

	c.publishedKeysMu.Lock()
	unchanged := fingerprint == c.publishedKeyFingerprint
	c.publishedKeysMu.Unlock()

	// The key set turns over on rotation and otherwise sits still for months,
	// so republishing an identical set every status cycle is pure noise.
	if unchanged {
		return false, nil
	}

	document, err := c.WorkloadIssuer.JWKSDocument()
	if err != nil {
		return false, fmt.Errorf("building JWKS document: %w", err)
	}

	result, err := c.authClient.PublishJWKS(ctx, document)
	if err != nil {
		return false, err
	}

	c.publishedKeysMu.Lock()
	c.publishedKeyFingerprint = fingerprint
	c.publishedKeysMu.Unlock()

	// Cloud pins a cluster's anchor on first publication and never moves it, so
	// a disagreement here means this process adopted an anchor cloud did not
	// assign — cloud's IDENTITY_ISSUER_BASE_URL changed after this cluster
	// registered, most likely. Tokens minted now carry an iss that does not
	// match the discovery document cloud serves, so they will not verify.
	// Nothing to do about it at runtime, since the issuer URL is fixed at
	// startup, but an operator needs to know a restart will fix it.
	if issuer := c.WorkloadIssuer.IssuerURL(); result.Issuer != issuer {
		c.Log.Warn("miren.cloud anchors this cluster's workload identity elsewhere than the tokens it is minting; "+
			"restart to adopt the assigned anchor",
			"assigned", result.Issuer,
			"minting_with", issuer)
	}

	c.Log.Info("published workload identity signing keys to cloud",
		"issuer", result.Issuer,
		"jwks_uri", result.JWKSURI,
		"key_count", result.KeyCount)

	return true, nil
}

// publishSigningKeysAtStartup makes a bounded attempt to get this cluster's
// public keys to cloud before it starts handing out tokens.
//
// It deliberately does not block startup on success. The tokens this cluster
// mints are verified in-process by its own services, so an unpublished key set
// costs external federation and nothing else — and wedging a cluster's boot on
// cloud being reachable would be a far worse failure than a delayed federation.
func (c *Coordinator) publishSigningKeysAtStartup(ctx context.Context) {
	if !c.anchoredAtCloud() {
		return
	}

	var lastErr error

	for attempt := 1; attempt <= publishRetries; attempt++ {
		published, err := c.publishSigningKeys(ctx)
		if err == nil {
			if !published {
				c.Log.Debug("workload identity key set already published")
			}
			return
		}

		// Cloud has no anchor configured. Retrying cannot change that.
		if errors.Is(err, cloudauth.ErrDiscoveryUnavailable) {
			c.Log.Warn("miren.cloud is not serving workload identity discovery; " +
				"this cluster's tokens can only be verified by its own services")
			return
		}

		lastErr = err

		select {
		case <-ctx.Done():
			return
		case <-time.After(publishRetryDelay):
		}
	}

	c.Log.Error("failed to publish workload identity signing keys to cloud; "+
		"external verifiers will not see this cluster's keys until the next status cycle succeeds",
		"error", lastErr)
}

// anchoredAtCloud reports whether the tokens this cluster mints carry the
// cloud-assigned issuer, which is the only case where cloud should be holding
// this cluster's keys.
//
// A cluster left on the default anchor serves its own discovery, and its tokens
// carry its own hostname as iss. Publishing its keys anyway would have cloud
// serving a discovery document for an issuer no token actually uses — verifiers
// pointed at it would fail closed against every token the cluster mints. So the
// test is not "is cloud reachable" but "is this process actually minting tokens
// under the anchor cloud assigned".
func (c *Coordinator) anchoredAtCloud() bool {
	if c.WorkloadIssuer == nil || c.authClient == nil || !c.CloudAuth.Enabled {
		return false
	}
	if c.CloudAuth.IdentityIssuerURL == "" {
		return false
	}
	return c.WorkloadIssuer.IssuerURL() == c.CloudAuth.IdentityIssuerURL
}
