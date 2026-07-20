package service

import (
	"context"
	"io"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

// newServiceController creates a ServiceController from TestDeps for testing.
func newServiceController(d *testutils.TestDeps) (*ServiceController, error) {
	cfg := ServiceControllerDeps{
		Log:             d.Log,
		EAC:             d.EAC,
		IPv4Routable:    d.IPv4Routable,
		ServicePrefixes: d.ServicePrefixes,
		DisableLocalNet: false,
	}
	return NewServiceController(cfg)
}

// newSandboxController creates a SandboxController from TestDeps for testing.
func newSandboxController(d *testutils.TestDeps) (*sandbox.SandboxController, error) {
	sbMetrics := sandbox.NewMetrics()
	sbMetrics.Log = d.Log
	sbMetrics.CPUUsage = d.CPU
	sbMetrics.MemUsage = d.Mem
	cfg := sandbox.SandboxControllerDeps{
		Log:            d.Log,
		CC:             d.CC,
		EAC:            d.EAC,
		Namespace:      d.Namespace,
		NodeId:         compute.NewNodeId("test-node"),
		NetServ:        d.NetServ,
		Bridge:         d.Bridge,
		Subnet:         d.Subnet,
		DataPath:       d.DataPath,
		Tempdir:        d.TempDir,
		LogsMaintainer: d.LogsMaintainer,
		LogWriter:      d.LogWriter,
		StatusMon:      d.StatusMon,
		Resolver:       d.Resolver,
		Metrics:        sbMetrics,
	}
	return sandbox.NewSandboxController(cfg)
}

func TestServiceController(t *testing.T) {
	sbName := func() string {
		return idgen.GenNS("sb")
	}

	svcName := func() string {
		return idgen.GenNS("svc")
	}

	checkClosed := func(t *testing.T, c io.Closer) {
		t.Helper()
		err := c.Close()
		if err != nil {
			t.Errorf("failed to close: %v", err)
		}
	}

	t.Run("creates service without errors", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "test"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("creates endpoints when sandbox matches service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		sbC, err := newSandboxController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, sbC)

		err = sbC.Init(ctx)
		r.NoError(err)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		svcEntity := entity.New(svc.Encode)

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create a sandbox with matching labels and containers with ports
		sbID := entity.Id(sbName())
		sb := &compute.Sandbox{
			ID: sbID,
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name:  "nginx",
						Image: "docker.io/library/nginx:latest",
						Port: []compute.SandboxSpecContainerPort{
							{
								Port: 80,
							},
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs := sb.Encode()
		attrs = append(attrs, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity := entity.New(attrs)

		sbEntity.SetID(sbID)

		// Store the sandbox in entity store
		rpcE.SetAttrs(sbEntity.Attrs())
		res, err := eac.Put(ctx, &rpcE)
		r.NoError(err)

		sbMeta := &entity.Meta{
			Entity:   sbEntity,
			Revision: res.Revision(),
		}

		err = sbC.Create(ctx, sb, sbMeta)
		r.NoError(err)

		// Poll for endpoints to be created
		require.Eventually(t, func() bool {
			endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
			return err == nil && len(endpoints.Values()) > 0
		}, 5*time.Second, 50*time.Millisecond, "Expected endpoints to be created")

		// Check that endpoints were created
		endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		r.Greater(len(endpoints.Values()), 0, "Expected to find endpoints for service")

		found := false
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			if ep.Service == svcID {
				found = true
				r.Len(ep.Endpoint, 1)
				r.NotEmpty(ep.Endpoint[0].Ip)
				r.Equal(int64(80), ep.Endpoint[0].Port)
				break
			}
		}
		r.True(found, "Expected to find endpoints for service")

		// Clean up
		err = sbC.Delete(ctx, sbID, nil)
		r.NoError(err)
	})

	t.Run("deletes endpoints when sandbox is deleted", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		sbC, err := newSandboxController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, sbC)

		err = sbC.Init(ctx)
		r.NoError(err)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create a sandbox with matching labels and containers with ports
		sbID := entity.Id(sbName())
		sb := &compute.Sandbox{
			ID: sbID,
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name:  "nginx",
						Image: "docker.io/library/nginx:latest",
						Port: []compute.SandboxSpecContainerPort{
							{
								Port: 80,
							},
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs := sb.Encode()
		attrs = append(attrs, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity := entity.New(attrs)

		sbEntity.SetID(sbID)

		// Store the sandbox in entity store
		rpcE.SetAttrs(sbEntity.Attrs())
		res, err := eac.Put(ctx, &rpcE)
		r.NoError(err)

		sbMeta := &entity.Meta{
			Entity:   sbEntity,
			Revision: res.Revision(),
		}

		err = sbC.Create(ctx, sb, sbMeta)
		r.NoError(err)

		// Poll for endpoints to be created
		require.Eventually(t, func() bool {
			endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
			return err == nil && len(endpoints.Values()) > 0
		}, 5*time.Second, 50*time.Millisecond, "Expected endpoints to be created")

		// Verify endpoints exist
		endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		var endpointID entity.Id
		var sandboxIP string
		found := false
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			if ep.Service == svcID {
				found = true
				endpointID = ep.ID
				r.Len(ep.Endpoint, 1)
				sandboxIP = ep.Endpoint[0].Ip
				break
			}
		}
		r.True(found, "Expected to find endpoints for service")
		r.NotEmpty(sandboxIP)

		// Delete the sandbox
		err = sbC.Delete(ctx, sbID, nil)
		r.NoError(err)

		// Poll for endpoints to be created
		require.Eventually(t, func() bool {
			endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
			return err == nil && len(endpoints.Values()) != 2
		}, 5*time.Second, 50*time.Millisecond, "Expected endpoints to be created")

		// Verify endpoints are deleted
		endpoints, err = eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		found = false
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			if ep.ID == endpointID {
				found = true
				break
			}
		}
		r.False(found, "Expected endpoints to be deleted when sandbox was deleted")
	})

	t.Run("handles service updates", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create initial service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Update service with additional port
		svc.Port = append(svc.Port, network_v1alpha.Port{
			Name: "https",
			Port: 443,
		})

		meta.Entity = entity.New(svc.Encode())
		meta.Revision = 2

		// Re-create the service with updated configuration
		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("handles multiple sandboxes matching one service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		sbC, err := newSandboxController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, sbC)

		err = sbC.Init(ctx)
		r.NoError(err)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create first sandbox with containers and ports
		sbID1 := entity.Id(sbName())
		sb1 := &compute.Sandbox{
			ID: sbID1,
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name:  "nginx",
						Image: "docker.io/library/nginx:latest",
						Port: []compute.SandboxSpecContainerPort{
							{
								Port: 80,
							},
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs1 := sb1.Encode()
		attrs1 = append(attrs1, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity1 := entity.New(attrs1)

		sbEntity1.SetID(sbID1)

		// Store the sandbox in entity store
		var rpcE1 entityserver_v1alpha.Entity
		rpcE1.SetAttrs(sbEntity1.Attrs())
		res, err := eac.Put(ctx, &rpcE1)
		r.NoError(err)

		sbMeta1 := &entity.Meta{
			Entity:   sbEntity1,
			Revision: res.Revision(),
		}

		err = sbC.Create(ctx, sb1, sbMeta1)
		r.NoError(err)

		// Create second sandbox with containers and ports
		sbID2 := entity.Id(sbName())
		sb2 := &compute.Sandbox{
			ID: sbID2,
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name:  "nginx",
						Image: "docker.io/library/nginx:latest",
						Port: []compute.SandboxSpecContainerPort{
							{
								Port: 80,
							},
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs2 := sb2.Encode()
		attrs2 = append(attrs2, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity2 := entity.New(attrs2)

		sbEntity2.SetID(sbID2)

		// Store the sandbox in entity store
		var rpcE2 entityserver_v1alpha.Entity
		rpcE2.SetAttrs(sbEntity2.Attrs())
		res, err = eac.Put(ctx, &rpcE2)
		r.NoError(err)

		sbMeta2 := &entity.Meta{
			Entity:   sbEntity2,
			Revision: res.Revision(),
		}

		err = sbC.Create(ctx, sb2, sbMeta2)
		r.NoError(err)

		// Poll for endpoints to be created
		require.Eventually(t, func() bool {
			endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
			return err == nil && len(endpoints.Values()) != 0
		}, 5*time.Second, 50*time.Millisecond, "Expected endpoints to be created")

		// Check that endpoints contain both sandboxes - each sandbox creates its own endpoint entity
		endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		serviceEndpoints := 0
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			if ep.Service == svcID {
				serviceEndpoints++
				// Each endpoint entity should have 1 endpoint (for one sandbox)
				r.Len(ep.Endpoint, 1)
			}
		}
		// Should have 2 separate endpoint entities, one for each sandbox
		r.Equal(2, serviceEndpoints, "Expected to find 2 endpoint entities for service")

		// Delete first sandbox
		err = sbC.Delete(ctx, sbID1, nil)
		r.NoError(err)

		// Poll for endpoints to be created
		require.Eventually(t, func() bool {
			endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
			return err == nil && len(endpoints.Values()) != 2
		}, 5*time.Second, 50*time.Millisecond, "Expected endpoints to be created")

		// Check that only one endpoint entity remains after deleting first sandbox
		endpoints, err = eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		serviceEndpoints = 0
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			if ep.Service == svcID {
				serviceEndpoints++
				// Each endpoint entity should have 1 endpoint (for one sandbox)
				r.Len(ep.Endpoint, 1)
			}
		}
		// Should have 1 endpoint entity remaining after deleting first sandbox
		r.Equal(1, serviceEndpoints, "Expected to find 1 endpoint entity for service after deleting first sandbox")

		// Clean up
		err = sbC.Delete(ctx, sbID2, nil)
		r.NoError(err)
	})

	t.Run("handles endpoint updates", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create endpoints
		epID := entity.Id("endpoints-" + svcID.String())
		eps := &network_v1alpha.Endpoints{
			ID:      epID,
			Service: svcID,
			Endpoint: []network_v1alpha.Endpoint{
				{
					Ip:   "10.0.0.1",
					Port: 80,
				},
			},
		}

		ent := entity.New(
			entity.DBId, epID,
			eps.Encode,
		)
		event := controller.Event{
			Entity: ent,
		}

		// Update endpoints through the controller
		_, err = sc.UpdateEndpoints(ctx, event)
		r.NoError(err)
	})

	t.Run("handles endpoint deletions by reconciling the affected service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create multiple services
		svcID1 := entity.Id(svcName())
		svc1 := &network_v1alpha.Service{
			ID: svcID1,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		svcID2 := entity.Id(svcName())
		svc2 := &network_v1alpha.Service{
			ID: svcID2,
			Match: types.Labels{
				types.Label{Key: "app", Value: "apache"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 8080,
				},
			},
		}

		// Store both services in entity store
		var rpcE1 entityserver_v1alpha.Entity
		rpcE1.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID1.String()),
			svc1.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE1)
		r.NoError(err)

		var rpcE2 entityserver_v1alpha.Entity
		rpcE2.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID2.String()),
			svc2.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE2)
		r.NoError(err)

		// Create both services
		svc1Entity := entity.New(svc1.Encode)

		meta1 := &entity.Meta{
			Entity:   svc1Entity,
			Revision: 1,
		}
		err = sc.Create(ctx, svc1, meta1)
		r.NoError(err)

		svc2Entity := entity.New(svc2.Encode)

		meta2 := &entity.Meta{
			Entity:   svc2Entity,
			Revision: 1,
		}
		err = sc.Create(ctx, svc2, meta2)
		r.NoError(err)

		// Create endpoints for first service
		epID := entity.Id("endpoints-" + svcID1.String())
		eps := &network_v1alpha.Endpoints{
			ID:      epID,
			Service: svcID1,
			Endpoint: []network_v1alpha.Endpoint{
				{
					Ip:   "10.0.0.1",
					Port: 80,
				},
			},
		}

		// Store the endpoints in entity store first
		var epEntity entityserver_v1alpha.Entity
		epEntity.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, epID.String()),
			eps.Encode).Attrs())
		_, err = eac.Put(ctx, &epEntity)
		r.NoError(err)

		// Delete the endpoint entity to simulate actual deletion
		_, err = eac.Delete(ctx, epID.String())
		r.NoError(err)

		// Verify endpoint is actually deleted
		_, err = eac.Get(ctx, epID.String())
		r.Error(err, "Expected endpoint to be deleted")

		// Simulate endpoint deletion event. The framework now carries the
		// pre-delete entity through (see pkg/entity/store.go and the etcd
		// WithPrevKV watch option), so the controller routes the event to
		// just the affected service via eps.Service rather than fanning
		// out to every Service entity.
		epsEntity := entity.New(
			entity.DBId, epID,
			eps.Encode,
		)
		event := controller.Event{
			Type:   controller.EventDeleted,
			Entity: epsEntity,
		}

		_, err = sc.UpdateEndpoints(ctx, event)
		r.NoError(err)

		// Both services should still be in the entity store; svc2 wasn't
		// touched, svc1 was reconciled with the now-empty endpoint set.
		_, err = eac.Get(ctx, svcID1.String())
		r.NoError(err)
		_, err = eac.Get(ctx, svcID2.String())
		r.NoError(err)
	})

	t.Run("handles service with nodeport", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create a service with nodeport
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				},
			},
		}

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("installs nodeport chain without service IP", func(t *testing.T) {
		// MIR-1032: a service with a NodePort must install the NodePort DNAT
		// chain on every node, including nodes that don't host the sandbox and
		// even when the service has no allocated cluster IP yet. Before the
		// fix, Create() returned early when srv.Ip was empty so the NodePort
		// rule was never installed on the coordinator.
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "rust-chat"},
			},
			Port: []network_v1alpha.Port{
				{
					Name:     "console",
					Port:     6669,
					NodePort: 30669,
				},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		epID := entity.Id("endpoints-" + svcID.String())
		eps := &network_v1alpha.Endpoints{
			ID:      epID,
			Service: svcID,
			Endpoint: []network_v1alpha.Endpoint{
				{Ip: "10.8.103.56", Port: 6669},
			},
		}

		var epRPC entityserver_v1alpha.Entity
		epRPC.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, epID.String()),
			eps.Encode).Attrs())
		_, err = eac.Put(ctx, &epRPC)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)
		meta := &entity.Meta{Entity: svcEntity, Revision: 1}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		npChain := sc.nodeportChain(30669, "tcp")
		epIP := netip.MustParseAddr("10.8.103.56")
		expectedEpChain := sc.endpointChain(epIP, 6669, "tcp")

		sc.mu.Lock()
		got, ok := sc.chainEndpoints[npChain]
		sc.mu.Unlock()

		r.True(ok, "NodePort chain should be tracked even without a service IP")
		r.Equal([]string{expectedEpChain}, got, "NodePort chain should DNAT to the endpoint chain")
	})

	t.Run("rebuilds nodeport vmap when endpoints change", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "rust-chat"},
			},
			Port: []network_v1alpha.Port{
				{
					Name:     "console",
					Port:     6669,
					NodePort: 30669,
				},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		epID := entity.Id("endpoints-" + svcID.String())
		eps := &network_v1alpha.Endpoints{
			ID:      epID,
			Service: svcID,
			Endpoint: []network_v1alpha.Endpoint{
				{Ip: "10.8.103.56", Port: 6669},
			},
		}

		var epRPC entityserver_v1alpha.Entity
		epRPC.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, epID.String()),
			eps.Encode).Attrs())
		putRes, err := eac.Put(ctx, &epRPC)
		r.NoError(err)
		epRevision := putRes.Revision()

		svcEntity := entity.New(svc.Encode)
		meta := &entity.Meta{Entity: svcEntity, Revision: 1}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		npChain := sc.nodeportChain(30669, "tcp")

		sc.mu.Lock()
		first := append([]string{}, sc.chainEndpoints[npChain]...)
		sc.mu.Unlock()
		r.Len(first, 1, "expected one endpoint chain after first reconcile")

		eps.Endpoint = append(eps.Endpoint, network_v1alpha.Endpoint{Ip: "10.8.104.42", Port: 6669})
		updated := entity.New(
			entity.Keyword(entity.Ident, epID.String()),
			eps.Encode)
		_, err = eac.Replace(ctx, updated.Attrs(), epRevision)
		r.NoError(err)

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		sc.mu.Lock()
		second := append([]string{}, sc.chainEndpoints[npChain]...)
		sc.mu.Unlock()

		r.Len(second, 2, "expected NodePort chain to track both endpoint chains after rebuild")
		r.NotEqual(first, second, "NodePort chain endpoint set should change when endpoints change")
	})

	t.Run("creates per-port endpoint chains for multi-port services", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		// Create a multi-port service (like tcp-echo: HTTP health + TCP echo)
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Ip: []string{"10.10.0.1"},
			Match: types.Labels{
				types.Label{Key: "app", Value: "tcp-echo"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "health",
					Port: 3000,
					Type: "http",
				},
				{
					Name:     "echo",
					Port:     7000,
					Type:     "tcp",
					NodePort: 7000,
				},
			},
		}

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		// Create endpoints for the service (simulating a sandbox at 10.8.0.5)
		epID := entity.Id("endpoints-" + svcID.String())
		eps := &network_v1alpha.Endpoints{
			ID:      epID,
			Service: svcID,
			Endpoint: []network_v1alpha.Endpoint{
				{
					Ip:   "10.8.0.5",
					Port: 3000,
				},
			},
		}

		var epRPC entityserver_v1alpha.Entity
		epRPC.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, epID.String()),
			eps.Encode).Attrs())
		_, err = eac.Put(ctx, &epRPC)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)
		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		// Create should succeed and set up per-port endpoint chains.
		// Before the fix, all endpoint chains would DNAT to port 3000 (srv.Port[0]).
		// After the fix, port 3000's chain DNATs to :3000 and port 7000's chain DNATs to :7000.
		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Verify per-port endpoint chains via the internal chainEndpoints map.
		// Each service port gets its own service chain, each with its own endpoint chains.
		svcIP := netip.MustParseAddr("10.10.0.1")
		epIP := netip.MustParseAddr("10.8.0.5")

		chain3000 := sc.serviceChain(svcIP, 3000, "tcp")
		chain7000 := sc.serviceChain(svcIP, 7000, "tcp")
		r.NotEqual(chain3000, chain7000, "service chains for different ports should differ")

		sc.mu.Lock()
		eps3000, ok3000 := sc.chainEndpoints[chain3000]
		eps7000, ok7000 := sc.chainEndpoints[chain7000]
		sc.mu.Unlock()

		r.True(ok3000, "should have endpoint chains for port 3000")
		r.True(ok7000, "should have endpoint chains for port 7000")
		r.Len(eps3000, 1, "port 3000 should have one endpoint chain")
		r.Len(eps7000, 1, "port 7000 should have one endpoint chain")

		// The endpoint chains should differ because they target different ports
		ep3000 := sc.endpointChain(epIP, 3000, "tcp")
		ep7000 := sc.endpointChain(epIP, 7000, "tcp")
		r.Equal(ep3000, eps3000[0], "port 3000 endpoint chain should DNAT to :3000")
		r.Equal(ep7000, eps7000[0], "port 7000 endpoint chain should DNAT to :7000")
		r.NotEqual(eps3000[0], eps7000[0], "endpoint chains for different ports must differ")
	})

	t.Run("uses correct protocol in nft rules for UDP ports", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Ip: []string{"10.10.0.1"},
			Match: types.Labels{
				types.Label{Key: "app", Value: "dns"},
			},
			Port: []network_v1alpha.Port{
				{
					Name:     "dns-udp",
					Port:     53,
					Type:     "tcp",
					Protocol: network_v1alpha.UDP,
				},
				{
					Name:     "dns-tcp",
					Port:     53,
					Type:     "tcp",
					Protocol: network_v1alpha.TCP,
				},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		eps := &network_v1alpha.Endpoints{
			ID:      entity.Id("endpoints-" + svcID.String()),
			Service: svcID,
			Endpoint: []network_v1alpha.Endpoint{
				{Ip: "10.8.0.5", Port: 53},
			},
		}

		var epRPC entityserver_v1alpha.Entity
		epRPC.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, eps.ID.String()),
			eps.Encode).Attrs())
		_, err = eac.Put(ctx, &epRPC)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)
		meta := &entity.Meta{Entity: svcEntity, Revision: 1}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// UDP and TCP on port 53 should produce different service chains
		svcIP := netip.MustParseAddr("10.10.0.1")
		chainUDP := sc.serviceChain(svcIP, 53, "udp")
		chainTCP := sc.serviceChain(svcIP, 53, "tcp")
		r.NotEqual(chainUDP, chainTCP, "same port with different protocols should have different chains")

		sc.mu.Lock()
		_, okUDP := sc.chainEndpoints[chainUDP]
		_, okTCP := sc.chainEndpoints[chainTCP]
		sc.mu.Unlock()

		r.True(okUDP, "should have endpoint chains for UDP port 53")
		r.True(okTCP, "should have endpoint chains for TCP port 53")

		// Endpoint chains should also differ by protocol
		epIP := netip.MustParseAddr("10.8.0.5")
		epUDP := sc.endpointChain(epIP, 53, "udp")
		epTCP := sc.endpointChain(epIP, 53, "tcp")
		r.NotEqual(epUDP, epTCP, "endpoint chains for same port different protocol must differ")
	})

	t.Run("handles service IP allocation", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		eac := testDeps.EAC

		sc, err := newServiceController(testDeps)
		r.NoError(err)

		// Set service prefixes for IP allocation
		servicePrefixes := []netip.Prefix{
			netip.MustParsePrefix("10.96.0.0/16"),
		}
		sc.ServicePrefixes = servicePrefixes

		err = sc.Init(ctx)
		r.NoError(err)

		// Create and start the IP allocator
		ipalloc := ipalloc.NewAllocator(sc.Log, servicePrefixes)

		// Start watching for services in background
		go func() {
			err := ipalloc.Watch(ctx, eac)
			if err != nil && ctx.Err() == nil {
				r.NoError(err)
			}
		}()

		// Give the watcher a moment to start
		time.Sleep(50 * time.Millisecond)

		// Create a service
		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID: svcID,
			Match: types.Labels{
				types.Label{Key: "app", Value: "nginx"},
			},
			Port: []network_v1alpha.Port{
				{
					Name: "http",
					Port: 80,
				},
			},
		}

		// Store the service in entity store first
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		svcEntity := entity.New(svc.Encode)

		meta := &entity.Meta{
			Entity:   svcEntity,
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Give ipalloc time to assign an IP
		time.Sleep(100 * time.Millisecond)

		// Re-read the service from entity storage to get the allocated IP
		result, err := eac.Get(ctx, svcID.String())
		r.NoError(err)

		var updatedSvc network_v1alpha.Service
		updatedSvc.Decode(result.Entity().Entity())

		// Verify service was allocated an IP
		r.NotEmpty(updatedSvc.Ip)
		r.Greater(len(updatedSvc.Ip), 0)
		ip, err := netip.ParseAddr(updatedSvc.Ip[0])
		r.NoError(err)
		r.True(sc.ServicePrefixes[0].Contains(ip))
	})
}
