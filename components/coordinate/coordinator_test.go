package coordinate_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/enttest"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/testutils"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

func TestControlPlaneParse(t *testing.T) {
	r := require.New(t)

	// Setup logging
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create temp directory for test data
	tempDir := t.TempDir()

	// Use dynamic port to avoid conflicts with parallel tests
	port := testutils.GetFreePort(t)

	// Setup coordinator config
	coordCfg := coordinate.CoordinatorConfig{
		Address:       fmt.Sprintf("localhost:%d", port),
		EtcdEndpoints: []string{"etcd:2379"},
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
		DataPath:      tempDir,                   // Use temp directory to prevent file leaks
		NoAuth:        true,
	}

	// Keep request contexts independent so cancelling the boot lifetime below
	// models Graph.Stop without cancelling the client call too.
	bootCtx, cancelBoot := context.WithCancel(t.Context())
	ctx := t.Context()

	// Start coordinator in background
	coord := coordinate.NewControlPlane(coordinate.NewFoundation(log, coordCfg))
	err := coord.Start(bootCtx)
	r.NoError(err)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, coord.Stop(shutdownCtx))
	})

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

	ent := res.File().Entities()[0].Entity()

	enttest.EqualAttr(t, ent, entity.Id("db/id"), types.Id("sandbox/nginx"))

	cv := entity.ComponentValue(
		compute.ContainerImageId, "docker.io/library/nginx:latest",
		compute.ContainerNameId, "nginx",
		compute.ContainerPortId, entity.ComponentValue(
			compute.PortNameId, "http",
			compute.PortPortId, 80,
			compute.PortTypeId, "http",
		),
	).Component()

	enttest.EqualAttr(t, ent, entity.Id("dev.miren.compute/sandbox.container"), cv)

	enttest.EqualAttr(t, ent, entity.Id("dev.miren.compute/sandbox.labels"), "app=nginx")
	enttest.EqualAttr(t, ent, entity.Id("dev.miren.core/metadata.labels"), types.Label{
		Key:   "app",
		Value: "nginx",
	})

	enttest.EqualAttr(t, ent, entity.Id("dev.miren.core/metadata.name"), "nginx")
	enttest.EqualAttr(t, ent, entity.Id("entity/kind"), types.Id("dev.miren.compute/kind.sandbox"))
	enttest.EqualAttr(t, ent, entity.Id("entity/kind"), types.Id("dev.miren.core/kind.metadata"))

	// Graph.Stop cancels the boot lifetime before it starts reverse-order stop
	// hooks. The foundation must remain available during that gap so dependent
	// drains can make their final RPCs over an already-warmed connection.
	cancelBoot()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, err := eac.Parse(ctx, data)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

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
