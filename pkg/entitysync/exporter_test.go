package entitysync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/uplink"
)

type sentMessage struct {
	typ     string
	payload any
}

type fakeLink struct {
	mu        sync.Mutex
	offers    []uplink.CapabilityOffer
	handlers  map[string]uplink.MessageHandler
	sessions  []func(context.Context, uplink.Session)
	messages  []sentMessage
	onSend    func(string, any)
	sendError func(string, any) error
}

type controlledWatchStore struct {
	*entity.MockStore
	watch chan clientv3.WatchResponse
}

type pageCall struct {
	cursor   string
	limit    int64
	revision int64
}

type recordingPageStore struct {
	*entity.MockStore
	mu    sync.Mutex
	calls []pageCall
	reads int
}

func (s *recordingPageStore) ListIndexPageAtRevision(
	ctx context.Context,
	attr entity.Attr,
	cursor string,
	limit, revision int64,
) (*entity.IndexPage, error) {
	s.mu.Lock()
	s.calls = append(s.calls, pageCall{cursor: cursor, limit: limit, revision: revision})
	s.mu.Unlock()
	return s.MockStore.ListIndexPageAtRevision(ctx, attr, cursor, limit, revision)
}

func (s *recordingPageStore) GetEntityAtRevision(
	ctx context.Context,
	id entity.Id,
	revision int64,
) (*entity.Entity, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	return s.MockStore.GetEntityAtRevision(ctx, id, revision)
}

func (s *recordingPageStore) observations() ([]pageCall, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pageCall(nil), s.calls...), s.reads
}

type staleIndexStore struct {
	*entity.MockStore
	stale entity.Id
}

func (s *staleIndexStore) ListIndexPageAtRevision(
	ctx context.Context,
	attr entity.Attr,
	cursor string,
	limit, revision int64,
) (*entity.IndexPage, error) {
	page, err := s.MockStore.ListIndexPageAtRevision(ctx, attr, cursor, limit, revision)
	if err == nil && cursor == "" {
		page.Ids = append(page.Ids, s.stale)
	}
	return page, err
}

func (s *controlledWatchStore) WatchIndex(context.Context, entity.Attr, int64) (clientv3.WatchChan, error) {
	return s.watch, nil
}

type sequencedWatchStore struct {
	*entity.MockStore
	mu       sync.Mutex
	watches  []chan clientv3.WatchResponse
	contexts []context.Context
	fromRevs []int64
}

func (s *sequencedWatchStore) WatchIndex(ctx context.Context, _ entity.Attr, fromRev int64) (clientv3.WatchChan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	watch := make(chan clientv3.WatchResponse)
	s.watches = append(s.watches, watch)
	s.contexts = append(s.contexts, ctx)
	s.fromRevs = append(s.fromRevs, fromRev)
	return watch, nil
}

func (s *sequencedWatchStore) watchRevisions() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.fromRevs...)
}

func (s *sequencedWatchStore) watchCalls() ([]chan clientv3.WatchResponse, []context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]chan clientv3.WatchResponse(nil), s.watches...), append([]context.Context(nil), s.contexts...)
}

func newFakeLink() *fakeLink {
	return &fakeLink{handlers: make(map[string]uplink.MessageHandler)}
}

func (f *fakeLink) OfferCapability(offer uplink.CapabilityOffer) {
	f.offers = append(f.offers, offer)
}

func (f *fakeLink) OnSession(fn func(context.Context, uplink.Session)) {
	f.sessions = append(f.sessions, fn)
}

func (f *fakeLink) Handle(typ string, handler uplink.MessageHandler) {
	f.handlers[typ] = handler
}

func (f *fakeLink) SendMessageBlocking(_ context.Context, typ string, payload any) error {
	f.mu.Lock()
	f.messages = append(f.messages, sentMessage{typ: typ, payload: payload})
	f.mu.Unlock()
	if f.sendError != nil {
		if err := f.sendError(typ, payload); err != nil {
			return err
		}
	}
	if f.onSend != nil {
		f.onSend(typ, payload)
	}
	return nil
}

