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
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

type ingressBootInputs struct {
	ingress serverconfig.IngressConfig
	tls     serverconfig.TLSConfig
	group   *errgroup.Group
}

type ingressBoot struct {
	component     *readiness.Component
	inputs        ingressBootInputs
	coordinator   *coordinatorBoot
	observability *observabilityBoot
}

func ingressInputs(options StartOptions) ingressBootInputs {
	return ingressBootInputs{ingress: options.Config.Ingress, tls: options.Config.TLS, group: options.Group}
}

func newIngressBoot(inputs ingressBootInputs, coordinator *coordinatorBoot, observability *observabilityBoot) *ingressBoot {
	b := &ingressBoot{inputs: inputs, coordinator: coordinator, observability: observability}
	b.component = readiness.NewComponent("ingress", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(coordinator.component),
			readiness.ReadyDep(observability.component),
		},
		Start: b.start,
	})
	return b
}

func (b *ingressBoot) start(ctx context.Context, _ readiness.Reporter) error {
	coordinator := b.coordinator.output().coordinator
	handler := coordinator.HttpIngress()
	log := b.observability.output().log
	switch mode := b.inputs.ingress.GetMode(); mode {
	case serverconfig.IngressModeAutoprovision:
		if b.inputs.tls.GetSelfSigned() {
			return autotls.ServeTLSSelfSigned(ctx, log, handler)
		}
		provider := coordinator.CertificateProvider()
		if provider == nil {
			return errors.New("no certificate provider available")
		}
		if err := autotls.ServeTLSWithController(ctx, log, provider, handler); err != nil {
			return err
		}
		if ready := coordinator.AutocertReadySignal(); ready != nil {
			ready()
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
		provider := coordinator.CertificateProvider()
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
		b.observability.output().log.Info("starting HTTP server", "addr", listener.Addr().String())
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
