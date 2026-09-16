package coordinate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/anywhere"
	"miren.dev/runtime/pkg/apphealthsync"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/cloudrpc"
	"miren.dev/runtime/pkg/clusternetwork"
	"miren.dev/runtime/pkg/clusterresources"
	"miren.dev/runtime/pkg/containerenv"
	"miren.dev/runtime/pkg/entitysync"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/serverinfo"
	"miren.dev/runtime/pkg/sysstats"
	"miren.dev/runtime/pkg/uplink"
	"miren.dev/runtime/servers/httpingress"
	"miren.dev/runtime/version"
)

// NewCloudControl constructs cloud status, identity publication, and uplink
// integration on top of cluster state.
func NewCloudControl(foundation *Foundation, applications *ApplicationManagement, diagnostics ...*entitysync.Diagnostics) *CloudControl {
	var entitySyncDiagnostics *entitysync.Diagnostics
	if len(diagnostics) > 0 {
		entitySyncDiagnostics = diagnostics[0]
	}
	if entitySyncDiagnostics == nil {
		entitySyncDiagnostics = entitysync.NewDiagnostics(core_v1alpha.CloudExportContract.Digest())
	}
	return &CloudControl{
		Foundation: foundation, applications: applications,
		entitySyncDiagnostics: entitySyncDiagnostics,
	}
}

// CloudControl owns cloud status, identity publication, and uplink integration.
type CloudControl struct {
	*Foundation
	// applications supplies the health classification the uplink reports. It is
	// the same AppInfo backing the app RPC surface, so the console cannot
	// disagree with `miren app list`.
	applications *ApplicationManagement

	// Instance is optional; when nil reports omit the instance id.
	Instance *serverinfo.Source
	// Lifecycle, when set, lets cloud restart and upgrade this server over the
	// uplink.
	Lifecycle *ServerLifecycle

	// anchorMu guards CloudAuth.IdentityIssuerURL, which the session callback
	// writes (recordIdentityAnchor) and the periodic loop reads
	// (anchoredAtCloud) on different goroutines.
	anchorMu sync.Mutex

	// pollSuppressed is set while a negotiated session carries both the
	// cluster-network and cluster-resources capabilities, which is when the
	// status poll has nothing left to say. See reportStatusPeriodically.
	pollSuppressed atomic.Bool
	// pollSuppressedBy names the session that set pollSuppressed, so the
	// teardown of an older session cannot clear what a newer one set.
	pollSuppressedMu sync.Mutex
	pollSuppressedBy string

	entitySyncDiagnostics   *entitysync.Diagnostics
	publishedKeysMu         sync.Mutex
	publishedKeyFingerprint string
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
}

// Stop cancels and joins the background reporting loop started by Start.
func (c *CloudControl) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

// EntitySyncDiagnostics is the runtime-local view exposed by miren debug.
func (c *CloudControl) EntitySyncDiagnostics() *entitysync.Diagnostics {
	return c.entitySyncDiagnostics
}

// Start begins cloud-facing reporting. The uplink itself remains a separate
// long-running boot task because it owns the connection until shutdown.
func (c *CloudControl) Start(ctx context.Context) error {
	if !c.CloudAuth.Enabled || c.authClient == nil || c.CloudAuth.ClusterID == "" {
		return nil
	}
	c.publishSigningKeysAtStartup(ctx)
	if err := c.ReportStartupStatus(ctx); err != nil {
		c.Log.Error("failed to report initial cluster status", "error", err)
	}
	reportCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.wg.Go(func() { c.reportStatusPeriodically(reportCtx) })
	return nil
}

