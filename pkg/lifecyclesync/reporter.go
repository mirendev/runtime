package lifecyclesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/pkg/uplink"
)

// recentLimit bounds how many finished operations a sync carries. Cloud keeps
// its own history; this is enough for it to settle anything it was following
// and to notice operations started from the host.
const recentLimit = 20

// Link is the part of *uplink.Client the reporter uses.
type Link interface {
	OfferCapabilityFunc(name string, versions []uint, provide uplink.CapabilityOfferFunc)
	OnSession(func(context.Context, uplink.Session))
	Handle(string, uplink.MessageHandler)
	SendMessageBlocking(context.Context, string, any) error
}

// Identity answers what the offer and every status carry about this process.
type Identity interface {
	InstanceID() string
	InstallKind() string
}

// Reporter is the runtime half of the server-lifecycle capability: it offers
// the actions this host supports, starts operations cloud asks for, and
// streams the ledger back.
type Reporter struct {
	log      *slog.Logger
	store    *serverlifecycle.Store
	launcher serverlifecycle.Launcher
	watcher  *Watcher
	identity Identity

	mu      sync.Mutex
	session *activeSession
}

type activeSession struct {
	ctx  context.Context
	link Link
}

func NewReporter(log *slog.Logger, store *serverlifecycle.Store, launcher serverlifecycle.Launcher, watcher *Watcher, identity Identity) *Reporter {
	return &Reporter{log: log, store: store, launcher: launcher, watcher: watcher, identity: identity}
}

// Actions this runtime will accept. The list is closed on purpose: cloud
// chooses among these, it does not send commands.
func Actions() []string {
	return []string{string(serverlifecycle.ActionRestart), string(serverlifecycle.ActionUpgrade)}
}

func (r *Reporter) Register(_ context.Context, link Link) error {
	link.OfferCapabilityFunc(Capability, []uint{Version1}, func(context.Context) (json.RawMessage, bool) {
		raw, err := json.Marshal(Offer{
			Actions:           Actions(),
			RuntimeInstanceID: r.identity.InstanceID(),
			InstallKind:       r.identity.InstallKind(),
		})
		if err != nil {
			r.log.Warn("server lifecycle offer unavailable", "error", err)
			return nil, false
		}
		return raw, true
	})
	link.Handle(TypeRequest, r.handleRequest)
	link.OnSession(func(ctx context.Context, session uplink.Session) {
		selection, ok := session.Capability(Capability)
		if !ok {
			return
		}
		var config Config
		if len(selection.Config) > 0 {
			if err := json.Unmarshal(selection.Config, &config); err != nil {
				r.log.Warn("invalid server lifecycle session config", "error", err)
				return
			}
		}
		active := &activeSession{ctx: ctx, link: link}
		r.mu.Lock()
		r.session = active
		r.mu.Unlock()
		go r.run(active, config)
	})
	return nil
}

// run announces the ledger and then relays each change for the life of the
// session. Subscribing before the sync is what keeps the two from racing: a
// change between the two would otherwise be in neither.
func (r *Reporter) run(active *activeSession, config Config) {
	ctx := active.ctx
	sub, stop := r.watcher.Subscribe()
	defer stop()

	sync, err := r.buildSync(config.Watch)
	if err != nil {
		r.log.Warn("could not read lifecycle ledger for cloud sync", "error", err)
		return
	}
	if err := active.link.SendMessageBlocking(ctx, TypeSync, sync); err != nil {
		return
	}
	r.log.Info("server lifecycle session started", "operations", len(sync.Operations), "missing", len(sync.Missing), "watched", len(config.Watch))

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Wake():
			for _, op := range sub.Drain() {
				status := Status{RuntimeInstanceID: r.identity.InstanceID(), Operation: op}
				if err := active.link.SendMessageBlocking(ctx, TypeStatus, status); err != nil {
					return
				}
			}
		}
	}
}

func (r *Reporter) buildSync(watch []string) (*Sync, error) {
	ops, err := r.store.List()
	if err != nil {
		return nil, err
	}
	sync := &Sync{RuntimeInstanceID: r.identity.InstanceID()}
	include := make(map[string]bool, len(ops))
	finished := 0
	// Newest first, so the recent-finished bound keeps the latest ones.
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		if !op.Done() {
			include[op.ID] = true
			continue
		}
		if finished < recentLimit {
			include[op.ID] = true
			finished++
		}
	}
	// List returns records sorted by id, which is the ULID file name, so a
	// binary search over it is sound.
	for _, id := range watch {
		if _, found := slices.BinarySearchFunc(ops, id, func(op *serverlifecycle.Operation, id string) int {
			return cmpString(op.ID, id)
		}); found {
			include[id] = true
		} else {
			sync.Missing = append(sync.Missing, id)
		}
	}
	for _, op := range ops {
		if include[op.ID] {
			sync.Operations = append(sync.Operations, op)
		}
	}
	if sync.Operations == nil {
		sync.Operations = []*serverlifecycle.Operation{}
	}
	return sync, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// handleRequest runs on the link's read loop, so the work goes to a
// goroutine: starting an executor is a systemd-run round trip, and the reply
// is a blocking send.
func (r *Reporter) handleRequest(_ context.Context, raw json.RawMessage) error {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("decode lifecycle request: %w", err)
	}
	r.mu.Lock()
	active := r.session
	r.mu.Unlock()
	if active == nil || active.ctx.Err() != nil {
		return nil
	}
	go r.start(active, req)
	return nil
}

func (r *Reporter) start(active *activeSession, req Request) {
	ctx := active.ctx
	op, err := r.startOperation(ctx, req)
	if err != nil {
		r.log.Warn("refused cloud lifecycle request", "operation", req.OperationID, "action", req.Action, "error", err)
		_ = active.link.SendMessageBlocking(ctx, TypeReject, Reject{OperationID: req.OperationID, Reason: err.Error()})
		return
	}
	// The watcher announces the new record on its next pass; a direct status
	// here answers the request without waiting for it, and covers the case
	// where the record already existed and the watcher has nothing new to say.
	status := Status{RuntimeInstanceID: r.identity.InstanceID(), Operation: op}
	_ = active.link.SendMessageBlocking(ctx, TypeStatus, status)
	r.watcher.Kick()
}

func (r *Reporter) startOperation(ctx context.Context, req Request) (*serverlifecycle.Operation, error) {
	if !slices.Contains(Actions(), req.Action) {
		return nil, fmt.Errorf("unsupported action %q", req.Action)
	}
	if req.OperationID == "" {
		return nil, errors.New("request has no operation id")
	}
	requestedBy := req.RequestedBy
	if requestedBy == "" {
		requestedBy = "cloud"
	}
	op := serverlifecycle.NewOperation(serverlifecycle.Action(req.Action), requestedBy)
	op.ID = req.OperationID
	op.TargetVersion = req.TargetVersion
	op.ArtifactType = req.ArtifactType
	op.NoRollback = req.NoRollback
	op.ReadyTimeoutSeconds = req.ReadyTimeoutSeconds

	started, created, err := serverlifecycle.Start(ctx, r.store, r.launcher, op)
	if err != nil {
		return nil, err
	}
	if created {
		r.log.Info("lifecycle operation started for cloud", "operation", started.ID, "action", started.Action, "target", started.TargetVersion, "requested_by", requestedBy)
	}
	return started, nil
}
