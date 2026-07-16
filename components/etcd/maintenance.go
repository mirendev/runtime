package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/metrics"
)

const (
	maintenanceInterval = 5 * time.Minute
	defragBloatRatio    = 2.0
	warnBloatRatio      = 1.5
	errorBloatRatio     = 3.0

	// quotaHighWaterFraction triggers a proactive compact+defrag once the physical
	// backend size passes this fraction of the quota — before it reaches the hard
	// limit and arms a NOSPACE alarm. Keyed off physical DbSize (what the quota is
	// enforced against), so it is immune to the pending-page distortion in DbSizeInUse.
	quotaHighWaterFraction = 0.80

	// clientRetryInterval is how long the maintenance loop waits before retrying to
	// build its etcd client, so a transient failure at startup does not permanently
	// disable maintenance (the loop used to return and never run again).
	clientRetryInterval = 10 * time.Second
)

// maintenanceAction is the remediation the maintenance loop should take for the
// observed backend state. Determined by the pure decideMaintenance function so the
// trigger thresholds are unit-testable without a live etcd.
type maintenanceAction int

const (
	actionNone     maintenanceAction = iota // healthy: nothing to do
	actionDefrag                            // bloated (ratio): defrag to return free pages to the OS
	actionReclaim                           // near quota with reclaimable bloat: compact-to-head + defrag
	actionWarnFull                          // near quota but mostly live data: defrag can't help — warn
	actionRecover                           // NOSPACE armed: reclaim + disarm the alarm
)

// decideMaintenance picks the remediation for the observed backend state. NOSPACE
// recovery takes precedence, then the absolute near-quota trigger (physical dbSize vs
// quota), then the bloat-ratio backstop.
//
// The near-quota branch only reclaims when compact+defrag can actually pull the physical
// file back under the high-water — i.e. the live data (dbSizeInUse) is itself below the
// high-water, so the excess is reclaimable bloat. If the live data is already over the
// high-water, defrag cannot shrink it and re-running compact+defrag every tick would
// thrash without progress; we surface a "raise the quota / shed keyspace" warning instead.
func decideMaintenance(dbSize, dbSizeInUse, quota int64, noSpace bool) maintenanceAction {
	highWater := quotaHighWaterFraction * float64(quota)
	switch {
	case noSpace:
		return actionRecover
	case quota > 0 && float64(dbSize) > highWater:
		if float64(dbSizeInUse) < highWater {
			return actionReclaim
		}
		return actionWarnFull
	case dbSizeInUse > 0 && dbSize > int64(defragBloatRatio*float64(dbSizeInUse)):
		return actionDefrag
	default:
		return actionNone
	}
}

// StartMaintenanceLoop runs a background goroutine that periodically checks
// etcd database health and triggers defragmentation when the BoltDB file has
// grown significantly larger than its live data. Compaction (already configured
// as periodic/1h) marks old revisions as deleted, but BoltDB never releases
// pages without an explicit defrag.
func (e *EtcdComponent) StartMaintenanceLoop(ctx context.Context) {
	go e.maintenanceLoop(ctx)
}

func (e *EtcdComponent) maintenanceLoop(ctx context.Context) {
	// Build the client with retry: a transient failure (endpoint not ready yet, dial
	// error) must not permanently disable maintenance for the process lifetime.
	client, endpoint := e.connectMaintenanceClient(ctx)
	if client == nil {
		return // ctx cancelled before we could connect
	}
	defer client.Close()

	// Run a check immediately so a wedged (NOSPACE) or near-full etcd is remediated
	// within seconds of startup rather than waiting for the first ticker interval.
	e.runMaintenanceCheck(ctx, client, endpoint)

	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.runMaintenanceCheck(ctx, client, endpoint)
		case <-e.metricsKick:
			// The metrics writer was just attached (see SetMetricsWriter); run a check
			// now so the first health sample lands promptly rather than up to a full
			// interval later. The immediate check above ran before the writer existed.
			e.runMaintenanceCheck(ctx, client, endpoint)
		case <-ctx.Done():
			return
		}
	}
}

