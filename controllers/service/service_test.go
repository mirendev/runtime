package service

import (
	"context"
	"io"
	"net/netip"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/observability"
	build "miren.dev/runtime/pkg/buildkit"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

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

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var sc ServiceController
		err := reg.Populate(&sc)
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

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("creates endpoints when sandbox matches service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			sbC sandbox.SandboxController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
		r.NoError(err)

		err = reg.Populate(&sbC)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, &sbC)

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

		svcEntity := &entity.Entity{
			ID:    svcID,
			Attrs: svc.Encode(),
		}

		// Store the service in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode))
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
			Container: []compute.Container{
				{
					Name:  "nginx",
					Image: "docker.io/library/nginx:latest",
					Port: []compute.Port{
						{
							Port: 80,
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs := sb.Encode()
		attrs = append(attrs, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity := &entity.Entity{
			ID:    sbID,
			Attrs: attrs,
		}

		sbMeta := &entity.Meta{
			Entity:   sbEntity,
			Revision: 1,
		}

		err = sbC.Create(ctx, sb, sbMeta)
		r.NoError(err)

		// Give it a moment for endpoints to be created
		time.Sleep(200 * time.Millisecond)

		// Check that endpoints were created
		endpoints, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
		r.NoError(err)

		r.Greater(len(endpoints.Values()), 0, "Expected to find endpoints for service")

		found := false
		for _, epEntity := range endpoints.Values() {
			var ep network_v1alpha.Endpoints
			ep.Decode(epEntity.Entity())
			spew.Dump(ep)
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
		err = sbC.Delete(ctx, sbID)
		r.NoError(err)
	})

	t.Run("deletes endpoints when sandbox is deleted", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			sbC sandbox.SandboxController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
		r.NoError(err)

		err = reg.Populate(&sbC)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, &sbC)

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
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode))
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create a sandbox with matching labels and containers with ports
		sbID := entity.Id(sbName())
		sb := &compute.Sandbox{
			ID: sbID,
			Container: []compute.Container{
				{
					Name:  "nginx",
					Image: "docker.io/library/nginx:latest",
					Port: []compute.Port{
						{
							Port: 80,
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs := sb.Encode()
		attrs = append(attrs, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity := &entity.Entity{
			ID:    sbID,
			Attrs: attrs,
		}

		sbMeta := &entity.Meta{
			Entity:   sbEntity,
			Revision: 1,
		}

		err = sbC.Create(ctx, sb, sbMeta)
		r.NoError(err)

		// Give it a moment for endpoints to be created
		time.Sleep(100 * time.Millisecond)

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
		err = sbC.Delete(ctx, sbID)
		r.NoError(err)

		// Give it a moment for endpoints to be deleted
		time.Sleep(100 * time.Millisecond)

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

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
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

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Update service with additional port
		svc.Port = append(svc.Port, network_v1alpha.Port{
			Name: "https",
			Port: 443,
		})

		meta.Attrs = svc.Encode()
		meta.Revision = 2

		// Re-create the service with updated configuration
		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("handles multiple sandboxes matching one service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			sbC sandbox.SandboxController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
		r.NoError(err)

		err = reg.Populate(&sbC)
		r.NoError(err)

		err = sc.Init(ctx)
		r.NoError(err)

		defer checkClosed(t, &sbC)

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
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode))
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)

		// Create first sandbox with containers and ports
		sbID1 := entity.Id(sbName())
		sb1 := &compute.Sandbox{
			ID: sbID1,
			Container: []compute.Container{
				{
					Name:  "nginx",
					Image: "docker.io/library/nginx:latest",
					Port: []compute.Port{
						{
							Port: 80,
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs1 := sb1.Encode()
		attrs1 = append(attrs1, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity1 := &entity.Entity{
			ID:    sbID1,
			Attrs: attrs1,
		}

		sbMeta1 := &entity.Meta{
			Entity:   sbEntity1,
			Revision: 1,
		}

		err = sbC.Create(ctx, sb1, sbMeta1)
		r.NoError(err)

		// Create second sandbox with containers and ports
		sbID2 := entity.Id(sbName())
		sb2 := &compute.Sandbox{
			ID: sbID2,
			Container: []compute.Container{
				{
					Name:  "nginx",
					Image: "docker.io/library/nginx:latest",
					Port: []compute.Port{
						{
							Port: 80,
						},
					},
				},
			},
		}

		// Add metadata labels to entity attributes
		attrs2 := sb2.Encode()
		attrs2 = append(attrs2, entity.Label(core_v1alpha.MetadataLabelsId, "app", "nginx"))

		sbEntity2 := &entity.Entity{
			ID:    sbID2,
			Attrs: attrs2,
		}

		sbMeta2 := &entity.Meta{
			Entity:   sbEntity2,
			Revision: 1,
		}

		err = sbC.Create(ctx, sb2, sbMeta2)
		r.NoError(err)

		// Give it a moment for endpoints to be created
		time.Sleep(100 * time.Millisecond)

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
		err = sbC.Delete(ctx, sbID1)
		r.NoError(err)

		// Give it a moment for endpoints to be updated
		time.Sleep(100 * time.Millisecond)

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
		err = sbC.Delete(ctx, sbID2)
		r.NoError(err)
	})

	t.Run("handles endpoint updates", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
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
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode))
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
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

		event := controller.Event{
			Entity: &entity.Entity{
				ID:    epID,
				Attrs: eps.Encode(),
			},
		}

		// Update endpoints through the controller
		_, err = sc.UpdateEndpoints(ctx, event)
		r.NoError(err)
	})

	t.Run("handles service with nodeport", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var sc ServiceController
		err := reg.Populate(&sc)
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

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
			Revision: 1,
		}

		err = sc.Create(ctx, svc, meta)
		r.NoError(err)
	})

	t.Run("handles service IP allocation", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			sc  ServiceController
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&eac)
		r.NoError(err)

		err = reg.Populate(&sc)
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
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, svcID.String()),
			svc.Encode))
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		meta := &entity.Meta{
			Entity: &entity.Entity{
				ID:    svcID,
				Attrs: svc.Encode(),
			},
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
