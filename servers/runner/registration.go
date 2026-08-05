package runner

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/enrolltoken"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/joincode"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	DefaultInviteExpiryHours = 1
	MaxInviteExpiryHours     = 168 // 7 days

	enrollmentCountRetries = 3
	networkBackend         = "wireguard"
)

type RegistrationServerConfig struct {
	Log             *slog.Logger
	Authority       *caauth.Authority
	EAC             *entityserver_v1alpha.EntityAccessClient
	CoordinatorAddr string
	EtcdEndpoints   []string
	EtcdPrefix      string

	// Observability endpoints provided to runners at join time
	VictoriametricsAddress string
	VictorialogsAddress    string

	// WorkloadIssuer mints workload identity tokens. Distributed runners, which
	// do not hold the cluster signing key, request tokens from the coordinator
	// through this server. May be nil when no issuer is configured.
	WorkloadIssuer *workloadidentity.Issuer
}

type RegistrationServer struct {
	RegistrationServerConfig
}

var _ runner_v1alpha.RunnerRegistration = (*RegistrationServer)(nil)

func NewRegistrationServer(cfg RegistrationServerConfig) *RegistrationServer {
	cfg.Log = cfg.Log.With("module", "runner-registration")
	return &RegistrationServer{RegistrationServerConfig: cfg}
}

func (s *RegistrationServer) CreateInvite(ctx context.Context, req *runner_v1alpha.RunnerRegistrationCreateInvite) error {
	args := req.Args()
	results := req.Results()

	reusable := args.HasReusable() && args.Reusable()

	// Determine expiry
	now := time.Now()
	var expiresAt time.Time

	// The generated client always sends ttl_seconds, so we use a negative
	// sentinel (-1) to mean "not specified." The CLI sends -1 when --ttl
	// is omitted, 0 for --ttl 0 (no expiry), and >0 for an explicit TTL.
	ttl := int64(-1)
	if args.HasTtlSeconds() {
		ttl = args.TtlSeconds()
	}

	switch {
	case ttl < -1:
		return cond.ValidationFailure("invalid-ttl", "TTL must be non-negative (use 0 for no expiry)")
	case ttl == 0 && reusable:
		// --ttl 0 on a reusable token means no expiry
		expiresAt = time.Time{}
	case ttl > 0:
		if !reusable && ttl > int64(MaxInviteExpiryHours)*3600 {
			return cond.ValidationFailure("invalid-ttl", fmt.Sprintf("TTL cannot exceed %d hours for one-time tokens", MaxInviteExpiryHours))
		}
		expiresAt = now.Add(time.Duration(ttl) * time.Second)
	default:
		// ttl == -1 (not specified) or ttl == 0 on a non-reusable token:
		// fall through to expires_in_hours
		expiryHours := int32(DefaultInviteExpiryHours)
		if args.HasExpiresInHours() && args.ExpiresInHours() > 0 {
			expiryHours = args.ExpiresInHours()
		}
		if !reusable && expiryHours > int32(MaxInviteExpiryHours) {
			return cond.ValidationFailure("invalid-expiry", fmt.Sprintf("expiry cannot exceed %d hours", MaxInviteExpiryHours))
		}
		expiresAt = now.Add(time.Duration(expiryHours) * time.Hour)
	}

	secret, err := enrolltoken.GenerateSecret()
	if err != nil {
		s.Log.Error("Failed to generate secret", "error", err)
		return cond.Error("failed to generate invite secret")
	}

	codeHash := joincode.Hash(secret)

	invite := &runner_v1alpha.RunnerInvite{
		CodeHash:  codeHash,
		Status:    runner_v1alpha.PENDING,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		Reusable:  reusable,
	}

	if args.HasName() {
		invite.Name = args.Name()
	}

	if args.HasLabels() {
		for _, labelStr := range args.Labels() {
			parts := strings.SplitN(labelStr, "=", 2)
			if len(parts) == 2 {
				invite.Labels = append(invite.Labels, types.Label{Key: parts[0], Value: parts[1]})
			}
		}
	}

	// Build entity with an ident so it gets a stable, unique key
	inviteIdent := "runner_invite/" + codeHash[:16]
	rpcEntity := &entityserver_v1alpha.Entity{}
	rpcEntity.SetAttrs(
		entity.New(
			invite.Encode,
			entity.Ident, types.Keyword(inviteIdent),
		).Attrs())

	putResp, err := s.EAC.Put(ctx, rpcEntity)
	if err != nil {
		s.Log.Error("Failed to create invite entity", "error", err)
		return cond.Error("failed to create invite")
	}

	// Build the token with the coordinator address baked in
	addr := s.CoordinatorAddr
	if args.HasCoordinatorAddr() && args.CoordinatorAddr() != "" {
		addr = args.CoordinatorAddr()
	}
	token := enrolltoken.Encode(addr, secret)

	s.Log.Info("Created runner invite",
		"invite_id", putResp.Id(),
		"reusable", reusable,
		"name", invite.Name,
		"expires_at", expiresAt.Format(time.RFC3339),
		"label_count", len(invite.Labels))

	results.SetCode(token)
	results.SetExpiresAt(standard.ToTimestamp(expiresAt))

	return nil
}

