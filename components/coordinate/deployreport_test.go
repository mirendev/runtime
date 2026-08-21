package coordinate

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// The four cases RFD-94 says must discard the cursor. Every one of them costs a
// re-list and nothing else, which is the property that lets the cursor be a
// pure optimization.
func TestDecideSyncModeFallsBackWhenTheCursorIsUnusable(t *testing.T) {
	cases := []struct {
		name      string
		watermark int64
		head      int64
		want      syncMode
	}{
		{"absent, first contact or discarded", 0, 5000, syncRelist},
		{"ahead of head, a rebuilt etcd reset the sequence", 9000, 5000, syncRelist},
		{"not a revision any store issued", -1, 5000, syncRelist},
		{"usable", 4000, 5000, syncDelta},
		{"usable at exactly head", 5000, 5000, syncDelta},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := decideSyncMode(tc.watermark, tc.head)
			assert.Equal(t, tc.want, got)
			if tc.want == syncRelist {
				assert.NotEmpty(t, reason, "a re-list should say why, since it is the expensive path")
			}
		})
	}
}

// The fourth case, compaction, is the one that cannot be decided up front: it
// only surfaces when the watch is opened and the store reports the resume point
// is gone. It has to demote a delta stream to a re-list mid-flight.
func TestCompactedWatchFallsBackToRelist(t *testing.T) {
	src := &fakeDeploySource{
		head: 5000,
		records: []*deploylifecycle.Record{
			deployRecord(t, "web", 100),
		},
		listRevision: 5000,
		watchErr:     map[int64]error{4000: errCompacted},
	}

	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 4000)

	assert.True(t, src.listed, "a compacted watch must fall back to the full re-list")
	require.NotEmpty(t, link.batches())
	assert.Equal(t, int64(0), link.batches()[0].FromRevision,
		"the re-list restarts the frontier from the bottom rather than from the dead cursor")
}

// A usable cursor means catch-up and live tail are one ordered stream, so the
// runtime never lists at all. That is the entire point of keeping the cursor.
func TestUsableCursorStreamsWithoutListing(t *testing.T) {
	src := &fakeDeploySource{
		head: 5000,
		watchOps: []*esv1.EntityOp{
			deployOp(t, "web", 5100),
			deployOp(t, "api", 5200),
		},
	}

	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 4000)

	assert.False(t, src.listed, "a usable cursor must not trigger a re-list")

	batches := link.batches()
	require.Len(t, batches, 2)
	assert.Equal(t, int64(4000), batches[0].FromRevision)
	assert.Equal(t, int64(5100), batches[0].ToRevision)
	assert.Equal(t, int64(5100), batches[1].FromRevision,
		"each batch must pick up exactly where the last one closed, or the range between them is claimed by nobody")
	assert.Equal(t, int64(5200), batches[1].ToRevision)
}

// A watch that reports progress with no entity still advances the range. This
// is what keeps the frontier moving on a cluster that deploys rarely, instead of
// freezing at the last deploy and re-walking a widening gap on every connect.
func TestProgressNotificationAdvancesTheFrontier(t *testing.T) {
	progress := &esv1.EntityOp{}
	progress.SetOperation(int64(esv1.EntityOperationProgress))
	progress.SetRevision(6000)

	src := &fakeDeploySource{head: 5000, watchOps: []*esv1.EntityOp{progress}}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 4000)

	batches := link.batches()
	require.Len(t, batches, 1)
	assert.Empty(t, batches[0].Deploys, "a progress notification carries no deploy")
	assert.Equal(t, int64(4000), batches[0].FromRevision)
	assert.Equal(t, int64(6000), batches[0].ToRevision,
		"the empty batch still has to move the frontier, or a quiet cluster never advances")
}

// A stream failure that is not compaction must not trigger a re-list. The
// cursor is not implicated by a transport blip, and re-sending a cluster's whole
// history on one is expensive for nothing.
func TestNonCompactionStreamErrorDoesNotRelist(t *testing.T) {
	src := &fakeDeploySource{
		head:         5000,
		records:      []*deploylifecycle.Record{deployRecord(t, "web", 100)},
		listRevision: 5000,
		watchErr:     map[int64]error{4000: io.ErrUnexpectedEOF},
	}

	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 4000)

	assert.False(t, src.listed, "only compaction should fall back to a re-list")
	assert.Empty(t, link.batches())
}

