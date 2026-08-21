package coordinate

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/uplink"
)

// The deploy.* message family carries the cluster's deploy log to cloud, the
// durable half of the visibility contract in RFD-94.
//
// These types are one half of a wire contract. The other half is
// DeployBatch/DeployState in mirendev/cloud, services/cluster_channel/
// deploy_handlers.go, and nothing enforces that the two agree: cloud does not
// import this module, so renaming a json tag here leaves both repos building
// and every test on both sides passing while the field silently stops
// arriving. Change a tag, a type or a message name here and you must change it
// there in the same breath. MIR-1601 is the fix that makes this checkable.
//
// It differs from app.* in what a lost message costs. App state is an ephemeral
// sample and a dropped one self-heals from the next report. A deploy is an event
// that happened once, and the runtime prunes its own records over time, so once
// cloud has a deploy it becomes the book of record for it. Nothing here can be
// left to best effort.
const (
	// TypeDeploySyncHello opens deploy sync on a connection, asking cloud where
	// its copy left off.
	//
	// The runtime asks rather than cloud volunteering because three of the four
	// reasons to distrust a cursor are only visible from this side: whether the
	// revision has been compacted away, whether it sits above the store's
	// current head, and whether it is coherent at all. Only "cloud has none"
	// is visible from over there, and that arrives as a zero. Putting the
	// question here keeps one decision in one function.
	TypeDeploySyncHello = "deploy.sync.hello"

	// TypeDeploySyncCursor is cloud's answer: the revision below which it
	// believes its copy is complete.
	TypeDeploySyncCursor = "deploy.sync.cursor"

	// TypeDeployBatch carries a contiguous run of deploys and the range it
	// covers.
	//
	// Named for the contiguity claim rather than for the write it performs —
	// RFD-94 sketches this as deploy.upsert — because the claim is the part
	// that matters. A message called "upsert" invites a second sender that
	// upserts without a range, and cloud advancing its watermark on that would
	// open exactly the permanent gap this design exists to prevent.
	TypeDeployBatch = "deploy.batch"

	// TypeDeploySyncReset is cloud telling this cluster that the cursor it is
	// reporting against is no longer usable and the sync has to start over.
	//
	// Cloud sends it two ways, and they mean the same thing here. An operator
	// repairing history discards the cursor and cloud says so directly; or a
	// batch arrives claiming to start above where cloud has reached, which
	// would leave a hole in the frontier, and cloud refuses it and says so in
	// reply. Either way the cursor this reporter is working from describes a
	// position cloud does not share, and the only way back into agreement is a
	// full re-list.
	//
	// It carries no payload on purpose. A reason would invite the reporter to
	// treat some reasons differently, and there is only one correct response.
	TypeDeploySyncReset = "deploy.sync.reset"
)

const (
	// deployBatchSize bounds one message so a backfill arrives as a slide
	// rather than a jump (RFD-94). Reporting must never take meaningful
	// horsepower from the cluster's real work.
	deployBatchSize = 100

	// deployBatchPause spaces batches out for the same reason, and leaves the
	// link available to its other tenants during a long backfill.
	deployBatchPause = 250 * time.Millisecond

	// deployConnectSpread is how far the opening handshake is scattered across
	// the fleet, and it deliberately starts after the app reporter's own
	// spread rather than overlapping it.
	//
	// A cluster that just came online should finish painting its app list
	// before a backfill's long tail starts competing for the same link. That
	// ordering is a preference rather than a requirement: both tenants send
	// with backpressure, so an overlap would resolve into them sharing the
	// pipe rather than one starving the other. Starting later simply makes the
	// common case the intended one.
	deployConnectSpread = 90 * time.Second

	// maxErrorReasonBytes bounds the failure reason put on the wire.
	//
	// RFD-94 asks for a short reason and never raw build output, and until now
	// that was a sentence in a comment with nothing enforcing it: the entity's
	// error message went out whole. A deploy that failed with a stack trace or
	// a wall of compiler output would ship all of it, which smuggles build logs
	// past the custody boundary the rest of this file is careful about, and
	// blows the frame budget on top.
	//
	// Truncation is deterministic, so the same record always produces the same
	// payload. That matters more than it looks: a value that varied per report
	// would defeat the upsert the way a drifting deployed_at would.
	maxErrorReasonBytes = 512

	// deployCursorTimeout bounds the wait for cloud's answer.
	//
	// A cloud that never answers is treated as a cloud with no cursor, which
	// costs a re-list and no correctness. Waiting forever instead would mean a
	// cluster silently stops reporting deploys against a cloud that is up
	// enough to hold a socket but not to answer.
	deployCursorTimeout = 30 * time.Second
)

// DeploySyncHello opens the exchange. It carries no fields today; the runtime
// is asking a question, not asserting anything.
type DeploySyncHello struct{}

