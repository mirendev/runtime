package coordinate

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/testutils"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

func TestCoordinatorParse(t *testing.T) {
	r := require.New(t)

	// Setup logging
	reg, cleanup := testutils.Registry(observability.TestInject)
	defer cleanup()

	var log *slog.Logger

	err := reg.Init(&log)
	r.NoError(err)

	// Setup coordinator config
	coordCfg := CoordinatorConfig{
		Address:       "localhost:9991",          // Use test port
		EtcdEndpoints: []string{"etcd:2379"},     // Default etcd port
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
	}

	// Create contexts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator in background
	coord := NewCoordinator(log, coordCfg)
	err = coord.Start(ctx)
	r.NoError(err)

	// Wait for coordinator to start
	time.Sleep(1 * time.Second)

	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	require.NoError(t, err)

	client, err := rs.Connect(coordCfg.Address, "entities")
	require.NoError(t, err)

	eac := v1alpha.EntityAccessClient{Client: client}

	data, err := os.ReadFile("testdata/sandbox.yaml")
	r.NoError(err)

	res, err := eac.Parse(ctx, data)
	r.NoError(err)

	attrs := res.Entity().Attrs()

	slices.SortFunc(attrs, func(a, b entity.Attr) int {
		return cmp.Compare(a.ID, b.ID)
	})

	r.Len(attrs, 3)

	r.Equal(attrs[0].ID, entity.Id("dev.miren.sandbox/container"))
	r.Equal(attrs[0].Value.Component(), &entity.EntityComponent{
		Attrs: entity.Attrs(
			compute.ContainerImageId, "nginx:latest",
			compute.ContainerNameId, "nginx",
		),
	})

	r.Equal(attrs[1].ID, entity.Id("dev.miren.sandbox/labels"))
	r.Equal(attrs[1].Value.String(), "app=nginx")

	r.Equal(attrs[2].ID, entity.Id("dev.miren.sandbox/port"))
	r.Equal(attrs[2].Value.Component(), &entity.EntityComponent{
		Attrs: entity.Attrs(
			compute.PortNameId, "http",
			compute.PortPortId, 80,
			compute.PortTypeId, "http",
		),
	})
}