func (s *RegistrationServer) Join(ctx context.Context, req *runner_v1alpha.RunnerRegistrationJoin) error {
	args := req.Args()
	results := req.Results()

	if !args.HasCode() || args.Code() == "" {
		results.SetError("join code is required")
		return nil
	}

	code := args.Code()
	if !enrolltoken.IsHexSecret(code) {
		results.SetError("invalid join code format")
		return nil
	}

	codeHash := joincode.Hash(code)

	invite, inviteRevision, err := s.findInviteByHash(ctx, codeHash)
	if err != nil {
		s.Log.Error("Failed to find invite", "error", err)
		results.SetError("failed to validate invite")
		return nil
	}
	if invite == nil {
		results.SetError("invalid or expired join code")
		return nil
	}

	if invite.Status != runner_v1alpha.PENDING {
		results.SetError("join code has already been used or revoked")
		return nil
	}

	if !invite.ExpiresAt.IsZero() && time.Now().After(invite.ExpiresAt) {
		results.SetError("join code has expired")
		return nil
	}

	runnerID := args.RunnerId()
	if runnerID == "" {
		runnerID = uuid.New().String()
	} else if _, err := uuid.Parse(runnerID); err != nil {
		results.SetError("runner_id must be a valid UUID")
		return nil
	}

	// Enforce runner_id uniqueness before claiming the invite or minting a
	// certificate. A runner_id maps directly to the mTLS certificate CommonName
	// (runner-<id>) that authorizes per-runner actions like minting workload
	// identity tokens for a sandbox, so a caller who could join as an id that
	// already backs a live runner would impersonate it. Only a client-supplied
	// id needs checking; a server-generated UUID is fresh by construction. The
	// node write below is create-only (its ident collapses to db/id node/<id>,
	// and CreateEntity is a put-if-absent), which is the atomic backstop against
	// a concurrent join racing this check. This read makes the rejection
	// explicit, keeps us from burning a one-time invite or issuing a cert on a
	// duplicate, and returns a clear, actionable error.
	if args.RunnerId() != "" {
		registered, err := s.runnerIDRegistered(ctx, runnerID)
		if err != nil {
			s.Log.Error("Failed to check for existing runner", "error", err, "runner_id", runnerID)
			results.SetError("failed to validate runner id")
			return nil
		}
		if registered {
			results.SetError(fmt.Sprintf("runner id %s is already registered; remove it first with 'miren runner remove %s'", runnerID, runnerID))
			return nil
		}
	}

	listenAddr := ""
	if args.HasListenAddr() {
		listenAddr = args.ListenAddr()
	}

	version := ""
	if args.HasVersion() {
		version = args.Version()
	}

	if !invite.Reusable {
		// One-time invite: claim it (PENDING->CLAIMED) with CAS to prevent
		// concurrent joins from minting multiple certificates
		invite.Status = runner_v1alpha.CLAIMED
		invite.ClaimedBy = runnerID
		invite.ClaimedAt = time.Now()

		updateAttrs := invite.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(invite.ID))
		updateEntity.SetAttrs(updateAttrs)
		updateEntity.SetRevision(inviteRevision)

		_, err = s.EAC.Put(ctx, updateEntity)
		if err != nil {
			s.Log.Error("Failed to update invite status", "error", err, "invite_id", invite.ID)
			results.SetError("failed to complete registration")
			return nil
		}
	}

	// Now that invite is claimed, issue the certificate with proper SANs
	// so the coordinator can connect to the runner's API by IP.
	certName := runnerCertName(runnerID)

	ips, dnsNames := buildRunnerSANs(listenAddr)

	cc, err := s.Authority.IssueCertificate(caauth.Options{
		CommonName:   certName,
		Organization: "miren",
		ValidFor:     365 * 24 * time.Hour,
		IPs:          ips,
		DNSNames:     dnsNames,
	})
	if err != nil {
		s.Log.Error("Failed to issue certificate", "error", err, "runner_id", runnerID)
		results.SetError("failed to issue certificate")
		return nil
	}

	labels := make(types.Labels, 0, len(invite.Labels))
	labels = append(labels, invite.Labels...)
	if args.HasLabels() {
		for _, labelStr := range args.Labels() {
			parts := strings.SplitN(labelStr, "=", 2)
			if len(parts) == 2 {
				labels = append(labels, types.Label{Key: parts[0], Value: parts[1]})
			}
		}
	}

	name := ""
	if args.HasName() {
		name = args.Name()
	}

	node := &compute_v1alpha.Node{
		RunnerId:     runnerID,
		Name:         name,
		ApiAddress:   listenAddr,
		Version:      version,
		RegisteredAt: time.Now(),
		Constraints:  labels,
	}

	// Create node entity with an ident so setupEntity can find it via CreateOrUpdate
	nodeEntity := &entityserver_v1alpha.Entity{}
	nodeEntity.SetAttrs(
		entity.New(
			(&core_v1alpha.Metadata{Name: runnerID}).Encode,
			node.Encode,
			entity.Ident, types.Keyword(node.ShortKind()+"/"+runnerID),
		).Attrs())

	nodePutResp, err := s.EAC.Put(ctx, nodeEntity)
	if err != nil {
		s.Log.Error("Failed to create node entity", "error", err, "runner_id", runnerID)
		results.SetError("failed to register runner")
		return nil
	}

	// Increment enrollment count after everything succeeded, so the count
	// only reflects runners that actually completed the join.
	if invite.Reusable {
		if err := s.incrementEnrollmentCount(ctx, invite, inviteRevision); err != nil {
			s.Log.Warn("Failed to increment enrollment count (runner joined successfully)",
				"error", err, "invite_id", invite.ID, "runner_id", runnerID)
		}
	}

	s.Log.Info("Runner joined successfully",
		"runner_id", runnerID,
		"name", name,
		"node_id", nodePutResp.Id(),
		"listen_addr", listenAddr,
		"version", version,
		"label_count", len(labels))

	results.SetCertPem(cc.CertPEM)
	results.SetKeyPem(cc.KeyPEM)
	results.SetCaPem(cc.CACert)
	results.SetCoordinatorAddr(s.CoordinatorAddr)
	results.SetRunnerId(runnerID)

	// Provide network configuration for distributed runners
	if len(s.EtcdEndpoints) > 0 {
		results.SetEtcdEndpoints(s.EtcdEndpoints)
	}
	if s.EtcdPrefix != "" {
		results.SetEtcdPrefix(s.EtcdPrefix + "/sub/flannel")
	}
	// Keep sending this field for older runner binaries, which persist the
	// backend selected by the coordinator at join time.
	results.SetNetworkBackend(networkBackend)
	if s.VictoriametricsAddress != "" {
		results.SetVictoriametricsAddress(s.VictoriametricsAddress)
	}
	if s.VictorialogsAddress != "" {
		results.SetVictorialogsAddress(s.VictorialogsAddress)
	}

	return nil
}

// buildRunnerSANs returns the IP and DNS subject alternative names a runner's
// server certificate should carry: always loopback (127.0.0.1, ::1, localhost)
// plus the host from the runner's advertised listen address (an IP becomes an
// IP SAN, a hostname becomes a DNS SAN).
func buildRunnerSANs(listenAddr string) ([]net.IP, []string) {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}
	dnsNames := []string{"localhost"}

	if listenAddr != "" {
		host, _, err := net.SplitHostPort(listenAddr)
		if err == nil && host != "" {
			if ip := net.ParseIP(host); ip != nil {
				ips = append(ips, ip)
			} else if host != "localhost" {
				dnsNames = append(dnsNames, host)
			}
		}
	}

	return ips, dnsNames
}

