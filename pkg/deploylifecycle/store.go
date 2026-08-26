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
	"miren.dev/runtime/pkg/idgen"
)

// pendingBuildSentinel and the failed-<id> pattern are values older clients wrote
// into app_version before a build had produced one. The field is optional now, so
// they are normalized away on read rather than migrated.
const pendingBuildSentinel = "pending-build"

// admissionReservationStatus is written on the otherwise anonymous entity that
// reserves a deployment ID before lock acquisition. It is deliberately not a
// public lifecycle status: an older runtime only needs to see a non-terminal
// record while the compatibility lock points at the ID. Omitting entity/kind
// and app_name keeps the reservation out of deployment history and migration.
const admissionReservationStatus = "_admitting"

// Record is a deployment entity together with what a caller needs to write it
// back: the revision it was read at, and the raw entity for short-id rendering.
type Record struct {
	Deployment *core_v1alpha.Deployment
	Entity     *entityserver_v1alpha.Entity
	Revision   int64
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
	// LegacyStatus selects the downgrade representation. It is used only for
	// compatibility bookkeeping such as settling the previously active row.
	LegacyStatus Status

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

func (s *Store) AppByName(ctx context.Context, name string) (*core_v1alpha.App, int64, error) {
	res, err := s.eac.Get(ctx, "app/"+name)
	if err != nil {
		return nil, 0, err
	}
	var app core_v1alpha.App
	app.Decode(&entityAttrs{entity: res.Entity()})
	return &app, res.Entity().Revision(), nil
}

func (s *Store) EnsureApp(ctx context.Context, name string) (*core_v1alpha.App, int64, error) {
	app, revision, err := s.AppByName(ctx, name)
	if err == nil {
		return app, revision, nil
	}
	if !errors.Is(err, cond.ErrNotFound{}) {
		return nil, 0, err
	}
	app = &core_v1alpha.App{ID: entity.Id("app/" + name), Project: "project/default"}
	attrs := append(app.Encode(), entity.Ref(entity.DBId, app.ID))
	if _, ensureErr := s.eac.Ensure(ctx, attrs); ensureErr != nil {
		return nil, 0, ensureErr
	}
	return s.AppByName(ctx, name)
}

func (s *Store) AppVersionByID(ctx context.Context, id string) (*core_v1alpha.AppVersion, error) {
	res, err := s.eac.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var version core_v1alpha.AppVersion
	version.Decode(&entityAttrs{entity: res.Entity()})
	return &version, nil
}

// SetActivePointers changes the two app pointers as one entity update. The
// revision guard prevents an unrelated stale app write from silently undoing
// an activation; callers retry after re-reading.
func (s *Store) SetActivePointers(ctx context.Context, appID entity.Id, revision int64, versionID, deploymentID entity.Id) error {
	attrs := []entity.Attr{
		entity.Ref(entity.DBId, appID),
		entity.Ref(core_v1alpha.AppActiveVersionId, versionID),
		entity.Ref(core_v1alpha.AppActiveDeploymentId, deploymentID),
	}
	if _, err := s.eac.Patch(ctx, attrs, revision); err != nil {
		return fmt.Errorf("failed to update active deployment pointers: %w", err)
	}
	return nil
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
		if !q.matches(rec) {
			continue
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt().After(records[j].StartedAt())
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
	switch {
	case q.AppName != "":
		return entity.String(core_v1alpha.DeploymentAppNameId, q.AppName)
	case q.LegacyStatus != "":
		return entity.String(core_v1alpha.DeploymentStatusId, string(q.LegacyStatus))
	case q.Status == StatusInProgress:
		// Canonical outcome is absent while an attempt is running. During the
		// downgrade window, the legacy status remains the narrow index for this
		// compatibility query. Operational reconciliation walks deployment
		// attempts directly, since the app lock is intentionally not a queue.
		return entity.String(core_v1alpha.DeploymentStatusId, string(StatusInProgress))
	case q.Status != "":
		// A settled canonical outcome has no compatibility-safe union with the
		// legacy status index. Scan the kind during the downgrade window so an
		// unmigrated matching record reaches Query.matches instead of disappearing
		// before the canonical-first decoder sees it.
		return entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment)
	default:
		return entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment)
	}
}

func (q Query) matches(rec *Record) bool {
	dep := rec.Deployment
	if q.AppName != "" && dep.AppName != q.AppName {
		return false
	}
	if q.Status != "" && rec.Status() != q.Status.Canonical() {
		return false
	}
	if q.LegacyStatus != "" && dep.Status != string(q.LegacyStatus) {
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

// Create writes a new deployment record. Ensure makes a fantastically unlikely
// ID collision explicit instead of updating an existing attempt.
func (s *Store) Create(ctx context.Context, dep *core_v1alpha.Deployment) (*Record, error) {
	if dep.ID == "" {
		dep.ID = entity.Id(idgen.GenNS("deployment"))
	}
	attrs := append(dep.Encode(), entity.Ref(entity.DBId, dep.ID))
	res, err := s.eac.Ensure(ctx, attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}
	if !res.Created() {
		return nil, cond.Conflict("deployment-id", string(dep.ID))
	}

	// Read back so the caller holds a revision it can write against.
	return s.Get(ctx, string(dep.ID))
}

// ReserveAdmission creates an anonymous placeholder for a deployment ID before
// lock acquisition. During a rolling upgrade, an older runtime treats a legacy
// lock whose deployment record is missing as abandoned and steals it. The
// placeholder closes that publication window without making a losing admission
// appear in deployment history.
func (s *Store) ReserveAdmission(ctx context.Context, deploymentID entity.Id) (int64, error) {
	attrs := []entity.Attr{
		entity.Ref(entity.DBId, deploymentID),
		entity.String(core_v1alpha.DeploymentStatusId, string(admissionReservationStatus)),
	}
	res, err := s.eac.Ensure(ctx, attrs)
	if err != nil {
		return 0, fmt.Errorf("failed to reserve deployment admission: %w", err)
	}
	if !res.Created() {
		return 0, cond.Conflict("deployment-id", string(deploymentID))
	}
	return res.Revision(), nil
}

// PublishAdmission atomically replaces an admission reservation with the full
// deployment record after both the compatibility and canonical locks are held.
func (s *Store) PublishAdmission(ctx context.Context, dep *core_v1alpha.Deployment, revision int64) (*Record, error) {
	attrs := append(dep.Encode(), entity.Ref(entity.DBId, dep.ID))
	if _, err := s.eac.Replace(ctx, attrs, revision); err != nil {
		return nil, fmt.Errorf("failed to publish deployment admission: %w", err)
	}
	return s.Get(ctx, string(dep.ID))
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
		AppName:      appName,
		LegacyStatus: StatusActive,
	})
	if err != nil {
		return err
	}

	for _, rec := range previous {
		if string(rec.Deployment.ID) == exceptID {
			continue
		}

		// This is compatibility-only bookkeeping. Never touch canonical outcome:
		// the prior attempt succeeded regardless of what is serving now.
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

func recordFrom(e *entityserver_v1alpha.Entity) *Record {
	var dep core_v1alpha.Deployment
	dep.Decode(&entityAttrs{entity: e})

	return &Record{
		Deployment: &dep,
		Entity:     e,
		Revision:   e.Revision(),
	}
}