// Batches have to tile the revision line with no gaps and no overlaps in the
// ranges they claim, because cloud advances its frontier to each ToRevision in
// turn. A gap between two batches is a range cloud believes was covered.
func TestRelistBatchesTileTheRevisionRange(t *testing.T) {
	var records []*deploylifecycle.Record
	for i := range 250 {
		records = append(records, deployRecord(t, "web", int64(1000+i)))
	}

	src := &fakeDeploySource{head: 9000, records: records, listRevision: 9000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	batches := link.batches()
	require.Len(t, batches, 3, "250 deploys at a batch size of 100")

	from := int64(0)
	total := 0
	for _, b := range batches {
		assert.Equal(t, from, b.FromRevision, "batches must tile without a gap")
		assert.Greater(t, b.ToRevision, b.FromRevision)
		for _, d := range b.Deploys {
			assert.Greater(t, d.Revision, b.FromRevision, "a deploy must fall inside the range that vouches for it")
			assert.LessOrEqual(t, d.Revision, b.ToRevision)
		}
		total += len(b.Deploys)
		from = b.ToRevision
	}

	assert.Equal(t, 250, total)
	assert.Equal(t, int64(9000), from,
		"the last batch closes at the read revision, since the listing proved everything up to there")
}

// An interrupted backfill has to resume from the frontier its committed batches
// left behind, without re-walking what landed and without skipping the rest.
// This is the resumability the issue asks for, seen from the runtime side.
func TestInterruptedBackfillResumesFromTheWatermark(t *testing.T) {
	var records []*deploylifecycle.Record
	for i := range 250 {
		records = append(records, deployRecord(t, "web", int64(1000+i)))
	}

	// First connection: dies after two batches, the way a dropped socket would.
	first := &fakeDeployLink{failAfter: 2}
	src := &fakeDeploySource{head: 9000, records: records, listRevision: 9000}
	runWithCursor(t, src, first, 0)

	delivered := first.batches()
	require.Len(t, delivered, 2)
	resumeFrom := delivered[len(delivered)-1].ToRevision

	// Cloud committed those two batches, so its watermark is where they closed.
	// The next connection asks and gets that back.
	// The watch replays what the interrupted backfill never sent, then carries
	// on into new activity. Both have to arrive, or "resumed" would only mean
	// "started in the right place" while the middle of the log stayed missing.
	remaining := []*esv1.EntityOp{
		deployOp(t, "web", resumeFrom+1),
		deployOp(t, "web", resumeFrom+2),
		deployOp(t, "api", 9100),
	}

	second := &fakeDeployLink{}
	src2 := &fakeDeploySource{
		head:         9000,
		records:      records,
		listRevision: 9000,
		watchOps:     remaining,
	}
	runWithCursor(t, src2, second, resumeFrom)

	assert.False(t, src2.listed,
		"resuming from a good watermark must stream, not start the backfill over")

	resumed := second.batches()
	require.Len(t, resumed, len(remaining))
	assert.Equal(t, resumeFrom, resumed[0].FromRevision,
		"the resumed stream must open exactly at the frontier the interrupted one left")

	// No gap between where the first connection stopped and where the second
	// picked up, and none between the batches that followed.
	from := resumeFrom
	for i, b := range resumed {
		assert.Equal(t, from, b.FromRevision, "batch %d must open where the last one closed", i)
		require.Len(t, b.Deploys, 1)
		assert.Equal(t, remaining[i].Revision(), b.Deploys[0].Revision)
		from = b.ToRevision
	}
	assert.Equal(t, int64(9100), from)
}

// Build logs are on the deployment entity and must never reach the wire. This
// is a custody boundary rather than a preference, so it gets a test that fails
// loudly if someone later swaps the hand-written projection for an encode.
func TestBuildLogsAndDeployerNeverReachTheWire(t *testing.T) {
	// A distinct sentinel per field. Sharing one would let a field leak
	// undetected whenever another field carrying the same substring is
	// correctly excluded.
	rec := deployRecord(t, "web", 100)
	rec.Deployment.BuildLogs = "SENTINEL-BUILD-LOGS line one\nline two"
	rec.Deployment.DeployedBy.UserEmail = "SENTINEL-DEPLOYER-EMAIL@example.com"
	rec.Deployment.DeployedBy.UserName = "SENTINEL-DEPLOYER-NAME"
	rec.Deployment.DeployedBy.UserId = "SENTINEL-DEPLOYER-ID"
	rec.Deployment.GitInfo.Author = "SENTINEL-GIT-AUTHOR"
	rec.Deployment.GitInfo.CommitAuthorEmail = "SENTINEL-GIT-EMAIL@example.com"
	rec.Deployment.GitInfo.Message = "SENTINEL-GIT-MESSAGE"

	src := &fakeDeploySource{head: 5000, records: []*deploylifecycle.Record{rec}, listRevision: 5000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	raw := link.raw()
	for _, sentinel := range []string{
		"SENTINEL-BUILD-LOGS",
		"SENTINEL-DEPLOYER-EMAIL",
		"SENTINEL-DEPLOYER-NAME",
		"SENTINEL-DEPLOYER-ID",
		"SENTINEL-GIT-AUTHOR",
		"SENTINEL-GIT-EMAIL",
		"SENTINEL-GIT-MESSAGE",
	} {
		assert.NotContains(t, raw, sentinel)
	}
}

// A deploy whose record carries no creation timestamp still has one in its id.
// Without this, pre-MIR-681 records would either be dropped or, far worse, get
// a fresh timestamp on every report and write a new row each time.
func TestDeployedAtFallsBackToTheIdWhenTheRecordHasNoTimestamp(t *testing.T) {
	rec := deployRecord(t, "web", 100)
	rec.Deployment.DeployedBy.Timestamp = ""

	first, ok := deployedAtFor(rec)
	require.True(t, ok, "an id-derived time should be available")

	fromID, ok := idgen.TimeOf(string(rec.Deployment.ID))
	require.True(t, ok)
	assert.Equal(t, fromID, first,
		"the fallback should be the id's own creation time, not some other clock reading")

	again, ok := deployedAtFor(rec)
	require.True(t, ok)
	assert.Equal(t, first, again,
		"the value is a partition key, so deriving it twice must give the same instant")
}

// A record with neither a timestamp nor a readable id has no stable partition
// key, and inventing one would write a duplicate row per report. Dropping it is
// the honest outcome.
func TestDeployWithNoStableTimeIsSkipped(t *testing.T) {
	rec := &deploylifecycle.Record{
		Deployment: &core_v1alpha.Deployment{ID: entity.Id("not-an-id"), AppName: "web"},
		Revision:   100,
	}

	src := &fakeDeploySource{head: 5000, records: []*deploylifecycle.Record{rec}, listRevision: 5000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	for _, b := range link.batches() {
		assert.Empty(t, b.Deploys, "a deploy with no stable creation time must not be reported")
	}
}

// A cluster that has never deployed still has to move cloud's frontier, or
// every reconnect re-lists nothing and cloud never learns it is current.
func TestEmptyClusterStillAdvancesTheFrontier(t *testing.T) {
	src := &fakeDeploySource{head: 5000, listRevision: 5000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	batches := link.batches()
	require.Len(t, batches, 1)
	assert.Empty(t, batches[0].Deploys)
	assert.Equal(t, int64(5000), batches[0].ToRevision)
}

// A cloud that holds the socket but never answers must not wedge reporting. It
// is treated as a cloud with no cursor, which costs a re-list and no data.
func TestSilentCloudFallsBackToRelist(t *testing.T) {
	src := &fakeDeploySource{
		head:         5000,
		records:      []*deploylifecycle.Record{deployRecord(t, "web", 100)},
		listRevision: 5000,
	}
	// No onHello, so the hello goes unanswered.
	link := &fakeDeployLink{}
	r := newTestReporter(src)
	r.cursorTimeout = 50 * time.Millisecond
	r.run(testCtx(t), link)

	assert.True(t, src.listed, "an unanswered hello must fall back to the re-list")
}

// --- helpers ---

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

// newTestReporter builds a reporter with the pacing collapsed, so a test
// exercises the protocol rather than waiting out a fleet spread.
func newTestReporter(src deploySource) *deployReporter {
	return &deployReporter{
		log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		source:        src,
		cursors:       make(chan int64, 1),
		spread:        time.Nanosecond,
		cursorTimeout: 5 * time.Second,
		batchPause:    time.Nanosecond,
	}
}

// runWithCursor runs one connection against a cloud that answers the hello with
// the given watermark.
//
// The answer is delivered in response to the hello rather than queued up front,
// because the reporter deliberately drains stale replies before asking. Seeding
// the channel early would test a sequence that never happens on a real link.
func runWithCursor(t *testing.T, src deploySource, link *fakeDeployLink, watermark int64) *deployReporter {
	t.Helper()

	r := newTestReporter(src)
	link.onHello = func() { r.cursors <- watermark }
	r.run(testCtx(t), link)

	return r
}

func deployRecord(t *testing.T, app string, revision int64) *deploylifecycle.Record {
	t.Helper()

	dep := &core_v1alpha.Deployment{
		ID:      entity.Id(idgen.GenNS("deployment")),
		AppName: app,
		// A real version reference, so tests that do not care about versions
		// still model a shape a cluster can actually report. "v1.0.0" was never
		// one, and a fixture asserting an impossible value is how a rendering
		// bug looks correct under test.
		AppVersion: "app_version/" + app + "-vCZ1eUgSgNd28ed6vt2DgY",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}

	// The store hands back records with their entity attached, and the reporter
	// now leans on that to avoid re-reading the deployment kind. A fixture
	// without one would leave that path untested and quietly passing.
	ent := &esv1.Entity{}
	ent.SetId(string(dep.ID))
	ent.SetAttrs(dep.Encode())
	ent.SetRevision(revision)

	return &deploylifecycle.Record{Deployment: dep, Entity: ent, Revision: revision}
}

func deployOp(t *testing.T, app string, revision int64) *esv1.EntityOp {
	t.Helper()

	rec := deployRecord(t, app, revision)
	ent := rec.Entity

	op := &esv1.EntityOp{}
	op.SetOperation(int64(esv1.EntityOperationUpdate))
	op.SetRevision(revision)
	op.SetEntity(ent)

	return op
}

type fakeDeploySource struct {
	head         int64
	records      []*deploylifecycle.Record
	listRevision int64
	watchOps     []*esv1.EntityOp
	watchErr     map[int64]error

	// shortIDs is the store's view of every entity's short id, and byKind says
	// which of them a bulk read for a kind would return. An id present in the
	// first but not the second is one only a per-id lookup can find, which is
	// how a version created after the backfill's bulk read is modelled.
	shortIDs map[string]string
	byKind   map[entity.Id][]string

	listed   bool
	getCalls []string
	kindList []entity.Id
}

func (f *fakeDeploySource) HeadRevision(context.Context) (int64, error) {
	return f.head, nil
}

func (f *fakeDeploySource) List(context.Context) ([]*deploylifecycle.Record, int64, error) {
	f.listed = true
	return f.records, f.listRevision, nil
}

func (f *fakeDeploySource) ShortID(_ context.Context, entityID string) string {
	f.getCalls = append(f.getCalls, entityID)
	return f.shortIDs[entityID]
}

func (f *fakeDeploySource) ShortIDsForKind(_ context.Context, kind entity.Id) map[string]string {
	f.kindList = append(f.kindList, kind)

	out := make(map[string]string)
	for _, id := range f.byKind[kind] {
		out[id] = f.shortIDs[id]
	}
	return out
}

func (f *fakeDeploySource) Watch(_ context.Context, from int64, fn func(*esv1.EntityOp) error) error {
	if err, ok := f.watchErr[from]; ok {
		return err
	}

	for _, op := range f.watchOps {
		if op.Revision() <= from {
			continue
		}
		if err := fn(op); err != nil {
			return err
		}
	}

	return nil
}

// fakeDeployLink records what the reporter put on the wire, and can fail
// partway to stand in for a connection that dropped mid-backfill.
type fakeDeployLink struct {
	mu   sync.Mutex
	sent []json.RawMessage

	// failAfter makes the nth send onward fail, zero meaning never.
	failAfter int
	count     int

	// onHello stands in for cloud answering the sync hello. Nil models a cloud
	// that holds the socket and never replies.
	onHello func()
}

func (f *fakeDeployLink) SendMessageBlocking(_ context.Context, msgType string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if msgType == TypeDeploySyncHello {
		if f.onHello != nil {
			f.onHello()
		}
		return nil
	}

	if msgType != TypeDeployBatch {
		return nil
	}

	f.count++
	if f.failAfter > 0 && f.count > f.failAfter {
		return io.ErrClosedPipe
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	f.sent = append(f.sent, raw)

	return nil
}

func (f *fakeDeployLink) batches() []DeployBatch {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]DeployBatch, 0, len(f.sent))
	for _, raw := range f.sent {
		var b DeployBatch
		if err := json.Unmarshal(raw, &b); err == nil {
			out = append(out, b)
		}
	}
	return out
}

func (f *fakeDeployLink) raw() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var all string
	for _, raw := range f.sent {
		all += string(raw)
	}
	return all
}

// cloudReadLimitBytes mirrors the read limit cloud sets on the channel
// (maxMessageBytes in mirendev/cloud, services/cluster_channel). It is
// duplicated here because this module cannot import that one, which is the
// same hand-matched-constant problem MIR-1601 exists to fix. Until it does,
// this test is what keeps the two honest.
const cloudReadLimitBytes = 1 << 20

// A full batch has to fit in one websocket frame, and nothing in the type
// system says so.
//
// This is a regression test for a real wedge. coder/websocket defaults to a 32
// KiB read limit, a hundred deploys carrying git provenance run past 45 KiB, and
// the failure is not a dropped batch: the read fails, the connection drops, the
// runtime reconnects and re-sends the same oversized batch forever. A cluster
// with more than about eighty deploys would never have ingested one.
func TestFullBatchFitsInOneFrame(t *testing.T) {
	now := time.Now().UTC()

	// The worst record this reporter can emit: every optional field populated,
	// a full-length sha, and a failure reason at the cap.
	worst := func() DeployState {
		completed := now
		reason := shortReason(strings.Repeat("x", maxErrorReasonBytes*4))
		return DeployState{
			ID:       idgen.GenNS("deployment"),
			Revision: 5100294,
			AppName:  "some-reasonably-long-app-name",
			Version: &EntityRef{
				ID:      "app_version/some-reasonably-long-app-name-vCZ1eUgSgNd28ed6vt2DgY",
				ShortID: "8kd0",
			},
			Status:      "failed",
			Phase:       "activating",
			DeployedAt:  now,
			CompletedAt: &completed,
			ErrorReason: &reason,
			Git: &DeployGit{
				SHA:        strings.Repeat("a1b2c3d4", 5),
				Branch:     "release/some-fairly-long-branch-name",
				Repository: "https://github.com/mirendev/some-repository-name",
			},
			SourceDeploy: &EntityRef{
				ID:      idgen.GenNS("deployment"),
				ShortID: "2DgY",
			},
		}
	}

	batch := DeployBatch{
		FromRevision: 1,
		ToRevision:   5100294,
		HeadRevision: 5100300,
		ObservedAt:   now,
	}
	for range deployBatchSize {
		batch.Deploys = append(batch.Deploys, worst())
	}

	raw, err := json.Marshal(batch)
	require.NoError(t, err)

	assert.Less(t, len(raw), cloudReadLimitBytes,
		"a full batch of worst-case records must fit the frame cloud will read; "+
			"lower deployBatchSize or maxErrorReasonBytes, or raise cloud's limit")
}

// The cap is what makes the frame bound reachable, and it has to be stable:
// a reason that varied per report would write a second row rather than update
// the first, the same way a drifting deployed_at would.
func TestShortReasonCapsAndIsStable(t *testing.T) {
	long := strings.Repeat("boom ", 500)

	first := shortReason(long)
	assert.LessOrEqual(t, len(first), maxErrorReasonBytes+len("… (truncated)"))
	assert.Contains(t, first, "truncated", "a trimmed reason should say so")
	assert.Equal(t, first, shortReason(long), "trimming must be deterministic")

	short := "build failed: exit code 1"
	assert.Equal(t, short, shortReason(short), "a reason within the cap is untouched")
}

// A deploy the reporter cannot key by creation time still has to let the
// frontier past it.
//
// The empty batch this produces means "a deploy was here and is not coming,"
// not "nothing happened." Holding the range back instead would wedge the
// watermark permanently: the next connection re-streams the same revision and
// drops it again, forever.
func TestUnkeyableDeployInTheTailStillAdvancesTheFrontier(t *testing.T) {
	// An id base58 cannot decode, and no timestamp on the record, so neither
	// source of a stable deployed_at is available.
	rec := &core_v1alpha.Deployment{ID: entity.Id("not-an-id"), AppName: "web"}

	ent := &esv1.Entity{}
	ent.SetId(string(rec.ID))
	ent.SetAttrs(rec.Encode())
	ent.SetRevision(5100)

	op := &esv1.EntityOp{}
	op.SetOperation(int64(esv1.EntityOperationUpdate))
	op.SetRevision(5100)
	op.SetEntity(ent)

	src := &fakeDeploySource{head: 5000, watchOps: []*esv1.EntityOp{op}}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 4000)

	batches := link.batches()
	require.Len(t, batches, 1)
	assert.Empty(t, batches[0].Deploys, "the deploy had no stable key, so it is not reported")
	assert.Equal(t, int64(4000), batches[0].FromRevision)
	assert.Equal(t, int64(5100), batches[0].ToRevision,
		"the frontier must still pass it, or this revision wedges the watermark forever")
}

// The whole point of MIR-1615: a deploy report has to carry the short id the
// console shows, not just the entity id it was already sending. The short id
// lives on the app_version entity rather than on the deploy, so nothing reaches
// the wire unless the reporter goes and joins it.
func TestVersionShortIdReachesTheWire(t *testing.T) {
	rec := deployRecord(t, "web", 100)
	rec.Deployment.AppVersion = "app_version/web-vCZ1eUgSgNd28ed6vt2DgY"

	src := &fakeDeploySource{
		head:         5000,
		records:      []*deploylifecycle.Record{rec},
		listRevision: 5000,
		shortIDs:     map[string]string{"app_version/web-vCZ1eUgSgNd28ed6vt2DgY": "8kd0"},
		byKind: map[entity.Id][]string{
			core_v1alpha.KindAppVersion: {"app_version/web-vCZ1eUgSgNd28ed6vt2DgY"},
		},
	}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	state := onlyDeploy(t, link)
	require.NotNil(t, state.Version)
	assert.Equal(t, "8kd0", state.Version.ShortID)
	assert.Equal(t, "app_version/web-vCZ1eUgSgNd28ed6vt2DgY", state.Version.ID,
		"the durable id travels alongside: short ids are cluster-scoped and are "+
			"freed for reuse when their entity is pruned, so cloud cannot key on one")
}

// A version the runtime can no longer resolve still has to be reported. The
// deploy happened, and the id it names is the honest thing to show — a reader
// falls back to it with the kind prefix stripped, exactly as the CLI does.
func TestDeployIsStillReportedWhenTheVersionHasNoShortId(t *testing.T) {
	rec := deployRecord(t, "web", 100)
	rec.Deployment.AppVersion = "app_version/web-vGone"

	// Nothing registered: the version entity was pruned before this ran.
	src := &fakeDeploySource{head: 5000, records: []*deploylifecycle.Record{rec}, listRevision: 5000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	state := onlyDeploy(t, link)
	require.NotNil(t, state.Version, "an unresolvable short id must not erase the version")
	assert.Equal(t, "app_version/web-vGone", state.Version.ID)
	assert.Empty(t, state.Version.ShortID)
}

// A deploy that failed before a build produced a version has no version to
// name. Absent says that; an empty string would be a value a reader has to
// recognize as meaning nothing.
func TestDeployWithNoVersionReportsNoVersionAtAll(t *testing.T) {
	rec := deployRecord(t, "web", 100)
	rec.Deployment.AppVersion = ""

	src := &fakeDeploySource{head: 5000, records: []*deploylifecycle.Record{rec}, listRevision: 5000}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	state := onlyDeploy(t, link)
	assert.Nil(t, state.Version)
	assert.Nil(t, state.SourceDeploy, "a fresh build was based on nothing")
	assert.Contains(t, link.raw(), `"version":null`,
		"absent has to survive serialization, since that is what cloud reads")
}

// A backfill walks every deploy a cluster has ever run. Resolving short ids one
// at a time there would be a read per deploy against a store that can answer
// for a whole kind at once, which is the difference between a backfill that is
// a slide and one that is a load.
func TestRelistResolvesShortIdsInBulkRatherThanPerDeploy(t *testing.T) {
	var records []*deploylifecycle.Record
	byKind := map[entity.Id][]string{}
	shortIDs := map[string]string{}

	for i := range 50 {
		rec := deployRecord(t, "web", int64(100+i))
		// Ten versions across fifty deploys: rollbacks and redeploys ship a
		// version that already shipped, so ids repeat by design.
		version := "app_version/web-v" + string(rune('a'+i%10))
		rec.Deployment.AppVersion = version
		records = append(records, rec)

		if _, seen := shortIDs[version]; !seen {
			shortIDs[version] = "s" + string(rune('a'+i%10))
			byKind[core_v1alpha.KindAppVersion] = append(byKind[core_v1alpha.KindAppVersion], version)
		}
	}

	src := &fakeDeploySource{
		head: 5000, records: records, listRevision: 5000,
		shortIDs: shortIDs, byKind: byKind,
	}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	assert.Empty(t, src.getCalls,
		"a re-list must not fall back to per-id reads for ids the bulk read already covered")
	assert.Equal(t, []entity.Id{core_v1alpha.KindAppVersion}, src.kindList,
		"versions cost a bulk read because the listing does not carry them; deploys "+
			"must not, since it returned every record with its entity attached, and "+
			"re-reading the largest kind on the backfill path is what this avoids")

	var seen int
	for _, b := range link.batches() {
		for _, d := range b.Deploys {
			require.NotNil(t, d.Version)
			assert.NotEmpty(t, d.Version.ShortID)
			seen++
		}
	}
	assert.Equal(t, 50, seen)
}

// The cache has to remember misses as well as hits. A version pruned before its
// deploy was reported will never resolve, and re-reading it on every batch for
// the life of the connection is the failure this guards.
func TestShortIdCacheRemembersMissesAndHits(t *testing.T) {
	src := &fakeDeploySource{
		shortIDs: map[string]string{"app_version/web-vLive": "8kd0"},
	}
	ids := newShortIDs(src)
	ctx := t.Context()

	for range 3 {
		hit := ids.ref(ctx, "app_version/web-vLive")
		require.NotNil(t, hit)
		assert.Equal(t, "8kd0", hit.ShortID)

		miss := ids.ref(ctx, "app_version/web-vGone")
		require.NotNil(t, miss)
		assert.Empty(t, miss.ShortID)
	}

	assert.Equal(t, []string{"app_version/web-vLive", "app_version/web-vGone"}, src.getCalls,
		"each id is read exactly once, misses included")

	assert.Nil(t, ids.ref(ctx, ""), "nothing to name means no reference at all")
}

// An entity created after the bulk read still has to resolve. The preloaded map
// is an optimization, so a miss in it falls through to a lookup rather than
// standing as an answer.
func TestPreloadDoesNotMaskEntitiesItDidNotCover(t *testing.T) {
	src := &fakeDeploySource{
		shortIDs: map[string]string{
			"app_version/web-vOld": "old0",
			"app_version/web-vNew": "new0",
		},
		// Only the older version existed when the bulk read ran.
		byKind: map[entity.Id][]string{
			core_v1alpha.KindAppVersion: {"app_version/web-vOld"},
		},
	}

	ids := newShortIDs(src)
	ctx := t.Context()
	ids.preload(ctx, core_v1alpha.KindAppVersion)

	assert.Equal(t, "old0", ids.ref(ctx, "app_version/web-vOld").ShortID)
	assert.Empty(t, src.getCalls, "the preloaded id needs no read of its own")

	assert.Equal(t, "new0", ids.ref(ctx, "app_version/web-vNew").ShortID)
	assert.Equal(t, []string{"app_version/web-vNew"}, src.getCalls,
		"an id the bulk read missed falls through to a lookup")
}

// A rollback names the deploy it was based on, and the console wants that
// reference to read the same way the version does.
func TestSourceDeployCarriesItsShortIdToo(t *testing.T) {
	rec := deployRecord(t, "web", 100)
	source := idgen.GenNS("deployment")
	rec.Deployment.SourceDeploymentId = source

	src := &fakeDeploySource{
		head: 5000, records: []*deploylifecycle.Record{rec}, listRevision: 5000,
		shortIDs: map[string]string{source: "r7x2"},
		byKind:   map[entity.Id][]string{core_v1alpha.KindDeployment: {source}},
	}
	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	state := onlyDeploy(t, link)
	require.NotNil(t, state.SourceDeploy)
	assert.Equal(t, source, state.SourceDeploy.ID)
	assert.Equal(t, "r7x2", state.SourceDeploy.ShortID)
}

// onlyDeploy returns the single deploy the link received, failing if the run
// reported any other number.
func onlyDeploy(t *testing.T, link *fakeDeployLink) DeployState {
	t.Helper()

	var found []DeployState
	for _, b := range link.batches() {
		found = append(found, b.Deploys...)
	}

	require.Len(t, found, 1)
	return found[0]
}

// The bulk read has to key on the same string ref looks up, and the two come
// from different places: ref uses the plain entity id off a deploy record,
// while the listing sees whole entities.
//
// Reading the id from a db/id attribute instead of the entity's own field is
// the trap, because attribute values render through Value.String, which
// prefixes an id-kinded value with its kind. That builds a map whose keys never
// match, so every reference misses and falls through to the per-id read the
// bulk read exists to avoid. Nothing errors and nothing fails; a backfill just
// quietly costs a lookup per deploy again.
func TestBulkReadKeysOnThePlainEntityId(t *testing.T) {
	const id = "app_version/web-vCZ1eUgSgNd28ed6vt2DgY"

	ent := &esv1.Entity{}
	ent.SetId(id)
	ent.SetAttrs([]entity.Attr{
		entity.Ref(entity.DBId, entity.Id(id)),
		entity.String(entity.DBShortId, "8kd0"),
	})

	gotID, gotShort := shortIDEntry(ent)
	assert.Equal(t, id, gotID,
		"the key has to be the plain id a deploy record names, with no kind prefix")
	assert.Equal(t, "8kd0", gotShort)

	// The specific way it goes wrong, pinned so the trap stays visible.
	var fromAttr string
	for _, attr := range ent.Attrs() {
		if entity.Id(attr.ID) == entity.DBId {
			fromAttr = attr.Value.String()
		}
	}
	assert.NotEqual(t, id, fromAttr,
		"if this ever starts matching, the comment on shortIDEntry is stale")
}

// A listed entity with no short id yields its id and an empty short id, rather
// than dropping out of the map. The empty value is a real answer — it is what
// makes the cache remember the miss instead of re-reading it every batch.
func TestBulkReadKeepsEntitiesWithNoShortId(t *testing.T) {
	ent := &esv1.Entity{}
	ent.SetId("app_version/web-vNone")

	id, short := shortIDEntry(ent)
	assert.Equal(t, "app_version/web-vNone", id)
	assert.Empty(t, short)
}

// A rollback names a deploy the listing already returned, so its short id costs
// nothing to resolve. Re-listing the deployment kind to learn it would re-read
// a cluster's whole history on the one path where that history is the workload.
func TestSourceDeployResolvesFromTheListingWithoutAnyRead(t *testing.T) {
	source := deployRecord(t, "web", 100)
	rollback := deployRecord(t, "web", 200)
	rollback.Deployment.SourceDeploymentId = string(source.Deployment.ID)

	src := &fakeDeploySource{
		head:         5000,
		records:      []*deploylifecycle.Record{source, rollback},
		listRevision: 5000,
		// Registered so a read would succeed, which is what makes the
		// assertion below mean "never asked" rather than "asked and missed".
		shortIDs: map[string]string{string(source.Deployment.ID): "r7x2"},
	}

	// The store's own short id for the source, as the listing carries it.
	src.records[0].Entity.SetAttrs(append(src.records[0].Entity.Attrs(),
		entity.String(entity.DBShortId, "r7x2")))

	link := &fakeDeployLink{}
	runWithCursor(t, src, link, 0)

	var seen *EntityRef
	for _, b := range link.batches() {
		for _, d := range b.Deploys {
			if d.SourceDeploy != nil {
				seen = d.SourceDeploy
			}
		}
	}

	require.NotNil(t, seen, "the rollback has to report what it was based on")
	assert.Equal(t, "r7x2", seen.ShortID)
	assert.NotContains(t, src.getCalls, string(source.Deployment.ID),
		"the source deploy was in the listing, so resolving it must cost no read")
	assert.NotContains(t, src.kindList, core_v1alpha.KindDeployment,
		"and the deployment kind must not be listed to learn it")
}