// RefreshCertificate re-issues the calling runner's server certificate with SANs
// derived from its current listen address. A runner needs this when its listen
// IP changes but its persisted certificate (e.g. on a disk that outlives the VM)
// still carries the old IP. The method is public at the RPC layer but authorizes
// the caller here: the presented client certificate must chain to the cluster CA
// and be a runner certificate, and the re-issued certificate keeps that
// certificate's CommonName so a runner can only refresh its own identity.
func (s *RegistrationServer) RefreshCertificate(ctx context.Context, req *runner_v1alpha.RunnerRegistrationRefreshCertificate) error {
	args := req.Args()
	results := req.Results()

	info := rpc.ConnectionInfo(ctx)
	var peer *x509.Certificate
	if info != nil {
		peer = info.PeerCertificate
	}

	listenAddr := ""
	if args.HasListenAddr() {
		listenAddr = args.ListenAddr()
	}

	cc, err := s.reissueRunnerCertificate(ctx, peer, listenAddr)
	if err != nil {
		results.SetError(err.Error())
		return nil
	}

	results.SetCertPem(cc.CertPEM)
	results.SetKeyPem(cc.KeyPEM)
	results.SetCaPem(cc.CACert)

	return nil
}

// reissueRunnerCertificate authorizes the caller solely by its presented client
// certificate and, if valid, issues a fresh runner server certificate with SANs
// derived from listenAddr. The new certificate keeps the caller's CommonName so
// a runner can only refresh its own identity. Authorization requires that the
// presented certificate is a CA-signed runner certificate and that a runner
// matching its identity is still registered (so a removed runner's still-valid
// certificate cannot perpetually renew itself). The runner identity is taken
// from the verified certificate, never from caller-supplied input. The returned
// error is safe to surface to the caller.
func (s *RegistrationServer) reissueRunnerCertificate(ctx context.Context, peer *x509.Certificate, listenAddr string) (*caauth.ClientCertificate, error) {
	if peer == nil {
		return nil, fmt.Errorf("a client certificate is required to refresh a certificate")
	}

	if err := s.Authority.VerifyCert(peer); err != nil {
		s.Log.Warn("RefreshCertificate rejected: peer cert not signed by cluster CA",
			"error", err, "subject", peer.Subject.String())
		return nil, fmt.Errorf("client certificate is not trusted")
	}

	commonName := peer.Subject.CommonName
	runnerID, ok := strings.CutPrefix(commonName, "runner-")
	if !ok || runnerID == "" || !slices.Contains(peer.Subject.Organization, "miren") {
		s.Log.Warn("RefreshCertificate rejected: peer cert is not a runner certificate",
			"subject", peer.Subject.String())
		return nil, fmt.Errorf("client certificate is not a runner certificate")
	}

	// Confirm the runner is still registered. caauth has no revocation, so this
	// is what prevents a removed runner's still-valid certificate from renewing
	// itself indefinitely. The runner ID is taken from the verified certificate's
	// CommonName (which embeds the full ID), so the caller cannot substitute
	// another runner's identity.
	node, _, err := s.findNodeByQuery(ctx, runnerID)
	if err != nil {
		s.Log.Error("RefreshCertificate failed to verify runner registration",
			"error", err, "subject", peer.Subject.String())
		return nil, fmt.Errorf("failed to verify runner registration")
	}
	if node == nil {
		s.Log.Warn("RefreshCertificate rejected: runner is not registered",
			"subject", peer.Subject.String())
		return nil, fmt.Errorf("runner is not registered")
	}

	ips, dnsNames := buildRunnerSANs(listenAddr)

	cc, err := s.Authority.IssueCertificate(caauth.Options{
		CommonName:   commonName,
		Organization: "miren",
		ValidFor:     365 * 24 * time.Hour,
		IPs:          ips,
		DNSNames:     dnsNames,
	})
	if err != nil {
		s.Log.Error("Failed to re-issue certificate", "error", err, "common_name", commonName)
		return nil, fmt.Errorf("failed to issue certificate")
	}

	s.Log.Info("Re-issued runner certificate",
		"common_name", commonName,
		"listen_addr", listenAddr)

	return cc, nil
}

func (s *RegistrationServer) ListInvites(ctx context.Context, req *runner_v1alpha.RunnerRegistrationListInvites) error {
	results := req.Results()

	listResp, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, runner_v1alpha.KindRunnerInvite))
	if err != nil {
		s.Log.Error("Failed to list invites", "error", err)
		return cond.Error("failed to list invites")
	}

	now := time.Now()
	invites := make([]*runner_v1alpha.InviteInfo, 0)

	for _, e := range listResp.Values() {
		var invite runner_v1alpha.RunnerInvite
		decodeEntity(e, &invite)

		if invite.Status == runner_v1alpha.PENDING && !invite.ExpiresAt.IsZero() && now.After(invite.ExpiresAt) {
			continue
		}

		info := &runner_v1alpha.InviteInfo{}
		info.SetId(e.Id())
		info.SetStatus(string(invite.Status))

		labelStrs := make([]string, 0, len(invite.Labels))
		for _, l := range invite.Labels {
			labelStrs = append(labelStrs, fmt.Sprintf("%s=%s", l.Key, l.Value))
		}
		info.SetLabels(labelStrs)

		info.SetExpiresAt(standard.ToTimestamp(invite.ExpiresAt))
		info.SetCreatedAt(standard.ToTimestamp(invite.CreatedAt))

		if invite.ClaimedBy != "" {
			info.SetClaimedBy(invite.ClaimedBy)
			info.SetClaimedAt(standard.ToTimestamp(invite.ClaimedAt))
		}

		if invite.Name != "" {
			info.SetName(invite.Name)
		}
		info.SetReusable(invite.Reusable)
		if invite.EnrollmentCount > 0 {
			info.SetEnrollmentCount(invite.EnrollmentCount)
		}

		invites = append(invites, info)
	}

	results.SetInvites(invites)
	return nil
}

func (s *RegistrationServer) RevokeInvite(ctx context.Context, req *runner_v1alpha.RunnerRegistrationRevokeInvite) error {
	args := req.Args()
	results := req.Results()

	if !args.HasInviteId() || args.InviteId() == "" {
		results.SetError("invite_id is required")
		return nil
	}

	inviteID := args.InviteId()

	inviteResp, err := s.EAC.Get(ctx, inviteID)
	if err != nil {
		s.Log.Error("Failed to get invite", "invite_id", inviteID, "error", err)
		results.SetError("invite not found")
		return nil
	}

	var invite runner_v1alpha.RunnerInvite
	decodeEntity(inviteResp.Entity(), &invite)

	if invite.Status != runner_v1alpha.PENDING {
		results.SetError(fmt.Sprintf("cannot revoke invite in %s state", invite.Status))
		return nil
	}

	invite.Status = runner_v1alpha.REVOKED

	updateAttrs := invite.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(inviteID)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(inviteResp.Entity().Revision())

	_, err = s.EAC.Put(ctx, updateEntity)
	if err != nil {
		s.Log.Error("Failed to revoke invite", "invite_id", inviteID, "error", err)
		results.SetError("failed to revoke invite")
		return nil
	}

	s.Log.Info("Revoked runner invite", "invite_id", inviteID)
	results.SetSuccess(true)
	return nil
}