// RunCloudUplink owns the shared cloud connection and all of its tenants.
// Entity sync alone waits for its source preparation; Anywhere and cloud RPC
// remain independent of that background migration.
func (c *CloudControl) RunCloudUplink(ctx context.Context, ingress *httpingress.Server, entitySyncReady <-chan struct{}) error {
	if !c.CloudAuth.Enabled || c.authClient == nil {
		c.entitySyncDiagnostics.SetDisabled("cloud-auth-disabled")
		return nil
	}
	cloudURL := c.CloudAuth.CloudURL
	if cloudURL == "" {
		cloudURL = DefaultCloudURL
	}

	// The negotiated session is the baseline: every capability rides it, and
	// cloud selects the ones it wants per cluster. Which capabilities cloud
	// puts to use is decided there, behind its own per-organization flags.
	link := uplink.NewClient(
		cloudURL,
		c.authClient,
		uplink.NewMessageRouter(),
		c.Log.With("component", "uplink"),
		uplink.WithStatus(c.entitySyncDiagnostics.ObserveUplink),
		uplink.WithSession(uplink.SessionIdentity{
			RuntimeVersion:    version.GetInfo().Version,
			RuntimeInstanceID: c.instanceID(),
		}),
	)
	// The welcome repeats the workload identity anchor the status poll's
	// response used to, so a cluster that no longer polls still learns where
	// cloud anchors it. Recording is not adopting; see recordIdentityAnchor.
	link.OnSession(func(_ context.Context, session uplink.Session) {
		c.recordIdentityAnchor(session.IdentityIssuerURL)
	})
	// With both halves of the status report negotiated onto the session, the
	// poll would only repeat what the session already said. Suppress it for
	// as long as such a session holds; a cloud that declines either
	// capability gets the poll as before, which is the compatibility switch
	// in this direction.
	link.OnSession(c.suppressPollWhile)
	if err := entitysync.NewExporter(
		c.Log.With("component", "entity-sync"), c.store, core_v1alpha.CloudExportContract,
		entitysync.WithStartGate(entitySyncReady),
		entitysync.WithDiagnostics(c.entitySyncDiagnostics),
	).Register(ctx, link); err != nil {
		// Entity visibility is additive. Local source metadata should not take
		// Anywhere or cloud RPC off the shared link when it is unavailable.
		c.Log.Warn("entity sync is unavailable for this uplink session", "error", err)
	}
	if c.applications != nil && c.applications.appInfo != nil {
		if err := apphealthsync.NewReporter(
			c.Log.With("component", "app-health"), c.applications.appInfo,
		).Register(ctx, link); err != nil {
			// Health reporting is additive, like entity sync. It must not take
			// Anywhere or cloud RPC off the shared link when it is unavailable.
			c.Log.Warn("app health reporting is unavailable for this uplink session", "error", err)
		}
	}
	// The network half of the status poll, over the session. Like the feeds
	// above it is additive; when cloud selects it the poll has nothing left
	// to say about the network.
	if err := clusternetwork.NewReporter(c.Log.With("component", "cluster-network"), c).Register(ctx, link); err != nil {
		c.Log.Warn("cluster network reporting is unavailable for this uplink session", "error", err)
	}
	// And the resource half, at the cadence cloud asks for.
	if err := clusterresources.NewReporter(c.Log.With("component", "cluster-resources"), c).Register(ctx, link); err != nil {
		c.Log.Warn("cluster resource reporting is unavailable for this uplink session", "error", err)
	}
	// Offered on every negotiated session, like the RPC relay: whether cloud
	// may drive the server is cloud's decision at negotiation, not a switch on
	// this side.
	if c.Lifecycle != nil {
		if err := c.Lifecycle.Register(ctx, link); err != nil {
			// Additive, like the tenants above: the ledger stays readable over
			// RPC and cloud simply cannot drive it this session.
			c.Log.Warn("server lifecycle is unavailable for this uplink session", "error", err)
		}
	}
	anywhereConn := anywhere.New(anywhere.Config{
		ClusterXID: c.CloudAuth.ClusterID,
		Ingress:    ingress,
		Log:        c.Log.With("component", "anywhere"),
		Uplink:     link,
	})
	defer anywhereConn.Close()

	// Serving RPC over the link is what lets an operator reach a cluster it has
	// no route to. Calls land on the same objects as the network listener and
	// pass through the same authentication and authorization chain.
	cloudrpc.New(cloudrpc.Config{
		Uplink: link,
		State:  c.state,
		Log:    c.Log.With("component", "cloudrpc"),
	})
	return link.Run(ctx)
}

