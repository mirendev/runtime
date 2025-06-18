package clientconfig

import (
	"context"
	"net"

	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
)

func Local(cc *caauth.ClientCertificate, listenAddr string) *Config {
	addr := listenAddr
	if addr == "" {
		addr = "localhost:8443"
	} else {
		_, port, err := net.SplitHostPort(addr)
		if err == nil {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
	}

	return &Config{
		ActiveCluster: "local",

		Clusters: map[string]*ClusterConfig{
			"local": {
				Hostname:   addr,
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

	return active.RPCOptions()
}

func (c *Config) State(ctx context.Context, opts ...rpc.StateOption) (*rpc.State, error) {
	opts = append(c.RPCOptions(), opts...)
	return rpc.NewState(ctx, opts...)
}

func (c *ClusterConfig) RPCOptions() []rpc.StateOption {
	if c.Insecure {
		return []rpc.StateOption{
			rpc.WithEndpoint(c.Hostname),
			rpc.WithBindAddr("[::]:0"),
			rpc.WithSkipVerify,
		}
	}

	return []rpc.StateOption{
		rpc.WithCertPEMs(
			[]byte(c.ClientCert),
			[]byte(c.ClientKey),
		),
		rpc.WithCertificateVerification([]byte(c.CACert)),
		rpc.WithEndpoint(c.Hostname),
		rpc.WithBindAddr("[::]:0"),
	}
}

func (c *ClusterConfig) State(ctx context.Context, opts ...rpc.StateOption) (*rpc.State, error) {
	opts = append(opts, c.RPCOptions()...)
	return rpc.NewState(ctx, opts...)
}
