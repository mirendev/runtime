package clientconfig

import (
	"context"

	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
)

func Local(cc *caauth.ClientCertificate) *Config {
	return &Config{
		ActiveCluster: "local",

		Clusters: map[string]*ClusterConfig{
			"local": {
				Hostname:   "127.0.0.1:8443",
				CACert:     string(cc.CACert),
				ClientCert: string(cc.CertPEM),
				ClientKey:  string(cc.KeyPEM),
			},
		},
	}
}

func (c *Config) RPCOptions() []rpc.StateOption {
	if c.ActiveCluster == "" {
		return nil
	}

	active, exists := c.Clusters[c.ActiveCluster]
	if !exists {
		return nil
	}

	return []rpc.StateOption{
		rpc.WithCertPEMs(
			[]byte(active.ClientCert),
			[]byte(active.ClientKey),
		),
		rpc.WithCertificateVerification([]byte(active.CACert)),
		rpc.WithEndpoint(active.Hostname),
		rpc.WithBindAddr("[::]:0"),
	}
}

func (c *Config) State(ctx context.Context, opts ...rpc.StateOption) (*rpc.State, error) {
	opts = append(opts, c.RPCOptions()...)
	return rpc.NewState(ctx, opts...)
}