func (s *RegistrationServer) ListRunners(ctx context.Context, req *runner_v1alpha.RunnerRegistrationListRunners) error {
	results := req.Results()

	listResp, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		s.Log.Error("Failed to list nodes", "error", err)
		return cond.Error("failed to list runners")
	}

	runners := make([]*runner_v1alpha.RunnerInfo, 0)

	for _, e := range listResp.Values() {
		var node compute_v1alpha.Node
		decodeEntity(e, &node)

		if node.RunnerId == "" {
			continue
		}

		info := &runner_v1alpha.RunnerInfo{}
		info.SetId(string(node.ID))
		info.SetRunnerId(node.RunnerId)
		name := node.Name
		if name == "" {
			name = string(node.ID)
		}
		info.SetName(name)

		if sid := e.Entity().ShortId(); sid != "" {
			info.SetShortId(sid)
		}
		info.SetStatus(string(node.Status))
		info.SetVersion(node.Version)
		info.SetApiAddress(node.ApiAddress)

		labelStrs := make([]string, 0, len(node.Constraints))
		for _, l := range node.Constraints {
			labelStrs = append(labelStrs, fmt.Sprintf("%s=%s", l.Key, l.Value))
		}
		info.SetLabels(labelStrs)

		if !node.RegisteredAt.IsZero() {
			info.SetRegisteredAt(standard.ToTimestamp(node.RegisteredAt))
		}

		if node.Scheduling != "" {
			info.SetScheduling(strings.TrimPrefix(string(node.Scheduling), "scheduling."))
		}

		runners = append(runners, info)
	}

	results.SetRunners(runners)
	return nil
}

func (s *RegistrationServer) RemoveRunner(ctx context.Context, req *runner_v1alpha.RunnerRegistrationRemoveRunner) error {
	args := req.Args()
	results := req.Results()

	if !args.HasQuery() || args.Query() == "" {
		results.SetError("runner name or ID is required")
		return nil
	}

	query := args.Query()
	force := args.HasForce() && args.Force()

	// Find the node entity matching the query
	node, nodeID, err := s.findNodeByQuery(ctx, query)
	if err != nil {
		s.Log.Error("Failed to find runner", "query", query, "error", err)
		results.SetError(err.Error())
		return nil
	}
	if node == nil {
		results.SetError(fmt.Sprintf("runner %q not found", query))
		return nil
	}

	// Check for active schedules (sandboxes assigned to this node).
	// Skip the check entirely when --force is set so that a query error
	// (e.g. missing index) can't block a forced removal.
	if !force {
		scheduleCount, err := s.countNodeSchedules(ctx, nodeID)
		if err != nil {
			s.Log.Error("Failed to check schedules", "node_id", nodeID, "error", err)
			results.SetError("failed to check for active sandboxes")
			return nil
		}

		if scheduleCount > 0 {
			results.SetError(fmt.Sprintf("runner has %d active sandbox schedule(s); use --force to remove anyway", scheduleCount))
			return nil
		}
	}

	// Clean up associated resources
	removedResources := int32(0)

	// Delete schedules for this node (only needed on --force; the non-force
	// path already rejected if any schedules existed).
	if force {
		deleted, err := s.deleteNodeSchedules(ctx, nodeID)
		if err != nil {
			s.Log.Warn("Failed to delete schedules (continuing with --force)", "node_id", nodeID, "error", err)
		} else {
			removedResources += int32(deleted)
		}
	}

	// Delete disk mounts, volumes, and leases for this node
	for _, ref := range []entity.Attr{
		entity.Ref(storage_v1alpha.DiskMountNodeIdId, nodeID),
		entity.Ref(storage_v1alpha.DiskVolumeNodeIdId, nodeID),
		entity.Ref(storage_v1alpha.DiskLeaseNodeIdId, nodeID),
	} {
		deleted, err := s.deleteEntitiesByIndex(ctx, ref)
		if err != nil {
			s.Log.Warn("Failed to clean up some resources", "index", ref.ID, "error", err)
		}
		removedResources += int32(deleted)
	}

	// Delete the node entity
	_, err = s.EAC.Delete(ctx, string(nodeID))
	if err != nil {
		s.Log.Error("Failed to delete node entity", "node_id", nodeID, "error", err)
		results.SetError("failed to delete runner")
		return nil
	}

	name := node.Name
	if name == "" {
		name = string(nodeID)
	}

	s.Log.Info("Removed runner",
		"name", name,
		"runner_id", node.RunnerId,
		"node_id", nodeID,
		"removed_resources", removedResources)

	results.SetName(name)
	results.SetRunnerId(node.RunnerId)
	results.SetRemovedResources(removedResources)
	return nil
}

const (
	// drainDefaultTimeout bounds how long DrainRunner waits for a node's
	// sandboxes to be descheduled before returning with timed_out set.
	drainDefaultTimeout = 2 * time.Minute
	// drainPollInterval is how often DrainRunner re-checks the node's schedule
	// count while waiting for it to empty.
	drainPollInterval = 500 * time.Millisecond
)

// setNodeScheduling writes the persistent scheduling eligibility on a node.
// Unlike the session-scoped Status attribute (which the runner resets to READY
// on every rejoin), this survives runner restarts, so a cordoned node stays
// parked until an operator explicitly uncordons it. The value is an explicit
// enum id (schedulable or cordoned), so uncordon is a plain write rather than a
// clear.
func (s *RegistrationServer) setNodeScheduling(ctx context.Context, nodeID entity.Id, scheduling entity.Id) error {
	attrs := []entity.Attr{
		entity.Ref(entity.DBId, nodeID),
		entity.Ref(compute_v1alpha.NodeSchedulingId, scheduling),
	}
	_, err := s.EAC.Patch(ctx, attrs, 0)
	return err
}

