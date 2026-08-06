package clientconfig

import (
	"fmt"
	"os"
	"strings"

	"miren.dev/runtime/pkg/rpc"
)

// Environment injected into every sandbox by the sandbox controller. See
// controllers/sandbox/sandbox.go.
const (
	// EnvInCluster marks a process as running inside a Miren sandbox. It is the
	// signal InCluster keys off: probing for the token file instead would fire
	// anywhere the path happened to exist.
	EnvInCluster = "MIREN_IN_CLUSTER"

	// EnvAPIAddress is the cluster API, as a literal IP:port. Deliberately not
	// MIREN_SERVER_ADDRESS, which already means the address a server binds to.
	EnvAPIAddress = "MIREN_API_ADDRESS"

	// EnvIdentityTokenPath is the sandbox's workload identity token.
	EnvIdentityTokenPath = "MIREN_IDENTITY_TOKEN_PATH"

	// EnvCACertPath is the cluster CA, for verifying the API certificate.
	EnvCACertPath = "MIREN_CA_CERT_PATH"
)

// InClusterName is the cluster name given to a config built by InCluster.
const InClusterName = "in-cluster"

// APIServerName is the name in-cluster clients verify the API certificate
// against. A sandbox dials the API by address, because the address it uses --
// its bridge router, or the coordinator on a distributed runner -- cannot be a
// certificate SAN. Nothing resolves this name; it is only ever a TLS server
// name. Must match coordinate.APIServerName.
const APIServerName = "api.miren"

// InCluster builds a Config for code running inside a Miren sandbox,
// authenticating with the workload identity token mounted into the container.
// It is the analogue of Kubernetes' rest.InClusterConfig.
//
// Returns ErrNoConfig when not running in a sandbox, so callers can fall
// through to an ordinary config file.
func InCluster() (*Config, error) {
	if os.Getenv(EnvInCluster) == "" {
		return nil, ErrNoConfig
	}

	address := os.Getenv(EnvAPIAddress)
	if address == "" {
		return nil, fmt.Errorf("%s is set but %s is not", EnvInCluster, EnvAPIAddress)
	}

	tokenPath := os.Getenv(EnvIdentityTokenPath)
	if tokenPath == "" {
		return nil, fmt.Errorf("%s is set but %s is not", EnvInCluster, EnvIdentityTokenPath)
	}
	if _, err := os.Stat(tokenPath); err != nil {
		return nil, fmt.Errorf("reading workload identity token %s: %w", tokenPath, err)
	}

	cluster := &ClusterConfig{
		Hostname:          address,
		IdentityTokenPath: tokenPath,
		// The API is dialed by address (bridge router / coordinator IP), which
		// is never a cert SAN, so verification is always against this name. Set
		// it unconditionally — the API cert only carries the api.miren SAN, so a
		// dial-host server name would be rejected even without a custom CA.
		TLSServerName: APIServerName,
	}

	// Verify the API against the cluster CA rather than skipping verification:
	// sandboxes share a bridge, so skipping it would mean trusting whichever
	// neighbour answered.
	if caPath := os.Getenv(EnvCACertPath); caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("reading cluster CA %s: %w", caPath, err)
		}
		cluster.CACert = string(caCert)
	}

	cfg := NewConfig()
	cfg.SetCluster(InClusterName, cluster)
	cfg.SetActiveCluster(InClusterName)

	return cfg, nil
}

// inClusterOptions returns the dial options for a workload identity token.
func (c *ClusterConfig) inClusterOptions(hostname string) []rpc.StateOption {
	return append(c.dialOptions(hostname),
		// Read the token per request rather than once: it expires hourly and the
		// sandbox controller rewrites this file in place (a rename would leave
		// the container's bind mount pinned to the old inode), so the current
		// bytes are always at this path.
		rpc.WithBearerTokenFunc(func() (string, error) {
			return readIdentityToken(c.IdentityTokenPath)
		}),
	)
}

func readIdentityToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading workload identity token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