// DeploySyncCursor is cloud's reply.
type DeploySyncCursor struct {
	// Watermark is the revision below which cloud believes its copy is
	// complete. Zero means it has none.
	//
	// A plain value rather than a pointer, unlike the app snapshot's count,
	// because here absent and zero call for the same thing: a full re-list.
	// The count guard needed the distinction because a missing field would
	// otherwise have authorized a destructive sweep. Nothing destructive hangs
	// off this one, so the two states can safely collapse.
	Watermark int64 `json:"watermark"`
}

// DeployBatch is a contiguous run of deploys: every deploy the runtime holds
// whose revision falls in (FromRevision, ToRevision], with nothing omitted.
//
// That claim is what lets cloud move its watermark, and it is why the range
// travels in the same message as the deploys rather than in a closing message
// of its own. The app snapshot needs a separate completion message and a count
// because its epoch spans many envelopes, so a dropped one is invisible. A
// batch is a single envelope: it arrives whole or not at all, and if it does
// not arrive the watermark simply does not move. There is nothing for a count
// to catch.
type DeployBatch struct {
	FromRevision int64 `json:"from_revision"`
	ToRevision   int64 `json:"to_revision"`

	// HeadRevision is the store's head when this connection's sync began.
	//
	// Cloud does not need it to decide anything — the runtime already made the
	// cursor decision, since it is the side that can see compaction. It is
	// reported so cloud can record what this store's revision sequence looked
	// like, which is what makes a rebuilt etcd legible after the fact rather
	// than only at the moment it is detected.
	HeadRevision int64 `json:"head_revision"`

	// ObservedAt is when the runtime read this state. Cloud keeps it distinct
	// from its own receipt time so staleness stays visible.
	ObservedAt time.Time `json:"observed_at"`

	// Deploys may be empty, and an empty batch is meaningful rather than
	// wasted: it says the range holds no deploys, which is how the watermark
	// advances across long stretches of unrelated entity writes. Revisions are
	// store-global, so most of the sequence belongs to other kinds entirely.
	Deploys []DeployState `json:"deploys"`
}

// DeployState is one deploy as the runtime holds it.
//
// Two things are deliberately absent, for different reasons.
//
// Build logs are absent permanently. They stay in the cluster on custody
// grounds and because they belong to observability (RFD-54), not to this
// contract. The entity carries them, so this projection is written by hand
// rather than encoded from the record — an Encode() here would ship them the
// first time anyone stopped thinking about it.
//
// The actor is absent for now. Deployer identity is captured nowhere in the
// runtime yet (MIR-1271), and the fields that exist hold empty strings on the
// real deploy path. RFD-94 is explicit that deploys land in cloud unattributed
// rather than carrying an empty or client-asserted actor, so there is no field
// here to fill in with a lie.
type DeployState struct {
	// ID is the deployment entity id, and cloud's idempotency key. Batches
	// overlap by design — a re-list re-sends what the previous connection
	// already delivered — so writes have to collapse on this.
	ID string `json:"id"`

	// Revision orders writes to this deploy. Store-global and monotonic, so it
	// settles which of two reports of the same deploy is newer without
	// consulting any clock.
	Revision int64 `json:"revision"`

	AppName string `json:"app_name"`

	// Version is the app version this deploy shipped, nil when it never got
	// one — a deploy that failed before a build produced a version has no
	// version to name, and saying so with an absent object is honest where an
	// empty string is a value that has to be recognized as meaning nothing.
	Version *EntityRef `json:"version"`

	Status string `json:"status"`
	Phase  string `json:"phase"`

	// DeployedAt is when the deploy was initiated, and must be identical in
	// every report of this deploy: cloud partitions on it, so a value that
	// drifted would write a second row instead of updating the first. It comes
	// from the record's creation time, which is written once and never
	// rewritten.
	DeployedAt  time.Time  `json:"deployed_at"`
	CompletedAt *time.Time `json:"completed_at"`

	// ErrorReason is a short failure reason, never raw build output.
	ErrorReason *string `json:"error_reason"`

	Git *DeployGit `json:"git"`

	// SourceDeploy links a rollback or redeploy to what it was based on, nil
	// for a fresh build.
	SourceDeploy *EntityRef `json:"source_deploy"`
}

// EntityRef names one runtime entity, in both the form that identifies it and
// the form a person reads.
//
// Both are carried because neither does the other's job. The id is the durable
// handle: UUIDv7 under its base58, never reused, and unique across every
// cluster. The short id is what the CLI and console actually show, but it is
// allocated per cluster against the entities alive at that moment
// (pkg/entity/store.go), so it collides across clusters and is freed for
// reallocation once its entity is pruned. Cloud becomes the book of record for
// a deploy after the runtime prunes it, so storing only the short id would
// leave a history whose identifiers can later name something else.
//
// ShortID is empty when the runtime could not resolve one — an entity that was
// pruned before this deploy was reported, most often. Readers fall back to the
// id with its kind prefix stripped, which is what pkg/ui.DisplayShortID does
// for the CLI.
type EntityRef struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
}

