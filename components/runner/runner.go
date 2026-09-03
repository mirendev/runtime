package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"path/filepath"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/sqlitedisk"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/controllers/service"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
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

// NewClusterAccess constructs the runner's connection to cluster-owned state
// and capabilities.
func NewClusterAccess(log *slog.Logger, deps RunnerDeps, cfg RunnerConfig) (*ClusterAccess, error) {
	if cfg.DataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}

	if cfg.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	return &ClusterAccess{
		RunnerConfig: cfg,
		Log:          log.With("module", "runner"),
		deps:         deps,
	}, nil
}

// NewSandboxHost constructs the part of a runner that owns local workload
// execution. Starting it joins the distributed network when needed, adopts
// surviving containers, and exposes the node-local exec endpoints. It does not
// publish the node as schedulable; NodePresence owns that later transition.
func NewSandboxHost(access *ClusterAccess, storage *NodeStorage, deps RunnerDeps, cfg RunnerConfig) (*SandboxHost, error) {
	if access == nil {
		return nil, fmt.Errorf("cluster access is required")
	}
	if storage == nil {
		return nil, fmt.Errorf("node storage is required")
	}
	if deps.CC == nil {
		return nil, fmt.Errorf("containerd client is required")
	}
	return &SandboxHost{
		RunnerConfig: cfg,
		Log:          access.Log,
		deps:         deps,
		access:       access,
		storage:      storage,
	}, nil
}

// Runner reconstitutes the independently startable pieces of the runner role.
// The standalone runner command keeps this convenience API; the server boot
// graph starts and stops the pieces as separate owned components.
type Runner struct {
	Access       *ClusterAccess
	Storage      *NodeStorage
	Host         *SandboxHost
	StorageAgent *StorageAgent
	SandboxAgent *SandboxAgent
	Presence     *NodePresence
}

