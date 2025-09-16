package serverconfig

// DefaultConfig returns a Config with all default values set
func DefaultConfig() *Config {
	return &Config{
		Mode: "standalone",
		Server: ServerConfig{
			Address:            "localhost:8443",
			RunnerAddress:      "localhost:8444",
			DataPath:           "/var/lib/miren",
			RunnerID:           "miren",
			ReleasePath:        "", // Auto-detected in standalone mode
			ConfigClusterName:  "local",
			SkipClientConfig:   false,
			HTTPRequestTimeout: 60,
		},
		TLS: TLSConfig{
			AdditionalNames: []string{},
			AdditionalIPs:   []string{},
			StandardTLS:     false,
		},
		Etcd: EtcdConfig{
			Endpoints:      []string{"http://etcd:2379"},
			Prefix:         "/miren",
			StartEmbedded:  false,
			ClientPort:     12379,
			PeerPort:       12380,
			HTTPClientPort: 12381,
		},
		ClickHouse: ClickHouseConfig{
			StartEmbedded:   false,
			HTTPPort:        8223,
			NativePort:      9009,
			InterServerPort: 9010,
			Address:         "",
		},
		Containerd: ContainerdConfig{
			StartEmbedded: false,
			BinaryPath:    "containerd",
			SocketPath:    "", // Auto-detected if not specified
		},
	}
}
