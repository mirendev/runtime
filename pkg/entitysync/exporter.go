// Package entitysync replicates schema-authorized runtime entities to Miren
// Cloud over the negotiated uplink.
package entitysync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	entityexport "miren.dev/runtime/pkg/entity/export"
	"miren.dev/runtime/pkg/uplink"
)

const snapshotBatchSize = 100
const maxLiveBatchesBetweenSnapshotBatches = 8
const sessionRetryDelay = time.Second
const defaultMinimumResnapshotInterval = time.Hour

var errCompacted = errors.New("entity sync cursor compacted")
var errResnapshotDue = errors.New("entity sync anti-entropy snapshot due")

type Link interface {
	OfferCapability(uplink.CapabilityOffer)
	OnSession(func(context.Context, uplink.Session))
	Handle(string, uplink.MessageHandler)
	SendMessageBlocking(context.Context, string, any) error
}

type Exporter struct {
	log                       *slog.Logger
	store                     entity.Store
	contract                  *entityexport.Contract
	sourceEpoch               string
	startGate                 <-chan struct{}
	minimumResnapshotInterval time.Duration

	mu     sync.Mutex
	active *stream
}

type stream struct {
	exporter *Exporter
	ctx      context.Context

	mu      sync.Mutex
	waiters map[string]chan Ack
}

type Option func(*Exporter)

// WithStartGate delays entity reads and transmission until source preparation
// has completed. Capability negotiation still happens immediately so other
// capabilities on the shared uplink are never gated on entity migration.
func WithStartGate(ready <-chan struct{}) Option {
	return func(exporter *Exporter) { exporter.startGate = ready }
}

func NewExporter(log *slog.Logger, store entity.Store, contract *entityexport.Contract, options ...Option) *Exporter {
	exporter := &Exporter{
		log: log, store: store, contract: contract,
		minimumResnapshotInterval: defaultMinimumResnapshotInterval,
	}
	for _, option := range options {
		option(exporter)
	}
	return exporter
}

func (t *Exporter) Register(ctx context.Context, link Link) error {
	sourceEpoch, err := t.store.SourceEpoch(ctx)
	if err != nil {
		return err
	}
	if sourceEpoch == "" {
		return errors.New("entity store returned an empty source epoch")
	}
	t.sourceEpoch = sourceEpoch
	link.OfferCapability(uplink.CapabilityOffer{
		Name:     uplink.CapabilityEntitySync,
		Versions: []uint{Version1},
		Offer:    marshalOffer(t.contract.Digest(), sourceEpoch),
	})
	link.Handle(TypeAck, t.handleAck)
	link.OnSession(func(ctx context.Context, session uplink.Session) {
		go t.runSession(ctx, session, link)
	})
	return nil
}

func (t *Exporter) runSession(ctx context.Context, session uplink.Session, link Link) {
	selection, ok := session.Capability(uplink.CapabilityEntitySync)
	if !ok {
		return
	}

	var config Config
	if err := json.Unmarshal(selection.Config, &config); err != nil {
		t.log.Warn("invalid entity sync session config", "error", err)
		return
	}
	if config.ExportSchema != t.contract.Digest() {
		t.log.Warn("cloud selected an unexpected entity export schema",
			"selected", config.ExportSchema, "local", t.contract.Digest())
		return
	}
	if config.SourceEpoch != t.sourceEpoch {
		t.log.Warn("cloud selected an unexpected entity source epoch",
			"selected", config.SourceEpoch, "local", t.sourceEpoch)
		return
	}
	resnapshotInterval, nextSnapshot := t.resnapshotSchedule(config)
	if t.startGate != nil {
		t.log.Info("entity sync waiting for source preparation")
		select {
		case <-ctx.Done():
			return
		case <-t.startGate:
			t.log.Info("entity sync source is ready")
		}
	}

	s := &stream{exporter: t, ctx: ctx, waiters: make(map[string]chan Ack)}
	t.mu.Lock()
	t.active = s
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		if t.active == s {
			t.active = nil
		}
		t.mu.Unlock()
	}()

	cursor := config.Cursor
	snapshotRequired := config.SnapshotRequired
	for {
		if !nextSnapshot.IsZero() && !time.Now().Before(nextSnapshot) {
			snapshotRequired = true
		}
		watchCtx, cancelWatch := context.WithCancel(ctx)
		headPage, err := t.store.ListIndexPageAtRevision(ctx, t.marker(), "", 1, 0)
		if err != nil {
			cancelWatch()
			if !s.retry(ctx, "read entity sync source head", err) {
				return
			}
			continue
		}
		currentHead := headPage.Revision

		var watcher clientv3.WatchChan
		didSnapshot := false
		if snapshotRequired || cursor > currentHead {
			var snapshotCursor int64
			snapshotCursor, watcher, err = s.snapshot(watchCtx, link)
			if err == nil {
				cursor = snapshotCursor
				didSnapshot = true
			}
		} else {
			watcher, err = t.store.WatchIndex(watchCtx, t.marker(), cursor+1)
			if errors.Is(err, rpctypes.ErrCompacted) {
				err = errCompacted
			}
		}
		if err != nil {
			cancelWatch()
			if errors.Is(err, errCompacted) {
				snapshotRequired = true
				if !s.retry(ctx, "entity sync snapshot raced etcd compaction", err) {
					return
				}
				continue
			}
			if !s.retry(ctx, "start entity sync session", err) {
				return
			}
			continue
		}

		if didSnapshot && resnapshotInterval > 0 {
			nextSnapshot = time.Now().Add(resnapshotInterval)
		}
		snapshotRequired = false
		cursor, err = s.tail(link, watcher, cursor, nextSnapshot)
		cancelWatch()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errCompacted) {
			snapshotRequired = true
			if !s.retry(ctx, "entity sync cursor was compacted; starting a complete snapshot", err) {
				return
			}
			continue
		}
		if errors.Is(err, errResnapshotDue) {
			t.log.Info("entity sync anti-entropy interval elapsed; starting a complete snapshot")
			snapshotRequired = true
			continue
		}
		if !s.retry(ctx, "entity sync stream interrupted", err) {
			return
		}
	}
}