func (f *fakeLink) sent() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.messages...)
}

func testExporter(store entity.Store) *Exporter {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tenant := NewExporter(log, store, core_v1alpha.CloudExportContract)
	tenant.sourceEpoch = "mock-source-epoch"
	return tenant
}

func TestRegisterOffersGeneratedContract(t *testing.T) {
	tenant := testExporter(entity.NewMockStore())
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))

	require.Len(t, link.offers, 1)
	require.Equal(t, uplink.CapabilityEntitySync, link.offers[0].Name)
	require.Equal(t, []uint{Version1}, link.offers[0].Versions)
	var offer Offer
	require.NoError(t, json.Unmarshal(link.offers[0].Offer, &offer))
	require.Equal(t, []string{core_v1alpha.CloudExportContract.Digest()}, offer.ExportSchemas)
	require.Equal(t, "mock-source-epoch", offer.SourceEpoch)
	require.Contains(t, link.handlers, TypeAck)
	require.Len(t, link.sessions, 1)
}

func TestSessionWaitsForSourcePreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{})
	store := entity.NewMockStore()
	tenant := NewExporter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		core_v1alpha.CloudExportContract,
		WithStartGate(ready),
	)
	tenant.sourceEpoch = "mock-source-epoch"
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeSnapshotComplete {
			return
		}
		complete := payload.(SnapshotComplete)
		tenant.mu.Lock()
		active := tenant.active
		tenant.mu.Unlock()
		active.deliver(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
	}
	config, err := json.Marshal(Config{
		ExportSchema:     core_v1alpha.CloudExportContract.Digest(),
		SourceEpoch:      "mock-source-epoch",
		SnapshotRequired: true,
	})
	require.NoError(t, err)
	session := uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}}

	go tenant.runSession(ctx, session, link)
	require.Never(t, func() bool { return len(link.sent()) != 0 }, 50*time.Millisecond, 5*time.Millisecond)

	close(ready)
	require.Eventually(t, func() bool {
		for _, message := range link.sent() {
			if message.typ == TypeSnapshotComplete {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

func TestSnapshotFiltersEntities(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	marker := core_v1alpha.CloudExportContract.MarkerID()

	app := entity.New(
		entity.Ref(entity.DBId, "app/web"),
		(&core_v1alpha.App{ID: "app/web", Project: "project/secret"}).Encode(),
		(&core_v1alpha.Metadata{Name: "web"}).Encode(),
		entity.Bool(marker, true),
	)
	app.SetRevision(4)
	store.AddEntity(app.Id(), app)
	deployment := entity.New(
		entity.Ref(entity.DBId, "deployment/d1"),
		(&core_v1alpha.Deployment{ID: "deployment/d1", AppName: "web", ErrorMessage: "token=super-secret"}).Encode(),
		entity.Bool(marker, true),
	)
	deployment.SetRevision(7)
	store.AddEntity(deployment.Id(), deployment)

	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeSnapshotComplete {
			return
		}
		complete := payload.(SnapshotComplete)
		s.deliver(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
	}

	head, _, err := s.snapshot(ctx, link)
	require.NoError(t, err)
	require.Equal(t, int64(7), head)
	require.Equal(t, []int64{8}, store.WatchFromRevsCopy(), "watch must open before snapshot delivery at head+1")

	messages := link.sent()
	require.Equal(t, []string{TypeSnapshotBegin, TypeSnapshotBatch, TypeSnapshotComplete}, []string{
		messages[0].typ, messages[1].typ, messages[2].typ,
	})
	batch := messages[1].payload.(SnapshotBatch)
	require.Equal(t, core_v1alpha.CloudExportContract.Digest(), batch.ExportSchemaDigest)
	require.Len(t, batch.Entities, 2)
	require.Equal(t, entity.Id("app/web"), batch.Entities[0].Id())
	require.Equal(t, entity.Id("deployment/d1"), batch.Entities[1].Id())
	require.Equal(t, []entity.Attr{
		entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
	}, batch.Entities[0].GetAll(entity.EntityKind), "composed runtime entities have one unambiguous wire kind")

	raw, err := json.Marshal(batch.Entities)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "project/secret")
	require.NotContains(t, string(raw), "super-secret")
	require.NotContains(t, string(raw), string(marker))
}

func TestSnapshotReadsAndSendsOnePinnedPageAtATime(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &recordingPageStore{MockStore: entity.NewMockStore()}
	marker := core_v1alpha.CloudExportContract.MarkerID()

	for i := range 205 {
		id := entity.Id(fmt.Sprintf("deployment/%03d", i))
		deployment := entity.New(
			entity.Ref(entity.DBId, id),
			(&core_v1alpha.Deployment{ID: id, AppName: "marmalade"}).Encode(),
			entity.Bool(marker, true),
		)
		deployment.SetRevision(int64(i + 1))
		store.AddEntity(id, deployment)
	}

	exporter := testExporter(store)
	stream := &stream{exporter: exporter, ctx: ctx, waiters: make(map[string]chan Ack)}
	exporter.active = stream
	link := newFakeLink()
	var readsAtSend []int
	link.onSend = func(typ string, payload any) {
		switch typ {
		case TypeSnapshotBatch:
			_, reads := store.observations()
			readsAtSend = append(readsAtSend, reads)
		case TypeSnapshotComplete:
			complete := payload.(SnapshotComplete)
			stream.deliver(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
		}
	}

	head, _, err := stream.snapshot(ctx, link)
	require.NoError(t, err)
	require.Equal(t, int64(205), head)
	require.Equal(t, []int{100, 200, 205}, readsAtSend,
		"the exporter must send and discard each page before reading the next one")

	calls, _ := store.observations()
	require.Len(t, calls, 3)
	require.Equal(t, int64(0), calls[0].revision, "the first page establishes the snapshot head")
	for _, call := range calls {
		require.Equal(t, int64(snapshotBatchSize), call.limit)
	}
	for _, call := range calls[1:] {
		require.Equal(t, head, call.revision, "continuation pages must stay pinned to the snapshot head")
	}
}

func TestSnapshotSkipsStaleMarkerIndexEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &staleIndexStore{MockStore: entity.NewMockStore(), stale: "app/missing"}
	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ == TypeSnapshotComplete {
			complete := payload.(SnapshotComplete)
			s.deliver(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
		}
	}

	cursor, _, err := s.snapshot(ctx, link)
	require.NoError(t, err)
	require.Zero(t, cursor)
	require.Equal(t, []string{TypeSnapshotBegin, TypeSnapshotComplete}, []string{
		link.sent()[0].typ, link.sent()[1].typ,
	})
}

func TestLiveChangePreemptsArchiveSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &controlledWatchStore{MockStore: entity.NewMockStore(), watch: make(chan clientv3.WatchResponse, 1)}
	marker := core_v1alpha.CloudExportContract.MarkerID()
	deployment := entity.New(
		entity.Ref(entity.DBId, "deployment/d1"),
		(&core_v1alpha.Deployment{ID: "deployment/d1", AppName: "web", Outcome: "in_progress"}).Encode(),
		entity.Bool(marker, true),
	)
	deployment.SetRevision(7)
	store.AddEntity(deployment.Id(), deployment)

	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		switch typ {
		case TypeSnapshotBegin:
			changed := entity.New(deployment.Attrs())
			changed.SetRevision(8)
			changed.Set(entity.String(core_v1alpha.DeploymentOutcomeId, "succeeded"))
			store.AddEntity(changed.Id(), changed)
			store.watch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{{
					Type: mvccpb.PUT,
					Kv:   &mvccpb.KeyValue{Value: []byte(changed.Id()), ModRevision: 8},
				}},
			}
		case TypeChangeBatch:
			batch := payload.(ChangeBatch)
			s.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
		case TypeSnapshotComplete:
			complete := payload.(SnapshotComplete)
			s.deliver(Ack{MessageID: complete.MessageID, Cursor: 8})
		}
	}

	cursor, _, err := s.snapshot(ctx, link)
	require.NoError(t, err)
	require.Equal(t, int64(8), cursor)
	messages := link.sent()
	require.Equal(t, []string{
		TypeSnapshotBegin, TypeSnapshotBatch, TypeChangeBatch, TypeSnapshotComplete,
	}, []string{messages[0].typ, messages[1].typ, messages[2].typ, messages[3].typ})
	change := messages[2].payload.(ChangeBatch)
	require.Equal(t, core_v1alpha.CloudExportContract.Digest(), change.ExportSchemaDigest)
	require.Equal(t, int64(8), change.ToRevision)
	require.Equal(t, "succeeded", entity.MustGet(change.Changes[0].Entity, core_v1alpha.DeploymentOutcomeId).Value.String())
}

