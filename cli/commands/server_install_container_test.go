package commands

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckHostTCPPort(t *testing.T) {
	r := require.New(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	port := ln.Addr().(*net.TCPAddr).Port

	// While the listener is open, binding 0.0.0.0:port overlaps and fails.
	r.Equal(portInUse, checkHostTCPPort(port))

	// Once it's closed the port is free again (Go sets SO_REUSEADDR).
	r.NoError(ln.Close())
	r.Equal(portFree, checkHostTCPPort(port))
}

func TestCheckHostUDPPort(t *testing.T) {
	r := require.New(t)

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	r.NoError(err)
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	r.Equal(portInUse, checkHostUDPPort(port))
}

func TestValidateIngressMode(t *testing.T) {
	r := require.New(t)

	r.NoError(validateIngressMode(""))
	r.NoError(validateIngressMode("tls-autoprovision"))
	r.NoError(validateIngressMode("behind-proxy-http"))
	r.NoError(validateIngressMode("behind-proxy-https"))
	r.Error(validateIngressMode("nonsense"))
	r.Error(validateIngressMode("behind-proxy")) // close but not valid
}

func TestRequiredHostPorts(t *testing.T) {
	hasRole := func(ports []requiredHostPort, role portRole) *requiredHostPort {
		for i := range ports {
			if ports[i].role == role {
				return &ports[i]
			}
		}
		return nil
	}

	t.Run("default mode requires 80, 443, and the API port", func(t *testing.T) {
		r := require.New(t)
		got := requiredHostPorts(containerConfig{HTTPPort: 80})
		r.Len(got, 3)
		r.Equal(80, hasRole(got, roleHTTP).hostPort)
		r.Equal(443, hasRole(got, roleHTTPS).hostPort)
		api := hasRole(got, roleAPI)
		r.Equal(8443, api.hostPort)
		r.Equal("udp", api.proto)
	})

	t.Run("custom http port flows through to the host side", func(t *testing.T) {
		r := require.New(t)
		got := requiredHostPorts(containerConfig{HTTPPort: 8080})
		http := hasRole(got, roleHTTP)
		r.Equal(8080, http.hostPort)
		r.Equal(80, http.containerPort)
	})

	t.Run("behind-proxy-http drops 443", func(t *testing.T) {
		r := require.New(t)
		got := requiredHostPorts(containerConfig{HTTPPort: 80, IngressMode: "behind-proxy-http"})
		r.Len(got, 2)
		r.Nil(hasRole(got, roleHTTPS))
		r.NotNil(hasRole(got, roleHTTP))
	})

	t.Run("behind-proxy-https drops 80", func(t *testing.T) {
		r := require.New(t)
		got := requiredHostPorts(containerConfig{HTTPPort: 80, IngressMode: "behind-proxy-https"})
		r.Len(got, 2)
		r.Nil(hasRole(got, roleHTTP))
		r.NotNil(hasRole(got, roleHTTPS))
	})

	// Host networking still needs the same ports checked (the server binds them
	// directly), but --http-port doesn't apply, so the HTTP port is 80.
	t.Run("host networking still requires 80, 443, and the API port", func(t *testing.T) {
		r := require.New(t)
		got := requiredHostPorts(containerConfig{HTTPPort: 8080, HostNetwork: true})
		r.Len(got, 3)
		r.Equal(80, hasRole(got, roleHTTP).hostPort) // not 8080 — host networking ignores --http-port
		r.Equal(443, hasRole(got, roleHTTPS).hostPort)
		r.NotNil(hasRole(got, roleAPI))
	})
}

func TestIngressArgs(t *testing.T) {
	joined := func(config containerConfig) string {
		return strings.Join(ingressArgs(config), " ")
	}

	t.Run("default mode publishes 80, 443, and 8443", func(t *testing.T) {
		r := require.New(t)
		got := joined(containerConfig{HTTPPort: 80})
		r.Contains(got, "-p 80:80/tcp")
		r.Contains(got, "-p 443:443/tcp")
		r.Contains(got, "-p 8443:8443/udp")
		r.NotContains(got, "MIREN_INGRESS_MODE")
	})

	t.Run("custom http port is published on the host side only", func(t *testing.T) {
		r := require.New(t)
		got := joined(containerConfig{HTTPPort: 8080})
		r.Contains(got, "-p 8080:80/tcp")
	})

	t.Run("behind-proxy-http drops 443 and pins the bind to 0.0.0.0:80", func(t *testing.T) {
		r := require.New(t)
		got := joined(containerConfig{HTTPPort: 80, IngressMode: "behind-proxy-http"})
		r.Contains(got, "-p 80:80/tcp")
		r.NotContains(got, "443:443/tcp")
		r.Contains(got, "MIREN_INGRESS_MODE=behind-proxy-http")
		r.Contains(got, "MIREN_INGRESS_ADDRESS=0.0.0.0:80")
	})

	t.Run("behind-proxy-https drops 80 and pins the bind to 0.0.0.0:443", func(t *testing.T) {
		r := require.New(t)
		got := joined(containerConfig{HTTPPort: 80, IngressMode: "behind-proxy-https"})
		r.Contains(got, "-p 443:443/tcp")
		r.NotContains(got, ":80/tcp")
		r.Contains(got, "MIREN_INGRESS_MODE=behind-proxy-https")
		r.Contains(got, "MIREN_INGRESS_ADDRESS=0.0.0.0:443")
	})

	t.Run("host networking ignores port mappings", func(t *testing.T) {
		r := require.New(t)
		got := joined(containerConfig{HTTPPort: 80, HostNetwork: true})
		r.Contains(got, "--network host")
		r.NotContains(got, "-p ")
	})
}

func TestPortConflictMessage(t *testing.T) {
	render := func(t *testing.T, p requiredHostPort) string {
		t.Helper()
		var buf bytes.Buffer
		ctx := &Context{Stdout: &buf}
		err := portConflict(ctx, p)
		require.Error(t, err)
		return buf.String()
	}

	t.Run("443 conflict points at behind-proxy mode", func(t *testing.T) {
		r := require.New(t)
		msg := render(t, requiredHostPort{hostPort: 443, containerPort: 443, proto: "tcp", role: roleHTTPS})
		t.Logf("\n%s", msg)
		r.Contains(msg, "Port 443 is already in use")
		r.Contains(msg, "--ingress-mode behind-proxy-http")
		r.Contains(msg, requiredPortsHelpURL)
		r.NotContains(msg, "--http-port") // behind-proxy is the right lever here
	})

	t.Run("HTTP conflict points at --http-port, not behind-proxy", func(t *testing.T) {
		r := require.New(t)
		msg := render(t, requiredHostPort{hostPort: 80, containerPort: 80, proto: "tcp", role: roleHTTP})
		r.Contains(msg, "--http-port")
		r.NotContains(msg, "behind-proxy") // moving HTTP, not handing off TLS
	})

	t.Run("API conflict says free it, offers no relocation", func(t *testing.T) {
		r := require.New(t)
		msg := render(t, requiredHostPort{hostPort: 8443, containerPort: 8443, proto: "udp", role: roleAPI})
		r.Contains(msg, "control-plane API")
		r.NotContains(msg, "--http-port")
		r.NotContains(msg, "behind-proxy")
	})
}

func TestEnginePortConflictError(t *testing.T) {
	// translate runs the backstop and returns whether it claimed the error plus
	// the user-facing text it rendered.
	translate := func(err error) (bool, string) {
		var buf bytes.Buffer
		ctx := &Context{Stdout: &buf}
		_, ok := enginePortConflictError(ctx, err)
		return ok, buf.String()
	}

	t.Run("Docker Desktop phrasing is translated to guidance", func(t *testing.T) {
		r := require.New(t)
		ok, msg := translate(errors.New(
			"exit status 125: docker: Error response from daemon: Ports are not available: " +
				"exposing port TCP 0.0.0.0:443 -> ...: bind: address already in use"))
		r.True(ok)
		// The friendly guidance names both levers without guessing the port.
		r.Contains(msg, "already in use")
		r.Contains(msg, "--http-port")
		r.Contains(msg, "behind-proxy-http")
		r.Contains(msg, requiredPortsHelpURL)
	})

	t.Run("rootful Linux phrasing is also translated", func(t *testing.T) {
		r := require.New(t)
		// Different wrapper than Docker Desktop, same stable tail — the coarse
		// match catches it where the old port-extracting regex would not have.
		ok, msg := translate(errors.New(
			"driver failed programming external connectivity on endpoint miren: " +
				"listen tcp4 0.0.0.0:443: bind: address already in use"))
		r.True(ok)
		r.Contains(msg, "already in use")
	})

	t.Run("unrelated errors pass through untouched", func(t *testing.T) {
		r := require.New(t)
		ok, msg := translate(errors.New("no such image: oci.miren.cloud/miren:latest"))
		r.False(ok)
		r.Empty(msg)
	})
}

func TestResolveContainerImage(t *testing.T) {
	cases := []struct {
		name    string
		image   string
		version string
		build   string
		want    string
		wantErr string
	}{
		{name: "image and version conflict", image: "x/y:z", version: "v1.0.0", build: "v0.15.0", wantErr: "mutually exclusive"},
		{name: "image passes through", image: "registry.example/miren:custom", build: "feature:abc1234", want: "registry.example/miren:custom"},
		{name: "image without a tag is not touched", image: "registry.example/miren", build: "v0.15.0", want: "registry.example/miren"},
		{name: "version picks a tag in the default repo", version: "v0.14.0", build: "v0.15.0", want: "oci.miren.cloud/miren:v0.14.0"},
		{name: "version latest is allowed when explicit", version: "latest", build: "dev", want: "oci.miren.cloud/miren:latest"},
		{name: "release build pins its own tag", build: "v0.15.0", want: "oci.miren.cloud/miren:v0.15.0"},
		{name: "prerelease build pins its own tag", build: "v0.16.0-rc.1", want: "oci.miren.cloud/miren:v0.16.0-rc.1"},
		{name: "main build uses main", build: "main:abc1234", want: "oci.miren.cloud/miren:main"},
		{name: "detached HEAD build uses main", build: "HEAD:abc1234", want: "oci.miren.cloud/miren:main"},
		{name: "feature branch build must choose", build: "feature:abc1234", wantErr: "--version"},
		{name: "dev build must choose", build: "dev", wantErr: "--version"},
		// A branch whose name merely starts with "v" is not a release. The
		// channel has its commit suffix stripped before the check, so these
		// only fail the grammar, not the "contains a colon" shape.
		{name: "branch named v1 is not a release", build: "v1:abc1234", wantErr: "--version"},
		{name: "branch named vnext is not a release", build: "vnext:abc1234", wantErr: "--version"},
		{name: "branch named v2-rewrite is not a release", build: "v2-rewrite:abc1234", wantErr: "--version"},
		{name: "branch named v-feature is not a release", build: "v-feature:abc1234", wantErr: "--version"},
		{name: "version without a v prefix is not a release", build: "0.15.0", wantErr: "--version"},
		{name: "unknown build must choose", build: "unknown", wantErr: "--version"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			got, err := resolveContainerImage(tc.image, tc.version, tc.build)
			if tc.wantErr != "" {
				r.Error(err)
				r.Contains(err.Error(), tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