// DeployGit is the provenance cloud keeps for a deploy.
//
// Commit author and author email are on the entity and are left there. They
// are the PII-carrying half of the git record, RFD-94 asks for the minimum
// that serves the feature, and the erasure story for git identity is still
// open. Nothing in visibility needs them today, so taking them would be
// collecting against a use we have not defined.
type DeployGit struct {
	SHA        string `json:"sha"`
	Branch     string `json:"branch"`
	Repository string `json:"repository"`
}

// deployLink is the slice of the uplink the reporter uses.
//
// Only the blocking send is here, on purpose. Everything this reporter sends is
// part of a sequence, and Send's drop-on-overflow is unsafe for sequences: a
// dropped batch is not a lost sample that the next report repairs, it is a
// range cloud will never be told about again unless the watermark stays put.
type deployLink interface {
	SendMessageBlocking(ctx context.Context, msgType string, data any) error
}

// deploySource is the runtime state the reporter reads.
//
// Narrow and injectable so the sync logic — which is where all the correctness
// lives — can be tested against a fake store rather than a live etcd.
type deploySource interface {
	// HeadRevision reports the store's current revision.
	HeadRevision(ctx context.Context) (int64, error)

	// List returns every deploy the runtime holds, with the revision the read
	// was taken at.
	List(ctx context.Context) ([]*deploylifecycle.Record, int64, error)

	// Watch streams deploy operations in revision order starting just after
	// fromRevision, blocking until the context ends or the stream fails.
	Watch(ctx context.Context, fromRevision int64, fn func(*esv1.EntityOp) error) error

	// ShortID resolves one entity's short id, returning "" when the entity is
	// gone or carries none.
	//
	// No error, on purpose. A short id is a display convenience, and there is
	// nothing a caller here could usefully do with a failure to read one: the
	// deploy still has to be reported, and the reference still has its durable
	// id. Returning "" collapses "pruned", "never had one" and "the read
	// failed" into the one case every reader already handles.
	ShortID(ctx context.Context, entityID string) string

	// ShortIDsForKind resolves every short id for a kind in a single read.
	//
	// This exists for the backfill. Resolving one at a time is fine on the live
	// path, where deploys arrive one at a time anyway, but a first re-list
	// walks every deploy a cluster has ever run and would turn into a Get per
	// deploy against a store that can answer the whole question at once.
	ShortIDsForKind(ctx context.Context, kind entity.Id) map[string]string
}

// errCompacted ends a stream that cannot continue because the revision it
// resumed from has been compacted away.
//
// A sentinel rather than a log-and-stop because it is the one stream ending
// that is recoverable and has a specific remedy: the caller re-lists. Every
// other reason a watch ends means the connection is going away, and the next
// one starts the exchange over anyway.
var errCompacted = errors.New("watch start revision has been compacted")

// errResetRequested ends a stream because cloud asked for a full re-list.
//
// Deliberately the same shape as errCompacted, and handled beside it, because
// the two are the same situation reached from opposite directions: the cursor
// this stream resumed from is not one cloud can accept, and re-listing is the
// remedy. Compaction is the cluster discovering that; a reset is cloud saying
// it.
var errResetRequested = errors.New("cloud asked for a full re-list")

// syncMode is how a connection's deploy sync proceeds.
type syncMode int

const (
	// syncDelta streams from cloud's watermark. Catch-up and live tail are the
	// same ordered stream in this mode, so there is nothing to run alongside
	// it.
	syncDelta syncMode = iota

	// syncRelist re-sends everything the runtime holds, oldest first, then
	// continues from the revision the listing was taken at.
	syncRelist
)

// decideSyncMode settles whether a cursor can be used, and says why when it
// cannot.
//
// This is the whole of RFD-94's fallback rule except the compaction case, which
// cannot be decided here because it is not visible until the watch is opened
// and reports it. Every case resolves to a re-list, which is the point: a wrong
// cursor costs a re-list and never data, so this only has to be conservative,
// not clever.
func decideSyncMode(watermark, head int64) (syncMode, string) {
	switch {
	case watermark == 0:
		// First contact, or a cursor cloud discarded. Both leave it absent.
		return syncRelist, "cloud has no watermark"

	case watermark < 0:
		// Not a revision any store ever issued. Nothing sane produces this, so
		// the interesting property is that it fails toward the safe path
		// rather than being used as an offset into a watch.
		return syncRelist, "watermark is not a valid revision"

	case watermark > head:
		// A rebuilt etcd starts its revision sequence over, leaving cloud's
		// watermark stranded above anything this store will issue for a long
		// time. Reporting head is what turns that into a detectable reset
		// instead of a watch that silently waits for a revision that already
		// came and went under different data.
		return syncRelist, "watermark is ahead of the store's head revision"

	default:
		return syncDelta, ""
	}
}