// connectMaintenanceClient builds the etcd client, retrying until it succeeds or the
// context is cancelled. Returns (nil, "") only if ctx is cancelled first.
func (e *EtcdComponent) connectMaintenanceClient(ctx context.Context) (*clientv3.Client, string) {
	for {
		if endpoint := e.ClientEndpoint(); endpoint != "" {
			client, err := e.newMaintenanceClient(endpoint)
			if err == nil {
				return client, endpoint
			}
			e.Log.Error("etcd maintenance loop: failed to create client, will retry", "error", err)
		} else {
			e.Log.Warn("etcd maintenance loop: no endpoint available yet, will retry")
		}

		select {
		case <-time.After(clientRetryInterval):
		case <-ctx.Done():
			return nil, ""
		}
	}
}

func (e *EtcdComponent) newMaintenanceClient(endpoint string) (*clientv3.Client, error) {
	cfg := clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	}

	if e.config.TLS != nil {
		tlsCfg, err := buildMaintenanceTLSConfig(e.config.TLS.CertsDir)
		if err != nil {
			return nil, fmt.Errorf("building TLS config: %w", err)
		}
		cfg.TLS = tlsCfg
	}

	return clientv3.New(cfg)
}

func buildMaintenanceTLSConfig(certsDir string) (*tls.Config, error) {
	certFile := filepath.Join(certsDir, "server.crt")
	keyFile := filepath.Join(certsDir, "server.key")
	caFile := filepath.Join(certsDir, "ca.crt")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("loading key pair: %w", err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}, nil
}

func (e *EtcdComponent) runMaintenanceCheck(ctx context.Context, client *clientv3.Client, endpoint string) {
	resp, err := client.Status(ctx, endpoint)
	if err != nil {
		e.Log.Warn("etcd maintenance: failed to get status", "error", err)
		return
	}

	dbSize := resp.DbSize
	dbSizeInUse := resp.DbSizeInUse
	var bloatRatio float64
	if dbSizeInUse > 0 {
		bloatRatio = float64(dbSize) / float64(dbSizeInUse)
	}

	noSpace, alarmedMembers := e.checkNoSpace(ctx, client)

	attrs := []any{
		"db_size_bytes", dbSize,
		"db_size_in_use_bytes", dbSizeInUse,
		"bloat_ratio", bloatRatio,
		"quota_bytes", e.quotaBackendBytes,
		"nospace_alarm", noSpace,
	}

	switch {
	case noSpace:
		e.Log.Error("etcd NOSPACE alarm is active", attrs...)
	case bloatRatio >= errorBloatRatio:
		e.Log.Error("etcd database bloat is critically high", attrs...)
	case bloatRatio >= warnBloatRatio:
		e.Log.Warn("etcd database bloat is elevated", attrs...)
	default:
		e.Log.Info("etcd database status", attrs...)
	}

	e.emitMetrics(ctx, dbSize, dbSizeInUse, bloatRatio, noSpace)

	switch decideMaintenance(dbSize, dbSizeInUse, e.quotaBackendBytes, noSpace) {
	case actionRecover:
		e.recoverFromNoSpace(ctx, client, endpoint, resp.Header.Revision, dbSize, alarmedMembers)
	case actionReclaim:
		e.Log.Warn("etcd backend near quota, reclaiming space",
			"db_size_bytes", dbSize, "db_size_in_use_bytes", dbSizeInUse, "quota_bytes", e.quotaBackendBytes)
		e.reclaimSpace(ctx, client, endpoint, resp.Header.Revision, dbSize)
	case actionWarnFull:
		e.Log.Error("etcd backend near quota and mostly live data; compaction/defrag cannot reclaim enough space — raise --quota-backend-bytes or reduce the keyspace",
			"db_size_bytes", dbSize, "db_size_in_use_bytes", dbSizeInUse, "quota_bytes", e.quotaBackendBytes)
	case actionDefrag:
		e.defragment(ctx, client, endpoint, dbSize)
	case actionNone:
	}
}

// checkNoSpace lists active alarms and reports whether any NOSPACE alarm is armed,
// returning the member IDs that raised it (for disarming).
func (e *EtcdComponent) checkNoSpace(ctx context.Context, client *clientv3.Client) (bool, []uint64) {
	resp, err := client.AlarmList(ctx)
	if err != nil {
		e.Log.Warn("etcd maintenance: failed to list alarms", "error", err)
		return false, nil
	}

	var members []uint64
	for _, a := range resp.Alarms {
		if a.Alarm == etcdserverpb.AlarmType_NOSPACE {
			members = append(members, a.MemberID)
		}
	}
	return len(members) > 0, members
}