// runNetcheck calls the cloud's netcheck endpoint over both IPv4 and IPv6
// to determine public reachability on each address family.
func (c *CloudControl) runNetcheck(ctx context.Context) {
	cloudURL := c.CloudAuth.CloudURL
	if cloudURL == "" {
		cloudURL = DefaultCloudURL
	}

	ports := []cloudauth.NetcheckPort{
		{Port: 8443, Protocol: "https"},
		{Port: 8443, Protocol: "http3"},
	}

	result, err := cloudauth.NetcheckDualStack(ctx, cloudURL, ports)
	if err != nil {
		// A check cut short by the caller going away (a session dropping
		// mid-check, now that the network reporter runs it) says nothing about
		// reachability. Leave the cache and its age alone so the next caller
		// re-runs it rather than reporting for an hour as if the check found
		// nothing.
		if ctx.Err() != nil {
			c.Log.Debug("netcheck: cancelled before completion; keeping the previous result")
			return
		}
		if errors.Is(err, cloudauth.ErrPrivateAddress) {
			c.Log.Info("netcheck: cluster is not publicly reachable (private IP)")
		} else {
			c.Log.Warn("netcheck: failed to check public reachability", "error", err)
		}
		c.netcheckMu.Lock()
		c.netcheckResult = nil
		c.netcheckCheckedAt = time.Now()
		c.netcheckMu.Unlock()
		return
	}

	// Validate source addresses — drop any that aren't public global unicast.
	if result.IPv4 != nil {
		sourceIP := net.ParseIP(result.IPv4.SourceAddress)
		if sourceIP == nil || !sourceIP.IsGlobalUnicast() || sourceIP.IsPrivate() {
			c.Log.Warn("netcheck: IPv4 source address is not a public IP, ignoring",
				"source_address", result.IPv4.SourceAddress)
			result.IPv4 = nil
		}
	}
	if result.IPv6 != nil {
		sourceIP := net.ParseIP(result.IPv6.SourceAddress)
		if sourceIP == nil || !sourceIP.IsGlobalUnicast() || sourceIP.IsPrivate() {
			c.Log.Warn("netcheck: IPv6 source address is not a public IP, ignoring",
				"source_address", result.IPv6.SourceAddress)
			result.IPv6 = nil
		}
	}

	if result.IPv4 == nil && result.IPv6 == nil {
		c.netcheckMu.Lock()
		c.netcheckResult = nil
		c.netcheckCheckedAt = time.Now()
		c.netcheckMu.Unlock()
		return
	}

	c.netcheckMu.Lock()
	c.netcheckResult = result
	c.netcheckCheckedAt = time.Now()
	c.netcheckMu.Unlock()

	// Log results for each address family
	for _, entry := range []struct {
		name string
		resp *cloudauth.NetcheckResponse
	}{
		{"IPv4", result.IPv4},
		{"IPv6", result.IPv6},
	} {
		if entry.resp == nil {
			continue
		}
		var reachable []string
		for _, r := range entry.resp.Results {
			if r.Reachable {
				reachable = append(reachable, fmt.Sprintf("%s/%d", r.Protocol, r.Port))
			}
		}
		c.Log.Info("netcheck: public reachability determined",
			"family", entry.name,
			"source_ip", entry.resp.SourceAddress,
			"reachable", reachable,
			"duration_ms", entry.resp.DurationMs,
		)
	}
}