// CordonRunner marks a runner unschedulable without disrupting its running
// sandboxes. The scheduler stops placing new work on the node; existing
// sandboxes keep running.
func (s *RegistrationServer) CordonRunner(ctx context.Context, req *runner_v1alpha.RunnerRegistrationCordonRunner) error {
	args := req.Args()
	results := req.Results()

	if !args.HasQuery() || args.Query() == "" {
		results.SetError("runner name or ID is required")
		return nil
	}
	query := args.Query()
	reason := ""
	if args.HasReason() {
		reason = args.Reason()
	}

	node, nodeID, err := s.findNodeByQuery(ctx, query)
	if err != nil {
		s.Log.Error("Failed to find runner", "query", query, "error", err)
		results.SetError(err.Error())
		return nil
	}
	if node == nil {
		results.SetError(fmt.Sprintf("runner %q not found", query))
		return nil
	}

	if err := s.setNodeScheduling(ctx, nodeID, compute_v1alpha.NodeSchedulingCordonedId); err != nil {
		s.Log.Error("Failed to cordon runner", "node_id", nodeID, "error", err)
		results.SetError("failed to cordon runner")
		return nil
	}

	name := node.Name
	if name == "" {
		name = string(nodeID)
	}
	s.Log.Info("Cordoned runner", "name", name, "runner_id", node.RunnerId, "node_id", nodeID, "reason", reason)
	results.SetName(name)
	results.SetRunnerId(node.RunnerId)
	return nil
}

// UncordonRunner clears a runner's cordon, making it eligible for scheduling
// again.
func (s *RegistrationServer) UncordonRunner(ctx context.Context, req *runner_v1alpha.RunnerRegistrationUncordonRunner) error {
	args := req.Args()
	results := req.Results()

	if !args.HasQuery() || args.Query() == "" {
		results.SetError("runner name or ID is required")
		return nil
	}
	query := args.Query()

	node, nodeID, err := s.findNodeByQuery(ctx, query)
	if err != nil {
		s.Log.Error("Failed to find runner", "query", query, "error", err)
		results.SetError(err.Error())
		return nil
	}
	if node == nil {
		results.SetError(fmt.Sprintf("runner %q not found", query))
		return nil
	}

	if err := s.setNodeScheduling(ctx, nodeID, compute_v1alpha.NodeSchedulingSchedulableId); err != nil {
		s.Log.Error("Failed to uncordon runner", "node_id", nodeID, "error", err)
		results.SetError("failed to uncordon runner")
		return nil
	}

	name := node.Name
	if name == "" {
		name = string(nodeID)
	}
	s.Log.Info("Uncordoned runner", "name", name, "runner_id", node.RunnerId, "node_id", nodeID)
	results.SetName(name)
	results.SetRunnerId(node.RunnerId)
	return nil
}

// DrainRunner cordons a runner and evicts its sandboxes so app controllers
// reschedule them onto other ready nodes. It marks each scheduled sandbox
// STOPPED — the same action the sandbox pool takes during a crash cooldown —
// which makes the owning runner tear down its local container (via its
// node-scoped watch on the sandbox status) and lets the pool controllers
// recreate the capacity elsewhere. The sandbox entities themselves are left in
// place (STOPPED, not deleted), as are disk mounts, volumes, and leases (unlike
// RemoveRunner --force). It blocks until no live sandboxes remain on the node or
// the timeout elapses, then reports how many sandboxes were evicted.
func (s *RegistrationServer) DrainRunner(ctx context.Context, req *runner_v1alpha.RunnerRegistrationDrainRunner) error {
	args := req.Args()
	results := req.Results()

	if !args.HasQuery() || args.Query() == "" {
		results.SetError("runner name or ID is required")
		return nil
	}
	query := args.Query()
	reason := ""
	if args.HasReason() {
		reason = args.Reason()
	}
	timeout := drainDefaultTimeout
	if args.HasTimeoutSeconds() && args.TimeoutSeconds() > 0 {
		timeout = time.Duration(args.TimeoutSeconds()) * time.Second
	}

	node, nodeID, err := s.findNodeByQuery(ctx, query)
	if err != nil {
		s.Log.Error("Failed to find runner", "query", query, "error", err)
		results.SetError(err.Error())
		return nil
	}
	if node == nil {
		results.SetError(fmt.Sprintf("runner %q not found", query))
		return nil
	}

	name := node.Name
	if name == "" {
		name = string(nodeID)
	}

	// Cordon first so the scheduler stops placing new work while we evict. The
	// flag is persistent, so the node stays drained across restarts until an
	// operator uncordons it (e.g. drain -> reissue cert -> uncordon).
	if err := s.setNodeScheduling(ctx, nodeID, compute_v1alpha.NodeSchedulingCordonedId); err != nil {
		s.Log.Error("Failed to cordon runner for drain", "node_id", nodeID, "error", err)
		results.SetError("failed to cordon runner")
		return nil
	}

	// Evict the node's sandboxes and wait until none remain live (or the timeout
	// elapses). The eviction sweep runs on every iteration rather than just
	// once, so a sandbox skipped by a transient stop failure — or one that slips
	// onto the node before the cordon fully takes effect — is retried instead of
	// stranding the drain until it times out. stopNodeSandboxes skips sandboxes
	// that are already terminal, so evicted accumulates only distinct stops.
	evicted := 0
	timedOut := false
	deadline := time.Now().Add(timeout)
	for {
		stopped, err := s.stopNodeSandboxes(ctx, nodeID)
		if err != nil {
			s.Log.Error("Failed to evict sandboxes", "node_id", nodeID, "error", err)
			results.SetError("failed to evict sandboxes from runner")
			return nil
		}
		evicted += stopped

		remaining, err := s.countLiveNodeSandboxes(ctx, nodeID)
		if err != nil {
			s.Log.Warn("Failed to poll node schedules during drain", "node_id", nodeID, "error", err)
			break
		}
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			timedOut = true
			s.Log.Warn("Drain timed out waiting for node to empty", "node_id", nodeID, "remaining", remaining)
			break
		}
		select {
		case <-ctx.Done():
			results.SetError("drain canceled")
			return nil
		case <-time.After(drainPollInterval):
		}
	}

	s.Log.Info("Drained runner",
		"name", name,
		"runner_id", node.RunnerId,
		"node_id", nodeID,
		"reason", reason,
		"evicted", evicted,
		"timed_out", timedOut)
	results.SetName(name)
	results.SetRunnerId(node.RunnerId)
	results.SetEvictedCount(int32(evicted))
	results.SetTimedOut(timedOut)
	return nil
}

