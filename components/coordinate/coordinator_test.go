package coordinate_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
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
	coordCfg := coordinate.CoordinatorConfig{
		Address:       "localhost:9991",          // Use test port
		EtcdEndpoints: []string{"etcd:2379"},     // Default etcd port
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
	}

	// Create contexts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator in background
	coord := coordinate.NewCoordinator(log, coordCfg)
	err = coord.Start(ctx)
	r.NoError(err)

	// Wait for coordinator to start
	time.Sleep(1 * time.Second)

	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	require.NoError(t, err)

	client, err := rs.Connect(coordCfg.Address, "entities")
	require.NoError(t, err)

	eac := entityserver_v1alpha.EntityAccessClient{Client: client}

	data, err := os.ReadFile("testdata/sandbox.yaml")
	r.NoError(err)

	res, err := eac.Parse(ctx, data)
	r.NoError(err)

	attrs := res.File().Entities()[0].Attrs()

	entity.SortedAttrs(attrs)

	r.Len(attrs, 7)

	r.Equal(attrs[0].ID, entity.Id("db/ident"))
	r.Equal(attrs[0].Value.Keyword(), types.Keyword("sandbox/nginx"))

	r.Equal(attrs[1].ID, entity.Id("dev.miren.compute/sandbox.container"))

	spew.Dump(attrs[1].Value.Component().Attrs)
	r.Equal(attrs[1].Value.Component(), &entity.EntityComponent{
		Attrs: entity.Attrs(
			compute.ContainerImageId, "docker.io/library/nginx:latest",
			compute.ContainerNameId, "nginx",
			compute.ContainerPortId, &entity.EntityComponent{
				Attrs: entity.Attrs(
					compute.PortNameId, "http",
					compute.PortPortId, 80,
					compute.PortTypeId, "http",
				),
			},
		),
	})

	r.Equal(attrs[2].ID, entity.Id("dev.miren.compute/sandbox.labels"))
	r.Equal(attrs[2].Value.String(), "app=nginx")

	r.Equal(attrs[3].ID, entity.Id("dev.miren.core/metadata.labels"))
	r.Equal(attrs[3].Value.Label(), types.Label{
		Key:   "app",
		Value: "nginx",
	})

	r.Equal(attrs[4].ID, entity.Id("dev.miren.core/metadata.name"))
	r.Equal(attrs[4].Value.String(), "nginx")

	r.Equal(attrs[5].ID, entity.Id("entity/kind"))
	r.Equal(attrs[5].Value.Id(), entity.Id("dev.miren.compute/kind.sandbox"))

	r.Equal(attrs[6].ID, entity.Id("entity/kind"))
	r.Equal(attrs[6].Value.Id(), entity.Id("dev.miren.core/kind.metadata"))

	/*
		r.Equal(attrs[2].ID, entity.Id("dev.miren.sandbox/port"))
		r.Equal(attrs[2].Value.Component(), &entity.EntityComponent{
			Attrs: entity.Attrs(
				compute.PortNameId, "http",
				compute.PortPortId, 80,
				compute.PortTypeId, "http",
			),
		})
	*/
}