// apiAddresses builds the list of API addresses the server should advertise.
// The heavy lifting lives in ComputeAdvertise so the same rules can be
// exercised by the 'miren debug advertise' command.
func (c *CloudControl) apiAddresses() []string {
	c.netcheckMu.RLock()
	netcheck := c.netcheckResult
	c.netcheckMu.RUnlock()

	_, final := ComputeAdvertise(AdvertiseInput{
		ListenAddr: c.Address,
		IPs:        c.IPs.All(),
		Netcheck:   netcheck,
	})

	c.logAddressesOnce.Do(func() {
		var explicit, discovered []string
		for _, sip := range c.IPs.All() {
			if sip.Explicit {
				explicit = append(explicit, sip.IP.String())
			} else {
				discovered = append(discovered, sip.IP.String())
			}
		}
		c.Log.Info("reporting API addresses", "listen", c.Address, "configured", explicit, "discovered", discovered, "result", final)
	})

	return final
}

// reachabilityVerdict synthesizes the agent's inbound-reachability verdict from
// the cached netcheck result, for reporting to cloud. Returns nil when netcheck
// has produced no usable public source address, so the field is simply omitted
// from the report and cloud falls back to its generic copy.
func (c *CloudControl) reachabilityVerdict() *cloudauth.ReachabilityVerdict {
	c.netcheckMu.RLock()
	netcheck := c.netcheckResult
	c.netcheckMu.RUnlock()

	return netcheck.ReachabilityVerdict()
}

// ReportStatus reports the current cluster status to miren.cloud
func (c *CloudControl) ReportStartupStatus(ctx context.Context) error {
	if c.authClient == nil {
		return fmt.Errorf("auth client not configured")
	}

	if c.CloudAuth.ClusterID == "" {
		return fmt.Errorf("cluster ID not configured")
	}

	// Build status report
	facts := c.NetworkFacts(ctx)
	status := &cloudauth.StatusReport{
		ClusterID:         c.CloudAuth.ClusterID,
		APIAddresses:      facts.APIAddresses,
		CACertFingerprint: facts.CACertFingerprint,
		Reachability:      facts.Reachability,
		Containerized:     facts.Containerized,
	}

	result, err := c.authClient.ReportClusterStatus(ctx, status)
	if err != nil {
		return err
	}

	c.recordIdentityAnchor(result.IdentityIssuerURL)
	return nil
}

// suppressPollWhile turns the status poll off for the life of a session that
// carries both of its replacements, and back on when that session ends. See
// the OnSession registration in RunCloudUplink.
func (c *CloudControl) suppressPollWhile(ctx context.Context, session uplink.Session) {
	_, network := session.Capability(uplink.CapabilityClusterNetwork)
	_, resources := session.Capability(uplink.CapabilityClusterResources)
	if !network || !resources {
		return
	}
	// Suppression belongs to the session that set it. On a fast reconnect
	// the new session's callback can run before the old session's teardown
	// goroutine wakes, and that teardown must not undo the new session's
	// claim; only the session that currently owns the flag may clear it.
	c.pollSuppressedMu.Lock()
	c.pollSuppressedBy = session.ID
	c.pollSuppressed.Store(true)
	c.pollSuppressedMu.Unlock()
	c.Log.Info("cluster status poll suppressed; the uplink session carries its replacements")
	go func() {
		<-ctx.Done()
		c.pollSuppressedMu.Lock()
		defer c.pollSuppressedMu.Unlock()
		if c.pollSuppressedBy == session.ID {
			c.pollSuppressedBy = ""
			c.pollSuppressed.Store(false)
		}
	}()
}

// ReportStatus sends the legacy status poll to miren.cloud. It is what a
// cloud that did not negotiate the cluster-network and cluster-resources
// capabilities still relies on; against one that did, the periodic loop
// skips it (see pollSuppressed).
func (c *CloudControl) ReportStatus(ctx context.Context) error {
	if c.authClient == nil {
		return fmt.Errorf("auth client not configured")
	}

	if c.CloudAuth.ClusterID == "" {
		return fmt.Errorf("cluster ID not configured")
	}

	// Build status report
	facts := c.NetworkFacts(ctx)
	status := &cloudauth.StatusReport{
		ClusterID:         c.CloudAuth.ClusterID,
		State:             "active",
		Version:           version.GetInfo().Version,
		NodeCount:         1, // Static value for now
		ResourceUsage:     c.collectResourceUsage(),
		APIAddresses:      facts.APIAddresses,
		CACertFingerprint: facts.CACertFingerprint,
		Reachability:      facts.Reachability,
		Containerized:     facts.Containerized,
	}

	result, err := c.authClient.ReportClusterStatus(ctx, status)
	if err != nil {
		return err
	}

	c.recordIdentityAnchor(result.IdentityIssuerURL)
	return nil
}