func TestDeleteCarriesFilteredLastEntityState(t *testing.T) {
	store := entity.NewMockStore()
	marker := core_v1alpha.CloudExportContract.MarkerID()
	app := entity.New(
		entity.Ref(entity.DBId, "app/web"),
		(&core_v1alpha.App{ID: "app/web", Project: "project/secret"}).Encode(),
		(&core_v1alpha.Metadata{Name: "web"}).Encode(),
		entity.Bool(marker, true),
	)
	app.SetRevision(4)
	store.AddEntity(app.Id(), app)
	s := &stream{exporter: testExporter(store), ctx: t.Context()}

	changes, err := s.changes([]*clientv3.Event{{
		Type: mvccpb.DELETE,
		Kv:   &mvccpb.KeyValue{ModRevision: 5},
		PrevKv: &mvccpb.KeyValue{
			Value: []byte(app.Id()), ModRevision: 4,
		},
	}}, 5)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, ChangeDelete, changes[0].Op)
	require.Equal(t, int64(5), changes[0].Revision)
	require.Equal(t, entity.Id("app/web"), changes[0].EntityID)
	require.NotNil(t, changes[0].Entity)
	raw, err := json.Marshal(changes[0].Entity)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "project/secret")
	require.NotContains(t, string(raw), string(marker))
}

