package clientconfig

import "miren.dev/runtime/pkg/caauth"

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
