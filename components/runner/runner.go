package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/sqlitedisk"
	"miren.dev/runtime/controllers/disk"
	"miren.dev/runtime/controllers/ingress"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/controllers/service"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/multierror"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/secret"
	remotesecret "miren.dev/runtime/pkg/secret/remote"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/exec"
	"miren.dev/runtime/version"
)

type RunnerConfig struct {
	Id            string `json:"id" cbor:"id" yaml:"id"`
	Name          string `json:"name" cbor:"name" yaml:"name"`
	ListenAddress string `json:"listen_address" cbor:"listen_address" yaml:"listen_address"`
	Workers       int    `json:"workers" cbor:"workers" yaml:"workers"`
	DataPath      string `json:"data_path" cbor:"data_path" yaml:"data_path"`

	// Optional RPC configuration for advanced setups
	// If not provided, a default insecure connection will be used
	// to connect to the server address.
	Config *clientconfig.Config `json:"config" cbor:"config" yaml:"config"`

	// Optional cloud authentication configuration for disk replication
	CloudAuth *coordinate.CloudAuthConfig `json:"cloud_auth,omitempty" cbor:"cloud_auth,omitempty" yaml:"cloud_auth,omitempty"`

	// DiskMode configures disk I/O mode ("", "auto", "universal", "accelerator")
	DiskMode string `json:"disk_mode,omitempty" cbor:"disk_mode,omitempty" yaml:"disk_mode,omitempty"`
}

// RunnerDeps holds dependencies needed by the Runner to construct controllers.
type RunnerDeps struct {
	CC        *containerd.Client
	Namespace string
	Bridge    string
	Tempdir   string
	Subnet    *netdb.Subnet

	// Network dependencies
	NetServ *network.ServiceManager

	// Observability dependencies
	LogsMaintainer *observability.LogsMaintainer
	LogWriter      observability.LogWriter
	StatusMon      *observability.StatusMonitor

	// Network config
	IPv4Routable    netip.Prefix
	ServicePrefixes []netip.Prefix
	DisableLocalNet bool

	// Resolver
	Resolver netresolve.Resolver

	// Sandbox metrics
	SandboxMetrics *sandbox.Metrics

	// MetricsWriter is where host-level resource series are published. It is
	// the same writer the sandbox collectors use, taken directly rather than
	// through them because node series are labeled by node rather than by
	// sandbox. Nil disables host metrics, as it does for sandbox metrics.
	MetricsWriter *metrics.VictoriaMetricsWriter

	// IsCoordinator indicates this runner is the coordinator node.
	// Affects scheduling: stateful sandboxes are routed to the coordinator.
	IsCoordinator bool

	// Flannel network configuration (for distributed runners)
	// If EtcdEndpoints is non-empty, the runner will join the Flannel network
	EtcdEndpoints []string
	EtcdPrefix    string

	// TLS configuration for etcd mTLS (for distributed runners, file paths)
	EtcdTLSCertFile string // Client certificate file path
	EtcdTLSKeyFile  string // Client private key file path
	EtcdTLSCAFile   string // CA certificate file path

	// WorkloadIssuer mints workload identity tokens for sandbox containers. On
	// the coordinator this is the concrete *workloadidentity.Issuer; on a
	// distributed runner it is a remote issuer that proxies minting to the
	// coordinator over RPC.
	WorkloadIssuer workloadidentity.TokenIssuer

	// ApiAddress is where sandboxes on this host reach the cluster API, as a
	// literal IP:port. On the coordinator that is the local bridge router; on a
	// distributed runner it is the coordinator itself. Empty disables
	// in-cluster API access.
	ApiAddress string

	// CACert is the cluster CA in PEM form, mounted into sandboxes so they can
	// verify the API certificate.
	CACert []byte

	// Secrets materializes the secret references a sandbox spec carries, at
	// container creation. On the coordinator this is the local backend
	// registry, where the keyring lives. A distributed runner holds no key
	// material, so it must resolve over RPC instead; until that exists, a
	// sandbox on such a runner fails rather than starting without its secret.
	Secrets secret.Resolver
}

const (
	DefaulWorkers = 3

	// Bounded retry for the coordinator's workload-issuer-info query at startup.
	issuerInfoMaxAttempts = 3
	issuerInfoRetryDelay  = 2 * time.Second
)

type shutdownCloser struct{ s interface{ Shutdown() } }

func (c shutdownCloser) Close() error { c.s.Shutdown(); return nil }

type waitCloser struct{ s interface{ Wait() } }

func (c waitCloser) Close() error { c.s.Wait(); return nil }

type stopCloser struct{ s interface{ Stop() } }

func (c stopCloser) Close() error { c.s.Stop(); return nil }

func NewRunner(log *slog.Logger, deps RunnerDeps, cfg RunnerConfig) (*Runner, error) {
	if cfg.DataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}

	if cfg.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	if deps.CC == nil {
		return nil, fmt.Errorf("containerd client is required")
	}

	return &Runner{
		RunnerConfig: cfg,
		Log:          log.With("module", "runner"),
		deps:         deps,
	}, nil
}