// WorkloadIssuerInfo reports whether the coordinator has a workload identity
// issuer configured and, if so, its issuer URL. Distributed runners call this
// once at startup to decide whether to mint workload identity tokens via the
// coordinator.
func (s *RegistrationServer) WorkloadIssuerInfo(ctx context.Context, req *runner_v1alpha.RunnerRegistrationWorkloadIssuerInfo) error {
	results := req.Results()

	if s.WorkloadIssuer == nil {
		results.SetEnabled(false)
		return nil
	}

	results.SetEnabled(true)
	results.SetIssuerUrl(s.WorkloadIssuer.IssuerURL())
	return nil
}

// IssueWorkloadToken mints a workload identity token for a sandbox on behalf of
// a distributed runner, which does not hold the cluster signing key. The caller
// is an mTLS-authenticated runner.
func (s *RegistrationServer) IssueWorkloadToken(ctx context.Context, req *runner_v1alpha.RunnerRegistrationIssueWorkloadToken) error {
	args := req.Args()
	results := req.Results()

	if s.WorkloadIssuer == nil {
		results.SetError("workload identity issuer is not configured")
		return nil
	}

	if !args.HasSandboxId() || args.SandboxId() == "" {
		results.SetError("sandbox_id is required")
		return nil
	}
	sandboxID := args.SandboxId()

	// A runner may only mint tokens for sandboxes scheduled to it. This prevents
	// a compromised or buggy runner from obtaining identities for workloads
	// running on other runners.
	if err := s.authorizeSandboxOwnership(ctx, sandboxID); err != nil {
		s.Log.Warn("workload token request denied", "sandbox", sandboxID, "error", err)
		results.SetError("not authorized to issue a token for this sandbox")
		return nil
	}

	// Derive the app identity and role from the sandbox itself rather than
	// trusting the caller. The app is part of the token subject that external
	// verifiers federate on, and the role is the authority the token carries —
	// a runner must be able to forge neither.
	appName, role := s.resolveSandboxAppAndRole(ctx, sandboxID)

	opts := workloadidentity.TokenOptions{Role: role}
	if args.HasAudience() {
		opts.Audience = args.Audience()
	}
	if args.HasTtlSeconds() && args.TtlSeconds() > 0 {
		opts.TTL = time.Duration(args.TtlSeconds()) * time.Second
	}

	token, err := s.WorkloadIssuer.IssueTokenWithOptions(appName, sandboxID, opts)
	if err != nil {
		s.Log.Error("failed to issue workload identity token",
			"sandbox", sandboxID, "app", appName, "error", err)
		results.SetError("failed to issue token")
		return nil
	}

	results.SetToken(token)
	return nil
}

// runnerSystemWorkloads are the identities a distributed runner is allowed to
// request. A runner cannot mint an identity for a workload it does not run,
// notably one belonging only to the coordinator, whose identity opens strictly
// more than a runner's does.
//
// Adding an entry is the intended way to onboard a new runner-side workload.
var runnerSystemWorkloads = []workloadidentity.SystemWorkload{
	// The sandbox controller pulls images from the cluster-local registry.
	workloadidentity.SystemWorkloadSandboxController,

	// The telemetry writers ship metrics and logs off the runner. Metrics and
	// logs share one identity deliberately: both writers are constructed by the
	// same process from the same certificate at the same moment, so splitting
	// them would add an allowlist entry without adding a boundary.
	workloadidentity.SystemWorkloadTelemetryWriter,
}

// IssueSystemWorkloadToken mints a system workload identity token on behalf of a
// distributed runner, which does not hold the cluster signing key. The caller is
// an mTLS-authenticated runner, and the workload it may request is
// constrained by runnerSystemWorkloads.
func (s *RegistrationServer) IssueSystemWorkloadToken(ctx context.Context, req *runner_v1alpha.RunnerRegistrationIssueSystemWorkloadToken) error {
	args := req.Args()
	results := req.Results()

	if s.WorkloadIssuer == nil {
		results.SetError("workload identity issuer is not configured")
		return nil
	}

	if !args.HasSystemWorkload() || args.SystemWorkload() == "" {
		results.SetError("system workload is required")
		return nil
	}
	workload, err := workloadidentity.ParseSystemWorkload(args.SystemWorkload())
	if err != nil {
		s.Log.Warn("system workload token request denied", "system_workload", args.SystemWorkload(), "error", err)
		results.SetError("not authorized to issue a token for this system workload")
		return nil
	}

	if err := s.authorizeSystemWorkloadRequest(ctx, workload); err != nil {
		s.Log.Warn("system workload token request denied", "system_workload", workload, "error", err)
		results.SetError("not authorized to issue a token for this system workload")
		return nil
	}

	// The audience names the service this workload intends to call, so it is
	// caller-selected rather than coupled to the workload here. The receiving
	// service verifies both the audience and expected workload before granting
	// access.
	opts := workloadidentity.TokenOptions{}
	if args.HasAudience() {
		opts.Audience = args.Audience()
	}
	if args.HasTtlSeconds() && args.TtlSeconds() > 0 {
		opts.TTL = time.Duration(args.TtlSeconds()) * time.Second
	}

	token, err := s.WorkloadIssuer.IssueSystemWorkloadToken(workload, opts)
	if err != nil {
		s.Log.Error("failed to issue system workload identity token",
			"system_workload", workload, "error", err)
		results.SetError("failed to issue token")
		return nil
	}

	results.SetToken(token)
	return nil
}

// authorizeSystemWorkloadRequest verifies that the named workload is available
// on runners and that the caller presents a certificate for a runner that is
// still registered. The registration check bounds a decommissioned runner's
// access, since caauth has no revocation and its certificate stays valid until
// it expires.
func (s *RegistrationServer) authorizeSystemWorkloadRequest(ctx context.Context, workload workloadidentity.SystemWorkload) error {
	if !slices.Contains(runnerSystemWorkloads, workload) {
		return fmt.Errorf("system workload %q is not one a runner may request", workload)
	}

	identity, err := requireRunnerCertIdentity(ctx)
	if err != nil {
		return err
	}
	if identity == nil {
		return nil
	}

	runnerID, ok := strings.CutPrefix(identity.Subject, "runner-")
	if !ok || runnerID == "" {
		return fmt.Errorf("caller %q is not a runner certificate", identity.Subject)
	}

	registered, err := s.runnerIDRegistered(ctx, runnerID)
	if err != nil {
		return fmt.Errorf("verifying registration of runner %s: %w", runnerID, err)
	}
	if !registered {
		return fmt.Errorf("runner %s is not registered", runnerID)
	}

	return nil
}