// netcheckMaxAge is how old a cached netcheck result may be before the next
// report re-runs it. Reachability changes rarely and the check costs a round
// trip to cloud on both address families, so an hour is the balance.
const netcheckMaxAge = 60 * time.Minute

// NetworkFacts is what this cluster says about how it can be reached, in the
// shape both the status poll and the cluster-network capability send. It
// refreshes netcheck when the cached verdict is older than netcheckMaxAge
// (or was never taken), so whichever path asks first pays for the check and
// the other reads the cache.
func (c *CloudControl) NetworkFacts(ctx context.Context) clusternetwork.Report {
	c.netcheckMu.RLock()
	stale := c.netcheckCheckedAt.IsZero() || time.Since(c.netcheckCheckedAt) > netcheckMaxAge
	c.netcheckMu.RUnlock()
	if stale {
		c.runNetcheck(ctx)
	}
	return clusternetwork.Report{
		APIAddresses:      c.apiAddresses(),
		CACertFingerprint: c.caCertFingerprint(),
		Reachability:      c.reachabilityVerdict(),
		Containerized:     containerenv.InContainer(),
	}
}

// caCertFingerprint is the hex SHA-1 of the cluster CA's DER bytes, which the
// CLI pins when it connects directly. Empty when there is no authority yet.
func (c *CloudControl) caCertFingerprint() string {
	if c.authority == nil {
		return ""
	}
	block, _ := pem.Decode(c.authority.GetCACertificate())
	if block == nil || block.Type != "CERTIFICATE" {
		return ""
	}
	sum := sha1.Sum(block.Bytes)
	return hex.EncodeToString(sum[:])
}

func (c *CloudControl) instanceID() string {
	if c.Instance == nil {
		return ""
	}
	return c.Instance.InstanceID()
}

// collectResourceUsage gathers basic host system resource usage metrics
func (c *CloudControl) collectResourceUsage() cloudauth.ResourceUsage {
	stats := sysstats.CollectSystemStats(c.DataPath)

	return cloudauth.ResourceUsage{
		CPUCores:       stats.CPUCores,
		CPUPercent:     stats.CPUPercent,
		MemoryBytes:    stats.MemoryBytes,
		MemoryPercent:  stats.MemoryPercent,
		StorageBytes:   stats.StorageBytes,
		StoragePercent: stats.StoragePercent,
	}
}

// ResourceSample is one host reading in the cluster-resources wire shape,
// stamped with when it was taken.
//
// The percentages are the ones the status poll carries. The totals are not:
// the poll's cpu_cores is a load average despite its name and its byte
// figures are used bytes, which cloud never read. The wire calls these
// capacities, so they come from the host's core count and total memory and
// storage.
func (c *CloudControl) ResourceSample() clusterresources.Sample {
	stats := sysstats.CollectSystemStats(c.DataPath)
	return clusterresources.Sample{
		ObservedAt:     time.Now().UTC(),
		CPUCores:       float64(stats.CPUCoreCount),
		CPUPercent:     stats.CPUPercent,
		MemoryBytes:    stats.MemoryTotalBytes,
		MemoryPercent:  stats.MemoryPercent,
		StorageBytes:   stats.StorageTotalBytes,
		StoragePercent: stats.StoragePercent,
	}
}

