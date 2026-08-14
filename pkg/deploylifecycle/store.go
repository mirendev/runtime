package deploylifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// pendingBuildSentinel and the failed-<id> pattern are values older clients wrote
// into app_version before a build had produced one. The field is optional now, so
// they are normalized away on read rather than migrated.
const pendingBuildSentinel = "pending-build"

// Record is a deployment entity together with what a caller needs to write it
// back: the revision it was read at, and the raw entity for short-id rendering.
type Record struct {
	Deployment *core_v1alpha.Deployment
	Entity     *entityserver_v1alpha.Entity
	Revision   int64
}

// Status returns the record's status as a typed value.
func (r *Record) Status() Status {
	return Status(r.Deployment.Status)
}

// AppVersion returns the recorded app version, with legacy placeholder values
// reported as empty — they never named a real version.
func (r *Record) AppVersion() string {
	version := r.Deployment.AppVersion
	if version == pendingBuildSentinel || isFailedSentinel(version, string(r.Deployment.ID)) {
		return ""
	}
	return version
}

func isFailedSentinel(version, deploymentID string) bool {
	return deploymentID != "" && version == "failed-"+deploymentID
}

// Query selects deployments. An empty field means "any".
//
// There is deliberately no cluster filter: a coordinator's store only holds its
// own cluster's deployments, and the client-supplied cluster_id is unreliable,
// so filtering on it would hide legitimate deploys (see MIR-1465).
type Query struct {
	AppName string
	Status  Status

	// Limit caps the result after sorting newest-first. Zero means no cap.
	Limit int
}

// Store reads and writes deployment records.
//
// Every query goes through an index when the filters allow one. The previous
// implementation listed every deployment ever created and filtered in memory on
// each history query, lock check, and activation.
type Store struct {
	eac *entityserver_v1alpha.EntityAccessClient
	log *slog.Logger

	// now is a test seam; the zero value means time.Now.
	now clockFn
}

func NewStore(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Store {
	return &Store{
		eac: eac,
		log: log.With("module", "deploylifecycle.store"),
	}
}

// Get reads one deployment record.
func (s *Store) Get(ctx context.Context, deploymentID string) (*Record, error) {
	res, err := s.eac.Get(ctx, deploymentID)
	if err != nil {
		return nil, err
	}

	return recordFrom(res.Entity()), nil
}

// Status reports just the status of a deployment, and satisfies StatusLookup so
// the lock manager can tell a live holder from a finished one.
func (s *Store) Status(ctx context.Context, deploymentID string) (Status, error) {
	rec, err := s.Get(ctx, deploymentID)
	if err != nil {
		return "", err
	}
	return rec.Status(), nil
}

// List returns matching deployments, newest first.
func (s *Store) List(ctx context.Context, q Query) ([]*Record, error) {
	res, err := s.eac.List(ctx, s.index(q))
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	records := make([]*Record, 0, len(res.Values()))
	for _, e := range res.Values() {
		rec := recordFrom(e)
		if !q.matches(rec.Deployment) {
			continue
		}
		records = append(records, rec)
	}

	// Timestamps are RFC3339, so lexical order is chronological order.
	sort.Slice(records, func(i, j int) bool {
		return records[i].Deployment.DeployedBy.Timestamp > records[j].Deployment.DeployedBy.Timestamp
	})

	if q.Limit > 0 && len(records) > q.Limit {
		records = records[:q.Limit]
	}

	return records, nil
}

// liveStatuses are the statuses whose index is bounded by how many apps exist
// rather than by how many deploys have ever run: the deploy lock allows one
// in-flight deployment per app, and activation settles the previous active one.
// Every other status accumulates forever.
var liveStatuses = map[Status]struct{}{
	StatusInProgress: {},
	StatusActive:     {},
}

// index picks the narrowest index the query's filters allow. Filters it does not
// cover are still applied in memory, but over a far smaller set than a kind scan.
//
// A live status beats the app index because the two grow along different axes.
// Activation asks for this app's active deployments on every single deploy: the
// app index returns everything that app has ever deployed, while the active
// index returns roughly one row per app no matter how long the app has been
// around. For a settled status the comparison flips, since those do grow without
// bound, so the app index stays the better choice there.
func (s *Store) index(q Query) entity.Attr {
	statusIndex := entity.String(core_v1alpha.DeploymentStatusId, string(q.Status))

	if _, live := liveStatuses[q.Status]; live {
		return statusIndex
	}

	switch {
	case q.AppName != "":
		return entity.String(core_v1alpha.DeploymentAppNameId, q.AppName)
	case q.Status != "":
		return statusIndex
	default:
		return entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment)
	}
}