type Runner struct {
	RunnerConfig

	Log *slog.Logger

	deps RunnerDeps

	cc *containerd.Client

	// sessMu guards ec/se, which are swapped when the health session is
	// re-established after a lost lease (see superviseSession).
	sessMu sync.Mutex
	ec     *entityserver.Client
	se     *entityserver.Session

	closers []io.Closer

	namespace string

	sbController sandbox.SandboxLifecycle

	// hubs is the stdio fan-out for attachable containers, shared between the
	// sandbox controller that creates them and the exec server that joins
	// clients to them. Both live in this process, which is what makes an
	// in-memory registry the right shape.
	hubs *sandbox.HubRegistry

	// Disk controllers, stored for SetRestartMode propagation
	dvc    *diskio.DiskVolumeController
	dmc    *diskio.DiskMountController
	diskGC *diskio.DeletedVolumeGC

	// Replicates SQLite-provider disks to the coordinator. Nil when the backup
	// service is unreachable, in which case it is inert and disks still attach.
	sqliteDisks *sqlitedisk.Manager
}

func (r *Runner) Close() error {
	var err error

	for _, c := range r.closers {
		xerr := c.Close()
		if xerr != nil {
			err = multierror.Append(err, xerr)
		}
	}

	return err
}

// SetRestartMode sets whether outboard processes should be preserved when closing.
// When true, disk mounts are left in place so the replacement process can pick them up.
func (r *Runner) SetRestartMode(v bool) {
	if r.dvc != nil {
		r.dvc.SetKeepMounts(v)
	}
	if r.dmc != nil {
		r.dmc.SetKeepMounts(v)
	}
}

// entityClient returns the current session-scoped entity client, which may be
// swapped out when the health session is re-established (see superviseSession).
func (r *Runner) entityClient() *entityserver.Client {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	return r.ec
}

// nodeId returns this runner's own node identity in canonical node/<raw>
// form. r.Id is the raw runner id from config; everything that references this
// node in the entity store goes through here so the node-id prefix is applied
// consistently in exactly one place (see compute_v1alpha.NewNodeId).
func (r *Runner) nodeId() compute_v1alpha.NodeId {
	return compute_v1alpha.NewNodeId(r.Id)
}

// Drain sets the runner's node status to disabled and stops all running sandboxes
func (r *Runner) Drain(ctx context.Context) error {
	ec := r.entityClient()
	if ec == nil || r.Id == "" {
		return fmt.Errorf("runner not initialized with entity client")
	}

	r.Log.Info("draining runner", "id", r.Id)

	// Set node status to disabled
	r.Log.Info("setting node status to disabled", "id", r.Id)
	err := ec.UpdateAttrs(ctx, r.nodeId().Id(), (&compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	}).Encode)
	if err != nil {
		return fmt.Errorf("failed to set node status to disabled: %w", err)
	}

	r.Log.Info("node status set to disabled", "id", r.Id)

	// List all sandboxes scheduled to this node
	idx := compute_v1alpha.Index(compute_v1alpha.KindSandbox, r.nodeId().Id())
	results, err := ec.List(ctx, idx)
	if err != nil {
		return fmt.Errorf("failed to query sandboxes on node: %w", err)
	}

	sandboxCount := results.Length()
	r.Log.Info("found sandboxes to drain", "count", sandboxCount, "node", r.Id)

	// Stop each sandbox
	var drainErr error
	stoppedCount := 0
	for results.Next() {
		md := results.Metadata()
		if md == nil {
			continue
		}

		r.Log.Info("stopping sandbox", "id", md.ID)
		err := r.sbController.Delete(ctx, md.ID, nil)
		if err != nil {
			r.Log.Error("failed to stop sandbox", "id", md.ID, "error", err)
			drainErr = multierror.Append(drainErr, fmt.Errorf("failed to stop sandbox %s: %w", md.ID, err))
		} else {
			r.Log.Info("stopped sandbox", "id", md.ID)
			stoppedCount++
		}
	}

	if drainErr != nil {
		return fmt.Errorf("errors during drain: %w", drainErr)
	}

	r.Log.Info("runner drained successfully", "id", r.Id, "sandboxes_stopped", stoppedCount)
	return nil
}

func (r *Runner) ContainerdNamespace() string {
	return r.namespace
}