// reportStatusPeriodically reports cluster status at regular intervals
func (c *CloudControl) reportStatusPeriodically(ctx context.Context) {
	// Initial report after a short delay to allow services to start
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	if c.pollSuppressed.Load() {
		c.Log.Debug("skipping cluster status poll; the uplink session carries its replacements")
	} else if err := c.ReportStatus(ctx); err != nil {
		c.Log.Error("failed to report initial cluster status", "error", err)
	} else {
		c.Log.Info("reported cluster status to cloud")
	}

	// Report status every 5 minutes. The ticker keeps running while the poll
	// is suppressed because it also drives the signing-key republish below.
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.pollSuppressed.Load() {
				c.Log.Debug("skipping cluster status poll; the uplink session carries its replacements")
			} else if err := c.ReportStatus(ctx); err != nil {
				c.Log.Error("failed to report cluster status", "error", err)
			} else {
				c.Log.Debug("reported cluster status to cloud")
			}

			// Republish only when the key set actually changed, which makes
			// this the path a rotation propagates through — and the retry for
			// a startup publish that failed.
			if _, err := c.publishSigningKeys(ctx); err != nil {
				c.Log.Error("failed to publish workload identity signing keys", "error", err)
			}
		}
	}
}

// publishRetries and publishRetryDelay bound the startup attempt. Publication
// is not on the critical path for the cluster's own services — they verify
// tokens in-process — so a few quick tries is the right trade against blocking
// startup on cloud being reachable. The periodic loop keeps trying after that.
const (
	publishRetries    = 3
	publishRetryDelay = 2 * time.Second
)

// publishSigningKeys sends the public half of the workload identity key set to
// miren.cloud, which serves it as OIDC discovery on this cluster's behalf.
//
// Only public material crosses the wire. The signing key was generated here and
// stays here, which is what keeps a compromise of cloud from being able to mint
// an identity — cloud can serve keys, not sign with them.
//
// Returns false when there was nothing to do (not anchored at cloud, or the key
// set is unchanged since the last publish).
func (c *CloudControl) publishSigningKeys(ctx context.Context) (bool, error) {
	if !c.anchoredAtCloud() {
		return false, nil
	}

	fingerprint := c.WorkloadIssuer.KeySetFingerprint()

	c.publishedKeysMu.Lock()
	unchanged := fingerprint == c.publishedKeyFingerprint
	c.publishedKeysMu.Unlock()

	// The key set turns over on rotation and otherwise sits still for months,
	// so republishing an identical set every status cycle is pure noise.
	if unchanged {
		return false, nil
	}

	document, err := c.WorkloadIssuer.JWKSDocument()
	if err != nil {
		return false, fmt.Errorf("building JWKS document: %w", err)
	}

	result, err := c.authClient.PublishJWKS(ctx, document)
	if err != nil {
		return false, err
	}

	c.publishedKeysMu.Lock()
	c.publishedKeyFingerprint = fingerprint
	c.publishedKeysMu.Unlock()

	// Cloud pins a cluster's anchor on first publication and never moves it, so
	// a disagreement here means this process adopted an anchor cloud did not
	// assign — cloud's IDENTITY_ISSUER_BASE_URL changed after this cluster
	// registered, most likely. Tokens minted now carry an iss that does not
	// match the discovery document cloud serves, so they will not verify.
	// Nothing to do about it at runtime, since the issuer URL is fixed at
	// startup, but an operator needs to know a restart will fix it.
	if issuer := c.WorkloadIssuer.IssuerURL(); result.Issuer != issuer {
		c.Log.Warn("miren.cloud anchors this cluster's workload identity elsewhere than the tokens it is minting; "+
			"restart to adopt the assigned anchor",
			"assigned", result.Issuer,
			"minting_with", issuer)
	}

	c.Log.Info("published workload identity signing keys to cloud",
		"issuer", result.Issuer,
		"jwks_uri", result.JWKSURI,
		"key_count", result.KeyCount)

	return true, nil
}

