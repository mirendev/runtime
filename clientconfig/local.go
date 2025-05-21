package clientconfig

import (
	"context"

	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
)

func Local(cc *caauth.ClientCertificate, cert []byte) *Config {
	return &Config{
		ActiveCluster: "local",

		Clusters: map[string]*ClusterConfig{
			"local": {
				Hostname:   "127.0.0.1:8443",
				CACert:     string(cert),
				ClientCert: string(cc.CertPEM),
				ClientKey:  string(cc.KeyPEM),
			},
		},
	}
}

func (c *Config) RPCOptions() []rpc.StateOption {
	return []rpc.StateOption{
		rpc.WithCertPEMs(
			[]byte(c.Clusters[c.ActiveCluster].ClientCert),
			[]byte(c.Clusters[c.ActiveCluster].ClientKey),
		),
		rpc.WithCertificateVerification([]byte(c.Clusters[c.ActiveCluster].CACert)),
	}
}

func (c *Config) State(ctx context.Context, opts ...rpc.StateOption) (*rpc.State, error) {
	opts = append(opts, c.RPCOptions()...)
	return rpc.NewState(ctx, opts...)
}