func (r *Runner) ContainerdContainerForSandbox(ctx context.Context, id entity.Id) (containerd.Container, error) {
	cl, err := r.cc.ContainerService().List(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range cl {
		if c.Labels["runtime.computer/entity-id"] == string(id) {
			return r.cc.LoadContainer(ctx, c.ID)
		}
	}

	return nil, nil
}

// Start initializes and starts the runner.
// The optional errgroup parameter is used for running background tasks like the Flannel network.
// If eg is nil and the runner needs to join a Flannel network, an internal errgroup will be created.
func (r *Runner) Start(ctx context.Context, eg ...*errgroup.Group) error {
	r.Log.Info("Starting runner", "id", r.Id)

	// Initialize Flannel/WireGuard network if distributed runner configuration is provided
	if len(r.deps.EtcdEndpoints) > 0 {
		if err := r.initializeNetwork(ctx, eg...); err != nil {
			return fmt.Errorf("failed to initialize network: %w", err)
		}
	}

	var (
		rs     *rpc.State
		err    error
		client *rpc.NetworkClient
	)

	r.Log.Info("Establishing RPC connection", "listen", r.ListenAddress, "distributed", r.Config != nil)

	if r.Config == nil {
		rs, err = rpc.NewState(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress), rpc.WithSkipVerify)
		if err != nil {
			return err
		}

		client, err = rs.Connect("", "entities")
		if err != nil {
			return err
		}
	} else {
		rs, err = r.Config.State(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress))
		if err != nil {
			return err
		}

		client, err = rs.Client("entities")
		if err != nil {
			return err
		}
	}

	r.Log.Info("Connected to entity server")

	eas := es.NewEntityAccessClient(client)

	ec := entityserver.NewClient(r.Log, eas)

	// Distributed runners mint workload identity tokens via the coordinator,
	// since they do not hold the cluster signing key. A failure here degrades
	// to no sandbox tokens rather than blocking runner startup.
	if err := r.setupRemoteWorkloadIssuer(ctx, rs); err != nil {
		r.Log.Warn("failed to set up workload identity issuer", "error", err)
	}

	// Likewise for secrets: a distributed runner can reach the entity store but
	// not the cluster keyring, so it resolves through the coordinator. This one
	// must not degrade quietly — a runner without it starts sandboxes whose
	// referenced secrets can never materialize.
	if err := r.setupRemoteSecrets(rs); err != nil {
		return fmt.Errorf("setting up secret resolution: %w", err)
	}

	r.setupSqliteDisks(rs)

	cm, err := r.SetupControllers(ctx, eas, rs.Server())
	if err != nil {
		return err
	}

	r.Log.Info("Controllers initialized")

	err = r.setupEntity(ctx, ec)
	if err != nil {
		return err
	}

	// Create exec server with explicit dependencies
	execServer := exec.NewServer(r.Log, r.deps.CC, eas, r.deps.Namespace, r.hubs)

	rs.Server().ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(execServer))

	r.Log.Info("Registered exec server")

	err = cm.Start(ctx)
	if err != nil {
		return err
	}

	r.Log.Info("Runner running", "id", r.Id)

	return nil
}

// WorkloadIssuer returns the issuer this runner mints identity tokens through,
// or nil if none is available.
//
// It is only meaningful after Start, which is where a distributed runner
// acquires its issuer from the coordinator. Callers that build something
// needing tokens before then should hold a source they can arm afterwards
// rather than reading this early and caching a nil.
func (r *Runner) WorkloadIssuer() workloadidentity.TokenIssuer {
	return r.deps.WorkloadIssuer
}

// setupSqliteDisks connects to the coordinator's SQLite backup service so
// sqlite-provider disks are replicated as they are written.
//
// A failure here is not fatal. The manager stays nil, which is inert, so disks
// still attach and apps still run — they are simply not backed up. Losing
// backups is better than refusing to start workloads.
//
// There is no reconnect: nothing retries this, so backups stay off for the
// lifetime of the process and an operator has to restart the runner once the
// coordinator is reachable. That is why the failure logs at Error rather than
// Warn, and why each sqlite disk says so again as it attaches.
func (r *Runner) setupSqliteDisks(rs *rpc.State) {
	var (
		client *rpc.NetworkClient
		err    error
	)
	if r.Config == nil {
		client, err = rs.Connect("", string(rpc.ServiceSqliteBackup))
	} else {
		client, err = rs.Client(string(rpc.ServiceSqliteBackup))
	}
	if err != nil {
		r.Log.Error("sqlite disk backups disabled for the life of this runner: cannot reach coordinator backup service; restart the runner once it is reachable", "error", err)
		return
	}

	r.sqliteDisks = sqlitedisk.NewManager(r.Log, sqlitebackup_v1alpha.NewSqliteBackupClient(client))
	r.closers = append(r.closers, sqliteDiskCloser{r.sqliteDisks})

	r.Log.Info("sqlite disk backups enabled")
}

// sqliteDiskCloser gives the final replication sync its own context. The
// runner's context is already cancelled by the time closers run, and a
// cancelled context would abandon the last transactions instead of shipping
// them.
type sqliteDiskCloser struct{ m *sqlitedisk.Manager }

func (c sqliteDiskCloser) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.m.Close(ctx)
}