func TestDeleteSkipsStaleIndexEntryWithoutPriorEntityState(t *testing.T) {
	stream := &stream{exporter: testExporter(entity.NewMockStore()), ctx: t.Context()}

	changes, err := stream.changes([]*clientv3.Event{{
		Type: mvccpb.DELETE,
		Kv:   &mvccpb.KeyValue{ModRevision: 5},
		PrevKv: &mvccpb.KeyValue{
			Value: []byte("deployment/missing"), ModRevision: 4,
		},
	}}, 5)

	require.NoError(t, err)
	require.Empty(t, changes)
}

func TestSessionResumesWatchAfterAcknowledgedCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := entity.NewMockStore()
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(50)
	store.AddEntity(head.Id(), head)
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))

	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       42,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})
	require.Eventually(t, func() bool {
		return len(store.WatchFromRevsCopy()) == 1
	}, time.Second, time.Millisecond)
	require.Equal(t, []int64{43}, store.WatchFromRevsCopy())
	cancel()
}

func TestFailedSnapshotPreservesAcknowledgedCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(10)
	store.AddEntity(head.Id(), head)
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))
	failed := false
	link.sendError = func(typ string, _ any) error {
		if typ == TypeSnapshotBegin && !failed {
			failed = true
			return context.DeadlineExceeded
		}
		return nil
	}
	link.onSend = func(typ string, payload any) {
		if typ == TypeSnapshotComplete {
			complete := payload.(SnapshotComplete)
			raw, _ := json.Marshal(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
			_ = link.handlers[TypeAck](ctx, raw)
		}
	}
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       42,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})

	require.Eventually(t, func() bool {
		return len(store.WatchFromRevsCopy()) >= 2
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, []int64{11, 11}, store.WatchFromRevsCopy()[:2],
		"snapshot failure discarded the acknowledged cursor and resumed from revision 1")
}