func NewRunner(log *slog.Logger, deps RunnerDeps, cfg RunnerConfig) (*Runner, error) {
	access, err := NewClusterAccess(log, deps, cfg)
	if err != nil {
		return nil, err
	}
	storage, err := NewNodeStorage(access, deps, cfg)
	if err != nil {
		return nil, err
	}
	host, err := NewSandboxHost(access, storage, deps, cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{
		Access:       access,
		Storage:      storage,
		Host:         host,
		StorageAgent: NewStorageAgent(storage),
		SandboxAgent: NewSandboxAgent(host),
		Presence:     NewNodePresence(host),
	}, nil
}

// ClusterAccess owns the RPC state and cluster-backed capabilities used by a
// runner. It has no container or host-network responsibilities.
type ClusterAccess struct {
	RunnerConfig
	Log  *slog.Logger
	deps RunnerDeps

	state       *rpc.State
	eac         *es.EntityAccessClient
	entityBase  *entityserver.Client
	sqliteDisks *sqlitedisk.Manager
	closers     []io.Closer
}

// SandboxHost owns the node-local execution substrate.
type SandboxHost struct {
	RunnerConfig

	Log *slog.Logger

	deps    RunnerDeps
	access  *ClusterAccess
	storage *NodeStorage

	cc *containerd.Client

	closers []io.Closer

	namespace string

	sbController sandbox.SandboxLifecycle

	// hubs is the stdio fan-out for attachable containers, shared between the
	// sandbox controller that creates them and the exec server that joins
	// clients to them. Both live in this process, which is what makes an
	// in-memory registry the right shape.
	hubs *sandbox.HubRegistry

	controllers *controller.ControllerManager
}

// NodePresence owns the session-scoped node registration. The graph starts it
// only after the node's storage and sandbox agents are running.
type NodePresence struct {
	host *SandboxHost

	// sessMu guards ec/se and closed. The session pointers are swapped when the health session is
	// re-established after a lost lease (see superviseSession).
	sessMu sync.Mutex
	ec     *entityserver.Client
	se     *entityserver.Session
	closed bool
}

var errNodePresenceClosed = errors.New("node presence is closed")

// NewNodePresence constructs the node registration boundary for a restored host.
func NewNodePresence(host *SandboxHost) *NodePresence {
	return &NodePresence{host: host}
}

func (r *Runner) Start(ctx context.Context, eg ...*errgroup.Group) error {
	if err := r.Access.Start(ctx); err != nil {
		return err
	}
	if err := r.Storage.Start(ctx); err != nil {
		return err
	}
	if err := r.Host.Start(ctx, eg...); err != nil {
		return err
	}
	if err := r.StorageAgent.Start(ctx); err != nil {
		return err
	}
	if err := r.SandboxAgent.Start(ctx); err != nil {
		return err
	}
	return r.Presence.Start(ctx)
}

func (r *Runner) Close() error {
	return errors.Join(
		r.Presence.Close(), r.SandboxAgent.Close(), r.StorageAgent.Close(),
		r.Host.Close(), r.Storage.Close(), r.Access.Close(),
	)
}

func (r *Runner) SetRestartMode(v bool)           { r.Storage.SetRestartMode(v) }
func (r *Runner) Drain(ctx context.Context) error { return r.Presence.Drain(ctx) }
func (r *Runner) ContainerdNamespace() string     { return r.Host.ContainerdNamespace() }
func (r *Runner) ContainerdContainerForSandbox(ctx context.Context, id entity.Id) (containerd.Container, error) {
	return r.Host.ContainerdContainerForSandbox(ctx, id)
}
func (r *Runner) WorkloadIssuer() workloadidentity.TokenIssuer { return r.Access.WorkloadIssuer() }

func (r *SandboxHost) Close() error {
	var err error

	for _, c := range r.closers {
		xerr := c.Close()
		if xerr != nil {
			err = multierror.Append(err, xerr)
		}
	}

	return err
}

// entityClient returns the current session-scoped entity client, which may be
// swapped out when the health session is re-established (see superviseSession).
func (r *NodePresence) entityClient() *entityserver.Client {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	return r.ec
}

// nodeId returns this runner's own node identity in canonical node/<raw>
// form. r.Id is the raw runner id from config; everything that references this
// node in the entity store goes through here so the node-id prefix is applied
// consistently in exactly one place (see compute_v1alpha.NewNodeId).
func (r *SandboxHost) nodeId() compute_v1alpha.NodeId {
	return compute_v1alpha.NewNodeId(r.Id)
}

// Drain sets the runner's node status to disabled and stops all running sandboxes
func (r *NodePresence) Drain(ctx context.Context) error {
	h := r.host
	ec := r.entityClient()
	if ec == nil || h.Id == "" {
		return fmt.Errorf("runner not initialized with entity client")
	}

	h.Log.Info("draining runner", "id", h.Id)

	// Set node status to disabled
	h.Log.Info("setting node status to disabled", "id", h.Id)
	err := ec.UpdateAttrs(ctx, h.nodeId().Id(), (&compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	}).Encode)
	if err != nil {
		return fmt.Errorf("failed to set node status to disabled: %w", err)
	}

	h.Log.Info("node status set to disabled", "id", h.Id)

	// List all sandboxes scheduled to this node
	idx := compute_v1alpha.Index(compute_v1alpha.KindSandbox, h.nodeId().Id())
	results, err := ec.List(ctx, idx)
	if err != nil {
		return fmt.Errorf("failed to query sandboxes on node: %w", err)
	}

	sandboxCount := results.Length()
	h.Log.Info("found sandboxes to drain", "count", sandboxCount, "node", h.Id)

	// Stop each sandbox
	var drainErr error
	stoppedCount := 0
	for results.Next() {
		md := results.Metadata()
		if md == nil {
			continue
		}

		h.Log.Info("stopping sandbox", "id", md.ID)
		err := h.sbController.Delete(ctx, md.ID, nil)
		if err != nil {
			h.Log.Error("failed to stop sandbox", "id", md.ID, "error", err)
			drainErr = multierror.Append(drainErr, fmt.Errorf("failed to stop sandbox %s: %w", md.ID, err))
		} else {
			h.Log.Info("stopped sandbox", "id", md.ID)
			stoppedCount++
		}
	}

	if drainErr != nil {
		return fmt.Errorf("errors during drain: %w", drainErr)
	}

	h.Log.Info("runner drained successfully", "id", h.Id, "sandboxes_stopped", stoppedCount)
	return nil
}

func (r *SandboxHost) ContainerdNamespace() string {
	return r.namespace
}