func (q Query) matches(dep *core_v1alpha.Deployment) bool {
	if q.AppName != "" && dep.AppName != q.AppName {
		return false
	}
	if q.Status != "" && dep.Status != string(q.Status) {
		return false
	}
	return true
}

// Put writes a record back under its revision, so a concurrent writer causes a
// conflict rather than a silent overwrite.
func (s *Store) Put(ctx context.Context, rec *Record) error {
	ent := &entityserver_v1alpha.Entity{}
	ent.SetId(string(rec.Deployment.ID))
	ent.SetAttrs(rec.Deployment.Encode())
	ent.SetRevision(rec.Revision)

	if _, err := s.eac.Put(ctx, ent); err != nil {
		return fmt.Errorf("failed to write deployment %s: %w", rec.Deployment.ID, err)
	}

	return nil
}

// Create writes a new deployment record and returns it with its assigned id.
func (s *Store) Create(ctx context.Context, dep *core_v1alpha.Deployment) (*Record, error) {
	ent := &entityserver_v1alpha.Entity{}
	ent.SetAttrs(dep.Encode())

	res, err := s.eac.Put(ctx, ent)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}

	dep.ID = entity.Id(res.Id())

	// Read back so the caller holds a revision it can write against.
	return s.Get(ctx, res.Id())
}

// Delete removes a deployment record.
func (s *Store) Delete(ctx context.Context, deploymentID string) error {
	if _, err := s.eac.Delete(ctx, deploymentID); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return fmt.Errorf("failed to delete deployment %s: %w", deploymentID, err)
	}
	return nil
}

// MarkPreviousActiveAs moves the deployments that were active for this app into
// a settled status, leaving the incoming deployment alone.
func (s *Store) MarkPreviousActiveAs(ctx context.Context, appName, exceptID string, target Status) error {
	previous, err := s.List(ctx, Query{
		AppName: appName,
		Status:  StatusActive,
	})
	if err != nil {
		return err
	}

	for _, rec := range previous {
		if string(rec.Deployment.ID) == exceptID {
			continue
		}

		if err := Transition(rec.Status(), target); err != nil {
			s.log.Warn("skipping previous deployment that cannot settle",
				"deployment_id", rec.Deployment.ID, "from", rec.Status(), "to", target, "error", err)
			continue
		}

		rec.Deployment.Status = string(target)
		if rec.Deployment.CompletedAt == "" {
			rec.Deployment.CompletedAt = s.now.Now().Format(time.RFC3339)
		}

		if err := s.Put(ctx, rec); err != nil {
			// One superseded record failing to settle must not fail the deploy
			// that superseded it; it is bookkeeping, and the next activation
			// will try again.
			s.log.Error("failed to settle previous active deployment",
				"deployment_id", rec.Deployment.ID, "target", target, "error", err)
			continue
		}

		s.log.Info("settled previous active deployment",
			"deployment_id", rec.Deployment.ID, "status", target, "app", appName)
	}

	return nil
}

// RecordFrom decodes a stored entity into a Record.
//
// Exported for readers that get entities from somewhere other than this store —
// the changefeed hands out entities directly, and a caller decoding them by
// hand would be a second copy of the mapping that drifts from this one.
func RecordFrom(e *entityserver_v1alpha.Entity) *Record {
	return recordFrom(e)
}

func recordFrom(e *entityserver_v1alpha.Entity) *Record {
	var dep core_v1alpha.Deployment
	dep.Decode(&entityAttrs{entity: e})

	return &Record{
		Deployment: &dep,
		Entity:     e,
		Revision:   e.Revision(),
	}
}