// startDeployReporter arranges for the cluster's deploy log to be reported to
// cloud for as long as each connection lasts.
func (c *Coordinator) startDeployReporter(link *uplink.Client) {
	reporter := &deployReporter{
		log:     c.Log.With("module", "coordinate.deployreport"),
		source:  &entityDeploySource{eac: c.eac},
		cursors: make(chan int64, 1),
	}

	link.Handle(TypeDeploySyncCursor, reporter.handleCursor)
	link.Handle(TypeDeploySyncReset, reporter.handleReset)

	link.OnConnect(func(ctx context.Context) {
		// Run off the connection goroutine: this reads the entity store, and
		// the callback returns before the read and write loops start.
		go reporter.run(ctx, link)
	})
}

// deployReporter reports the deploy log over one connection at a time.
type deployReporter struct {
	log    *slog.Logger
	source deploySource

	// cursors carries cloud's replies from the router, which is registered
	// once for the process, to whichever run is currently waiting. Buffered by
	// one and drained at the start of each run, so a reply that arrived too
	// late to be used cannot be mistaken for the answer to the next
	// connection's question.
	cursors chan int64

	// mu guards resetWanted and cancelStream.
	//
	// A flag rather than a queue, and the distinction is load-bearing. A
	// channel makes "is a reset pending" and "take the reset" the same
	// operation, so every place that wants to *ask* also has to consume, and
	// any of them that then fails to re-list drops the request on the floor.
	// Separating the two means only the code that actually starts a re-list
	// clears it, and a connection dying anywhere else leaves it standing for
	// the next one.
	//
	// It collapses for free: two resets and one reset ask for the same thing.
	//
	// cancelStream is the running stream's cancel, or nil when none is
	// running. The lock is held only long enough to read or replace these; the
	// cancel is called outside it, since it runs arbitrary work.
	mu           sync.Mutex
	resetWanted  bool
	cancelStream context.CancelFunc

	// Pacing, held as fields rather than read from the constants directly so a
	// test can drive the protocol without waiting out a spread measured in
	// minutes. Zero means the constant.
	spread        time.Duration
	cursorTimeout time.Duration
	batchPause    time.Duration
}

func (r *deployReporter) spreadWindow() time.Duration {
	if r.spread == 0 {
		return deployConnectSpread
	}
	return r.spread
}

func (r *deployReporter) timeout() time.Duration {
	if r.cursorTimeout == 0 {
		return deployCursorTimeout
	}
	return r.cursorTimeout
}

func (r *deployReporter) pause() time.Duration {
	if r.batchPause == 0 {
		return deployBatchPause
	}
	return r.batchPause
}

func (r *deployReporter) handleCursor(_ context.Context, data json.RawMessage) error {
	var cursor DeploySyncCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return err
	}

	select {
	case r.cursors <- cursor.Watermark:
	default:
		// Nobody is waiting, or an answer is already queued. Dropping is right:
		// an unmatched cursor is stale by definition, and the next connection
		// asks again.
		r.log.Debug("discarding unexpected deploy sync cursor", "watermark", cursor.Watermark)
	}

	return nil
}

// handleReset records that cloud wants a full re-list.
//
// Nothing is read out of the message, and a reset that arrives when no stream
// is running is deliberately left in the buffer rather than dropped. The two
// senders differ in timing: a rejected batch arrives mid-stream and wants to
// interrupt it, while an operator's reset can land between connections, and
// keeping it means the next run honours it instead of needing cloud to say it
// again.
func (r *deployReporter) handleReset(_ context.Context, _ json.RawMessage) error {
	r.log.Info("cloud asked for a full deploy re-list")

	r.mu.Lock()
	r.resetWanted = true
	cancel := r.cancelStream
	r.mu.Unlock()

	// Wake a stream that is parked waiting for the next deploy.
	//
	// Queuing alone is not enough, because the only thing that reads the queue
	// mid-stream is the watch callback, and the callback does not run until the
	// cluster produces a deploy. On a quiet cluster that could be hours, which
	// is exactly the cluster where an operator most needs the reset to land
	// now: they were told the nudge was delivered.
	//
	// Done even when a reset was already pending. A second one against a
	// parked stream still has to wake it.
	if cancel != nil {
		cancel()
	}

	return nil
}

// setStreamCancel records the running stream's cancel, or clears it with nil.
func (r *deployReporter) setStreamCancel(cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelStream = cancel
}

// resetPending reports whether cloud has asked for a re-list, without taking
// the request. Everything that decides uses this; only takeReset clears it.
func (r *deployReporter) resetPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resetWanted
}

// takeReset clears a pending request.
//
// Called from exactly one place, where a re-list actually begins. Anything
// else that cleared it could fail to re-list afterwards, and the request would
// be gone with the cursor it was meant to invalidate still in use.
func (r *deployReporter) takeReset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetWanted = false
}

