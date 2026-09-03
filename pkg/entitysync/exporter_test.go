package entitysync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
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
	mu            sync.Mutex
	registrations []fakeCapabilityRegistration
	handlers      map[string]uplink.MessageHandler
	sessions      []func(context.Context, uplink.Session)
	messages      []sentMessage
	onSend        func(string, any)
	sendError     func(string, any) error
}

type fakeCapabilityRegistration struct {
	name     string
	versions []uint
	provide  uplink.CapabilityOfferFunc
}

type mutableEpochStore struct {
	*entity.MockStore
	mu    sync.Mutex
	epoch string
	err   error
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (s *mutableEpochStore) SourceEpoch(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch, s.err
}

func (s *mutableEpochStore) setEpoch(epoch string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch = epoch
	s.err = err
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

func (f *fakeLink) OfferCapabilityFunc(name string, versions []uint, provide uplink.CapabilityOfferFunc) {
	f.registrations = append(f.registrations, fakeCapabilityRegistration{
		name: name, versions: versions, provide: provide,
	})
}

func (f *fakeLink) sessionOffers(ctx context.Context) []uplink.CapabilityOffer {
	var offers []uplink.CapabilityOffer
	for _, registration := range f.registrations {
		offer, ok := registration.provide(ctx)
		if !ok {
			continue
		}
		offers = append(offers, uplink.CapabilityOffer{
			Name: registration.name, Versions: registration.versions, Offer: offer,
		})
	}
	return offers
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
	return NewExporter(log, store, core_v1alpha.CloudExportContract)
}

func TestRegisterOffersGeneratedContract(t *testing.T) {
	tenant := testExporter(entity.NewMockStore())
	link := newFakeLink()
	require.NoError(t, tenant.Register(t.Context(), link))

	offers := link.sessionOffers(t.Context())
	require.Len(t, offers, 1)
	require.Equal(t, uplink.CapabilityEntitySync, offers[0].Name)
	require.Equal(t, []uint{Version1}, offers[0].Versions)
	var offer Offer
	require.NoError(t, json.Unmarshal(offers[0].Offer, &offer))
	require.Equal(t, []string{core_v1alpha.CloudExportContract.Digest()}, offer.ExportSchemas)
	require.Equal(t, "mock-source-epoch", offer.SourceEpoch)
	require.Contains(t, link.handlers, TypeAck)
	require.Len(t, link.sessions, 1)
}

func TestCapabilityOfferRetriesEpochDiscoveryAndRefreshesAfterRestore(t *testing.T) {
	store := &mutableEpochStore{MockStore: entity.NewMockStore()}
	store.setEpoch("", errors.New("etcd unavailable"))
	exporter := testExporter(store)
	link := newFakeLink()
	require.NoError(t, exporter.Register(t.Context(), link))

	require.Empty(t, link.sessionOffers(t.Context()), "a failed epoch read omits only entity sync")
	for _, epoch := range []string{"epoch-before-restore", "epoch-after-restore"} {
		store.setEpoch(epoch, nil)
		offers := link.sessionOffers(t.Context())
		require.Len(t, offers, 1)
		var offer Offer
		require.NoError(t, json.Unmarshal(offers[0].Offer, &offer))
		require.Equal(t, epoch, offer.SourceEpoch)
	}
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

func TestSessionLogsWhileSourcePreparationRemainsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var logs lockedBuffer
	exporter := NewExporter(
		slog.New(slog.NewTextHandler(&logs, nil)),
		entity.NewMockStore(),
		core_v1alpha.CloudExportContract,
		WithStartGate(make(chan struct{})),
	)
	exporter.preparationLogInterval = 10 * time.Millisecond
	link := newFakeLink()
	config, err := json.Marshal(Config{
		ExportSchema: core_v1alpha.CloudExportContract.Digest(),
		SourceEpoch:  "mock-source-epoch",
	})
	require.NoError(t, err)

	go exporter.runSession(ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}}, link)
	require.Eventually(t, func() bool {
		return bytes.Contains([]byte(logs.String()), []byte("entity sync still waiting for source preparation"))
	}, time.Second, time.Millisecond)
}

func TestSessionValidatesEpochAfterSourcePreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var logs lockedBuffer
	store := &mutableEpochStore{MockStore: entity.NewMockStore(), epoch: "epoch-before-restore"}
	ready := make(chan struct{})
	exporter := NewExporter(
		slog.New(slog.NewTextHandler(&logs, nil)),
		store,
		core_v1alpha.CloudExportContract,
		WithStartGate(ready),
	)
	config, err := json.Marshal(Config{
		ExportSchema:     core_v1alpha.CloudExportContract.Digest(),
		SourceEpoch:      "epoch-before-restore",
		SnapshotRequired: true,
	})
	require.NoError(t, err)
	link := newFakeLink()

	go exporter.runSession(ctx, uplink.Session{Capabilities: []uplink.CapabilitySelection{{
		Name: uplink.CapabilityEntitySync, Version: Version1, Config: config,
	}}}, link)
	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "entity sync waiting for source preparation")
	}, time.Second, time.Millisecond)

	store.setEpoch("epoch-after-restore", nil)
	close(ready)
	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "cloud selected an unexpected entity source epoch")
	}, time.Second, time.Millisecond)
	require.Empty(t, link.sent(), "the restored source must not be sent under the negotiated old epoch")
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
	s := &stream{exporter: tenant, ctx: ctx, sourceEpoch: "mock-source-epoch", waiters: make(map[string]chan Ack)}
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
	stream := &stream{exporter: exporter, ctx: ctx, sourceEpoch: "mock-source-epoch", waiters: make(map[string]chan Ack)}
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
	s := &stream{exporter: tenant, ctx: ctx, sourceEpoch: "mock-source-epoch", waiters: make(map[string]chan Ack)}
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
	s := &stream{exporter: tenant, ctx: ctx, sourceEpoch: "mock-source-epoch", waiters: make(map[string]chan Ack)}
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