// runnerCertName is the client-certificate CommonName issued to a runner during
// Join. It embeds the full runner ID so the coordinator can attribute an mTLS
// connection back to a specific runner and authorize per-runner actions.
//
// The full ID (rather than a prefix) is required for authorization: a runner
// chooses its own runner ID at Join, so a short prefix would let a malicious
// runner pick an ID whose prefix collides with a victim's cert name and mint
// tokens for the victim's sandboxes. A runner ID is a UUID, so "runner-" plus
// the ID stays within the certificate CommonName length limit.
func runnerCertName(runnerID string) string {
	return fmt.Sprintf("runner-%s", runnerID)
}

// authorizeSandboxOwnership verifies the authenticated caller is the runner the
// given sandbox is scheduled to. Callers authenticate with the mTLS client
// certificate issued during Join, whose CommonName maps back to the runner via
// runnerCertName. When authentication is disabled (anonymous), there is nothing
// to verify against and the check is skipped.
func (s *RegistrationServer) authorizeSandboxOwnership(ctx context.Context, sandboxID string) error {
	identity, err := requireRunnerCertIdentity(ctx)
	if err != nil {
		return err
	}
	if identity == nil {
		return nil
	}

	sbResp, err := s.EAC.Get(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("looking up sandbox %s: %w", sandboxID, err)
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(sbResp.Entity().Entity())
	if sch.Key.Node == "" {
		return fmt.Errorf("sandbox %s is not scheduled to a node", sandboxID)
	}

	nodeResp, err := s.EAC.Get(ctx, string(sch.Key.Node))
	if err != nil {
		return fmt.Errorf("looking up node %s: %w", sch.Key.Node, err)
	}

	var node compute_v1alpha.Node
	node.Decode(nodeResp.Entity().Entity())
	if node.RunnerId == "" {
		return fmt.Errorf("node %s has no runner id", sch.Key.Node)
	}

	if expected := runnerCertName(node.RunnerId); identity.Subject != expected {
		return fmt.Errorf("caller %q is not the owner of sandbox %s (owned by %q)",
			identity.Subject, sandboxID, expected)
	}

	return nil
}

// requireRunnerCertIdentity validates the common authentication boundary for
// runner token issuance. An anonymous identity means authentication is disabled
// on this coordinator, so there is no certificate identity to authorize against.
// NoAuth is only used by in-process test helpers and is not configurable in a
// real deployment.
func requireRunnerCertIdentity(ctx context.Context) (*rpc.Identity, error) {
	identity := rpc.IdentityFromContext(ctx)
	if identity == nil {
		return nil, fmt.Errorf("no caller identity")
	}
	if identity.Method == rpc.AuthMethodAnonymous {
		return nil, nil
	}
	if identity.Method != rpc.AuthMethodCert {
		return nil, fmt.Errorf("caller must authenticate with a runner certificate, got %q", identity.Method)
	}
	return identity, nil
}

// resolveSandboxApp derives the application name for a sandbox from the entity
// store (sandbox -> app version -> app metadata name), mirroring the sandbox
// controller's local resolution. The app name is part of the workload identity
// token subject, so it must be derived server-side rather than trusted from the
// calling runner. Returns "" when the app cannot be resolved.
func (s *RegistrationServer) resolveSandboxApp(ctx context.Context, sandboxID string) string {
	name, _ := s.resolveSandboxAppAndRole(ctx, sandboxID)
	return name
}

// resolveSandboxAppAndRole additionally returns the app's workload role. Like
// the app name, the role is derived server-side and never trusted from the
// runner — it is the authority the minted token carries.
func (s *RegistrationServer) resolveSandboxAppAndRole(ctx context.Context, sandboxID string) (name, role string) {
	sbResp, err := s.EAC.Get(ctx, sandboxID)
	if err != nil {
		return "", ""
	}

	var sb compute_v1alpha.Sandbox
	sb.Decode(sbResp.Entity().Entity())
	if sb.Spec.Version == "" {
		return "", ""
	}

	versionResp, err := s.EAC.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		return "", ""
	}

	var version core_v1alpha.AppVersion
	version.Decode(versionResp.Entity().Entity())
	if version.App == "" {
		return "", ""
	}

	appResp, err := s.EAC.Get(ctx, version.App.String())
	if err != nil {
		return "", ""
	}

	appEnt := appResp.Entity().Entity()

	var appMeta core_v1alpha.Metadata
	appMeta.Decode(appEnt)

	var app core_v1alpha.App
	app.Decode(appEnt)

	return appMeta.Name, app.WorkloadRole
}

// runnerIDRegistered reports whether a node is already registered with the
// given runner_id. Unlike findNodeByQuery, it matches only the exact RunnerId,
// never a node name, entity id, or short id. runner_id is the value that maps
// to the authorizing certificate CommonName (runner-<id>), so it is the
// property Join must keep unique; a node whose human name happens to equal an
// incoming UUID must not be mistaken for a duplicate.
func (s *RegistrationServer) runnerIDRegistered(ctx context.Context, runnerID string) (bool, error) {
	listResp, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return false, err
	}
	for _, e := range listResp.Values() {
		var node compute_v1alpha.Node
		decodeEntity(e, &node)
		if node.RunnerId == runnerID {
			return true, nil
		}
	}
	return false, nil
}

// findNodeByQuery looks up a node entity by name, runner ID, entity ID, or short ID prefix.
// Exact matches (name, runner ID, entity ID) are returned immediately. Prefix
// matches are collected and only returned when unambiguous.
func (s *RegistrationServer) findNodeByQuery(ctx context.Context, query string) (*compute_v1alpha.Node, entity.Id, error) {
	listResp, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return nil, "", err
	}

	query = strings.TrimSpace(query)

	type candidate struct {
		node compute_v1alpha.Node
		id   entity.Id
	}
	var prefixMatches []candidate

	for _, e := range listResp.Values() {
		var node compute_v1alpha.Node
		decodeEntity(e, &node)

		if node.RunnerId == "" {
			continue
		}

		id := entity.Id(e.Id())

		// Exact match by entity ID, runner ID, name, or short ID
		if string(id) == query ||
			node.RunnerId == query ||
			(node.Name != "" && node.Name == query) ||
			e.Entity().ShortId() == query {
			return &node, id, nil
		}

		// Prefix match by entity ID
		if strings.HasPrefix(string(id), query) {
			prefixMatches = append(prefixMatches, candidate{node, id})
		}
	}

	switch len(prefixMatches) {
	case 0:
		return nil, "", nil
	case 1:
		return &prefixMatches[0].node, prefixMatches[0].id, nil
	default:
		return nil, "", fmt.Errorf("ambiguous query %q matches %d runners", query, len(prefixMatches))
	}
}