func TestSessionRetriesClosedWatchAndCancelsOldWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &sequencedWatchStore{MockStore: entity.NewMockStore()}
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(50)
	store.AddEntity(head.Id(), head)
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       42,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})

	require.Eventually(t, func() bool {
		watches, _ := store.watchCalls()
		return len(watches) == 1
	}, time.Second, time.Millisecond)
	watches, contexts := store.watchCalls()
	close(watches[0])
	require.Eventually(t, func() bool {
		watches, _ := store.watchCalls()
		return len(watches) == 2
	}, 2*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, contexts[0].Err(), context.Canceled)
}

func TestSessionSnapshotsImmediatelyAfterCompaction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &sequencedWatchStore{MockStore: entity.NewMockStore()}
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(50)
	store.AddEntity(head.Id(), head)
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))
	link.onSend = func(typ string, payload any) {
		if typ == TypeSnapshotComplete {
			complete := payload.(SnapshotComplete)
			raw, _ := json.Marshal(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
			_ = link.handlers[TypeAck](ctx, raw)
		}
	}
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       42,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})

	require.Eventually(t, func() bool {
		watches, _ := store.watchCalls()
		return len(watches) == 1
	}, time.Second, time.Millisecond)
	watches, contexts := store.watchCalls()
	watches[0] <- clientv3.WatchResponse{CompactRevision: 45}
	require.Eventually(t, func() bool {
		for _, message := range link.sent() {
			if message.typ == TypeSnapshotComplete {
				return true
			}
		}
		return false
	}, 2*time.Second, time.Millisecond)
	require.ErrorIs(t, contexts[0].Err(), context.Canceled)
	require.Equal(t, []int64{43, 51}, store.watchRevisions())
}

func TestSessionResnapshotsAfterCloudControlledInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &sequencedWatchStore{MockStore: entity.NewMockStore()}
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(10)
	store.AddEntity(head.Id(), head)
	exporter := testExporter(store)
	exporter.minimumResnapshotInterval = time.Second
	link := newFakeLink()
	require.NoError(t, exporter.Register(t.Context(), link))
	link.onSend = func(typ string, payload any) {
		var ack Ack
		switch typ {
		case TypeChangeBatch:
			batch := payload.(ChangeBatch)
			ack = Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision}
		case TypeSnapshotComplete:
			complete := payload.(SnapshotComplete)
			ack = Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead}
		default:
			return
		}
		raw, _ := json.Marshal(ack)
		_ = link.handlers[TypeAck](ctx, raw)
	}
	config, err := json.Marshal(Config{
		ExportSchema:              core_v1alpha.CloudExportContract.Digest(),
		Cursor:                    10,
		SourceEpoch:               "mock-source-epoch",
		ResnapshotAfterSeconds:    1,
		ResnapshotIntervalSeconds: 1,
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})

	require.Eventually(t, func() bool {
		watches, _ := store.watchCalls()
		return len(watches) == 1
	}, time.Second, time.Millisecond)
	head = entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(11)
	store.AddEntity(head.Id(), head)
	watches, _ := store.watchCalls()
	go func() {
		watches[0] <- clientv3.WatchResponse{Header: etcdserverpb.ResponseHeader{Revision: 11}}
	}()

	require.Eventually(t, func() bool {
		for _, message := range link.sent() {
			if message.typ == TypeChangeBatch {
				return message.payload.(ChangeBatch).ToRevision == 11
			}
		}
		return false
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		for _, message := range link.sent() {
			if message.typ == TypeSnapshotComplete {
				return message.payload.(SnapshotComplete).SourceHead == 11
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []int64{11, 12}, store.watchRevisions())

	messages := link.sent()
	changeIndex, snapshotIndex := -1, -1
	for index, message := range messages {
		switch message.typ {
		case TypeChangeBatch:
			changeIndex = index
		case TypeSnapshotBegin:
			snapshotIndex = index
		}
	}
	require.NotEqual(t, -1, changeIndex)
	require.NotEqual(t, -1, snapshotIndex)
	require.Less(t, changeIndex, snapshotIndex, "live changes continue until the anti-entropy deadline")
}

func TestResnapshotScheduleAppliesRuntimeSafetyFloor(t *testing.T) {
	exporter := testExporter(entity.NewMockStore())
	exporter.minimumResnapshotInterval = 2 * time.Hour
	started := time.Now()

	interval, deadline := exporter.resnapshotSchedule(Config{
		ResnapshotAfterSeconds:    10,
		ResnapshotIntervalSeconds: 60,
	})
	require.Equal(t, 2*time.Hour, interval)
	require.WithinDuration(t, started.Add(10*time.Second), deadline, 50*time.Millisecond)

	interval, deadline = exporter.resnapshotSchedule(Config{})
	require.Zero(t, interval)
	require.True(t, deadline.IsZero(), "zero disables periodic anti-entropy")
}

func TestSessionResumesZeroCursorAfterCompletedEmptySnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       0,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})
	require.Eventually(t, func() bool {
		return len(store.WatchFromRevsCopy()) == 1
	}, time.Second, time.Millisecond)
	require.Equal(t, []int64{1}, store.WatchFromRevsCopy())
	require.Empty(t, link.sent(), "cloud explicitly confirmed that the empty snapshot was complete")
}

