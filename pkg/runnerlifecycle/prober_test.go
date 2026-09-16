package runnerlifecycle

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/serverinfo"
	serverinfosrv "miren.dev/runtime/servers/serverinfo"
)

// A runner as the prober sees it: one CA-signed certificate that serves its
// listener and authenticates the executor dialing it.
func startRunner(t *testing.T, ready bool) (configPath string, instance *serverinfo.Source) {
	t.Helper()
	ctx := t.Context()
	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)
	cert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "runner-1", Organization: "miren", ValidFor: time.Hour,
		IPs: []net.IP{net.IPv4(127, 0, 0, 1)},
	})
	require.NoError(t, err)

	state, err := rpc.NewState(ctx,
		rpc.WithBindAddr("127.0.0.1:0"),
		rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM),
		rpc.WithCertificateVerification(ca.GetCACertificate()),
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { state.Close() })

	instance = serverinfo.New()
	if ready {
		instance.MarkReady()
	}
	state.Server().ExposeValue(ServerInfoService, server_v1alpha.AdaptServerInfo(serverinfosrv.NewServer(instance)))

	configPath = filepath.Join(t.TempDir(), "config.yaml")
	cfg := &runnerconfig.Config{
		RunnerID:           "runner-1",
		CoordinatorAddress: "127.0.0.1:1",
		CACert:             string(ca.GetCACertificate()),
		ClientCert:         string(cert.CertPEM),
		ClientKey:          string(cert.KeyPEM),
		ListenAddress:      state.ListenAddr(),
	}
	require.NoError(t, cfg.Save(configPath))
	return configPath, instance
}

func TestProberReadsTheRunnerProcess(t *testing.T) {
	configPath, instance := startRunner(t, true)
	snap, err := (&Prober{ConfigPath: configPath}).Probe(t.Context())
	require.NoError(t, err)
	require.Equal(t, instance.InstanceID(), snap.InstanceID)
	require.True(t, snap.Ready)
	require.Equal(t, instance.Info().Version, snap.Version)
}

func TestProberSeesNotReady(t *testing.T) {
	configPath, instance := startRunner(t, false)
	p := &Prober{ConfigPath: configPath}
	snap, err := p.Probe(t.Context())
	require.NoError(t, err)
	require.False(t, snap.Ready)

	instance.MarkReady()
	snap, err = p.Probe(t.Context())
	require.NoError(t, err)
	require.True(t, snap.Ready)
}

func TestProberWithoutListenAddress(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, (&runnerconfig.Config{RunnerID: "runner-1"}).Save(configPath))
	_, err := (&Prober{ConfigPath: configPath}).Probe(t.Context())
	require.ErrorIs(t, err, ErrNoListenAddress)
}

func TestProberWhenRunnerIsDown(t *testing.T) {
	configPath, _ := startRunner(t, true)
	cfg, err := runnerconfig.Load(configPath)
	require.NoError(t, err)
	// A port nothing listens on, so the dial fails rather than hangs.
	cfg.ListenAddress = "127.0.0.1:1"
	require.NoError(t, cfg.Save(configPath))
	_, err = (&Prober{ConfigPath: configPath, Timeout: 2 * time.Second}).Probe(t.Context())
	require.Error(t, err)
}

func TestLocalAddress(t *testing.T) {
	for listen, want := range map[string]string{
		"0.0.0.0:8444":        "127.0.0.1:8444",
		":8444":               "127.0.0.1:8444",
		"[::]:8444":           "[::1]:8444",
		"10.0.0.5:8444":       "10.0.0.5:8444",
		"[fd00::5]:8444":      "[fd00::5]:8444",
		"runner-1.local:8444": "runner-1.local:8444",
		"not-an-address":      "not-an-address",
	} {
		require.Equal(t, want, LocalAddress(listen), listen)
	}
}

func TestCoordinatorVersion(t *testing.T) {
	// The test runner serves ServerInfo the way a coordinator does; point
	// the config's coordinator address at it.
	configPath, instance := startRunner(t, true)
	cfg, err := runnerconfig.Load(configPath)
	require.NoError(t, err)
	cfg.CoordinatorAddress = cfg.ListenAddress
	got, err := CoordinatorVersion(t.Context(), cfg, nil)
	require.NoError(t, err)
	require.Equal(t, instance.Info().Version, got)

	cfg.CoordinatorAddress = "127.0.0.1:1"
	_, err = CoordinatorVersion(t.Context(), cfg, nil)
	require.Error(t, err)
}
