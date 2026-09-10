package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/testutils"
)

func TestDescribePullFailure(t *testing.T) {
	// Shaped like containerd's wrapping: resolver -> http client -> dialer.
	dialTimeout := fmt.Errorf("failed to resolve reference %q: %w", "x",
		fmt.Errorf("failed to do request: %w", &url.Error{
			Op:  "Head",
			URL: "http://10.0.0.5:5000/v2/app/manifests/v1",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		}))

	tests := []struct {
		name     string
		ref      string
		err      error
		wantHint bool
	}{
		{
			name:     "network error against the cluster registry gets the hint",
			ref:      ocireg.Host + "/app:v1",
			err:      dialTimeout,
			wantHint: true,
		},
		{
			name:     "missing image on the cluster registry gets no hint",
			ref:      ocireg.Host + "/app:v1",
			err:      fmt.Errorf("failed to resolve reference %q: %w", "x", errdefs.ErrNotFound),
			wantHint: false,
		},
		{
			name:     "network error against a public registry gets no hint",
			ref:      "docker.io/library/nope:latest",
			err:      dialTimeout,
			wantHint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describePullFailure(tt.ref, tt.err)

			assert.True(t, strings.HasPrefix(got, "failed to pull image "+tt.ref+": "), got)
			assert.Contains(t, got, tt.err.Error())
			assert.Equal(t, tt.wantHint, strings.Contains(got, registryUnreachableHint), got)
		})
	}
}

// TestEnsureImageEmitsPullFailure drives a real containerd pull against a
// port nothing listens on and checks that the failure lands on the sandbox's
// log stream with the app entity, not just in the returned error.
func TestEnsureImageEmitsPullFailure(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()

	lw := &mockLogWriter{}
	co.LogWriter = lw

	sb := &compute.Sandbox{ID: entity.Id("sandbox/pull-test")}
	sb.Spec.LogEntity = "app/pull-test"

	const ref = "127.0.0.1:1/nope:latest"
	ctx = namespaces.WithNamespace(ctx, co.Namespace)
	img, err := co.ensureImage(ctx, sb, "abc123", ref)
	r.Error(err)
	r.Nil(img)
	assert.Contains(t, err.Error(), "failed to pull image "+ref)

	r.Len(lw.entries, 1)
	got := lw.entries[0]
	assert.Equal(t, "app/pull-test", got.entity)
	assert.Equal(t, observability.Stderr, got.log.Stream)
	assert.True(t, strings.HasPrefix(got.log.Body, "[miren] failed to pull image "+ref), got.log.Body)
	assert.Equal(t, "pull-test", got.log.Attributes["source"])
	assert.Equal(t, "abc123", got.log.Attributes["miren.short_id"])
	// Not a cluster-registry ref, so no reachability hint.
	assert.NotContains(t, got.log.Body, registryUnreachableHint)
}