func (r *SandboxHost) ContainerdContainerForSandbox(ctx context.Context, id entity.Id) (containerd.Container, error) {
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

func (r *ClusterAccess) Start(ctx context.Context) (retErr error) {
	var (
		rs     *rpc.State
		err    error
		client *rpc.NetworkClient
	)

	r.Log.Info("establishing cluster access", "listen", r.ListenAddress, "distributed", r.Config != nil)
	if r.Config == nil {
		rs, err = rpc.NewState(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress), rpc.WithSkipVerify)
	} else {
		rs, err = r.Config.State(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress))
	}
	if err != nil {
		return err
	}
	defer func() {
		if retErr == nil || rs == nil {
			return
		}
		if err := rs.Close(); err != nil {
			r.Log.Warn("failed to close cluster access after startup failure", "error", err)
		}
		if r.state == rs {
			r.state = nil
		}
	}()
	if r.Config == nil {
		client, err = rs.Connect("", "entities")
	} else {
		client, err = rs.Client("entities")
	}
	if err != nil {
		return err
	}

	r.state = rs
	r.eac = es.NewEntityAccessClient(client)
	r.entityBase = entityserver.NewClient(r.Log, r.eac)
	if err := r.setupRemoteWorkloadIssuer(ctx, rs); err != nil {
		r.Log.Warn("failed to set up workload identity issuer", "error", err)
	}
	if err := r.setupRemoteSecrets(rs); err != nil {
		return fmt.Errorf("setting up secret resolution: %w", err)
	}
	r.setupSqliteDisks(rs)
	r.Log.Info("cluster access ready")
	return nil
}

func (r *ClusterAccess) Close() error {
	var errs []error
	for _, closer := range r.closers {
		errs = append(errs, closer.Close())
	}
	if r.state != nil {
		errs = append(errs, r.state.Close())
	}
	return errors.Join(errs...)
}

// Start restores the local execution substrate without making the node
// schedulable. The optional errgroup runs distributed-network background work.
// The optional errgroup parameter is used for running background tasks like the Flannel network.
// If eg is nil and the runner needs to join a Flannel network, an internal errgroup will be created.
func (r *SandboxHost) Start(ctx context.Context, eg ...*errgroup.Group) error {
	r.Log.Info("starting sandbox host", "id", r.Id)

	// Initialize Flannel/WireGuard network if distributed runner configuration is provided
	if len(r.deps.EtcdEndpoints) > 0 {
		if err := r.initializeNetwork(ctx, eg...); err != nil {
			return fmt.Errorf("failed to initialize network: %w", err)
		}
	}

	if r.access.state == nil || r.access.eac == nil || r.access.entityBase == nil {
		return errors.New("cluster access is not ready")
	}
	r.deps.WorkloadIssuer = r.access.deps.WorkloadIssuer
	r.deps.Secrets = r.access.deps.Secrets

	cm, err := r.SetupControllers(ctx, r.access.eac, r.access.state.Server())
	if err != nil {
		return err
	}

	r.Log.Info("sandbox host restored")
	r.controllers = cm

	// Create exec server with explicit dependencies
	execServer := exec.NewServer(r.Log, r.deps.CC, r.access.eac, r.deps.Namespace, r.hubs)

	r.access.state.Server().ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(execServer))

	r.Log.Info("Registered exec server")

	return nil
}

// SandboxAgent owns desired-state reconciliation for sandboxes and services.
// SandboxHost has already adopted surviving containers before this starts.
type SandboxAgent struct{ host *SandboxHost }

// NewSandboxAgent constructs reconciliation over a restored sandbox host.
func NewSandboxAgent(host *SandboxHost) *SandboxAgent { return &SandboxAgent{host: host} }

func (a *SandboxAgent) Start(ctx context.Context) error {
	if a.host.controllers == nil {
		return errors.New("sandbox host is not ready")
	}
	return a.host.controllers.Start(ctx)
}

func (a *SandboxAgent) Close() error {
	if a.host.controllers != nil {
		a.host.controllers.Stop()
	}
	return nil
}

// Start advertises the node only after the graph has started its storage and
// sandbox agents.
func (r *NodePresence) Start(ctx context.Context) error {
	if r.host.access.entityBase == nil {
		return errors.New("cluster access is not ready")
	}
	if err := r.setupEntity(ctx, r.host.access.entityBase); err != nil {
		return err
	}
	r.host.Log.Info("node presence published", "id", r.host.Id)
	return nil
}

