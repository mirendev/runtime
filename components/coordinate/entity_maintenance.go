package coordinate

import (
	"context"
	"errors"

	aes "miren.dev/runtime/api/entityserver"
	artifactctrl "miren.dev/runtime/controllers/artifact"
	ephemeralctrl "miren.dev/runtime/controllers/ephemeral"
	indexgcctrl "miren.dev/runtime/controllers/indexgc"
	runctrl "miren.dev/runtime/controllers/run"
	sagagcctrl "miren.dev/runtime/controllers/sagagc"
	schemareindexctrl "miren.dev/runtime/controllers/schemareindex"
	versionctrl "miren.dev/runtime/controllers/version"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/saga"
)

// NewEntityMaintenance constructs the background repair and garbage-collection
// loops for cluster-owned entity state.
func NewEntityMaintenance(foundation *Foundation) *EntityMaintenance {
	return &EntityMaintenance{Foundation: foundation}
}

// EntityMaintenance owns background repair and garbage collection for durable
// cluster state. It does not reconcile live workloads.
type EntityMaintenance struct {
	*Foundation
	artifactGC    *artifactctrl.GCController
	runGC         *runctrl.GCController
	ephemeralGC   *ephemeralctrl.GCController
	versionGC     *versionctrl.GCController
	indexGC       *indexgcctrl.GCController
	sagaGC        *sagagcctrl.GCController
	schemaReindex *schemareindexctrl.Controller
}

func (c *EntityMaintenance) Start(ctx context.Context) error {
	if c.etcdStore == nil || c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}
	ec := aes.NewClient(c.Log, c.eac)
	eac := c.eac

	c.runGC = runctrl.NewGCController(c.Log, ec, eac)
	c.runGC.Start(ctx)
	c.artifactGC = &artifactctrl.GCController{Log: c.Log.With("module", "artifact-gc"), EAC: eac, Config: artifactctrl.DefaultGCConfig()}
	c.artifactGC.Start(ctx)
	c.ephemeralGC = &ephemeralctrl.GCController{Log: c.Log.With("module", "ephemeral-gc"), EAC: eac, Config: ephemeralctrl.DefaultGCConfig()}
	c.ephemeralGC.Start(ctx)

	versionConfig := versionctrl.DefaultGCConfig()
	if c.AppVersionRetentionCount > 0 {
		versionConfig.RetentionCount = c.AppVersionRetentionCount
	}
	if c.AppVersionRetentionPeriod > 0 {
		versionConfig.RetentionPeriod = c.AppVersionRetentionPeriod
	}
	c.versionGC = &versionctrl.GCController{
		Log: c.Log.With("module", "version-gc"), EAC: eac,
		Config: versionConfig, DataPath: c.DataPath,
	}
	c.versionGC.Start(ctx)
	c.indexGC = &indexgcctrl.GCController{
		Log: c.Log.With("module", "index-gc"), Store: c.etcdStore,
		Config: indexgcctrl.DefaultGCConfig(),
	}
	c.indexGC.Start(ctx)

	sagaConfig := sagagcctrl.DefaultGCConfig()
	if c.SagaRetentionPeriod >= 0 {
		sagaConfig.Retention = c.SagaRetentionPeriod
	}
	c.sagaGC = &sagagcctrl.GCController{
		Log:     c.Log.With("module", "saga-gc"),
		Storage: saga.NewEntityStorage(c.etcdStore, c.Log), Config: sagaConfig,
	}
	c.sagaGC.Start(ctx)
	c.schemaReindex = &schemareindexctrl.Controller{
		Log: c.Log.With("module", "schema-reindex"), Store: c.etcdStore,
		CurrentHash: schema.IndexHash, Config: schemareindexctrl.DefaultConfig(),
	}
	c.schemaReindex.Start(ctx)
	return nil
}

func (c *EntityMaintenance) Stop() {
	if c.artifactGC != nil {
		c.artifactGC.Stop()
	}
	if c.ephemeralGC != nil {
		c.ephemeralGC.Stop()
	}
	if c.runGC != nil {
		c.runGC.Stop()
	}
	if c.versionGC != nil {
		c.versionGC.Stop()
	}
	if c.indexGC != nil {
		c.indexGC.Stop()
	}
	if c.sagaGC != nil {
		c.sagaGC.Stop()
	}
	if c.schemaReindex != nil {
		c.schemaReindex.Stop()
	}
}
