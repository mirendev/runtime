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
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/anywhere"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/cloudrpc"
	"miren.dev/runtime/pkg/containerenv"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entitysync"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/sysstats"
	"miren.dev/runtime/pkg/uplink"
	"miren.dev/runtime/servers/httpingress"
	"miren.dev/runtime/version"
)

// NewCloudControl constructs cloud status, identity publication, and uplink
// integration on top of cluster state.
func NewCloudControl(foundation *Foundation, diagnostics ...*entitysync.Diagnostics) *CloudControl {
	var entitySyncDiagnostics *entitysync.Diagnostics
	if len(diagnostics) > 0 {
		entitySyncDiagnostics = diagnostics[0]
	}
	if entitySyncDiagnostics == nil {
		entitySyncDiagnostics = entitysync.NewDiagnostics(core_v1alpha.CloudExportContract.Digest())
	}
	return &CloudControl{Foundation: foundation, entitySyncDiagnostics: entitySyncDiagnostics}
}

// CloudControl owns cloud status, identity publication, and uplink integration.
type CloudControl struct {
	*Foundation
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

	uplinkOptions := []uplink.ClientOption{uplink.WithStatus(c.entitySyncDiagnostics.ObserveUplink)}
	if labs.AppVisibility() {
		uplinkOptions = append(uplinkOptions, uplink.WithSession(version.GetInfo().Version))
	} else {
		c.entitySyncDiagnostics.SetCapabilityDisabled("app-visibility-disabled")
	}
	link := uplink.NewClient(
		cloudURL,
		c.authClient,
		uplink.NewMessageRouter(),
		c.Log.With("component", "uplink"),
		uplinkOptions...,
	)
	if labs.AppVisibility() {
		if err := entitysync.NewExporter(
			c.Log.With("component", "entity-sync"), c.store, core_v1alpha.CloudExportContract,
			entitysync.WithStartGate(entitySyncReady),
			entitysync.WithDiagnostics(c.entitySyncDiagnostics),
		).Register(ctx, link); err != nil {
			// Entity visibility is additive. Local source metadata should not take
			// Anywhere or cloud RPC off the shared link when it is unavailable.
			c.Log.Warn("entity sync is unavailable for this uplink session", "error", err)
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

	// Get CA certificate fingerprint
	var caFingerprint string
	if c.authority != nil {
		caCertPEM := c.authority.GetCACertificate()
		if caCertPEM != nil {
			// Parse the PEM to get the certificate
			block, _ := pem.Decode(caCertPEM)
			if block != nil && block.Type == "CERTIFICATE" {
				// Calculate SHA1 fingerprint of the raw DER bytes
				sum := sha1.Sum(block.Bytes)
				caFingerprint = hex.EncodeToString(sum[:])
			}
		}
	}

	// Run netcheck to determine public reachability
	c.runNetcheck(ctx)

	// Build status report
	status := &cloudauth.StatusReport{
		ClusterID:         c.CloudAuth.ClusterID,
		APIAddresses:      c.apiAddresses(),
		CACertFingerprint: caFingerprint,
		Reachability:      c.reachabilityVerdict(),
		Containerized:     containerenv.InContainer(),
	}

	result, err := c.authClient.ReportClusterStatus(ctx, status)
	if err != nil {
		return err
	}

	c.recordIdentityAnchor(result.IdentityIssuerURL)
	return nil
}

// ReportStatus reports the current cluster status to miren.cloud
func (c *CloudControl) ReportStatus(ctx context.Context) error {
	if c.authClient == nil {
		return fmt.Errorf("auth client not configured")
	}

	if c.CloudAuth.ClusterID == "" {
		return fmt.Errorf("cluster ID not configured")
	}

	// Get version information
	versionInfo := version.GetInfo()

	// Count apps (workloads) from entity store
	var workloadCount int
	appList, err := c.eac.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		c.Log.Warn("failed to count apps for status report", "error", err)
	} else {
		workloadCount = len(appList.Values())
	}

	// Re-run netcheck if the cached result is older than 60 minutes
	c.netcheckMu.RLock()
	netcheckAge := time.Since(c.netcheckCheckedAt)
	c.netcheckMu.RUnlock()
	if netcheckAge > 60*time.Minute {
		c.runNetcheck(ctx)
	}

	// Collect resource usage metrics
	resourceUsage := c.collectResourceUsage()

	// Build status report
	status := &cloudauth.StatusReport{
		ClusterID:     c.CloudAuth.ClusterID,
		State:         "active",
		Version:       versionInfo.Version,
		NodeCount:     1, // Static value for now
		WorkloadCount: workloadCount,
		ResourceUsage: resourceUsage,
		APIAddresses:  c.apiAddresses(),
		Reachability:  c.reachabilityVerdict(),
		Containerized: containerenv.InContainer(),
	}

	result, err := c.authClient.ReportClusterStatus(ctx, status)
	if err != nil {
		return err
	}

	c.recordIdentityAnchor(result.IdentityIssuerURL)
	return nil
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

	if err := c.ReportStatus(ctx); err != nil {
		c.Log.Error("failed to report initial cluster status", "error", err)
	} else {
		c.Log.Info("reported cluster status to cloud")
	}

	// Report status every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.ReportStatus(ctx); err != nil {
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
	if c.CloudAuth.IdentityIssuerURL == "" {
		return false
	}
	return c.WorkloadIssuer.IssuerURL() == c.CloudAuth.IdentityIssuerURL
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
