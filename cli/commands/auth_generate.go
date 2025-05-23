package commands

import (
	"net"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
)

func AuthGenerate(ctx *Context, opts struct {
	DataPath    string `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	ConfigPath  string `short:"c" long:"config-path" description:"Path to the config file, - for stdout" default:"clientconfig.yaml"`
	Name        string `short:"n" long:"name" description:"Name of the client certificate" default:"runtime-user"`
	Target      string `short:"t" long:"target" description:"Hostname to embed in the config" default:"localhost"`
	ClusterName string `short:"C" long:"cluster-name" description:"Name of the cluster" default:"local"`
}) error {
	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		DataPath: opts.DataPath,
	})

	err := co.LoadCA(ctx)
	if err != nil {
		return err
	}

	err = co.LoadAPICert(ctx)
	if err != nil {
		return err
	}

	cc, err := co.IssueCertificate(opts.Name)
	if err != nil {
		return err
	}

	tgt := opts.Target
	if !strings.Contains(tgt, ":") {
		tgt = net.JoinHostPort(tgt, "8443")
	}

	lcfg := &clientconfig.Config{
		ActiveCluster: opts.ClusterName,

		Clusters: map[string]*clientconfig.ClusterConfig{
			opts.ClusterName: {
				Hostname:   tgt,
				CACert:     string(cc.CACert),
				ClientCert: string(cc.CertPEM),
				ClientKey:  string(cc.KeyPEM),
			},
		},
	}

	return lcfg.SaveTo(opts.ConfigPath)
}