func TestSessionSnapshotsWhenCursorIsAheadOfSource(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	head := entity.New(entity.Ref(entity.DBId, "unexported/head"))
	head.SetRevision(10)
	store.AddEntity(head.Id(), head)
	tenant := testExporter(store)
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))
	link.onSend = func(typ string, payload any) {
		if typ != TypeSnapshotComplete {
			return
		}
		complete := payload.(SnapshotComplete)
		raw, _ := json.Marshal(Ack{MessageID: complete.MessageID, Cursor: complete.SourceHead})
		_ = link.handlers[TypeAck](ctx, raw)
	}
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		Cursor:       42,
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)
	link.sessions[0](ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}})
	require.Eventually(t, func() bool {
		return len(link.sent()) >= 2
	}, time.Second, time.Millisecond)
	require.Equal(t, []int64{11}, store.WatchFromRevsCopy(), "a regressed source snapshots instead of watching beyond its head")
}

// addMarkedDeployment is a test helper that stores a marker-tagged deployment
// at a pinned revision so its watch event can be resolved by GetEntityAtRevision.
func addMarkedDeployment(store *entity.MockStore, id entity.Id, rev int64) {
	marker := core_v1alpha.CloudExportContract.MarkerID()
	e := entity.New(
		entity.Ref(entity.DBId, id),
		(&core_v1alpha.Deployment{ID: id, AppName: "app"}).Encode(),
		entity.Bool(marker, true),
	)
	e.SetRevision(rev)
	store.AddEntity(id, e)
}

func putEvent(id entity.Id, rev int64) *clientv3.Event {
	return &clientv3.Event{
		Type: mvccpb.PUT,
		Kv:   &mvccpb.KeyValue{Value: []byte(id), ModRevision: rev},
	}
}

// deleteEvent mirrors the shape etcd emits for a marker-index delete with
// WithPrevKV: the deleted entity id is carried on PrevKv (and on Kv for the
// marker key), and Kv.ModRevision is the deletion's revision.
func deleteEvent(id entity.Id, rev, prevRev int64) *clientv3.Event {
	return &clientv3.Event{
		Type:   mvccpb.DELETE,
		Kv:     &mvccpb.KeyValue{ModRevision: rev},
		PrevKv: &mvccpb.KeyValue{Value: []byte(id), ModRevision: prevRev},
	}
}