// setupRemoteWorkloadIssuer wires a remote workload identity issuer for
// distributed runners. Runners do not hold the cluster signing key, so they
// mint tokens by calling the coordinator's RunnerRegistration service. When the
// coordinator reports no issuer is configured, token issuance stays disabled
// (deps.WorkloadIssuer remains nil). The coordinator's embedded runner
// (r.Config == nil) keeps the concrete issuer it was constructed with.
func (r *Runner) setupRemoteWorkloadIssuer(ctx context.Context, rs *rpc.State) error {
	if r.Config == nil || r.deps.WorkloadIssuer != nil {
		return nil
	}

	client, err := rs.Client(string(rpc.ServiceRunner))
	if err != nil {
		return fmt.Errorf("connecting to coordinator runner service: %w", err)
	}

	regClient := runner_v1alpha.NewRunnerRegistrationClient(client)

	// Retry transient failures: the entities connection was just established, so
	// a failure here is usually a brief blip. Giving up immediately would leave
	// the runner with no token issuance until it is restarted.
	var info *runner_v1alpha.RunnerRegistrationClientWorkloadIssuerInfoResults
	for attempt := 1; ; attempt++ {
		info, err = queryWorkloadIssuerInfo(ctx, regClient)
		if err == nil {
			break
		}
		if attempt >= issuerInfoMaxAttempts {
			return fmt.Errorf("querying workload issuer info after %d attempts: %w", attempt, err)
		}
		r.Log.Warn("workload issuer info query failed; retrying",
			"attempt", attempt, "max", issuerInfoMaxAttempts, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(issuerInfoRetryDelay):
		}
	}

	if !info.Enabled() {
		r.Log.Info("coordinator has no workload identity issuer; sandbox tokens disabled")
		return nil
	}

	r.deps.WorkloadIssuer = newRemoteIssuer(ctx, regClient, info.IssuerUrl())
	r.Log.Info("workload identity issuer enabled via coordinator", "issuer", info.IssuerUrl())
	return nil
}

// queryWorkloadIssuerInfo performs a single WorkloadIssuerInfo call bounded by a
// per-attempt timeout, so a hung coordinator RPC cannot stall runner startup
// indefinitely and the retry budget is allowed to expire.
func queryWorkloadIssuerInfo(ctx context.Context, regClient *runner_v1alpha.RunnerRegistrationClient) (*runner_v1alpha.RunnerRegistrationClientWorkloadIssuerInfoResults, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTokenTimeout)
	defer cancel()
	return regClient.WorkloadIssuerInfo(ctx)
}

// setupRemoteSecrets points a distributed runner's secret resolution at the
// coordinator. Runners do not hold the cluster keyring, so they cannot decrypt
// a stored secret themselves.
//
// The coordinator's embedded runner (r.Config == nil) already holds the local
// backend registry it was constructed with, and keeps it.
func (r *Runner) setupRemoteSecrets(rs *rpc.State) error {
	if r.Config == nil || r.deps.Secrets != nil {
		return nil
	}

	client, err := rs.Client("dev.miren.runtime/secrets")
	if err != nil {
		return fmt.Errorf("connecting to coordinator secrets service: %w", err)
	}

	r.deps.Secrets = remotesecret.NewResolver(secret_v1alpha.NewSecretsClient(client))
	r.Log.Info("secret resolution enabled via coordinator")
	return nil
}

// initializeNetwork sets up the Flannel network for distributed runners.
// This is only called when EtcdEndpoints are configured (distributed runner mode).
func (r *Runner) initializeNetwork(ctx context.Context, eg ...*errgroup.Group) error {
	r.Log.Info("Initializing distributed runner network",
		"etcd_endpoints", r.deps.EtcdEndpoints,
		"etcd_prefix", r.deps.EtcdPrefix)

	grungeOpts := grunge.NetworkOptions{
		EtcdEndpoints: r.deps.EtcdEndpoints,
		EtcdPrefix:    r.deps.EtcdPrefix,
		PrevIPv4:      r.deps.IPv4Routable,
	}

	// Add TLS config if provided
	if r.deps.EtcdTLSCertFile != "" && r.deps.EtcdTLSKeyFile != "" && r.deps.EtcdTLSCAFile != "" {
		r.Log.Info("Using etcd TLS", "cert", r.deps.EtcdTLSCertFile, "ca", r.deps.EtcdTLSCAFile)
		grungeOpts.TLSCertFile = r.deps.EtcdTLSCertFile
		grungeOpts.TLSKeyFile = r.deps.EtcdTLSKeyFile
		grungeOpts.TLSCAFile = r.deps.EtcdTLSCAFile
	}

	gn, err := grunge.NewNetwork(r.Log, grungeOpts)
	if err != nil {
		return fmt.Errorf("failed to create grunge network: %w", err)
	}

	// Get or create an errgroup for running the network
	var runGroup *errgroup.Group
	localGroup := false
	if len(eg) > 0 && eg[0] != nil {
		runGroup = eg[0]
	} else {
		runGroup, ctx = errgroup.WithContext(ctx)
		localGroup = true
	}

	// Start the network (joins the Flannel mesh, doesn't setup config - coordinator did that)
	if err := gn.Start(ctx, runGroup); err != nil {
		return fmt.Errorf("failed to start grunge network: %w", err)
	}

	// If we created a local errgroup, monitor it so errors aren't silently lost
	if localGroup {
		go func() {
			if err := runGroup.Wait(); err != nil {
				r.Log.Error("network errgroup failed", "error", err)
			}
		}()
	}

	// Update deps with the leased IP and subnet
	lease := gn.Lease()
	r.deps.IPv4Routable = lease.IPv4()

	// Initialize netdb subnet from the flannel lease so the sandbox
	// controller can allocate IPs within this runner's subnet.
	ndb, err := netdb.New(filepath.Join(r.DataPath, "net.db"))
	if err != nil {
		return fmt.Errorf("failed to open netdb: %w", err)
	}
	subnet, err := ndb.Subnet(lease.IPv4().String())
	if err != nil {
		return fmt.Errorf("failed to create subnet from lease: %w", err)
	}
	r.deps.Subnet = subnet

	r.Log.Info("Joined Flannel network", "ipv4", lease.IPv4().String())

	return nil
}