func (r *NodePresence) Close() error {
	r.sessMu.Lock()
	r.closed = true
	se := r.se
	r.se = nil
	r.ec = nil
	r.sessMu.Unlock()
	if se != nil {
		return se.Close()
	}
	return nil
}

// WorkloadIssuer returns the issuer this runner mints identity tokens through,
// or nil if none is available.
//
// It is only meaningful after Start, which is where a distributed runner
// acquires its issuer from the coordinator. Callers that build something
// needing tokens before then should hold a source they can arm afterwards
// rather than reading this early and caching a nil.
func (r *ClusterAccess) WorkloadIssuer() workloadidentity.TokenIssuer {
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
func (r *ClusterAccess) setupSqliteDisks(rs *rpc.State) {
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
func (r *ClusterAccess) setupRemoteWorkloadIssuer(ctx context.Context, rs *rpc.State) error {
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
func (r *ClusterAccess) setupRemoteSecrets(rs *rpc.State) error {
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
func (r *SandboxHost) initializeNetwork(ctx context.Context, eg ...*errgroup.Group) error {
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
func (r *NodePresence) setupEntity(ctx context.Context, base *entityserver.Client) error {
	if r.host.Id == "" {
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
func (r *NodePresence) establishSession(ctx context.Context, base *entityserver.Client) error {
	h := r.host
	r.sessMu.Lock()
	closed := r.closed
	r.sessMu.Unlock()
	if closed {
		return errNodePresenceClosed
	}
	h.Log.Info("Creating health session")

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
			h.Log.Warn("failed to revoke unregistered health session", "error", rerr)
		}
	}()

	role := "runner"
	if h.deps.IsCoordinator {
		role = "coordinator"
	}

	node := compute_v1alpha.Node{
		RunnerId:    h.Id,
		Name:        h.Name,
		Constraints: types.LabelSet("compute", "generic", "role", role),
		ApiAddress:  h.ListenAddress,
		Version:     version.GetInfo().Version,
	}

	h.Log.Info("Registering node entity", "role", role, "address", h.ListenAddress)

	res, err := ec.CreateOrUpdate(ctx, h.Id, &node)
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
	if r.closed {
		r.sessMu.Unlock()
		return errNodePresenceClosed
	}
	r.ec = ec
	r.se = sess
	published = true
	r.sessMu.Unlock()

	h.Log.Info("Runner registered and ready", "id", res, "status", "ready")

	return nil
}

// superviseSession watches the current health session and re-establishes it
// when its lease is lost. Without this, a coordinator restart orphans the
// runner's lease, the session-scoped READY status is dropped, and the runner
// stays not-ready until manually restarted (MIR-1305). Re-establishing well
// within nodehealth's grace period keeps the runner's sandboxes from being
// evacuated.
func (r *NodePresence) superviseSession(ctx context.Context, base *entityserver.Client) {
	h := r.host
	for {
		r.sessMu.Lock()
		se := r.se
		closed := r.closed
		r.sessMu.Unlock()
		if closed || se == nil {
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
		h.Log.Warn("runner health session lease lost, re-establishing", "id", h.Id)

		backoff := time.Second
		for {
			r.sessMu.Lock()
			closed := r.closed
			r.sessMu.Unlock()
			if ctx.Err() != nil || closed {
				return
			}

			if err := r.establishSession(ctx, base); err == nil {
				h.Log.Info("runner health session re-established", "id", h.Id)
				break
			} else if errors.Is(err, errNodePresenceClosed) {
				return
			} else {
				h.Log.Error("failed to re-establish runner health session, retrying",
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

func (r *SandboxHost) SetupControllers(
	ctx context.Context,
	eas *es.EntityAccessClient,
	rs *rpc.Server,
) (
	_ *controller.ControllerManager,
	retErr error,
) {
	cm := controller.NewControllerManager()

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
		SqliteDisks:    r.access.sqliteDisks,
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

	workers := r.Workers
	if workers <= 0 {
		workers = DefaulWorkers
	}

	err = sbc.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = serviceController.Init(ctx)
	if err != nil {
		return nil, err
	}

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

	return cm, nil
}