// TestSendWatchResponseUsesLastEventRevisionWhenHeaderExceedsEvents asserts the
// fix for the catch-up data-loss bug: when etcd's catch-up path stamps
// Header.Revision with the store head (above the last delivered event), the
// cursor and ToRevision must track the last event's ModRevision, not the
// inflated header.
func TestSendWatchResponseUsesLastEventRevisionWhenHeaderExceedsEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	addMarkedDeployment(store, "deployment/a", 5000)
	addMarkedDeployment(store, "deployment/b", 6000)

	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeChangeBatch {
			return
		}
		batch := payload.(ChangeBatch)
		s.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
	}

	// Catch-up shape observed from a real etcd: Header.Revision is the live
	// store head (12000), far above the last event's ModRevision (6000).
	response := clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 12000},
		Events: []*clientv3.Event{
			putEvent("deployment/a", 5000),
			putEvent("deployment/b", 6000),
		},
	}

	cursor, err := s.sendWatchResponse(link, response, 4999)
	require.NoError(t, err)
	require.Equal(t, int64(6000), cursor,
		"cursor must advance to the last delivered event, not the store head")

	messages := link.sent()
	require.Len(t, messages, 1)
	batch := messages[0].payload.(ChangeBatch)
	require.Equal(t, int64(5000), batch.FromRevision)
	require.Equal(t, int64(6000), batch.ToRevision,
		"ToRevision must be the last event's ModRevision, not Header.Revision")
	require.Len(t, batch.Changes, 2)
	require.Equal(t, []entity.Id{"deployment/a", "deployment/b"},
		[]entity.Id{batch.Changes[0].EntityID, batch.Changes[1].EntityID})
}

// TestChunkedCatchUpDeliversAllChanges reproduces the multi-chunk catch-up
// scenario end-to-end through tail: a first chunk carrying events below the
// store head must not advance the cursor so far that the second chunk is
// silently dropped. Both chunks (and every entity) must be delivered, and the
// cursor must end at the last delivered event across all chunks.
func TestChunkedCatchUpDeliversAllChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	addMarkedDeployment(store, "deployment/a", 5000)
	addMarkedDeployment(store, "deployment/b", 6000)
	addMarkedDeployment(store, "deployment/c", 6001)
	addMarkedDeployment(store, "deployment/d", 7000)

	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	var mu sync.Mutex
	var batches []ChangeBatch
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeChangeBatch {
			return
		}
		batch := payload.(ChangeBatch)
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		s.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
	}

	watcher := make(chan clientv3.WatchResponse, 4)
	// Two catch-up chunks, both stamped with the same store head (12000), as
	// observed from a real etcd on the unsynced catch-up path; each chunk's
	// last event ModRevision (6000, 7000) is well below the header.
	watcher <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 12000},
		Events: []*clientv3.Event{putEvent("deployment/a", 5000), putEvent("deployment/b", 6000)},
	}
	watcher <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 12000},
		Events: []*clientv3.Event{putEvent("deployment/c", 6001), putEvent("deployment/d", 7000)},
	}

	type tailResult struct {
		cursor int64
		err    error
	}
	done := make(chan tailResult, 1)
	go func() {
		cursor, err := s.tail(link, watcher, 4999, time.Time{})
		done <- tailResult{cursor: cursor, err: err}
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(batches) == 2
	}, time.Second, 5*time.Millisecond)
	close(watcher)

	var result tailResult
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("tail did not return after watcher closed")
	}
	require.ErrorContains(t, result.err, "watch closed")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, batches, 2, "both catch-up chunks must be delivered, not just the first")
	require.Equal(t, int64(5000), batches[0].FromRevision)
	require.Equal(t, int64(6000), batches[0].ToRevision,
		"chunk 1 ToRevision must be its last event's ModRevision, not Header.Revision")
	require.Equal(t, int64(6001), batches[1].FromRevision,
		"chunk 2 resumes immediately after chunk 1's last delivered event")
	require.Equal(t, int64(7000), batches[1].ToRevision,
		"chunk 2 ToRevision must be its last event's ModRevision, not Header.Revision")
	require.Equal(t, int64(7000), result.cursor,
		"cursor must end at the last delivered event across all chunks")

	delivered := map[entity.Id]bool{}
	for _, batch := range batches {
		for _, ch := range batch.Changes {
			delivered[ch.EntityID] = true
		}
	}
	require.True(t, delivered["deployment/a"])
	require.True(t, delivered["deployment/b"])
	require.True(t, delivered["deployment/c"], "chunk 2 event c must not be silently dropped")
	require.True(t, delivered["deployment/d"], "chunk 2 event d must not be silently dropped")
}