// reclaimSpace compacts to the given revision (physically) and defragments, which is
// the only way to shrink the on-disk file. Used both proactively near the quota and
// during NOSPACE recovery. Compaction discards MVCC history to head; acceptable since
// this only fires near-quota or when already wedged (uptime > history).
func (e *EtcdComponent) reclaimSpace(ctx context.Context, client *clientv3.Client, endpoint string, rev, dbSizeBefore int64) {
	if rev > 0 {
		e.Log.Info("etcd compaction starting", "revision", rev)
		if _, err := client.Compact(ctx, rev, clientv3.WithCompactPhysical()); err != nil {
			e.Log.Error("etcd compaction failed", "error", err)
			// Fall through to defrag anyway — it still reclaims freelist bloat.
		} else {
			e.Log.Info("etcd compaction completed", "revision", rev)
		}
	}
	e.defragment(ctx, client, endpoint, dbSizeBefore)
}

// recoverFromNoSpace self-heals a read-only etcd: reclaim space, then disarm the
// NOSPACE alarm(s) so writes are accepted again, turning a hard outage that requires
// manual operator intervention into a self-healing event.
func (e *EtcdComponent) recoverFromNoSpace(ctx context.Context, client *clientv3.Client, endpoint string, rev, dbSize int64, members []uint64) {
	e.Log.Error("etcd NOSPACE alarm armed, attempting self-recovery (compact+defrag+disarm)",
		"db_size_bytes", dbSize, "quota_bytes", e.quotaBackendBytes)

	e.reclaimSpace(ctx, client, endpoint, rev, dbSize)

	for _, id := range members {
		_, err := client.AlarmDisarm(ctx, &clientv3.AlarmMember{
			MemberID: id,
			Alarm:    etcdserverpb.AlarmType_NOSPACE,
		})
		if err != nil {
			e.Log.Error("etcd NOSPACE alarm disarm failed", "member_id", id, "error", err)
			continue
		}
		e.Log.Warn("etcd NOSPACE alarm disarmed", "member_id", id)
	}

	if stillNoSpace, _ := e.checkNoSpace(ctx, client); stillNoSpace {
		e.Log.Error("etcd still NOSPACE after recovery attempt; manual intervention may be required")
	} else {
		e.Log.Warn("etcd NOSPACE recovery succeeded, backend is writable again")
	}
}

// emitMetrics publishes etcd backend health gauges. No-op if no writer is configured.
func (e *EtcdComponent) emitMetrics(ctx context.Context, dbSize, dbSizeInUse int64, bloatRatio float64, noSpace bool) {
	writer := e.writer.Load()
	if writer == nil {
		return
	}

	now := time.Now()
	var headroom int64
	if e.quotaBackendBytes > 0 {
		headroom = e.quotaBackendBytes - dbSize // quota is enforced against physical DbSize
	}
	alarm := 0.0
	if noSpace {
		alarm = 1.0
	}

	points := []metrics.MetricPoint{
		{Name: "etcd_db_size_bytes", Value: float64(dbSize), Timestamp: now},
		{Name: "etcd_db_size_in_use_bytes", Value: float64(dbSizeInUse), Timestamp: now},
		{Name: "etcd_backend_quota_bytes", Value: float64(e.quotaBackendBytes), Timestamp: now},
		{Name: "etcd_quota_headroom_bytes", Value: float64(headroom), Timestamp: now},
		{Name: "etcd_bloat_ratio", Value: bloatRatio, Timestamp: now},
		{Name: "etcd_nospace_alarm", Value: alarm, Timestamp: now},
	}
	if err := writer.WritePoints(ctx, points); err != nil {
		e.Log.Warn("etcd maintenance: failed to write metrics", "error", err)
	}
}

func (e *EtcdComponent) defragment(ctx context.Context, client *clientv3.Client, endpoint string, dbSizeBefore int64) {
	e.Log.Info("etcd defrag starting", "db_size_before_bytes", dbSizeBefore)

	_, err := client.Defragment(ctx, endpoint)
	if err != nil {
		e.Log.Error("etcd defrag failed", "error", err)
		return
	}

	resp, err := client.Status(ctx, endpoint)
	if err != nil {
		e.Log.Warn("etcd defrag completed but failed to get post-defrag status", "error", err)
		return
	}

	reclaimed := dbSizeBefore - resp.DbSize
	e.Log.Info("etcd defrag completed",
		"db_size_before_bytes", dbSizeBefore,
		"db_size_after_bytes", resp.DbSize,
		"reclaimed_bytes", reclaimed,
	)
}