// run reports deploys until the connection drops.
//
// Failure here is deliberately non-fatal and unretried within a connection.
// Reporting is value flowing up to a value-add control plane, never a
// dependency flowing down: a cluster runs indefinitely with no cloud attached,
// and the next connection starts the whole exchange over. Nothing the cluster
// does for its own users may degrade because this failed.
func (r *deployReporter) run(ctx context.Context, link deployLink) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(uplink.SpreadOnConnect(r.spreadWindow())):
	}

	watermark, err := r.requestCursor(ctx, link)
	if err != nil {
		r.log.Warn("failed to learn cloud's deploy watermark", "error", err)
		return
	}

	head, err := r.source.HeadRevision(ctx)
	if err != nil {
		r.log.Warn("failed to read store head revision", "error", err)
		return
	}

	// One cache for the whole connection, so a delta stream that falls back to
	// a re-list keeps what it already learned.
	ids := newShortIDs(r.source)

	mode, reason := decideSyncMode(watermark, head)

	// A reset that arrived while nothing was streaming outranks the cursor.
	// Cloud may well have answered the hello with a watermark it is about to
	// disown, since clearing the cursor and saying so are two writes and the
	// handshake can land between them.
	if r.resetPending() {
		mode, reason = syncRelist, "cloud asked for a full re-list"
	}

	if mode == syncDelta {
		r.log.Info("resuming deploy sync from cloud's watermark",
			"watermark", watermark, "head", head)

		err := r.stream(ctx, link, watermark, head, ids)
		if err == nil || ctx.Err() != nil {
			return
		}

		// Compaction is the one stream failure a re-list actually fixes: the
		// gap between cloud's watermark and what the store still remembers can
		// no longer be replayed, so the only way to close it is to send
		// everything again.
		//
		// Anything else — a transport error, the entity server going away — is
		// not evidence the cursor is bad, and re-listing on it would turn a
		// blip into a full re-send of the cluster's history. The connection is
		// coming down anyway, and the next one starts the handshake over.
		switch {
		case errors.Is(err, errCompacted):
			r.log.Info("deploy watermark has been compacted away, re-listing")
		case errors.Is(err, errResetRequested):
			r.log.Info("cloud disowned the deploy watermark mid-stream, re-listing")
		default:
			r.log.Warn("deploy stream ended, waiting for the next connection", "error", err)
			return
		}
	} else {
		r.log.Info("re-listing deploys for cloud", "reason", reason,
			"watermark", watermark, "head", head)
	}

	r.relist(ctx, link, head, ids)
}

// requestCursor asks cloud where its copy left off and waits for the answer.
func (r *deployReporter) requestCursor(ctx context.Context, link deployLink) (int64, error) {
	// Drain first: a reply from the previous connection that lost the race with
	// its own teardown would otherwise be read as this connection's answer.
	select {
	case <-r.cursors:
	default:
	}

	if err := link.SendMessageBlocking(ctx, TypeDeploySyncHello, DeploySyncHello{}); err != nil {
		return 0, err
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case watermark := <-r.cursors:
		return watermark, nil
	case <-time.After(r.timeout()):
		// Treated as "no cursor" rather than as a failure, which costs a
		// re-list and nothing else.
		r.log.Warn("cloud did not answer the deploy sync hello, re-listing")
		return 0, nil
	}
}

// stream follows the store from a revision, shipping each deploy as its own
// contiguous batch.
//
// In this mode catch-up and live tail are the same thing: the watch replays
// what was missed and then continues into new activity without a seam, so every
// message can carry a range and advance the watermark. The two-stream split
// RFD-94 describes is only needed when there is no usable cursor, which is what
// relist handles.
//
// Returns nil when the connection ends, and an error when the stream must fall
// back to a re-list.
func (r *deployReporter) stream(ctx context.Context, link deployLink, from, head int64, ids *shortIDs) error {
	// The watch runs under a context this reporter can cancel, so a reset can
	// end a stream that is parked between deploys rather than waiting for one.
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.setStreamCancel(cancel)
	defer r.setStreamCancel(nil)

	// A reset that landed between the caller's check and the registration
	// above found no stream to cancel, and a parked watch would never notice
	// it. Checked here, after the cancel is visible, so that window is closed.
	if r.resetPending() {
		return errResetRequested
	}

	sent := from

	err := r.source.Watch(watchCtx, from, func(op *esv1.EntityOp) error {
		if op.IsCompacted() {
			return errCompacted
		}

		// The callback fires as the cluster produces deploys, which is exactly
		// when a stream is advancing cloud's watermark past the position a
		// reset was trying to clear, so a busy cluster stops here on its next
		// event. A parked one never reaches this at all, and is woken by the
		// cancellation above instead.
		if r.resetPending() {
			return errResetRequested
		}

		revision := op.Revision()
		if revision <= sent {
			return nil
		}

		batch := DeployBatch{
			FromRevision: sent,
			ToRevision:   revision,
			HeadRevision: head,
			ObservedAt:   time.Now().UTC(),
		}

		// A batch here can be empty for two different reasons, and both still
		// advance the range.
		//
		// The ordinary one is a progress notification, which carries no entity:
		// the store saying "nothing you care about happened up to here."
		// Shipping it as an empty batch is what lets the watermark keep pace
		// during quiet periods instead of freezing at the last deploy, which on
		// a cluster that deploys rarely would mean re-walking a widening gap on
		// every connect.
		//
		// The other is a deploy this reporter had to drop, having no stable
		// creation time to key it by. The empty batch then means "a deploy was
		// here and is not coming," which is worth reading as different from
		// silence. Advancing anyway is still right: holding the range back
		// would wedge the watermark permanently, since the next connection
		// re-streams the same revision and drops it again, and a record with no
		// stable timestamp has no home in cloud to hold open for it either.
		if op.HasEntity() {
			if state, ok := r.stateFrom(ctx, op.Entity(), ids); ok {
				batch.Deploys = append(batch.Deploys, state)
			} else {
				// relist counts these; the tail said nothing, which made the
				// same event visible on one path and invisible on the other.
				r.log.Debug("skipping deploy with no usable creation time",
					"entity_id", op.EntityId(), "revision", revision)
			}
		}

		if err := link.SendMessageBlocking(ctx, TypeDeployBatch, batch); err != nil {
			return err
		}

		sent = revision
		return nil
	})

	// A watch cancelled by a reset is indistinguishable from any other
	// cancellation at this point, and the pending flag is what tells them
	// apart. No parent-context guard is needed: nothing is consumed here, so a
	// connection going away mid-answer leaves the request standing for the
	// next one rather than losing it.
	if err != nil && !errors.Is(err, errResetRequested) && r.resetPending() {
		return errResetRequested
	}

	return err
}