func TestWatchBatchStopsAtLastDeliveredEvent(t *testing.T) {
	store := entity.NewMockStore()
	marker := core_v1alpha.CloudExportContract.MarkerID()
	for _, id := range []entity.Id{"deployment/a", "deployment/b"} {
		store.AddEntity(id, entity.New(
			entity.Ref(entity.DBId, id),
			(&core_v1alpha.Deployment{ID: id, AppName: "web"}).Encode(),
			entity.Bool(marker, true),
		))
	}
	exporter := testExporter(store)
	stream := &stream{
		exporter: exporter, ctx: t.Context(), sourceEpoch: "mock-source-epoch",
		waiters: make(map[string]chan Ack),
	}
	exporter.active = stream
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ == TypeChangeBatch {
			batch := payload.(ChangeBatch)
			stream.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
		}
	}

	cursor, err := stream.sendWatchResponse(link, clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 100},
		Events: []*clientv3.Event{
			{Type: mvccpb.PUT, Kv: &mvccpb.KeyValue{Value: []byte("deployment/a"), ModRevision: 10}},
			{Type: mvccpb.PUT, Kv: &mvccpb.KeyValue{Value: []byte("deployment/b"), ModRevision: 20}},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, int64(20), cursor)
	require.Len(t, link.sent(), 1)
	require.Equal(t, int64(20), link.sent()[0].payload.(ChangeBatch).ToRevision)
}

func TestTailDeliversEveryCatchUpChunkBeforeStoreHead(t *testing.T) {
	store := entity.NewMockStore()
	marker := core_v1alpha.CloudExportContract.MarkerID()
	for _, id := range []entity.Id{"deployment/a", "deployment/b"} {
		store.AddEntity(id, entity.New(
			entity.Ref(entity.DBId, id),
			(&core_v1alpha.Deployment{ID: id, AppName: "web"}).Encode(),
			entity.Bool(marker, true),
		))
	}
	exporter := testExporter(store)
	stream := &stream{
		exporter: exporter, ctx: t.Context(), sourceEpoch: "mock-source-epoch",
		waiters: make(map[string]chan Ack),
	}
	exporter.active = stream
	link := newFakeLink()
	link.onSend = func(typ string, payload any) {
		if typ == TypeChangeBatch {
			batch := payload.(ChangeBatch)
			stream.deliver(Ack{MessageID: batch.MessageID, Cursor: batch.ToRevision})
		}
	}
	watcher := make(chan clientv3.WatchResponse, 2)
	watcher <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 100},
		Events: []*clientv3.Event{{
			Type: mvccpb.PUT, Kv: &mvccpb.KeyValue{Value: []byte("deployment/a"), ModRevision: 10},
		}},
	}
	watcher <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{Revision: 100},
		Events: []*clientv3.Event{{
			Type: mvccpb.PUT, Kv: &mvccpb.KeyValue{Value: []byte("deployment/b"), ModRevision: 20},
		}},
	}
	close(watcher)

	cursor, err := stream.tail(link, watcher, 0, time.Time{})
	require.ErrorContains(t, err, "watch closed")
	require.Equal(t, int64(20), cursor)
	messages := link.sent()
	require.Len(t, messages, 2)
	require.Equal(t, int64(10), messages[0].payload.(ChangeBatch).ToRevision)
	require.Equal(t, int64(20), messages[1].payload.(ChangeBatch).ToRevision)
}

func TestSendAndWaitTimesOutWithoutAcknowledgment(t *testing.T) {
	exporter := testExporter(entity.NewMockStore())
	exporter.ackTimeout = 10 * time.Millisecond
	stream := &stream{exporter: exporter, ctx: t.Context(), waiters: make(map[string]chan Ack)}

	_, err := stream.sendAndWait(newFakeLink(), TypeChangeBatch, struct{}{}, "message-1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "await entity.change.batch ack")
}

func TestSendAndWaitPreservesCancellationAndRejection(t *testing.T) {
	t.Run("session cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		exporter := testExporter(entity.NewMockStore())
		exporter.ackTimeout = time.Hour
		stream := &stream{exporter: exporter, ctx: ctx, waiters: make(map[string]chan Ack)}

		_, err := stream.sendAndWait(newFakeLink(), TypeChangeBatch, struct{}{}, "message-1")
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("cloud rejection", func(t *testing.T) {
		exporter := testExporter(entity.NewMockStore())
		exporter.ackTimeout = time.Hour
		stream := &stream{exporter: exporter, ctx: t.Context(), waiters: make(map[string]chan Ack)}
		link := newFakeLink()
		link.onSend = func(string, any) {
			stream.deliver(Ack{MessageID: "message-1", Error: "not accepted"})
		}

		_, err := stream.sendAndWait(link, TypeChangeBatch, struct{}{}, "message-1")
		require.ErrorContains(t, err, "cloud rejected entity.change.batch: not accepted")
	})
}

func TestRetryDoesNotWarnAfterSessionCancellation(t *testing.T) {
	var logs lockedBuffer
	exporter := NewExporter(
		slog.New(slog.NewTextHandler(&logs, nil)),
		entity.NewMockStore(),
		core_v1alpha.CloudExportContract,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stream := &stream{exporter: exporter, ctx: ctx}

	require.False(t, stream.retry(ctx, "start entity sync session", context.Canceled))
	require.NotContains(t, logs.String(), "start entity sync session")
}