func (t *Exporter) resnapshotSchedule(config Config) (time.Duration, time.Time) {
	interval := durationSeconds(config.ResnapshotIntervalSeconds)
	if interval == 0 {
		return 0, time.Time{}
	}
	if interval < t.minimumResnapshotInterval {
		t.log.Warn("cloud requested entity resnapshots below the runtime safety floor",
			"requested", interval, "minimum", t.minimumResnapshotInterval)
		interval = t.minimumResnapshotInterval
	}
	after := durationSeconds(config.ResnapshotAfterSeconds)
	if after == 0 {
		after = interval
	}
	return interval, time.Now().Add(after)
}

func durationSeconds(seconds int64) time.Duration {
	const maxDurationSeconds = int64((time.Duration(1<<63 - 1)) / time.Second)
	if seconds <= 0 || seconds > maxDurationSeconds {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (s *stream) retry(ctx context.Context, message string, err error) bool {
	if err != nil {
		s.exporter.log.Warn(message, "error", err, "retry_in", sessionRetryDelay)
	}
	timer := time.NewTimer(sessionRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (t *Exporter) marker() entity.Attr {
	return entity.Bool(t.contract.MarkerID(), true)
}

func (t *Exporter) handleAck(_ context.Context, raw json.RawMessage) error {
	var ack Ack
	if err := json.Unmarshal(raw, &ack); err != nil {
		return fmt.Errorf("decode entity sync ack: %w", err)
	}
	if ack.MessageID == "" {
		return errors.New("entity sync ack has no message id")
	}

	t.mu.Lock()
	s := t.active
	t.mu.Unlock()
	if s == nil {
		return nil
	}
	s.deliver(ack)
	return nil
}

func (s *stream) snapshot(watchCtx context.Context, link Link) (int64, clientv3.WatchChan, error) {
	page, err := s.exporter.store.ListIndexPageAtRevision(
		s.ctx, s.exporter.marker(), "", snapshotBatchSize, 0,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("list first cloud export index page: %w", err)
	}
	head := page.Revision
	watcher, err := s.exporter.store.WatchIndex(watchCtx, s.exporter.marker(), head+1)
	if err != nil {
		if errors.Is(err, rpctypes.ErrCompacted) {
			return 0, nil, errCompacted
		}
		return 0, nil, fmt.Errorf("watch cloud export index: %w", err)
	}

	snapshotID := uuid.NewString()
	if err := link.SendMessageBlocking(s.ctx, TypeSnapshotBegin, SnapshotBegin{
		SnapshotID: snapshotID, SourceHead: head, ExportSchemaDigest: s.exporter.contract.Digest(),
		SourceEpoch: s.exporter.sourceEpoch,
	}); err != nil {
		return 0, nil, err
	}

	counts := make(map[string]int64)
	tailCursor := head
	for {
		entities := make([]*entity.Entity, 0, len(page.Ids))
		for _, id := range page.Ids {
			source, err := s.exporter.store.GetEntityAtRevision(s.ctx, id, head)
			if err != nil {
				if errors.Is(err, rpctypes.ErrCompacted) {
					return 0, nil, errCompacted
				}
				if errors.Is(err, cond.ErrNotFound{}) {
					s.exporter.log.Warn("skipping stale cloud export index entry", "entity", id, "revision", head)
					continue
				}
				return 0, nil, fmt.Errorf("read snapshot entity %s at revision %d: %w", id, head, err)
			}
			filtered, _, err := s.exporter.contract.Filter(source)
			if err != nil {
				return 0, nil, fmt.Errorf("filter snapshot entity %s: %w", id, err)
			}
			entities = append(entities, filtered)
		}

		for _, item := range entities {
			kind, _ := item.Get(entity.EntityKind)
			counts[string(kind.Value.Id())]++
		}
		if len(entities) > 0 {
			if err := link.SendMessageBlocking(s.ctx, TypeSnapshotBatch, SnapshotBatch{
				SnapshotID: snapshotID, ExportSchemaDigest: s.exporter.contract.Digest(),
				Entities: entities,
			}); err != nil {
				return 0, nil, err
			}
		}
		tailCursor, err = s.drainLiveChanges(link, watcher, tailCursor)
		if err != nil {
			return 0, nil, err
		}

		if page.Cursor == "" {
			break
		}
		page, err = s.exporter.store.ListIndexPageAtRevision(
			s.ctx, s.exporter.marker(), page.Cursor, snapshotBatchSize, head,
		)
		if err != nil {
			if errors.Is(err, rpctypes.ErrCompacted) {
				return 0, nil, errCompacted
			}
			return 0, nil, fmt.Errorf("list cloud export index page at revision %d: %w", head, err)
		}
	}

	messageID := uuid.NewString()
	ack, err := s.sendAndWait(link, TypeSnapshotComplete, SnapshotComplete{
		MessageID: messageID, SnapshotID: snapshotID, SourceHead: head, CountsByKind: counts,
	}, messageID)
	if err != nil {
		return 0, nil, err
	}
	if ack.Cursor != tailCursor {
		return 0, nil, fmt.Errorf("snapshot ack cursor %d does not match committed tail %d", ack.Cursor, tailCursor)
	}
	return ack.Cursor, watcher, nil
}

func (s *stream) tail(link Link, watcher clientv3.WatchChan, cursor int64, nextSnapshot time.Time) (int64, error) {
	var timer *time.Timer
	var snapshotDue <-chan time.Time
	if !nextSnapshot.IsZero() {
		delay := time.Until(nextSnapshot)
		if delay < 0 {
			delay = 0
		}
		timer = time.NewTimer(delay)
		defer timer.Stop()
		snapshotDue = timer.C
	}
	for {
		select {
		case <-s.ctx.Done():
			return cursor, s.ctx.Err()
		case <-snapshotDue:
			return cursor, errResnapshotDue
		case response, ok := <-watcher:
			if !ok {
				return cursor, errors.New("entity export watch closed")
			}
			var err error
			cursor, err = s.sendWatchResponse(link, response, cursor)
			if err != nil {
				return cursor, err
			}
		}
	}
}

func (s *stream) drainLiveChanges(link Link, watcher clientv3.WatchChan, cursor int64) (int64, error) {
	for range maxLiveBatchesBetweenSnapshotBatches {
		select {
		case <-s.ctx.Done():
			return cursor, s.ctx.Err()
		case response, ok := <-watcher:
			if !ok {
				return cursor, errors.New("entity export watch closed")
			}
			var err error
			cursor, err = s.sendWatchResponse(link, response, cursor)
			if err != nil {
				return cursor, err
			}
		default:
			return cursor, nil
		}
	}
	return cursor, nil
}

func (s *stream) sendWatchResponse(link Link, response clientv3.WatchResponse, cursor int64) (int64, error) {
	if response.CompactRevision > 0 || errors.Is(response.Err(), rpctypes.ErrCompacted) {
		return cursor, errCompacted
	}
	if err := response.Err(); err != nil {
		return cursor, fmt.Errorf("watch cloud export index: %w", err)
	}
	// The watch header is an upper-bound progress watermark, not a "last
	// delivered" marker: on the unsynced catch-up path etcd stamps
	// Header.Revision with the current store head, which can exceed the last
	// event's ModRevision in the same response. Treat the last delivered
	// event's ModRevision as authoritative when events are present (mirrors
	// client/v3/watch.go's nextRev = Events[last].Kv.ModRevision + 1); only
	// fall back to Header.Revision for empty progress-notify responses.
	// syncWatchers emits a chunk's events in strictly-increasing ModRevision
	// order, so the trailing event's revision is the chunk's maximum.
	to := response.Header.Revision
	if n := len(response.Events); n > 0 {
		last := response.Events[n-1]
		if last.Kv != nil && last.Kv.ModRevision > 0 {
			to = last.Kv.ModRevision
		}
	}
	if to <= cursor {
		return cursor, nil
	}
	changes, err := s.changes(response.Events, to)
	if err != nil {
		return cursor, err
	}
	messageID := uuid.NewString()
	ack, err := s.sendAndWait(link, TypeChangeBatch, ChangeBatch{
		MessageID: messageID, FromRevision: cursor + 1, ToRevision: to,
		ExportSchemaDigest: s.exporter.contract.Digest(),
		Changes:            changes, SourceEpoch: s.exporter.sourceEpoch,
	}, messageID)
	if err != nil {
		return cursor, err
	}
	if ack.Cursor != to {
		return cursor, fmt.Errorf("change ack cursor %d does not match batch end %d", ack.Cursor, to)
	}
	return to, nil
}

func (s *stream) changes(events []*clientv3.Event, fallbackRevision int64) ([]Change, error) {
	changes := make([]Change, 0, len(events))
	for _, event := range events {
		revision := fallbackRevision
		if event.Kv != nil && event.Kv.ModRevision > 0 {
			revision = event.Kv.ModRevision
		}
		var id entity.Id
		if event.Kv != nil {
			id = entity.Id(event.Kv.Value)
		}
		if id == "" && event.PrevKv != nil {
			id = entity.Id(event.PrevKv.Value)
		}
		if id == "" {
			return nil, errors.New("cloud export watch event has no entity id")
		}

		if event.Type == mvccpb.DELETE {
			source, err := s.exporter.store.GetEntityAtRevision(s.ctx, id, revision-1)
			if err != nil {
				if errors.Is(err, rpctypes.ErrCompacted) {
					return nil, errCompacted
				}
				if errors.Is(err, cond.ErrNotFound{}) {
					s.exporter.log.Warn("skipping stale cloud export index deletion",
						"entity", id, "revision", revision)
					continue
				}
				return nil, fmt.Errorf("read deleted entity %s: %w", id, err)
			}
			filtered, _, err := s.exporter.contract.Filter(source)
			if err != nil {
				return nil, fmt.Errorf("filter deleted entity %s: %w", id, err)
			}
			changes = append(changes, Change{
				Op: ChangeDelete, Revision: revision, EntityID: id, Kind: entityKind(filtered), Entity: filtered,
			})
			continue
		}

		source, err := s.exporter.store.GetEntityAtRevision(s.ctx, id, revision)
		if err != nil {
			if errors.Is(err, rpctypes.ErrCompacted) {
				return nil, errCompacted
			}
			return nil, fmt.Errorf("read changed entity %s at revision %d: %w", id, revision, err)
		}
		filtered, _, err := s.exporter.contract.Filter(source)
		if err != nil {
			return nil, fmt.Errorf("filter changed entity %s: %w", id, err)
		}
		changes = append(changes, Change{
			Op: ChangePut, Revision: revision, EntityID: id, Kind: entityKind(filtered), Entity: filtered,
		})
	}
	return changes, nil
}

func (s *stream) sendAndWait(link Link, messageType string, payload any, messageID string) (Ack, error) {
	waiter := make(chan Ack, 1)
	s.mu.Lock()
	s.waiters[messageID] = waiter
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, messageID)
		s.mu.Unlock()
	}()

	if err := link.SendMessageBlocking(s.ctx, messageType, payload); err != nil {
		return Ack{}, err
	}
	select {
	case <-s.ctx.Done():
		return Ack{}, s.ctx.Err()
	case ack := <-waiter:
		if ack.Error != "" {
			return Ack{}, fmt.Errorf("cloud rejected %s: %s", messageType, ack.Error)
		}
		return ack, nil
	}
}

func (s *stream) deliver(ack Ack) {
	s.mu.Lock()
	waiter := s.waiters[ack.MessageID]
	s.mu.Unlock()
	if waiter == nil {
		return
	}
	select {
	case waiter <- ack:
	default:
	}
}

func entityKind(item *entity.Entity) entity.Id {
	attr, _ := item.Get(entity.EntityKind)
	return attr.Value.Id()
}