// relist re-sends every deploy the runtime holds, oldest first, then continues
// from the revision the listing was taken at.
//
// Oldest-first is what makes the watermark truthful. A single number cannot
// express holes, so the frontier can only advance along a contiguous line from
// the bottom; walking newest-first would either advance it over deploys not yet
// sent or not advance it at all. It also makes an interrupted backfill free to
// resume: whatever ranges committed stay committed, and the next connection
// picks up from the watermark they left behind.
func (r *deployReporter) relist(ctx context.Context, link deployLink, head int64, ids *shortIDs) {
	// The single place a pending reset is cleared, because this is the single
	// place its request is carried out.
	r.takeReset()

	records, readRevision, err := r.source.List(ctx)
	if err != nil {
		r.log.Warn("failed to list deploys for cloud", "error", err)
		return
	}

	// Resolve short ids before projecting anything, and take the cheapest
	// source for each kind.
	//
	// Versions are not in hand, so they cost one bulk read of the kind rather
	// than a lookup per deploy. Deploys are: the listing above returned every
	// record with its entity attached, so the short id of anything a rollback
	// can name is already in memory. Listing the deployment kind for them would
	// re-read the largest kind a cluster has, on the one path where that is a
	// whole history, to learn what the caller is already holding.
	//
	// Both are optimizations and neither is authoritative. A version created
	// after the bulk read, or a source deploy already pruned from the listing,
	// falls through to a single lookup.
	ids.preload(ctx, core_v1alpha.KindAppVersion)
	for _, rec := range records {
		ids.seed(rec.Entity)
	}

	states := make([]DeployState, 0, len(records))
	skipped := 0
	for _, rec := range records {
		state, ok := r.stateFor(ctx, rec, ids)
		if !ok {
			skipped++
			continue
		}
		states = append(states, state)
	}

	if skipped > 0 {
		r.log.Warn("skipped deploys with no usable creation time", "count", skipped)
	}

	// By revision, because that is the axis the watermark advances along.
	// Sorting by deployed_at would order them by a clock cloud does not use
	// for this, and a batch's range would then no longer bound its contents.
	sort.Slice(states, func(i, j int) bool {
		return states[i].Revision < states[j].Revision
	})

	from := int64(0)
	for start := 0; start < len(states); start += deployBatchSize {
		end := min(start+deployBatchSize, len(states))
		chunk := states[start:end]

		// The last batch closes at the read revision rather than at its
		// newest deploy, because the listing covered everything up to there.
		// Stopping at the newest deploy would leave the frontier short of what
		// was actually proven, and the next connect would re-walk the
		// difference for nothing.
		to := chunk[len(chunk)-1].Revision
		if end == len(states) {
			to = max(to, readRevision)
		}

		batch := DeployBatch{
			FromRevision: from,
			ToRevision:   to,
			HeadRevision: head,
			ObservedAt:   time.Now().UTC(),
			Deploys:      chunk,
		}

		if err := link.SendMessageBlocking(ctx, TypeDeployBatch, batch); err != nil {
			r.log.Warn("failed to send deploy batch", "error", err)
			return
		}

		from = to

		select {
		case <-ctx.Done():
			return
		case <-time.After(r.pause()):
		}
	}

	// A cluster with no deploys still has to move the frontier, or every
	// connect re-lists nothing and cloud never learns it is caught up.
	if len(states) == 0 {
		batch := DeployBatch{
			FromRevision: 0,
			ToRevision:   readRevision,
			HeadRevision: head,
			ObservedAt:   time.Now().UTC(),
		}
		if err := link.SendMessageBlocking(ctx, TypeDeployBatch, batch); err != nil {
			r.log.Warn("failed to send empty deploy batch", "error", err)
			return
		}
	}

	r.log.Info("re-listed deploys for cloud", "deploys", len(states), "revision", readRevision)

	// Straight into the tail from where the listing left off. New deploys made
	// during the backfill have revisions above the read revision, so they are
	// caught here rather than missed.
	//
	// This is also why there is no separate live-tail message in v1: the only
	// window where one would help is a first-contact backfill long enough for a
	// deploy to happen inside it, and RFD-94 explicitly defers that accelerator.
	// Adding it later means a message that carries no range and therefore
	// cannot advance the watermark — the invariant to preserve if it lands.
	if err := r.stream(ctx, link, readRevision, head, ids); err != nil && ctx.Err() == nil {
		r.log.Warn("deploy tail ended", "error", err)
	}
}

