package commands

import (
	"fmt"
	"net"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/ipdiscovery"
)

func AuthGenerate(ctx *Context, opts struct {
	DataPath    string `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	ConfigPath  string `short:"c" long:"config-path" description:"Path to the config file, - for stdout" default:"clientconfig.yaml"`
	Name        string `short:"n" long:"name" description:"Name of the client certificate" default:"miren-user"`
	Target      string `short:"t" long:"target" description:"Hostname to embed in the config" default:"localhost"`
	ClusterName string `short:"C" long:"cluster-name" description:"Name of the cluster" default:"local"`
	PublicIP    bool   `short:"p" long:"public-ip" description:"Use public IP for the target, if available"`
}) error {
	foundation := coordinate.NewFoundation(ctx.Log, coordinate.CoordinatorConfig{
		DataPath: opts.DataPath,
	})

	err := foundation.LoadCA(ctx)
	if err != nil {
		return err
	}

	err = foundation.LoadAPICert(ctx)
	if err != nil {
		return err
	}

	cc, err := foundation.IssueCertificate(opts.Name)
	if err != nil {
		return err
	}

	var tgt string

	if opts.PublicIP {
		discovery, err := ipdiscovery.DiscoverWithTimeout(10*time.Second, ctx.Log, ipdiscovery.Options{
			NetcheckURL: coordinate.DefaultCloudURL,
		})
		if err != nil {
			return err
		}

		// Find the first public (non-private, non-loopback) IP from local interfaces
		var publicIP string
		for _, addr := range discovery.Addresses {
			ip := net.ParseIP(addr.IP)
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				publicIP = addr.IP
				break
			}
		}
		if publicIP == "" {
			return fmt.Errorf("no public IP found, use --target to specify a hostname")
		}
		tgt = publicIP + ":8443"
	} else {
		tgt = opts.Target
		if !strings.Contains(tgt, ":") {
			tgt = net.JoinHostPort(tgt, "8443")
		}
	}

	if tgt == "" {
		return fmt.Errorf("target hostname is empty, use --target to specify a hostname")
	}

	lcfg := clientconfig.NewConfig()
	lcfg.SetCluster(opts.ClusterName, &clientconfig.ClusterConfig{
		Hostname:   tgt,
		CACert:     string(cc.CACert),
		ClientCert: string(cc.CertPEM),
		ClientKey:  string(cc.KeyPEM),
	})
	if err := lcfg.SetActiveCluster(opts.ClusterName); err != nil {
		return fmt.Errorf("failed to set active cluster: %w", err)
	}

	return lcfg.SaveTo(opts.ConfigPath)
}