// setupEntity establishes the runner's coordinator health session and node
// registration, then supervises the session so a lost lease (e.g. from a
// coordinator restart) is transparently re-established. base is the plain,
// non-session entity client; each session is minted from it.
func (r *Runner) setupEntity(ctx context.Context, base *entityserver.Client) error {
	if r.Id == "" {
		return nil
	}

	if err := r.establishSession(ctx, base); err != nil {
		return err
	}

	go r.superviseSession(ctx, base)

	return nil
}

// establishSession mints a fresh health session from base, registers the node
// entity, and marks it READY. The node's READY status is session-scoped: it
// lives under the etcd lease and vanishes when the lease dies, so this must be
// re-run to bring the runner back after a lost session. base is the plain
// (non-session) client; the session-scoped client it returns is stored on r.ec.
func (r *Runner) establishSession(ctx context.Context, base *entityserver.Client) error {
	r.Log.Info("Creating health session")

	sess, ec, err := base.NewSession(ctx, "runner health")
	if err != nil {
		return err
	}

	// If registration below fails we abandon this session, so revoke its
	// lease rather than leaking a keepalive goroutine and orphaned lease.
	// The retry path can otherwise pile these up on a flaky coordinator.
	published := false
	defer func() {
		if published {
			return
		}
		revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rerr := sess.Revoke(revokeCtx); rerr != nil {
			r.Log.Warn("failed to revoke unregistered health session", "error", rerr)
		}
	}()

	role := "runner"
	if r.deps.IsCoordinator {
		role = "coordinator"
	}

	node := compute_v1alpha.Node{
		RunnerId:    r.Id,
		Name:        r.Name,
		Constraints: types.LabelSet("compute", "generic", "role", role),
		ApiAddress:  r.ListenAddress,
		Version:     version.GetInfo().Version,
	}

	r.Log.Info("Registering node entity", "role", role, "address", r.ListenAddress)

	res, err := ec.CreateOrUpdate(ctx, r.Id, &node)
	if err != nil {
		return err
	}

	err = ec.UpdateAttrs(ctx, res, (&compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	}).Encode)
	if err != nil {
		return err
	}

	r.sessMu.Lock()
	r.ec = ec
	r.se = sess
	published = true
	r.sessMu.Unlock()

	r.Log.Info("Runner registered and ready", "id", res, "status", "ready")

	return nil
}

// superviseSession watches the current health session and re-establishes it
// when its lease is lost. Without this, a coordinator restart orphans the
// runner's lease, the session-scoped READY status is dropped, and the runner
// stays not-ready until manually restarted (MIR-1305). Re-establishing well
// within nodehealth's grace period keeps the runner's sandboxes from being
// evacuated.
func (r *Runner) superviseSession(ctx context.Context, base *entityserver.Client) {
	for {
		r.sessMu.Lock()
		se := r.se
		r.sessMu.Unlock()
		if se == nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-se.Dead():
		}

		// No explicit Close on the old session: by the time Dead() fires its
		// keepalive goroutine has already stopped and revoked what it could,
		// so establishSession can simply overwrite r.se with a fresh one.
		r.Log.Warn("runner health session lease lost, re-establishing", "id", r.Id)

		backoff := time.Second
		for {
			if ctx.Err() != nil {
				return
			}

			if err := r.establishSession(ctx, base); err == nil {
				r.Log.Info("runner health session re-established", "id", r.Id)
				break
			} else {
				r.Log.Error("failed to re-establish runner health session, retrying",
					"error", err, "backoff", backoff)
			}

			// Jitter the wait so a fleet of runners knocked offline by the
			// same coordinator restart doesn't reconnect in lockstep. Sleep a
			// random 50-100% of the current backoff.
			wait := backoff/2 + time.Duration(rand.Int64N(int64(backoff/2)+1))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}

			backoff = min(backoff*2, 30*time.Second)
		}
	}
}