// shortIDs remembers short-id lookups for the life of one connection.
//
// Both halves of that matter. Remembering is what keeps a rollback and the
// deploy it was based on from costing two reads of the same version. Misses are
// remembered too, which is the less obvious half: a version pruned before its
// deploy was reported will never resolve, and without a negative entry it would
// be re-read on every batch for as long as the connection lasts.
//
// Scoped to a connection rather than to the process so an answer cannot outlive
// the store that gave it. A short id freed by a prune and reallocated to
// another entity would otherwise be served from here indefinitely.
//
// Not safe for concurrent use, and does not need to be: it belongs to one run,
// which is a single goroutine from the handshake through to the tail.
type shortIDs struct {
	src   deploySource
	known map[string]string
}

func newShortIDs(src deploySource) *shortIDs {
	return &shortIDs{src: src, known: make(map[string]string)}
}

// preload fills in every short id for a kind in one read.
//
// Existing entries win, so a preload can never overwrite something already
// observed — the preloaded view is older than any lookup that beat it here.
func (s *shortIDs) preload(ctx context.Context, kind entity.Id) {
	for id, short := range s.src.ShortIDsForKind(ctx, kind) {
		if _, ok := s.known[id]; !ok {
			s.known[id] = short
		}
	}
}

// seed records an entity the caller already holds, sparing a read for it.
//
// Existing entries win, for the same reason they do in preload: whatever is
// already here was observed no earlier than this.
func (s *shortIDs) seed(ent *esv1.Entity) {
	id, short := shortIDEntry(ent)
	if id == "" {
		return
	}
	if _, ok := s.known[id]; !ok {
		s.known[id] = short
	}
}

// ref builds the reference cloud stores for an entity id, or nil when there is
// no entity to name.
func (s *shortIDs) ref(ctx context.Context, id string) *EntityRef {
	if id == "" {
		return nil
	}

	short, ok := s.known[id]
	if !ok {
		short = s.src.ShortID(ctx, id)
		s.known[id] = short
	}

	return &EntityRef{ID: id, ShortID: short}
}

func (r *deployReporter) stateFrom(ctx context.Context, ent *esv1.Entity, ids *shortIDs) (DeployState, bool) {
	return r.stateFor(ctx, deploylifecycle.RecordFrom(ent), ids)
}

// stateFor projects a record onto the wire shape, and reports false for a
// record that cannot be reported at all.
func (r *deployReporter) stateFor(ctx context.Context, rec *deploylifecycle.Record, ids *shortIDs) (DeployState, bool) {
	deployedAt, ok := deployedAtFor(rec)
	if !ok {
		// Cloud partitions on this, so a deploy without a stable one has no
		// safe home. Dropping it is better than inventing a timestamp, which
		// would differ on every report and write a fresh duplicate row each
		// time rather than updating one.
		return DeployState{}, false
	}

	dep := rec.Deployment

	state := DeployState{
		ID:         string(dep.ID),
		Revision:   rec.Revision,
		AppName:    dep.AppName,
		Version:    ids.ref(ctx, rec.AppVersion()),
		Status:     dep.Status,
		Phase:      dep.Phase,
		DeployedAt: deployedAt,
	}

	if dep.CompletedAt != "" {
		if t, err := time.Parse(time.RFC3339, dep.CompletedAt); err == nil {
			completed := t.UTC()
			state.CompletedAt = &completed
		}
	}

	if dep.ErrorMessage != "" {
		reason := shortReason(dep.ErrorMessage)
		state.ErrorReason = &reason
	}

	state.SourceDeploy = ids.ref(ctx, dep.SourceDeploymentId)

	if git := dep.GitInfo; git.Sha != "" || git.Branch != "" || git.Repository != "" {
		state.Git = &DeployGit{
			SHA:        git.Sha,
			Branch:     git.Branch,
			Repository: git.Repository,
		}
	}

	return state, true
}