// publishSigningKeysAtStartup makes a bounded attempt to get this cluster's
// public keys to cloud before it starts handing out tokens.
//
// It deliberately does not block startup on success. The tokens this cluster
// mints are verified in-process by its own services, so an unpublished key set
// costs external federation and nothing else — and wedging a cluster's boot on
// cloud being reachable would be a far worse failure than a delayed federation.
func (c *CloudControl) publishSigningKeysAtStartup(ctx context.Context) {
	if !c.anchoredAtCloud() {
		return
	}

	var lastErr error

	for attempt := 1; attempt <= publishRetries; attempt++ {
		published, err := c.publishSigningKeys(ctx)
		if err == nil {
			if !published {
				c.Log.Debug("workload identity key set already published")
			}
			return
		}

		// Cloud has no anchor configured. Retrying cannot change that.
		if errors.Is(err, cloudauth.ErrDiscoveryUnavailable) {
			c.Log.Warn("miren.cloud is not serving workload identity discovery; " +
				"this cluster's tokens can only be verified by its own services")
			return
		}

		lastErr = err

		select {
		case <-ctx.Done():
			return
		case <-time.After(publishRetryDelay):
		}
	}

	c.Log.Error("failed to publish workload identity signing keys to cloud; "+
		"external verifiers will not see this cluster's keys until the next status cycle succeeds",
		"error", lastErr)
}

// anchoredAtCloud reports whether the tokens this cluster mints carry the
// cloud-assigned issuer, which is the only case where cloud should be holding
// this cluster's keys.
//
// A cluster left on the default anchor serves its own discovery, and its tokens
// carry its own hostname as iss. Publishing its keys anyway would have cloud
// serving a discovery document for an issuer no token actually uses — verifiers
// pointed at it would fail closed against every token the cluster mints. So the
// test is not "is cloud reachable" but "is this process actually minting tokens
// under the anchor cloud assigned".
func (c *CloudControl) anchoredAtCloud() bool {
	if c.WorkloadIssuer == nil || c.authClient == nil || !c.CloudAuth.Enabled {
		return false
	}
	c.anchorMu.Lock()
	anchor := c.CloudAuth.IdentityIssuerURL
	c.anchorMu.Unlock()
	if anchor == "" {
		return false
	}
	return c.WorkloadIssuer.IssuerURL() == anchor
}

// recordIdentityAnchor persists the anchor cloud reports, so a cluster that
// registered before anchors existed can be moved to one without re-registering.
//
// Registration is otherwise the only place this value is handed out, which
// would leave exactly the clusters that most want to move — already registered,
// and not reachable from the internet — with no way to obtain it. Cloud repeats
// it on every status report; this writes it down the first time it changes.
//
// Recording is not adopting. The anchor a cluster mints under is fixed at
// startup from its configured setting, so writing this only makes the move
// available; `miren server identity-anchor` still has to ask for it.
func (c *CloudControl) recordIdentityAnchor(issuerURL string) {
	// Held for the whole read-compare-save so the session callback and the
	// poll cannot interleave two registration writes.
	c.anchorMu.Lock()
	defer c.anchorMu.Unlock()
	if issuerURL == "" || issuerURL == c.CloudAuth.IdentityIssuerURL {
		return
	}

	registrationDir := filepath.Join(c.DataPath, "server")
	reg, err := registration.LoadRegistration(registrationDir)
	if err != nil || reg == nil {
		// Nothing to update: an unregistered cluster has no file, and a
		// registration we cannot read is the status loop's problem to report,
		// not this one's.
		return
	}
	if reg.IdentityIssuerURL == issuerURL {
		// Already on disk; only our in-memory copy was stale.
		c.CloudAuth.IdentityIssuerURL = issuerURL
		return
	}

	reg.IdentityIssuerURL = issuerURL
	if err := registration.SaveRegistration(registrationDir, reg); err != nil {
		c.Log.Warn("failed to record the workload identity anchor reported by cloud",
			"issuer", issuerURL, "error", err)
		return
	}

	c.CloudAuth.IdentityIssuerURL = issuerURL
	c.Log.Info("recorded the workload identity anchor miren.cloud assigned this cluster",
		"issuer", issuerURL,
		"note", "run 'miren server identity-anchor cloud' to adopt it")
}