func (r *Runner) SetupControllers(
	ctx context.Context,
	eas *es.EntityAccessClient,
	rs *rpc.Server,
) (
	_ *controller.ControllerManager,
	retErr error,
) {
	cm := controller.NewControllerManager()

	// Host-level metrics start here rather than alongside the other collectors
	// in boot because this is the one place that runs for both the
	// coordinator's embedded runner and a distributed one, and it is where the
	// node identity these series are keyed by is known.
	if r.deps.MetricsWriter != nil {
		nodeUsage := metrics.NewNodeUsage(r.Log, r.deps.MetricsWriter, r.nodeId().String(), r.Id, r.DataPath)
		go nodeUsage.Monitor(ctx)
	}

	// Initialize NetServ if not provided (distributed runner mode)
	if r.deps.NetServ == nil {
		r.deps.NetServ = network.NewServiceManager(r.Log, eas)
	}

	// Create sandbox controller with explicit dependencies
	r.hubs = sandbox.NewHubRegistry()

	sbcDeps := sandbox.SandboxControllerDeps{
		Hubs:           r.hubs,
		Log:            r.Log,
		CC:             r.deps.CC,
		EAC:            eas,
		Namespace:      r.deps.Namespace,
		NodeId:         r.nodeId(),
		NetServ:        r.deps.NetServ,
		Bridge:         r.deps.Bridge,
		Subnet:         r.deps.Subnet,
		DataPath:       r.DataPath,
		Tempdir:        r.deps.Tempdir,
		LogsMaintainer: r.deps.LogsMaintainer,
		LogWriter:      r.deps.LogWriter,
		StatusMon:      r.deps.StatusMon,
		Resolver:       r.deps.Resolver,
		Metrics:        r.deps.SandboxMetrics,
		WorkloadIssuer: r.deps.WorkloadIssuer,
		ApiAddress:     r.deps.ApiAddress,
		CACert:         r.deps.CACert,
		Secrets:        r.deps.Secrets,
		SqliteDisks:    r.sqliteDisks,
	}

	var sbc sandbox.SandboxLifecycle
	var sbcHandler controller.HandlerFunc

	if labs.Sagas() {
		sagaStorage := saga.NewEACStorage(eas, r.Log)
		sagaSbc, sagaErr := sandbox.NewSagaSandboxController(sbcDeps, sagaStorage, r.Log)
		if sagaErr != nil {
			return nil, fmt.Errorf("failed to create saga sandbox controller: %w", sagaErr)
		}
		sbc = sagaSbc
		sbcHandler = controller.AdaptController(sagaSbc)
	} else {
		origSbc, origErr := sandbox.NewSandboxController(sbcDeps)
		if origErr != nil {
			return nil, fmt.Errorf("failed to create sandbox controller: %w", origErr)
		}
		sbc = origSbc
		sbcHandler = controller.AdaptController(origSbc)
	}

	r.closers = append(r.closers, sbc)

	rs.ExposeValue("dev.miren.runtime/sandbox.metrics", metric_v1alpha.AdaptSandboxMetrics(sbcDeps.Metrics))

	// Create service controller with explicit dependencies
	serviceController, err := service.NewServiceController(service.ServiceControllerDeps{
		Log:             r.Log,
		EAC:             eas,
		IPv4Routable:    r.deps.IPv4Routable,
		ServicePrefixes: r.deps.ServicePrefixes,
		DisableLocalNet: r.deps.DisableLocalNet,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create service controller: %w", err)
	}

	log := r.Log

	defaultRouteAppController := ingress.NewDefaultRouteAppController(log, eas)
	defaultRouteController := ingress.NewDefaultRouteController(log, eas)

	workers := r.Workers
	if workers <= 0 {
		workers = DefaulWorkers
	}

	// Initialize disk I/O controllers for universal mode (loop devices)
	dataPath := filepath.Join(r.DataPath, "disk-data")
	err = os.MkdirAll(dataPath, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk data path: %w", err)
	}

	// Ensure loop device nodes exist (they may be missing in containers)
	if err := diskio.EnsureLoopDevices(log); err != nil {
		log.Warn("Loop devices not available, disk mounts will fail", "error", err)
	}

	// Try to set up lbd devices for accelerator mode
	if err := diskio.EnsureLbdDevices(log); err != nil {
		log.Info("lbd devices not available, accelerator mode will not work", "error", err)
	}

	diskioState, err := diskio.LoadState(dataPath)
	if err != nil {
		log.Warn("failed to load disk state, starting fresh", "error", err)
		diskioState = diskio.NewState()
		diskioState.SetPath(dataPath)
	}

	volOps := diskio.NewRealDiskVolumeOps(log)
	mntOps := diskio.NewRealDiskMountOps(log)

	r.dvc = diskio.NewDiskVolumeController(log, dataPath, r.nodeId(), diskioState, volOps, mntOps)
	r.dvc.SetEAC(eas)

	if err := r.dvc.Init(ctx); err != nil {
		return nil, fmt.Errorf("disk volume controller init: %w", err)
	}

	r.dmc = diskio.NewDiskMountController(log, dataPath, r.nodeId(), diskioState, mntOps)
	r.dmc.SetEAC(eas)

	// Prepare deleted volume GC (started after all controller init succeeds)
	r.diskGC = &diskio.DeletedVolumeGC{
		Log:      log.With("module", "deleted-volume-gc"),
		DataPath: dataPath,
		Config:   diskio.DefaultDeletedVolumeGCConfig(),
	}

	// Register volume controller shutdown before mount controller so mounts
	// owned by the volume controller are cleaned up first
	r.closers = append(r.closers, shutdownCloser{r.dvc})
	r.closers = append(r.closers, shutdownCloser{r.dmc})

	// Set up cloud auth for disk replication if configured
	var logUploader diskio.LogSegmentUploader
	var updatesClient diskio.CloudUpdatesClient
	if r.CloudAuth != nil && r.CloudAuth.Enabled && r.CloudAuth.PrivateKey != "" {
		cloudURL := r.CloudAuth.CloudURL
		if cloudURL == "" {
			cloudURL = coordinate.DefaultCloudURL
		}

		var keyData []byte
		if strings.HasPrefix(r.CloudAuth.PrivateKey, "-----BEGIN PRIVATE KEY-----") {
			keyData = []byte(r.CloudAuth.PrivateKey)
		} else {
			keyData, err = os.ReadFile(r.CloudAuth.PrivateKey)
			if err != nil {
				log.Warn("failed to load cloud auth private key for log watcher", "error", err)
			}
		}

		if keyData != nil {
			keyPair, kerr := cloudauth.LoadKeyPairFromPEM(string(keyData))
			if kerr != nil {
				log.Warn("failed to parse cloud auth private key for log watcher", "error", kerr)
			} else {
				authClient, aerr := cloudauth.NewAuthClient(cloudURL, keyPair)
				if aerr != nil {
					log.Warn("failed to create auth client for log watcher", "error", aerr)
				} else {
					// One client serves both kinds: lbd log segments for
					// accelerator mode, image snapshots for universal mode.
					updatesClient = diskio.NewCloudUpdatesClient(log, cloudURL, authClient)

					cloudDiskClient := diskio.NewCloudDiskClientWithUpdates(log, cloudURL, authClient, updatesClient)
					r.dmc.SetCloudClient(cloudDiskClient)
					r.dmc.SetUpdatesClient(updatesClient)

					// Universal volumes are mounted by the volume controller, so
					// that is where a lost image has to be noticed.
					r.dvc.SetUpdatesClient(updatesClient)

					// Give local disks an identity in the cloud. Without this
					// every backup call would carry an id the cloud has never
					// heard of.
					r.dvc.SetCloudVolumeRegistrar(
						diskio.NewCloudVolumeRegistrar(log, cloudURL, authClient),
						r.CloudAuth.ClusterID,
					)

					logUploader = diskio.NewCloudSegmentUploaderWithClient(log, updatesClient, diskioState)
				}
			}
		}
	}

	// Reconcile only after the cloud clients above are in place. Startup is
	// precisely when a host that lost its disks has images to restore, and a
	// pass that ran before the updates client was wired would see no cloud,
	// mount the volume, and format a fresh empty image over the gap.
	//
	// This also re-mounts universal volumes that were mounted before the last
	// shutdown, and gives unregistered volumes their first shot at a cloud id.
	if err := r.dvc.ReconcileWithEntities(ctx); err != nil {
		log.Warn("failed to reconcile disk volumes on startup", "error", err)
	}

	// Reconcile mounts with entity server on startup to re-mount any
	// accelerator volumes that were mounted before the last shutdown.
	if err := r.dmc.ReconcileWithEntities(ctx); err != nil {
		log.Warn("failed to reconcile disk mounts on startup", "error", err)
	}

	// Always start the log watcher so accelerator mode log segments are
	// cleaned up even when cloud is not configured (nil uploader = delete only).
	watcher := diskio.NewLogWatcher(log, diskioState, logUploader, 5*time.Second)
	go func() {
		if werr := watcher.Run(ctx); werr != nil {
			log.Error("log watcher stopped", "error", werr)
		}
	}()
	r.closers = append(r.closers, waitCloser{watcher})
	if logUploader != nil {
		log.Info("started log watcher with cloud upload")
	} else {
		log.Info("started log watcher in delete-only mode (no cloud configured)")
	}

	// Universal-mode volumes are not backed up on a timer. Snapshotting a
	// mounted image means reading it while the loop device is still writing,
	// which yields a state that never existed on disk -- see ImageSnapshotter.
	// `miren disk backup --cloud` takes one when an operator decides it is safe.

	volHandler := controller.AdaptReconcileController[storage_v1alpha.DiskVolume](r.dvc)
	cm.AddController(controller.NewReconcileController(
		"disk-volume", log, r.dvc.Index(), eas, volHandler, 5*time.Minute, workers,
	))

	mntHandler := controller.AdaptReconcileController[storage_v1alpha.DiskMount](r.dmc)
	mntController := controller.NewReconcileController(
		"disk-mount", log, r.dmc.Index(), eas, mntHandler, 5*time.Minute, workers,
	)
	// Record the mount controller's direct entity writes so the watch skips its
	// own events instead of self-retriggering reconcile in a tight loop (MIR-1345).
	r.dmc.SetWriteTracker(mntController.WriteTracker())
	// Periodically delete mounts whose backing volume is gone. Such a mount can
	// never reach its desired state; sweeping bounds how long an orphan lingers
	// and re-attempts a failing reconcile (MIR-1345).
	mntController.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		if err := r.dmc.ReconcileOrphanMounts(ctx, diskio.OrphanMountSweepGracePeriod); err != nil {
			log.Warn("periodic orphan mount sweep failed", "error", err)
		}
		return nil
	})
	cm.AddController(mntController)

	// Use entity mode controllers
	diskController := disk.NewDiskController(log, eas, r.nodeId(), r.DiskMode, r.deps.IsCoordinator)
	diskLeaseController := disk.NewDiskLeaseController(log, eas, r.nodeId(), r.DiskMode)

	// Add disk controller to closers list so it gets cleaned up on shutdown
	r.closers = append(r.closers, diskController)

	err = sbc.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = serviceController.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = diskController.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = diskLeaseController.Init(ctx)
	if err != nil {
		return nil, err
	}

	// All controllers initialized — safe to start the GC goroutine now
	r.diskGC.Start(ctx)
	r.closers = append(r.closers, stopCloser{r.diskGC})

	r.cc = r.deps.CC
	r.namespace = r.deps.Namespace
	r.sbController = sbc

	sbController := controller.NewReconcileController(
		"sandbox",
		log,
		compute_v1alpha.Index(compute_v1alpha.KindSandbox, r.nodeId().Id()),
		eas,
		sbcHandler,
		time.Minute,
		workers,
	)

	// Wire up write tracker so manual Patch calls can skip self-generated watch events
	sbc.SetWriteTracker(sbController.WriteTracker())

	sbController.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		return sbc.Periodic(ctx, time.Hour)
	})

	cm.AddController(sbController)

	svcController := controller.NewReconcileController(
		"service",
		log,
		entity.Ref(entity.EntityKind, network_v1alpha.KindService),
		eas,
		controller.AdaptController(serviceController),
		time.Minute,
		workers,
	)
	svcController.SetPeriodic(5*time.Minute, serviceController.Periodic)
	cm.AddController(svcController)

	cm.AddController(
		controller.NewReconcileController(
			"endpoints",
			log,
			entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints),
			eas,
			serviceController.UpdateEndpoints,
			0,
			workers,
		),
	)

	cm.AddController(
		controller.NewReconcileController(
			"default-route-app",
			log,
			entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
			eas,
			controller.AdaptController(defaultRouteAppController),
			0, // No periodic resync needed
			1, // Single worker is sufficient for this controller
		),
	)

	cm.AddController(
		controller.NewReconcileController(
			"default-route",
			log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute),
			eas,
			controller.AdaptController(defaultRouteController),
			0, // No periodic resync needed
			1, // Single worker is sufficient for this controller
		),
	)

	// Add disk controller
	diskRC := controller.NewReconcileController(
		"disk",
		log,
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk),
		eas,
		controller.AdaptController(diskController),
		time.Minute,
		workers,
	)
	cm.AddController(diskRC)

	// Add disk lease controller
	diskLeaseRC := controller.NewReconcileController(
		"disk-lease",
		log,
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease),
		eas,
		controller.AdaptController(diskLeaseController),
		time.Minute,
		workers,
	)

	// Set up periodic lease maintenance (every 5 minutes): sweep orphan leases
	// stranded by sandboxes that died without releasing (SIGKILL, boot failure),
	// then clean up old released leases. The orphan sweep also runs at Init, but
	// the periodic tick bounds the worst-case wedge to one interval for sandboxes
	// that die while the controller is already running.
	diskLeaseRC.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		if err := diskLeaseController.ReconcileOrphanLeases(ctx, disk.OrphanSweepGracePeriod); err != nil {
			log.Warn("periodic orphan lease sweep failed", "error", err)
		}
		return diskLeaseController.CleanupOldReleasedLeases(ctx)
	})

	cm.AddController(diskLeaseRC)

	// Add disk watch controller to trigger lease reconciliation on disk changes
	diskWatchController := disk.NewDiskWatchController(log, eas, diskLeaseRC)
	cm.AddController(
		controller.NewReconcileController(
			"disk-watch",
			log,
			entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk),
			eas,
			controller.AdaptController(diskWatchController),
			time.Minute,
			1,
		),
	)

	// Add disk_volume watch controller to trigger disk re-reconciliation when
	// disk_volume entities change (e.g. volume becomes DV_READY after provisioning)
	diskVolumeWatchController := disk.NewDiskVolumeWatchController(log, eas, diskRC, r.nodeId())
	cm.AddController(
		controller.NewReconcileController(
			"disk-volume-watch",
			log,
			entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume),
			eas,
			controller.AdaptController(diskVolumeWatchController),
			0,
			1,
		),
	)

	// Add disk_mount watch controller to trigger disk lease re-reconciliation when
	// disk_mount entities change (e.g. mount becomes DM_MOUNTED after mounting)
	diskMountWatchController := disk.NewDiskMountWatchController(log, eas, diskLeaseRC, r.nodeId())
	cm.AddController(
		controller.NewReconcileController(
			"disk-mount-watch",
			log,
			entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskMount),
			eas,
			controller.AdaptController(diskMountWatchController),
			0,
			1,
		),
	)

	return cm, nil
}
