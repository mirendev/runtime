//go:build linux

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExternalEtcdRequiresEndpoints(t *testing.T) {
	b := &etcdBoot{}
	_, err := b.startExternal(t.Context())
	require.ErrorContains(t, err, "etcd endpoints not specified")
}