// shortReason trims a failure message to something that fits the contract.
//
// The marker matters: a reader who sees a reason end mid-sentence should be
// able to tell that cloud has a summary rather than that the deploy failed
// with a truncated message. Cutting on a byte boundary is deliberate too,
// since the point is bounding the payload rather than presenting prose.
func shortReason(msg string) string {
	if len(msg) <= maxErrorReasonBytes {
		return msg
	}
	return msg[:maxErrorReasonBytes] + "… (truncated)"
}

// deployedAtFor returns the moment a deploy was initiated, which must be stable
// across every report of it.
//
// The record's own timestamp is written once at creation and never rewritten,
// so it is the right answer whenever it exists. It does not always exist:
// records written before the server owned the deploy lifecycle (MIR-681) can
// carry an empty one, and a backfill reaches back far enough to meet them.
//
// The id is the fallback because it is the only other immutable thing on the
// record that knows when it was made — entity ids are UUIDv7 under their
// base58, so creation time is recoverable from the identity itself. Deriving it
// twice always gives the same instant, which is the property that matters here.
func deployedAtFor(rec *deploylifecycle.Record) (time.Time, bool) {
	if ts := rec.Deployment.DeployedBy.Timestamp; ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.UTC(), true
		}
	}

	return idgen.TimeOf(string(rec.Deployment.ID))
}

// entityDeploySource reads deploys from the entity store.
//
// It carries no logger on purpose: every method returns its error to the
// reporter, which is the layer that decides how loud a failure to report
// deploys should be.
type entityDeploySource struct {
	eac *esv1.EntityAccessClient
}

// HeadRevision reads the store's current revision without listing the deploy
// log.
//
// It lists the in-progress index to get there, which sounds indirect but is
// the cheap way to ask: a list returns the store's global read revision
// whatever index it was given, and this index holds at most one entry per app
// rather than one per deploy ever run. Listing the deploy kind instead would
// mean paying for the full history on every connect to learn one number — the
// exact cost the watermark exists to avoid.
func (s *entityDeploySource) HeadRevision(ctx context.Context) (int64, error) {
	res, err := s.eac.List(ctx, entity.String(
		core_v1alpha.DeploymentStatusId, string(deploylifecycle.StatusInProgress)))
	if err != nil {
		return 0, err
	}
	return res.Revision(), nil
}

func (s *entityDeploySource) List(ctx context.Context) ([]*deploylifecycle.Record, int64, error) {
	res, err := s.eac.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment))
	if err != nil {
		return nil, 0, err
	}

	records := make([]*deploylifecycle.Record, 0, len(res.Values()))
	for _, ent := range res.Values() {
		records = append(records, deploylifecycle.RecordFrom(ent))
	}

	return records, res.Revision(), nil
}

func (s *entityDeploySource) ShortID(ctx context.Context, entityID string) string {
	resp, err := s.eac.Get(ctx, entityID)
	if err != nil {
		// Swallowed rather than reported, per the interface: an entity pruned
		// between the deploy and this read is the ordinary case, not a fault,
		// and the caller has nothing better to do with either.
		return ""
	}
	return shortIDOf(resp.Entity())
}

func (s *entityDeploySource) ShortIDsForKind(ctx context.Context, kind entity.Id) map[string]string {
	res, err := s.eac.List(ctx, entity.Ref(entity.EntityKind, kind))
	if err != nil {
		// An empty map degrades to per-id lookups rather than to missing short
		// ids, so a failure here costs reads and never correctness.
		return nil
	}

	short := make(map[string]string, len(res.Values()))
	for _, ent := range res.Values() {
		if id, sid := shortIDEntry(ent); id != "" {
			short[id] = sid
		}
	}
	return short
}

// shortIDEntry reads the identity and short id off a listed entity.
//
// The id comes from the entity's own field rather than from a db/id attribute,
// which is not the same string. Attribute values render through Value.String,
// and that prefixes an id-kinded value with its kind ("id: app_version/…"), so
// keying the map on one would build a map that ref can never hit — every
// lookup would miss and fall through to the per-id read the bulk read exists to
// avoid, silently and with nothing failing.
func shortIDEntry(ent *esv1.Entity) (id, shortID string) {
	if ent == nil {
		return "", ""
	}
	return ent.Id(), shortIDOf(ent)
}

// shortIDOf reads the db/short-id attribute off an RPC entity, or "" when it
// carries none. That attribute is string-kinded, so String is the right reader
// for it.
func shortIDOf(ent *esv1.Entity) string {
	if ent == nil {
		return ""
	}
	for _, attr := range ent.Attrs() {
		if entity.Id(attr.ID) == entity.DBShortId {
			return attr.Value.String()
		}
	}
	return ""
}

func (s *entityDeploySource) Watch(ctx context.Context, fromRevision int64, fn func(*esv1.EntityOp) error) error {
	_, err := s.eac.WatchIndex(ctx,
		entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment),
		fromRevision,
		stream.Callback(fn))
	return err
}
