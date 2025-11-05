package testutils

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	apiserver "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/entityserver"
)

// InMemEntityServer provides an in-memory entity server for testing
type InMemEntityServer struct {
	Store  *entity.MockStore
	Server *entityserver.EntityServer
	EAC    *entityserver_v1alpha.EntityAccessClient
	Client *apiserver.Client
}

// NewInMemEntityServer creates a new in-memory entity server for testing
func NewInMemEntityServer(t *testing.T) (*InMemEntityServer, func()) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create mock store
	mockStore := entity.NewMockStore()

	// Apply schema to the store
	err := schema.Apply(ctx, mockStore)
	if err != nil {
		t.Fatalf("failed to apply schema: %v", err)
	}

	// Create entity server
	es, err := entityserver.NewEntityServer(log, mockStore)
	if err != nil {
		t.Fatalf("failed to create entity server: %v", err)
	}

	// Create entity access client with local transport
	localClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(es))
	eac := entityserver_v1alpha.NewEntityAccessClient(localClient)

	// Create the high-level entityserver client
	client := apiserver.NewClient(log, eac)

	cleanup := func() {
		// Nothing to clean up with local client
	}

	return &InMemEntityServer{
		Store:  mockStore,
		Server: es,
		EAC:    eac,
		Client: client,
	}, cleanup
}

// AddEntity adds an entity to the mock store
func (s *InMemEntityServer) AddEntity(ent *entity.Entity) {
	s.Store.AddEntity(ent.Id(), ent)
}

// GetEntity retrieves an entity from the mock store
func (s *InMemEntityServer) GetEntity(id entity.Id) *entity.Entity {
	ent, err := s.Store.GetEntity(context.Background(), id)
	if err != nil {
		return nil
	}
	return ent
}

// TestLogger creates a test logger that discards all output
func TestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestDebugLogger creates a test logger that outputs logs
func TestDebugLogger(t *testing.T) *slog.Logger {
	w := &testWriter{t: t}
	t.Cleanup(func() { w.closed.Store(true) })
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// testWriter wraps *testing.T to implement io.Writer
type testWriter struct {
	t      *testing.T
	closed atomic.Bool
}

func (tw *testWriter) Write(p []byte) (n int, err error) {
	if tw.closed.Load() {
		return len(p), nil
	}
	tw.t.Helper()
	tw.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// EtcdEntityServer provides an etcd-backed entity server for testing
// This enforces proper optimistic concurrency control unlike the in-memory version
type EtcdEntityServer struct {
	Store  *entity.EtcdStore
	Server *entityserver.EntityServer
	EAC    *entityserver_v1alpha.EntityAccessClient
	Client *apiserver.Client
	Prefix string
	etcd   *clientv3.Client
}

// NewEtcdEntityServer creates a new etcd-backed entity server for testing
// It connects to etcd:2379 and uses a random prefix for isolation
// This should be run with ./hack/dev-exec or similar to have etcd available
func NewEtcdEntityServer(t *testing.T) (*EtcdEntityServer, func()) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// Connect to etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"etcd:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to connect to etcd: %v", err)
	}

	// Generate random prefix for isolation
	prefix := "/" + idgen.Gen("test")

	// Create etcd-backed store
	store, err := entity.NewEtcdStore(ctx, log, etcdClient, prefix)
	if err != nil {
		etcdClient.Close()
		t.Fatalf("failed to create etcd store: %v", err)
	}

	// Apply schema to the store
	err = schema.Apply(ctx, store)
	if err != nil {
		etcdClient.Close()
		t.Fatalf("failed to apply schema: %v", err)
	}

	// Create entity server
	es, err := entityserver.NewEntityServer(log, store)
	if err != nil {
		etcdClient.Close()
		t.Fatalf("failed to create entity server: %v", err)
	}

	// Create entity access client with local transport
	localClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(es))
	eac := entityserver_v1alpha.NewEntityAccessClient(localClient)

	// Create the high-level entityserver client
	client := apiserver.NewClient(log, eac)

	cleanup := func() {
		// Delete all keys with this prefix
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := etcdClient.Delete(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			t.Logf("warning: failed to cleanup etcd prefix %s: %v", prefix, err)
		}
		etcdClient.Close()
	}

	return &EtcdEntityServer{
		Store:  store,
		Server: es,
		EAC:    eac,
		Client: client,
		Prefix: prefix,
		etcd:   etcdClient,
	}, cleanup
}