// TestSendWatchResponseUsesHeaderRevisionForEmptyProgressNotify guards that
// the fix does not regress the empty progress-notify path: a response with no
// events must still advance the cursor to Header.Revision to keep the resume
// watermark fresh during idle periods.
func TestSendWatchResponseUsesHeaderRevisionForEmptyProgressNotify(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeChangeBatch {
			return
		}
		batch := payload.(ChangeBatch)
		s.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
	}

	// Empty progress-notify response: cursor advances to Header.Revision to
	// keep the resume watermark fresh during idle periods.
	response := clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 777},
	}
	cursor, err := s.sendWatchResponse(link, response, 700)
	require.NoError(t, err)
	require.Equal(t, int64(777), cursor,
		"empty progress-notify advances the cursor to Header.Revision")
	require.Len(t, link.sent(), 1)
	require.Equal(t, int64(777), link.sent()[0].payload.(ChangeBatch).ToRevision)
}

// TestSendWatchResponseSkipsResponseWithNoProgressPastCursor guards that the
// to <= cursor guard still correctly drops a response that carries no new
// progress (the silent-skip is correct here because there is nothing to
// deliver), distinct from the catch-up bug where progress existed but was
// dropped.
func TestSendWatchResponseSkipsResponseWithNoProgressPastCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()

	// An empty progress-notify at or below the cursor must not emit a batch or
	// advance the cursor: it carries no new progress.
	response := clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 500},
	}
	cursor, err := s.sendWatchResponse(link, response, 500)
	require.NoError(t, err)
	require.Equal(t, int64(500), cursor,
		"a response with no progress past the cursor is a no-op")
	require.Empty(t, link.sent(), "no change batch is sent when nothing advances")
}

// TestCatchUpChunkEndingInDeleteUsesDeleteEventRevision (§2.7) guards G9: a
// catch-up chunk whose last event is a DELETE must set ToRevision to that
// DELETE event's ModRevision (not Header.Revision), and the generated
// Change{Op: ChangeDelete} must be included. The `changes` function reads
// the deleted entity at revision-1 via GetEntityAtRevision, so the prior
// deployment is stored at the pre-delete revision.
func TestCatchUpChunkEndingInDeleteUsesDeleteEventRevision(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := entity.NewMockStore()
	// deployment/a exists at 5000; the DELETE event at 5050 carries its id on
	// PrevKv. changes() reads it at revision 5049 via GetEntityAtRevision, so
	// keep the entity present (the mock's GetEntityAtResolution resolves it).
	addMarkedDeployment(store, "deployment/a", 5000)
	addMarkedDeployment(store, "deployment/b", 6000)

	tenant := testExporter(store)
	s := &stream{exporter: tenant, ctx: ctx, waiters: make(map[string]chan Ack)}
	tenant.active = s
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ != TypeChangeBatch {
			return
		}
		batch := payload.(ChangeBatch)
		s.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
	}

	// Catch-up shape: Header.Revision (12000) outruns the events; the chunk's
	// last event is a DELETE at 5050, so to must be 5050, not 12000.
	response := clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 12000},
		Events: []*clientv3.Event{
			putEvent("deployment/b", 6000),
			deleteEvent("deployment/a", 5050, 5000),
		},
	}
	cursor, err := s.sendWatchResponse(link, response, 4999)
	require.NoError(t, err)
	require.Equal(t, int64(5050), cursor,
		"chunk ending in a DELETE must set the cursor to the DELETE's ModRevision, not Header.Revision")

	messages := link.sent()
	require.Len(t, messages, 1)
	batch := messages[0].payload.(ChangeBatch)
	require.Equal(t, int64(5050), batch.ToRevision,
		"ToRevision must be the last (DELETE) event's ModRevision, not the store head")
	require.Len(t, batch.Changes, 2)
	require.Equal(t, ChangePut, batch.Changes[0].Op)
	require.Equal(t, entity.Id("deployment/b"), batch.Changes[0].EntityID)
	require.Equal(t, ChangeDelete, batch.Changes[1].Op)
	require.Equal(t, entity.Id("deployment/a"), batch.Changes[1].EntityID)
	require.Equal(t, int64(5050), batch.Changes[1].Revision)
}
