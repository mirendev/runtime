//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/servers/httpingress"
)

type ingressBootInputs struct {
	ingress        serverconfig.IngressConfig
	tls            serverconfig.TLSConfig
	group          *errgroup.Group
	dataPath       string
	requestTimeout time.Duration
}

type ingressBoot struct {
	component     *boot.Component
	inputs        ingressBootInputs
	observability observabilityBootOutput
	output        boot.Output[ingressBootOutput]
}

type ingressBootOutput struct {
	server *httpingress.Server
}

func ingressInputs(options StartOptions) ingressBootInputs {
	return ingressBootInputs{
		ingress:        options.Config.Ingress,
		tls:            options.Config.TLS,
		group:          options.Group,
		dataPath:       options.Config.Server.GetDataPath(),
		requestTimeout: options.Config.Server.HTTPRequestTimeoutDuration(),
	}
}

func newIngressBoot(inputs ingressBootInputs, workloadControl boot.Output[workloadControlBootOutput], runnerReady *boot.Component, identity boot.Output[workloadIdentityBootOutput], entityAccess boot.Output[entityAccessBootOutput], observability boot.Output[observabilityBootOutput]) *ingressBoot {
	b := &ingressBoot{inputs: inputs}
	b.component, b.output = boot.Provide4(
		"ingress", workloadControl, identity, entityAccess, observability, b.start,
		boot.DependsOn(runnerReady),
	)
	return b
}

func (b *ingressBoot) start(ctx context.Context, workloadControlOutput workloadControlBootOutput, identity workloadIdentityBootOutput, entityAccess entityAccessBootOutput, observability observabilityBootOutput) (ingressBootOutput, error) {
	b.observability = observability
	workloadControl := workloadControlOutput.workloadControl
	handler := httpingress.NewServer(ctx, observability.log, httpingress.IngressConfig{
		RequestTimeout: b.inputs.requestTimeout,
		DataPath:       b.inputs.dataPath,
		WorkloadIssuer: identity.issuer,
	}, entityAccess.rpcClient, workloadControl.Activator(), observability.http, observability.logWriter)
	if err := b.serve(ctx, handler, workloadControl.CertificateProvider(), workloadControl.AutocertReadySignal()); err != nil {
		return ingressBootOutput{}, err
	}
	return ingressBootOutput{server: handler}, nil
}

func (b *ingressBoot) serve(ctx context.Context, handler http.Handler, provider autotls.CertificateProvider, autocertReady func()) error {
	log := b.observability.log
	switch mode := b.inputs.ingress.GetMode(); mode {
	case serverconfig.IngressModeAutoprovision:
		if b.inputs.tls.GetSelfSigned() {
			return autotls.ServeTLSSelfSigned(ctx, log, handler)
		}
		if provider == nil {
			return errors.New("no certificate provider available")
		}
		if err := autotls.ServeTLSWithController(ctx, log, provider, handler); err != nil {
			return err
		}
		if autocertReady != nil {
			autocertReady()
		}
		return nil

	case serverconfig.IngressModeBehindProxyHTTPS:
		addr := b.inputs.ingress.GetAddress()
		if addr == "" {
			addr = "127.0.0.1:443"
		}
		if b.inputs.tls.GetSelfSigned() {
			return autotls.ServeTLSSelfSignedOnAddr(ctx, log, handler, addr)
		}
		if b.inputs.tls.GetAcmeDNSProvider() == "" {
			return errors.New("ingress.mode behind-proxy-https requires tls.self_signed or tls.acme_dns_provider because it does not bind port 80 for HTTP-01")
		}
		if provider == nil {
			return errors.New("no certificate provider available")
		}
		return autotls.ServeTLSWithControllerOnAddr(ctx, log, provider, handler, addr)

	case serverconfig.IngressModeBehindProxyHTTP:
		return b.startHTTP(ctx, handler)
	default:
		return fmt.Errorf("unrecognized ingress.mode %q", mode)
	}
}

func (b *ingressBoot) startHTTP(ctx context.Context, handler http.Handler) error {
	addr := b.inputs.ingress.GetAddress()
	if addr == "" {
		addr = "127.0.0.1:80"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	b.inputs.group.Go(func() error {
		b.observability.log.Info("starting HTTP server", "addr", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP on %s: %w", addr, err)
		}
		return nil
	})
	b.inputs.group.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	})
	return nil
}