func (s *RegistrationServer) countNodeSchedules(ctx context.Context, nodeID entity.Id) (int, error) {
	listResp, err := s.EAC.List(ctx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, nodeID))
	if err != nil {
		return 0, err
	}
	return len(listResp.Values()), nil
}

func (s *RegistrationServer) deleteNodeSchedules(ctx context.Context, nodeID entity.Id) (int, error) {
	listResp, err := s.EAC.List(ctx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, nodeID))
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, e := range listResp.Values() {
		if _, err := s.EAC.Delete(ctx, e.Id()); err != nil {
			s.Log.Warn("Failed to delete schedule", "id", e.Id(), "error", err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

// sandboxTerminal reports whether a sandbox status is terminal (no longer
// counts as live capacity on a node).
func sandboxTerminal(status compute_v1alpha.SandboxStatus) bool {
	return status == compute_v1alpha.STOPPED || status == compute_v1alpha.DEAD
}

// stopNodeSandboxes marks every non-terminal sandbox scheduled to a node as
// STOPPED, the same action the sandbox pool takes during a crash cooldown. The
// owning runner observes the status change through its node-scoped watch and
// tears down the local container; the pool controllers then recreate the
// capacity on other ready nodes. It returns the number of sandboxes stopped.
func (s *RegistrationServer) stopNodeSandboxes(ctx context.Context, nodeID entity.Id) (int, error) {
	listResp, err := s.EAC.List(ctx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, nodeID))
	if err != nil {
		return 0, err
	}

	stopped := 0
	for _, e := range listResp.Values() {
		var sb compute_v1alpha.Sandbox
		decodeEntity(e, &sb)
		if sandboxTerminal(sb.Status) {
			continue
		}

		attrs := append([]entity.Attr{entity.Ref(entity.DBId, entity.Id(e.Id()))},
			(&compute_v1alpha.Sandbox{Status: compute_v1alpha.STOPPED}).Encode()...)
		if _, err := s.EAC.Patch(ctx, attrs, 0); err != nil {
			s.Log.Warn("Failed to stop sandbox during drain", "id", e.Id(), "error", err)
			continue
		}
		stopped++
	}
	return stopped, nil
}

// countLiveNodeSandboxes counts sandboxes scheduled to a node that are not yet
// terminal. Drain uses this to wait until the node has been fully vacated.
func (s *RegistrationServer) countLiveNodeSandboxes(ctx context.Context, nodeID entity.Id) (int, error) {
	listResp, err := s.EAC.List(ctx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, nodeID))
	if err != nil {
		return 0, err
	}

	live := 0
	for _, e := range listResp.Values() {
		var sb compute_v1alpha.Sandbox
		decodeEntity(e, &sb)
		if !sandboxTerminal(sb.Status) {
			live++
		}
	}
	return live, nil
}

func (s *RegistrationServer) deleteEntitiesByIndex(ctx context.Context, ref entity.Attr) (int, error) {
	listResp, err := s.EAC.List(ctx, ref)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, e := range listResp.Values() {
		if _, err := s.EAC.Delete(ctx, e.Id()); err != nil {
			s.Log.Warn("Failed to delete entity", "id", e.Id(), "error", err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

// incrementEnrollmentCount atomically increments the enrollment count on a
// reusable invite. It retries on CAS contention.
func (s *RegistrationServer) incrementEnrollmentCount(ctx context.Context, invite *runner_v1alpha.RunnerInvite, revision int64) error {
	for attempt := 0; attempt < enrollmentCountRetries; attempt++ {
		if attempt > 0 {
			// Re-read the invite to get the latest revision and count
			refreshed, rev, err := s.findInviteByHash(ctx, invite.CodeHash)
			if err != nil {
				return fmt.Errorf("re-reading invite: %w", err)
			}
			if refreshed == nil {
				return fmt.Errorf("invite no longer exists")
			}
			invite = refreshed
			revision = rev
		}

		// Re-check state in case the invite was revoked or expired
		// between the initial check and this attempt
		if invite.Status != runner_v1alpha.PENDING {
			return fmt.Errorf("invite is no longer pending")
		}
		if !invite.ExpiresAt.IsZero() && time.Now().After(invite.ExpiresAt) {
			return fmt.Errorf("invite has expired")
		}

		invite.EnrollmentCount++

		updateAttrs := invite.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(invite.ID))
		updateEntity.SetAttrs(updateAttrs)
		updateEntity.SetRevision(revision)

		_, err := s.EAC.Put(ctx, updateEntity)
		if err == nil {
			return nil
		}

		s.Log.Warn("CAS contention incrementing enrollment count, retrying",
			"attempt", attempt+1,
			"invite_id", invite.ID,
			"error", err)
	}
	return fmt.Errorf("failed to increment enrollment count after %d retries", enrollmentCountRetries)
}

func (s *RegistrationServer) findInviteByHash(ctx context.Context, codeHash string) (*runner_v1alpha.RunnerInvite, int64, error) {
	listResp, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, runner_v1alpha.KindRunnerInvite))
	if err != nil {
		return nil, 0, err
	}

	for _, e := range listResp.Values() {
		var invite runner_v1alpha.RunnerInvite
		decodeEntity(e, &invite)
		if invite.CodeHash == codeHash {
			return &invite, e.Revision(), nil
		}
	}

	return nil, 0, nil
}

func decodeEntity(rpcEntity *entityserver_v1alpha.Entity, target interface{}) {
	type decoder interface {
		Decode(entity.AttrGetter)
	}

	if d, ok := target.(decoder); ok {
		d.Decode(&rpcEntityWrapper{entity: rpcEntity})
	}
}

type rpcEntityWrapper struct {
	entity *entityserver_v1alpha.Entity
}

func (w *rpcEntityWrapper) Get(id entity.Id) (entity.Attr, bool) {
	if id == entity.DBId {
		return entity.Ref(entity.DBId, entity.Id(w.entity.Id())), true
	}

	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == id {
			return attr, true
		}
	}
	return entity.Attr{}, false
}

func (w *rpcEntityWrapper) GetAll(name entity.Id) []entity.Attr {
	var result []entity.Attr
	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == name {
			result = append(result, attr)
		}
	}
	return result
}

func (w *rpcEntityWrapper) Attrs() []entity.Attr {
	return w.entity.Attrs()
}
