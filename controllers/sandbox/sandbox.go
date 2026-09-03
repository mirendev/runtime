package sandbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/mr-tron/base58"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/sqlitedisk"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/workloadidentity"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/ocireg"
)

const (
	// defaultSandboxOOMAdj is default omm adj for sandbox container. (kubernetes#47938).
	defaultSandboxOOMAdj = -998
)

var sandboxImage = imagerefs.Pause

// cleanupAttach is the cio.Attach to pass to container.Task when we're
// retrieving a task purely to delete it. The non-nil attach makes containerd
// populate t.io with a FIFO closer that removes /run/containerd/fifo/<n>;
// passing nil leaves t.io nil and task.Delete silently leaks the directory.
func cleanupAttach() cio.Attach {
	return cio.NewAttach()
}

type containerPorts struct {
	Ports []observability.BoundPort
}

// SandboxControllerDeps holds required dependencies for SandboxController.
type SandboxControllerDeps struct {
	Log       *slog.Logger
	CC        *containerd.Client
	EAC       *entityserver_v1alpha.EntityAccessClient
	Namespace string
	NodeId    compute.NodeId
	NetServ   *network.ServiceManager
	Bridge    string
	Subnet    *netdb.Subnet
	DataPath  string
	Tempdir   string

	LogsMaintainer *observability.LogsMaintainer
	LogWriter      observability.LogWriter
	StatusMon      *observability.StatusMonitor
	Resolver       netresolve.Resolver
	Metrics        *Metrics
	WorkloadIssuer workloadidentity.TokenIssuer

	// ApiAddress is where a sandbox reaches the cluster API, as a literal
	// host:port. It must be an IP: sandbox DNS resolves only app.miren names,
	// so a hostname here would not resolve inside the container. On a
	// coordinator this is the local bridge router; on a distributed runner it
	// is the coordinator, which is a different machine — the local router would
	// be wrong there, since no API listens on it.
	//
	// Empty disables in-cluster API access.
	ApiAddress string

	// CACert is the cluster CA in PEM form, mounted into sandboxes so they can
	// verify the API certificate rather than skipping verification.
	CACert []byte

	// Secrets materializes the secret references a sandbox spec carries, at the
	// moment a container is created. Nil where no backend is reachable, in which
	// case a spec that references one fails rather than starting the container
	// with the reference in place of the value.
	Secrets secret.Resolver

	// Hubs is the stdio fan-out registry shared with the runner's exec server.
	// Optional; one is created if absent, which is what tests want.
	Hubs *HubRegistry

	// SqliteDisks replicates sqlite-provider disks to the coordinator. Nil
	// disables replication; the disks still mount.
	SqliteDisks *sqlitedisk.Manager
}

type SandboxController struct {
	Log *slog.Logger
	CC  *containerd.Client

	EAC *entityserver_v1alpha.EntityAccessClient

	Namespace string
	NodeId    compute.NodeId

	NetServ *network.ServiceManager

	Bridge string
	Subnet *netdb.Subnet

	DataPath string
	Tempdir  string

	LogsMaintainer *observability.LogsMaintainer
	LogWriter      observability.LogWriter

	StatusMon *observability.StatusMonitor

	Resolver       netresolve.Resolver
	Metrics        *Metrics
	WorkloadIssuer workloadidentity.TokenIssuer
	ApiAddress     string
	CACert         []byte

	// Secrets materializes the secret references a sandbox spec carries, at the
	// moment a container is created. Nil where no backend is reachable, in which
	// case a spec that references one fails rather than starting the container
	// with the reference in place of the value.
	Secrets secret.Resolver

	tokenRefresher *tokenRefresher
	tokenSecrets   *tokenSecretRegistry
	lookupRefresh  refreshLimiter

	topCtx context.Context
	cancel func()

	mu       sync.Mutex
	monitors int
	cond     *sync.Cond

	running sync.WaitGroup

	portMu      sync.Mutex
	portCond    *sync.Cond
	portMap     map[string]*containerPorts
	portMonitor *PortMonitor

	watchdog      *ContainerWatchdog
	imageWatchdog *ImageWatchdog
	ipReconciler  *IPReconciler

	// writeTracker tracks entity write revisions to skip self-generated watch events
	writeTracker controller.WriteTracker

	// hubs holds the stdio fan-out for attachable containers on this node. The
	// exec server reads from the same registry, which is why it is shared
	// rather than owned outright.
	hubs *HubRegistry

	// SqliteDisks replicates sqlite-provider disks to the coordinator. Nil is
	// inert, so disks still mount when backups are unavailable.
	SqliteDisks *sqlitedisk.Manager
}

// Hubs exposes the registry of attachable containers so the runner's exec
// server can join a client to a running container's terminal.
func (c *SandboxController) Hubs() *HubRegistry { return c.hubs }

// NewSandboxController creates a new SandboxController with validated dependencies.
func NewSandboxController(cfg SandboxControllerDeps) (*SandboxController, error) {
	if cfg.Log == nil {
		return nil, fmt.Errorf("sandbox: Log is required")
	}
	if cfg.CC == nil {
		return nil, fmt.Errorf("sandbox: containerd client is required")
	}
	if cfg.EAC == nil {
		return nil, fmt.Errorf("sandbox: entity access client is required")
	}
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("sandbox: Namespace is required")
	}
	if cfg.NodeId == "" {
		return nil, fmt.Errorf("sandbox: NodeId is required")
	}
	if cfg.Subnet == nil {
		return nil, fmt.Errorf("sandbox: Subnet is required")
	}
	if cfg.NetServ == nil {
		return nil, fmt.Errorf("sandbox: NetServ is required")
	}
	if cfg.Metrics == nil {
		return nil, fmt.Errorf("sandbox: Metrics is required")
	}
	if cfg.LogsMaintainer == nil {
		return nil, fmt.Errorf("sandbox: LogsMaintainer is required")
	}
	if cfg.LogWriter == nil {
		return nil, fmt.Errorf("sandbox: LogWriter is required")
	}
	if cfg.Resolver == nil {
		return nil, fmt.Errorf("sandbox: Resolver is required")
	}

	hubs := cfg.Hubs
	if hubs == nil {
		hubs = NewHubRegistry()
	}

	return &SandboxController{
		hubs:           hubs,
		Log:            cfg.Log.With("module", "sandbox"),
		CC:             cfg.CC,
		EAC:            cfg.EAC,
		Namespace:      cfg.Namespace,
		NodeId:         cfg.NodeId,
		NetServ:        cfg.NetServ,
		Bridge:         cfg.Bridge,
		Subnet:         cfg.Subnet,
		DataPath:       cfg.DataPath,
		Tempdir:        cfg.Tempdir,
		LogsMaintainer: cfg.LogsMaintainer,
		LogWriter:      cfg.LogWriter,
		StatusMon:      cfg.StatusMon,
		Resolver:       cfg.Resolver,
		Metrics:        cfg.Metrics,
		WorkloadIssuer: cfg.WorkloadIssuer,
		ApiAddress:     cfg.ApiAddress,
		CACert:         cfg.CACert,
		Secrets:        cfg.Secrets,
		SqliteDisks:    cfg.SqliteDisks,
	}, nil
}

// localAPIPort returns the port to open to the bridge so sandboxes can reach
// the cluster API, or 0 if none should be.
//
// The rule is only wanted where the API is on this host and traffic from the
// bridge is delivered locally. On a distributed runner ApiAddress names the
// coordinator instead, and packets are forwarded and masqueraded rather than
// hitting INPUT — opening the port there would grant nothing and would expose
// whatever happened to be listening on that port on the runner.
func (c *SandboxController) localAPIPort() int {
	if c.ApiAddress == "" {
		return 0
	}

	host, portStr, err := net.SplitHostPort(c.ApiAddress)
	if err != nil {
		c.Log.Warn("ignoring malformed API address", "address", c.ApiAddress, "error", err)
		return 0
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		c.Log.Warn("ignoring API address with bad port", "address", c.ApiAddress)
		return 0
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		c.Log.Warn("ignoring non-IP API address", "address", c.ApiAddress)
		return 0
	}

	if addr != c.Subnet.Router().Addr() {
		return 0
	}

	return port
}

// SetWriteTracker sets the write tracker for recording manual entity writes
func (c *SandboxController) SetWriteTracker(wt controller.WriteTracker) {
	c.writeTracker = wt
}

func (c *SandboxController) SetPortStatus(id string, port observability.BoundPort, status observability.PortStatus) {
	c.portMu.Lock()
	defer c.portMu.Unlock()

	ports, ok := c.portMap[id]
	if !ok {
		ports = &containerPorts{}
		c.portMap[id] = ports
	}

	c.Log.Debug("setting port status", "id", id, "port", port, "status", status)

	switch status {
	case observability.PortStatusBound:
		if !slices.Contains(ports.Ports, port) {
			ports.Ports = append(ports.Ports, port)
		}
	case observability.PortStatusUnbound:
		ports.Ports = slices.DeleteFunc(ports.Ports, func(p observability.BoundPort) bool {
			return p == port
		})
	case observability.PortStatusActive:
		// Liveness signal only; does not change the bound set.
	}

	c.portCond.Broadcast()
}

func (c *SandboxController) WaitForPort(ctx context.Context, id string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Create a channel to signal when port is ready
	done := make(chan struct{})
	cancelled := make(chan struct{})

	go func() {
		c.portMu.Lock()
		defer c.portMu.Unlock()

		for {
			select {
			case <-cancelled:
				return
			default:
			}

			ports, ok := c.portMap[id]
			if !ok {
				ports = &containerPorts{}
				c.portMap[id] = ports
			}

			for _, p := range ports.Ports {
				if p.Port == port {
					close(done)
					return
				}
			}

			c.portCond.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		close(cancelled)
		c.portCond.Broadcast() // Wake up the waiting goroutine
		return fmt.Errorf("context cancelled while waiting for port %d: %w", port, ctx.Err())
	case <-time.After(time.Until(deadline)):
		// Re-check under the lock before declaring timeout: the wait goroutine
		// may have already observed the port bound and closed done at (close
		// to) the same instant this timer became ready. Go's select picks
		// uniformly at random among ready cases, so without this check it can
		// return a spurious timeout even though the port is in fact bound.
		c.portMu.Lock()
		if ports, ok := c.portMap[id]; ok {
			for _, p := range ports.Ports {
				if p.Port == port {
					c.portMu.Unlock()
					return nil
				}
			}
		}
		c.portMu.Unlock()
		close(cancelled)
		c.portCond.Broadcast() // Wake up the waiting goroutine
		return fmt.Errorf("timeout waiting for port %d to be bound after %v", port, timeout)
	}
}

// diagnoseListening reports the ports a container is actually listening on,
// split into routable and loopback-only sets. Returns ok=false when the port
// monitor is unavailable or the container's pid is unknown. Shared by the
// legacy create flow and the saga ops wrapper.
func (c *SandboxController) diagnoseListening(id string) (routable []int, loopback []int, ok bool) {
	if c.portMonitor == nil {
		return nil, nil, false
	}
	return c.portMonitor.DiagnoseListening(id)
}

// mapLegacyProtocol converts legacy PortProtocol values to SandboxSpecContainerPortProtocol
func mapLegacyProtocol(legacy compute.PortProtocol) compute.SandboxSpecContainerPortProtocol {
	switch legacy {
	case compute.TCP, "tcp":
		return compute.SandboxSpecContainerPortTCP
	case compute.UDP, "udp":
		return compute.SandboxSpecContainerPortUDP
	default:
		// Default to TCP for unknown protocols
		return compute.SandboxSpecContainerPortTCP
	}
}

// reconcileSandboxesOnBoot checks all Running sandboxes and marks unhealthy ones as DEAD
// This is called during controller initialization to clean up after containerd restarts
func (c *SandboxController) reconcileSandboxesOnBoot(ctx context.Context) error {
	c.Log.Info("reconciling sandboxes on boot")

	// Create a context with timeout for the entire reconciliation
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// List sandboxes scheduled to this node only. Using the same node-scoped
	// index as the controller watch (see runner.go) ensures we don't
	// accidentally mark sandboxes on other nodes as unhealthy because their
	// containers don't exist in our local containerd.
	resp, err := c.EAC.List(ctx, compute.Index(compute.KindSandbox, c.NodeId.Id()))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var unhealthySandboxes []entity.Id
	runningCount := 0
	reattachedCount := 0
	// Counts surviving sandboxes whose mounted token was minted under a
	// different issuer, i.e. the anchor moved across this restart.
	staleAnchorTokens := 0

	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Only check sandboxes that think they're running
		if sb.Status != compute.RUNNING {
			continue
		}
		runningCount++

		shortID := entityShortID(e.Entity())

		// Reattach logs to pause container
		pauseID := pauseContainerId(sb.ID)
		if err := c.reattachLogs(ctx, &sb, pauseID, "", shortID); err != nil {
			c.Log.Warn("failed to reattach logs to pause container",
				"sandbox_id", sb.ID,
				"pause_container_id", pauseID,
				"error", err)
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
			continue
		}

		// Check pause container health
		if !c.isContainerHealthy(ctx, pauseID) {
			c.Log.Warn("found unhealthy sandbox during boot reconciliation",
				"sandbox_id", sb.ID,
				"pause_container_id", pauseID)
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
			continue
		}

		// Reattach logs and check subcontainers health
		allHealthy := true
		for _, container := range sb.Spec.Container {
			containerID := fmt.Sprintf("%s-%s", containerPrefix(sb.ID), container.Name)

			// Reattach logs for this subcontainer
			if err := c.reattachLogs(ctx, &sb, containerID, container.Name, shortID); err != nil {
				c.Log.Warn("failed to reattach logs to subcontainer",
					"sandbox_id", sb.ID,
					"container_name", container.Name,
					"container_id", containerID,
					"error", err)
				allHealthy = false
				break
			}

			if !c.isContainerHealthy(ctx, containerID) {
				c.Log.Warn("found unhealthy subcontainer during boot reconciliation",
					"sandbox_id", sb.ID,
					"container_name", container.Name,
					"container_id", containerID)
				allHealthy = false
				break
			}
		}

		if !allHealthy {
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
		} else {
			reattachedCount++

			// Replication lives in the runner process, not the sandbox, so a
			// runner restart leaves surviving sandboxes writing to databases
			// nothing is backing up. Re-register them for the same reason the
			// IPs below are re-registered: the sandbox outlived the state.
			c.reregisterSqliteDisks(ctx, &sb)

			// Re-register IPs for healthy surviving sandboxes to ensure netdb
			// tracks them as reserved. This prevents duplicate IP allocation
			// if the netdb state was lost during the crash/restart.
			if c.Subnet != nil {
				for _, net := range sb.Network {
					ipStr, err := netutil.ParseNetworkAddress(net.Address)
					if err != nil {
						c.Log.Warn("failed to parse sandbox IP during boot reconciliation",
							"sandbox_id", sb.ID, "address", net.Address, "error", err)
						continue
					}
					addr, err := netip.ParseAddr(ipStr)
					if err != nil {
						c.Log.Warn("failed to parse IP addr during boot reconciliation",
							"sandbox_id", sb.ID, "ip", ipStr, "error", err)
						continue
					}
					if err := c.Subnet.ReserveSpecificAddr(addr); err != nil {
						c.Log.Warn("failed to re-reserve IP during boot reconciliation",
							"sandbox_id", sb.ID, "ip", addr, "error", err)
					} else {
						c.Log.Debug("re-reserved IP for surviving sandbox",
							"sandbox_id", sb.ID, "ip", addr)
					}
				}
			}

			// Re-register with token refresher so tokens keep getting renewed. Only
			// sandboxes that actually use workload identity have an identity-token file;
			// skip the rest so the refresh loop doesn't spew write errors for sandboxes
			// that predate workload identity.
			if c.tokenRefresher != nil {
				tokenPath := c.sandboxPath(&sb, "identity-token")
				if data, err := os.ReadFile(tokenPath); err == nil {
					// Recover the app and role from the existing token so a
					// running sandbox keeps what it was built with — a role the
					// app was reconfigured to since a restart should not take
					// effect until the sandbox is rebuilt. Fall back to resolving
					// from the entity graph for an unparseable/legacy token.
					appName, role, ok := workloadidentity.PeekSandboxClaims(string(data))
					if !ok {
						appName, role = c.resolveAppAndRole(ctx, &sb)
					}
					c.tokenRefresher.register(sb.ID.String(), tokenPath, appName, role)
					c.Log.Debug("re-registered sandbox for token refresh",
						"sandbox_id", sb.ID, "app", appName, "role", role)

					// A mounted token minted under a different anchor means the
					// cluster's identity anchor moved while this sandbox was
					// running. Waiting for the 45-minute refresh tick would
					// leave it presenting a superseded issuer for most of an
					// hour, so note it and rewrite every token once
					// reconciliation is done.
					if issuer, ok := workloadidentity.PeekTokenIssuer(string(data)); ok &&
						c.WorkloadIssuer != nil && issuer != c.WorkloadIssuer.IssuerURL() {
						staleAnchorTokens++
					}
				}
			}

			// Re-register the token-request secret so the still-running sandbox keeps
			// authenticating to the token server. The in-memory registry starts empty
			// after a restart; without this the sandbox's token requests 403 forever
			// until it is restarted (MIR-1235).
			if c.tokenSecrets != nil {
				secretPath := c.sandboxPath(&sb, tokenSecretFilename)
				secret, ok, err := loadTokenSecret(secretPath)
				switch {
				case err != nil:
					c.Log.Warn("failed to load persisted token secret during boot reconciliation",
						"sandbox_id", sb.ID, "error", err)
				case !ok:
					c.Log.Debug("no persisted token secret for surviving sandbox; cannot re-register",
						"sandbox_id", sb.ID)
				default:
					c.tokenSecrets.register(sb.ID.String(), secret)
					c.Log.Debug("re-registered token secret for surviving sandbox",
						"sandbox_id", sb.ID)
				}
			}

			// Re-register cgroup metrics. Logs are reattached above, so a
			// surviving sandbox that skips this looks entirely healthy while
			// its CPU and memory series stop dead at the restart and never
			// resume (MIR-1013). Failing here must not cost us the sandbox:
			// unhealthySandboxes gets deleted, and a metrics gap is a far
			// better outcome than killing a running workload.
			if err := c.ensureMetrics(ctx, &sb); err != nil {
				c.Log.Warn("failed to re-register metrics during boot reconciliation",
					"sandbox_id", sb.ID, "error", err)
			}
		}
	}

	// Mark unhealthy sandboxes as DEAD and clean them up
	for _, id := range unhealthySandboxes {
		c.Log.Info("marking unhealthy sandbox as DEAD and cleaning up", "id", id)

		// Try to clean up the sandbox
		err := c.Delete(ctx, id, nil)
		if err != nil {
			c.Log.Error("failed to cleanup unhealthy sandbox", "id", id, "err", err)
			// Continue with other sandboxes even if one fails
		}
	}

	// Rewrite every mounted token now rather than at the next refresh tick. The
	// old tokens still verify — the superseded anchor is accepted for its
	// overlap window — but a workload that re-reads its token file should see
	// the new issuer promptly, and anything holding a cached copy is on the
	// clock until that window closes.
	// Every stale-anchor sandbox is already in the refresher: register() runs
	// sequentially in the loop above, in the same iteration that counts it. The
	// rewrite therefore reaches all of them. That ordering is load-bearing — if
	// the loop above ever becomes concurrent, or registration moves elsewhere,
	// this call has to be re-checked or it will silently cover only some.
	if staleAnchorTokens > 0 {
		c.Log.Info("workload identity anchor changed while sandboxes were running; rewriting mounted tokens",
			"issuer", c.WorkloadIssuer.IssuerURL(),
			"sandboxes", staleAnchorTokens)
		c.refreshTokens()
	}

	c.Log.Info("boot reconciliation complete",
		"total_running_sandboxes", runningCount,
		"reattached_sandboxes", reattachedCount,
		"unhealthy_sandboxes", len(unhealthySandboxes))

	return nil
}

func (c *SandboxController) Init(ctx context.Context) error {
	c.portCond = sync.NewCond(&c.portMu)
	c.portMap = make(map[string]*containerPorts)

	// Initialize port monitor
	c.portMonitor = NewPortMonitor(c.Log, c)

	err := c.LogsMaintainer.Setup(ctx)
	if err != nil {
		return err
	}

	c.topCtx, c.cancel = context.WithCancel(ctx)

	c.cond = sync.NewCond(&c.mu)

	bc := &network.BridgeConfig{
		Name:      c.Bridge,
		Addresses: []netip.Prefix{c.Subnet.Router()},
		APIPort:   c.localAPIPort(),
	}

	link, err := network.SetupBridge(bc)
	if err != nil {
		return err
	}

	ep := &network.EndpointConfig{
		Bridge: bc,
	}

	err = network.ConfigureGW(link, ep)
	if err != nil {
		return err
	}

	// Drop any bridge addresses, NAT rules, or POSTROUTING jumps left over
	// from a previous flannel lease era. Without this, a runner whose lease
	// rotated (typically after >24h offline) ends up with two pod subnets
	// pinned to the same bridge and a NAT chain whose ACCEPT rules end up
	// after the catch-all MASQUERADE (MIR-1108).
	err = network.ReconcileBridgeAddresses(c.Log, link, bc.Addresses)
	if err != nil {
		return err
	}

	err = network.MasqueradeEndpoint(ep)
	if err != nil {
		return err
	}

	err = c.NetServ.SetupDNS(c.topCtx, bc)
	if err != nil {
		return err
	}

	// Initialize token refresh state before reconcile so surviving sandboxes
	// can be re-registered during boot reconciliation.
	if c.WorkloadIssuer != nil {
		c.tokenRefresher = newTokenRefresher()
		c.tokenSecrets = newTokenSecretRegistry()
	}

	// Reconcile sandboxes after containerd restart
	// This must happen after bridge setup but before starting normal operations
	err = c.reconcileSandboxesOnBoot(ctx)
	if err != nil {
		// Log error but don't fail Init - we want the controller to start
		// and handle new requests even if some cleanup failed
		c.Log.Error("failed to reconcile sandboxes on boot", "err", err)
	}

	if err := c.Metrics.Validate(); err != nil {
		return fmt.Errorf("sandbox metrics validation failed: %w", err)
	}
	go c.Metrics.Monitor(c.topCtx)

	// Initialize and start the container watchdog
	c.watchdog = &ContainerWatchdog{
		Log:           c.Log.With("module", "watchdog"),
		CC:            c.CC,
		EAC:           c.EAC,
		Namespace:     c.Namespace,
		NodeId:        c.NodeId,
		CheckInterval: 5 * time.Minute,
		Subnet:        c.Subnet,
	}
	c.watchdog.Start(c.topCtx)

	// Initialize and start the image watchdog for garbage collection
	c.imageWatchdog = &ImageWatchdog{
		Log:       c.Log.With("module", "image-gc"),
		CC:        c.CC,
		EAC:       c.EAC,
		Namespace: c.Namespace,
		DataPath:  c.DataPath,
		Config:    DefaultImageGCConfig(),
	}
	c.imageWatchdog.Start(c.topCtx)

	// Initialize and start the IP reconciler, which keeps netdb lease bookkeeping
	// in agreement with the addresses actually live on the bridge (MIR-1238). Its
	// initial run also re-reserves the IPs of containers that survived a restart.
	if c.Subnet != nil {
		c.ipReconciler = &IPReconciler{
			Log:    c.Log.With("module", "ip-reconciler"),
			Subnet: c.Subnet,
			LiveIPs: func(ctx context.Context) (map[netip.Addr]bool, error) {
				return c.liveBridgeIPs(ctx)
			},
			// CheckInterval is left at the IPReconciler default (see Start).
		}
		c.ipReconciler.Start(c.topCtx)
	}

	// Start workload identity token refresh loop and token request server
	// (tokenRefresher and tokenSecrets were created earlier, before reconcile)
	if c.WorkloadIssuer != nil {
		go c.runTokenRefresh(c.topCtx)
		go c.startTokenServer(c.topCtx)
	}

	return nil
}

func (c *SandboxController) Close() error {
	if c.cancel != nil {
		c.cancel()
	}

	if c.cond != nil {
		c.mu.Lock()
		for c.monitors > 0 {
			c.cond.Wait()
		}
		c.mu.Unlock()
	}

	var err error

	if c.portMonitor != nil {
		err = c.portMonitor.Close()
	}

	if c.watchdog != nil {
		c.watchdog.Stop()
	}

	if c.imageWatchdog != nil {
		c.imageWatchdog.Stop()
	}

	if c.ipReconciler != nil {
		c.ipReconciler.Stop()
	}

	c.running.Wait()

	// Shutdown DNS and other network services
	if c.NetServ != nil {
		if shutdownErr := c.NetServ.ShutdownAll(); shutdownErr != nil {
			c.Log.Error("failed to shutdown network services", "error", shutdownErr)
			if err == nil {
				err = shutdownErr
			}
		}
	}

	return err
}

const (
	sandboxVersionLabel = "runtime.computer/sandbox-version"
	// SandboxEntityLabel is the container label key used to associate containers with sandbox entities.
	SandboxEntityLabel     = "runtime.computer/entity-id"
	sandboxEntityLabel     = SandboxEntityLabel
	sandboxVerEntityLabel  = "runtime.computer/version-entity"
	sandboxKindLabel       = "runtime.computer/container-kind"
	shutdownTimeoutLabel   = "runtime.computer/shutdown-timeout"
	defaultShutdownTimeout = 10 * time.Second
	shutdownPollInterval   = 100 * time.Millisecond
)

const (
	notFound = iota
	same
	unhealthy // container exists but task is missing or dead
)

func containerPrefix(id entity.Id) string {
	cid := id.String()
	cid = strings.TrimPrefix(cid, "sandbox/")
	return "sandbox." + cid
}

// sandboxHostname returns a valid hostname for a sandbox container.
// This is used both for the UTS hostname and the /etc/hosts entry so that
// processes like EPMD that resolve their own hostname can find themselves.
func sandboxHostname(id entity.Id) string {
	return strings.TrimPrefix(id.String(), "sandbox/")
}

func pauseContainerId(id entity.Id) string {
	return PauseContainerID(id)
}

// PauseContainerID returns the containerd container ID for a sandbox's pause container.
func PauseContainerID(id entity.Id) string {
	return containerPrefix(id) + "_pause"
}

func (c *SandboxController) CheckSandbox(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) (int, error) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	_, err := c.CC.LoadContainer(ctx, pauseContainerId(co.ID))
	if err != nil {
		if errdefs.IsNotFound(err) {
			return notFound, nil
		}

		return 0, err
	}

	// Sandboxes are immutable. If a sandbox with this ID exists,
	// we only check if it's healthy. Version changes are handled
	// by creating new sandboxes with new IDs via sandboxpools.

	// Check if the pause container has a healthy task
	pauseID := pauseContainerId(co.ID)
	if !c.isContainerHealthy(ctx, pauseID) {
		c.Log.Warn("sandbox container exists but task is unhealthy", "id", co.ID, "pause_id", pauseID)
		return unhealthy, nil
	}

	// Check subcontainers health (from Spec)
	for _, container := range co.Spec.Container {
		containerID := fmt.Sprintf("%s-%s", containerPrefix(co.ID), container.Name)
		if !c.isContainerHealthy(ctx, containerID) {
			c.Log.Warn("sandbox subcontainer exists but task is unhealthy",
				"sandbox_id", co.ID,
				"container_name", container.Name,
				"container_id", containerID)
			return unhealthy, nil
		}
	}

	// Check network connectivity for RUNNING sandboxes
	// This catches cases where the task is running but the network has failed
	if co.Status == compute.RUNNING {
		if !c.checkNetworkHealth(ctx, co) {
			c.Log.Warn("sandbox network health check failed", "sandbox_id", co.ID)
			return unhealthy, nil
		}
	}

	return same, nil
}

// isContainerHealthy checks if a container has a running task
// Returns true if the container and its task are healthy, false otherwise
func (c *SandboxController) isContainerHealthy(ctx context.Context, containerID string) bool {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, containerID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			c.Log.Debug("container not found when checking health", "id", containerID)
		} else {
			c.Log.Error("failed to load container when checking health", "id", containerID, "err", err)
		}
		return false
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			c.Log.Debug("task not found for container", "id", containerID)
		} else {
			c.Log.Error("failed to get task for container", "id", containerID, "err", err)
		}
		return false
	}

	if task == nil {
		c.Log.Debug("task is nil for container", "id", containerID)
		return false
	}

	status, err := task.Status(ctx)
	if err != nil {
		c.Log.Error("failed to get task status", "id", containerID, "err", err)
		return false
	}

	// Check if task is in a healthy state
	// Only Running and Created (starting) are considered healthy
	// Everything else (Stopped, Paused, Pausing, Unknown) is unhealthy
	switch status.Status {
	case containerd.Running:
		// Definitely healthy - task is actively running
		return true
	case containerd.Created:
		// Task created but not yet started - might still be starting up
		c.Log.Debug("task in created state, considering healthy", "id", containerID)
		return true
	case containerd.Stopped:
		// Task has stopped
		c.Log.Debug("task stopped, marking unhealthy", "id", containerID)
		return false
	case containerd.Paused, containerd.Pausing:
		// We don't expect paused sandboxes in normal operation
		c.Log.Debug("task in paused/pausing state, marking unhealthy", "id", containerID, "status", status.Status)
		return false
	case containerd.Unknown:
		// Unknown status is unhealthy.
		fallthrough
	default:
		// Any other status is unhealthy
		c.Log.Debug("task in unknown/unhealthy state", "id", containerID, "status", status.Status)
		return false
	}
}

// checkNetworkHealth verifies that a sandbox's app is still listening on at
// least one declared TCP port. Returns true for sandboxes with no exposed
// TCP ports (background workers, UDP-only) or no allocated network. Returns
// false if TCP ports are declared but none are in LISTEN state inside the
// pause container's network namespace.
//
// The check reads /proc/<pause-pid>/net/tcp{,6} rather than dialing the pod
// IP from the host. Host-to-pod TCP dials traverse the bridge's POSTROUTING
// chain and can be intercepted by MASQUERADE, breaking connection tracking
// on the dialing host. PortMonitor switched away from dials for the same
// reason (commit 63661df3); MIR-1108 is the same class of bug here.
//
// Only TCP ports are probed because /proc/net/tcp does not expose UDP
// listeners; a UDP-only sandbox would be wrongly killed if we treated UDP
// ports as health-relevant.
func (c *SandboxController) checkNetworkHealth(ctx context.Context, sb *compute.Sandbox) bool {
	if len(sb.Network) == 0 {
		c.Log.Debug("sandbox has no network, skipping network health check", "sandbox_id", sb.ID)
		return true
	}

	hasTCPPorts := false
	for _, container := range sb.Spec.Container {
		if slices.ContainsFunc(container.Port, portIsTCP) {
			hasTCPPorts = true
		}
		if hasTCPPorts {
			break
		}
	}
	if !hasTCPPorts {
		return true
	}

	pauseID := pauseContainerId(sb.ID)
	pid, err := c.pauseContainerPID(ctx, pauseID)
	if err != nil {
		c.Log.Debug("failed to resolve pause container PID for health check",
			"sandbox_id", sb.ID, "pause_id", pauseID, "error", err)
		return false
	}

	for _, container := range sb.Spec.Container {
		for _, p := range container.Port {
			if !portIsTCP(p) {
				continue
			}
			port := int(p.Port)
			if checkPort(pid, port) {
				return true
			}
			c.Log.Debug("network health check found no listener for port",
				"sandbox_id", sb.ID, "container_name", container.Name, "port", port)
		}
	}

	// An app that ignored $PORT and bound a different port has that port
	// recorded as an observed bound_port (and we route to it). Treat that port
	// as health-relevant too, or the periodic check would kill a sandbox that is
	// running fine on the port we're actually sending traffic to.
	for _, bp := range sb.BoundPort {
		port := int(bp.Port)
		if checkPort(pid, port) {
			return true
		}
		c.Log.Debug("network health check found no listener for observed bound port",
			"sandbox_id", sb.ID, "port", port)
	}

	return false
}

// portIsTCP returns true for ports declared as TCP. Empty Protocol defaults
// to TCP (matches mapLegacyProtocol), so ports without a protocol set are
// treated as TCP as well.
func portIsTCP(p compute.SandboxSpecContainerPort) bool {
	return p.Protocol == "" || p.Protocol == compute.SandboxSpecContainerPortTCP
}

// pauseContainerPID returns the PID of the pause container's task. The
// pause container shares its network namespace with all sub-containers,
// so its /proc/<pid>/net/tcp{,6} reflects the listening sockets of the
// app processes inside the sandbox.
func (c *SandboxController) pauseContainerPID(ctx context.Context, pauseID string) (int, error) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, pauseID)
	if err != nil {
		return 0, fmt.Errorf("loading pause container: %w", err)
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("getting pause task: %w", err)
	}

	return int(task.Pid()), nil
}

// sandboxMetricsIdentity returns the log entity a sandbox's cgroup metrics are
// keyed by, plus the attributes those metrics carry. Every registration site
// shares this so the key always matches the one StopSandbox removes, and so
// new attributes reach all of them at once.
func sandboxMetricsIdentity(sb *compute.Sandbox) (string, map[string]string) {
	le := sb.Spec.LogEntity
	if le == "" {
		le = sb.ID.String()
	}

	attrs := map[string]string{
		"miren.sandbox": sb.ID.String(),
	}

	if sb.Spec.Version != "" {
		attrs["miren.version"] = sb.Spec.Version.String()
	}

	for _, lbl := range sb.Spec.LogAttribute {
		attrs[lbl.Key] = lbl.Value
	}

	return le, attrs
}

// sandboxCgroups reads the cgroup path of each of a sandbox's live containers
// from containerd, keyed the way createSandbox keys them: "" for the pause
// container, the container name for each subcontainer.
func (c *SandboxController) sandboxCgroups(ctx context.Context, sb *compute.Sandbox) (map[string]string, error) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	ids := map[string]string{"": pauseContainerId(sb.ID)}
	for _, container := range sb.Spec.Container {
		ids[container.Name] = fmt.Sprintf("%s-%s", containerPrefix(sb.ID), container.Name)
	}

	cgroups := make(map[string]string, len(ids))

	for name, id := range ids {
		container, err := c.CC.LoadContainer(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("loading container %s: %w", id, err)
		}

		spec, err := container.Spec(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting spec for container %s: %w", id, err)
		}

		if spec.Linux == nil {
			return nil, fmt.Errorf("container %s has no linux spec", id)
		}

		cgroups[name] = spec.Linux.CgroupsPath
	}

	return cgroups, nil
}

// ensureMetrics registers cgroup metrics for a sandbox this process did not
// boot itself, so CPU and memory keep flowing for containers that outlived a
// restart. Sandboxes already being monitored are left alone: this runs on
// every reconcile for adopted sandboxes, and re-adding would reset the CPU
// delta baseline each time (MIR-1013).
func (c *SandboxController) ensureMetrics(ctx context.Context, sb *compute.Sandbox) error {
	le, attrs := sandboxMetricsIdentity(sb)

	// Cheap bail-out so the common case doesn't ask containerd for a spec per
	// container on every reconcile. AddIfAbsent below is what actually keeps
	// two concurrent callers from both registering.
	if c.Metrics.Has(le) {
		return nil
	}

	cgroups, err := c.sandboxCgroups(ctx, sb)
	if err != nil {
		return err
	}

	added, err := c.Metrics.AddIfAbsent(le, cgroups, attrs)
	if err != nil {
		return err
	}

	if added {
		c.Log.Debug("registered metrics for surviving sandbox",
			"sandbox_id", sb.ID, "log_entity", le)
	}

	return nil
}

// reattachLogs reattaches log consumers to a container's task after controller restart.
// This is critical to prevent stdout/stderr buffers from filling up and blocking the process.
// containerName should be empty string for the pause container, or the subcontainer name otherwise.
func (c *SandboxController) reattachLogs(ctx context.Context, sb *compute.Sandbox, containerID, containerName, shortID string) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	// Create log consumer for this container
	sl := c.logConsumer(sb, containerName, shortID)

	// Rebuild the Hub for an attachable container so clients can rejoin after
	// the runner restarts. cio.NewAttach reopens whichever FIFOs the task was
	// created with and skips any stream we pass as nil, so supplying a stdin
	// reader here restores input for a task that was created with one — and is
	// harmlessly ignored for a task that was not.
	var hub *Hub
	if containerIsAttachable(sb, containerName) {
		hub = c.hubs.GetOrCreate(sb.ID, containerName)
	}

	streams := cio.WithStreams(nil, sl, sl.Stderr())
	if hub != nil {
		streams = cio.WithStreams(
			hub.Stdin(),
			io.MultiWriter(sl, hub),
			io.MultiWriter(sl.Stderr(), hub),
		)
	}

	// Reattach to the existing task with our log consumer
	// This drains stdout/stderr and prevents the process from blocking on writes
	task, err := container.Task(ctx, cio.NewAttach(streams))
	if err != nil {
		return fmt.Errorf("failed to attach to task: %w", err)
	}

	if hub != nil {
		hub.SetResizer(task)
	}

	if task == nil {
		return fmt.Errorf("task is nil after attach")
	}

	c.Log.Info("reattached logs to container",
		"sandbox_id", sb.ID,
		"container_id", containerID,
		"container_name", containerName)

	// Re-establish task exit monitoring for this container
	// This ensures we have consistent crash detection even after server restarts
	exitCh, err := task.Wait(ctx)
	if err != nil {
		c.Log.Warn("failed to set up task wait during reattach", "id", containerID, "error", err)
	} else {
		// Launch goroutine to monitor process exit
		go c.monitorTaskExit(sb, containerID, containerName, task, exitCh)
		c.Log.Debug("re-established task exit monitoring", "sandbox", sb.ID, "container", containerID)
	}

	return nil
}

func (c *SandboxController) Create(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) error {
	switch co.Status {
	case compute.DEAD:
		return nil
	case compute.STOPPED:
		c.Log.Debug("sandbox is stopped, verifying it is no longer running")
		return c.StopSandbox(ctx, co.ID, co)
	case "", compute.PENDING, compute.RUNNING:
		searchRes, err := c.CheckSandbox(ctx, co, meta)
		if err != nil {
			c.Log.Error("error checking sandbox, proceeding with create", "err", err)
		} else {
			switch searchRes {
			case same:
				// The containers are healthy but this process may not have
				// booted them: a sandbox still PENDING at restart is skipped by
				// boot reconciliation, so this is the only place its metrics get
				// re-registered (MIR-1013). ensureMetrics is a no-op once the
				// sandbox is monitored, so running it every reconcile is safe.
				if err := c.ensureMetrics(ctx, co); err != nil {
					c.Log.Warn("failed to ensure metrics for existing sandbox",
						"id", co.ID, "error", err)
				}

				// If sandbox exists and is healthy but status is PENDING,
				// update it to RUNNING. This is a fallback to recover from failed
				// status updates (e.g. due to OCC conflicts during creation).
				// Only do this if entity is stale (created > 2 minutes ago)
				// to avoid conflicting with active goroutines working on booting.
				if co.Status == compute.PENDING {
					createdAt := meta.GetCreatedAt()
					age := time.Since(createdAt)
					const staleThreshold = 2 * time.Minute

					if age > staleThreshold {
						c.Log.Info("sandbox exists and is healthy but status is PENDING (stale), updating to RUNNING",
							"id", co.ID,
							"createdAt", createdAt,
							"age", age)
						patchAttrs := entity.New(
							entity.Ref(entity.DBId, co.ID),
							(&compute.Sandbox{
								Status: compute.RUNNING,
							}).Encode,
						)
						_, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), meta.Revision)
						if err != nil {
							c.Log.Error("failed to update sandbox status to RUNNING", "id", co.ID, "error", err)
							return fmt.Errorf("failed to update sandbox status to RUNNING: %w", err)
						}
						return nil
					} else {
						c.Log.Debug("sandbox is PENDING but was recently created, skipping status correction",
							"id", co.ID,
							"age", age,
							"threshold", staleThreshold)
						return nil
					}
				}
				return nil
			case unhealthy:
				c.Log.Info("sandbox container exists but is unhealthy", "id", co.ID)

				// A sandbox that must not re-run its command is finished the
				// moment its containers stop being healthy. Fall through to the
				// recreate path below and we would execute it a second time.
				//
				// Gated on RUNNING because that is what distinguishes "it ran
				// and its containers are gone" from "it has not started yet":
				// only the former would be a re-execution.
				if shouldRetireInsteadOfRestart(co) {
					return c.markDeadNoRestart(ctx, co, "unhealthy")
				}

				// Mark sandbox as DEAD first if it was RUNNING
				// This prevents infinite recreation loops
				if co.Status == compute.RUNNING {
					c.Log.Info("marking unhealthy sandbox as DEAD", "id", co.ID)
					patchAttrs := entity.New(
						entity.Ref(entity.DBId, co.ID),
						(&compute.Sandbox{
							Status: compute.DEAD,
						}).Encode,
					)
					result, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), 0)
					if err != nil {
						c.Log.Error("failed to mark sandbox as DEAD", "id", co.ID, "error", err)
						return fmt.Errorf("failed to mark sandbox as DEAD: %w", err)
					}
					if c.writeTracker != nil && result.HasRevision() {
						c.writeTracker.RecordWrite(result.Revision())
					}
				}

				// Clean up the unhealthy sandbox
				err := c.StopSandbox(ctx, co.ID, co)
				if err != nil {
					c.Log.Error("failed to cleanup unhealthy sandbox", "id", co.ID, "err", err)
					return fmt.Errorf("failed to cleanup unhealthy sandbox: %w", err)
				}
				// Don't fall through - we've marked it DEAD, let the next reconciliation handle recreation
				return nil
			}
		}

		// Reaching here means the containers are gone (CheckSandbox returned
		// notFound, or erred and we're proceeding optimistically). For a normal
		// sandbox that's a reboot. For one whose command must execute at most
		// once it is the end of the road: the realistic case is a runner
		// restarting mid-run, where re-running a migration is not a recoverable
		// mistake.
		//
		// Only once it has actually been RUNNING, though. A PENDING sandbox has
		// no containers because none have been created yet, and refusing to
		// create them would retire every no-restart sandbox before it ever ran.
		if shouldRetireInsteadOfRestart(co) {
			return c.markDeadNoRestart(ctx, co, "containers missing")
		}

		return c.createSandbox(ctx, co, meta, false)
	case compute.NOT_READY:
		// Transient boot state; nothing to reconcile until it resolves.
		fallthrough
	default:
		c.Log.Warn("ignoring sandbox status", "status", co.Status)
		return nil
	}
}

// shouldRetireInsteadOfRestart reports whether a sandbox whose containers are
// missing or unhealthy must be retired rather than (re)created.
//
// The RUNNING check is what separates "it ran and its containers are gone" from
// "it has not started yet". Only the first is a re-execution; without the
// distinction every no-restart sandbox is retired on its first reconcile,
// before it has run anything at all.
func shouldRetireInsteadOfRestart(co *compute.Sandbox) bool {
	return co.Status == compute.RUNNING && co.Spec.RestartPolicy == compute.SandboxSpecNEVER
}

// markDeadNoRestart retires a sandbox whose spec forbids restarting it, then
// runs the ordinary teardown so nothing is left behind.
//
// It deliberately records no Exit: the command's own exit was never observed,
// and inventing a code here would be indistinguishable from the process
// actually having returned one.
func (c *SandboxController) markDeadNoRestart(ctx context.Context, co *compute.Sandbox, reason string) error {
	c.Log.Info("retiring sandbox rather than restarting it",
		"id", co.ID, "reason", reason, "restart_policy", "never")

	if co.Status != compute.DEAD {
		patchAttrs := entity.New(
			entity.Ref(entity.DBId, co.ID),
			(&compute.Sandbox{Status: compute.DEAD}).Encode,
		)
		result, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), 0)
		if err != nil {
			return fmt.Errorf("failed to mark no-restart sandbox as DEAD: %w", err)
		}
		if c.writeTracker != nil && result.HasRevision() {
			c.writeTracker.RecordWrite(result.Revision())
		}
	}

	if err := c.StopSandbox(ctx, co.ID, co); err != nil {
		return fmt.Errorf("failed to clean up no-restart sandbox: %w", err)
	}
	return nil
}

func (c *SandboxController) createSandbox(ctx context.Context, co *compute.Sandbox, meta *entity.Meta, recreate bool) (err error) {
	c.Log.Debug("creating sandbox", "id", co.ID)

	// Catch-all: any error during sandbox creation marks it DEAD so the pool
	// controller's crash-backoff logic kicks in instead of retrying forever.
	defer func() {
		if err != nil {
			c.Log.Error("sandbox boot failed, marking DEAD", "id", co.ID, "err", err)
			co.Status = compute.DEAD
			meta.Update(co.Encode())

			// Boot can fail after we've already bound a disk lease (e.g. a
			// later port health check fails). Unlike the graceful StopSandbox
			// path, this defer doesn't tear the sandbox down, so release any
			// leases here — otherwise the lease stays status.bound pointing at
			// a dead sandbox and wedges every replacement until it times out.
			// Use a non-cancelled context: the boot error may itself be a
			// cancellation, and we still want the lease freed. Bound it with a
			// timeout so a hung release can't pin the reconcile worker, matching
			// the cleanup defer below.
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if relErr := c.ReleaseDiskLeases(cleanupCtx, co.ID); relErr != nil {
				c.Log.Error("failed to release disk leases after boot failure", "id", co.ID, "err", relErr)
			}

			// Same reason as the leases: this defer is the only cleanup a failed
			// boot gets. A Hub is created before the task is, so a container that
			// never started still leaves one behind, and an attaching client
			// finds it and waits on output that can never arrive -- forever,
			// since the client's start deadline is only consulted between
			// attempts and it is now inside a successful attach.
			c.hubs.RemoveAll(co.ID)
			// ConfigureVolumes registered any sqlite disks before the failure,
			// and this path never reaches StopSandbox, so release them here or
			// a crash-looping app accumulates a replicator per boot attempt.
			c.releaseSqliteDisks(cleanupCtx, co.ID)
		}
	}()

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	ep, err := c.AllocateNetwork(ctx, co)
	if err != nil {
		return fmt.Errorf("failed to allocate network: %w", err)
	}

	// Patch entity with network address before starting sandbox
	networkAttrs := []any{
		entity.Ref(entity.DBId, co.ID),
	}

	for _, v := range co.Network {
		networkAttrs = append(networkAttrs, entity.Component(compute.SandboxNetworkId, v.Encode()))
	}

	patchAttrs := entity.New(networkAttrs...)

	// Use 0 as the revision in the case that the sandbox has been updated before we got
	// here. This update must go through.
	res, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), 0)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return fmt.Errorf("failed to patch sandbox with network address: %w", err)
	}

	meta.Revision = res.Revision()
	if c.writeTracker != nil && res.HasRevision() {
		c.writeTracker.RecordWrite(res.Revision())
	}

	opts, err := c.BuildSpec(ctx, co, ep, meta)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return fmt.Errorf("failed to build container spec: %w", err)
	}

	volumeMounts, err := c.ConfigureVolumes(ctx, co, meta)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return fmt.Errorf("failed to configure volumes: %w", err)
	}

	cid := pauseContainerId(co.ID)

	container, err := c.CC.NewContainer(ctx, cid, opts...)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return errors.Wrapf(err, "failed to create container %s", co.ID)
	}

	defer func() {
		if err != nil {
			c.Log.Error("failed to create sandbox, cleaning up container resources", "id", co.ID, "err", err)

			// Be sure we have at least 60 seconds to do this action.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			// Clean up network resources if they were allocated
			c.deallocateNetwork(ctx, ep)

			// Clean up any subcontainers that might have been created
			c.DestroySubContainers(ctx, co.ID)

			// Clean up the pause container using the common cleanup function
			c.CleanupContainer(ctx, container)

			// Boot may have gotten far enough to register the sandbox for token
			// refresh (see buildSubContainerSpec). This path marks the sandbox DEAD
			// without going through StopSandbox, so release that state here or it
			// lingers until the entity is swept.
			c.ReleaseTokenState(co.ID)

			// Update sandbox status to DEAD in entity store
			co.Status = compute.DEAD
			meta.Update(co.Encode())
			c.Log.Info("marked sandbox as DEAD due to boot failure", "id", co.ID)
		}
	}()

	task, err := c.BootInitialTask(ctx, co, ep, container, meta.ShortId())
	if err != nil {
		return err
	}

	rootSpec, err := container.Spec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container spec: %w", err)
	}

	cgroups := map[string]string{
		"": rootSpec.Linux.CgroupsPath,
	}

	waitPorts, err := c.BootContainers(ctx, co, ep, int(task.Pid()), cgroups, meta, volumeMounts)
	if err != nil {
		return err
	}

	le, attrs := sandboxMetricsIdentity(co)

	err = c.Metrics.Add(le, cgroups, attrs)
	if err != nil {
		return err
	}

	c.Log.Info("sandbox started", "id", co.ID, "namespace", c.Namespace)

	// Wait for ports to verify network connectivity before marking RUNNING.
	// Fail-hard: if ports never bind, fail the sandbox so pool can retry. An app
	// that ignored $PORT and bound a different port is detected here and routed
	// to its actual port instead of being killed.
	// Default 15s; spec.PortWaitTimeout overrides for slow-cold-init images.
	portTimeout := resolvePortWaitTimeout(co.Spec.PortWaitTimeout)
	var remapped bool
	for _, wp := range waitPorts {
		c.Log.Info("waiting for ports to be bound", "id", cid, "port", wp.Port, "timeout", portTimeout)
		err := c.WaitForPort(ctx, wp.ID, wp.Port, portTimeout)
		if err == nil {
			continue // configured port bound — the normal case
		}
		if ctx.Err() != nil {
			// We're shutting down, not looking at a port mismatch — skip
			// diagnosis so we don't emit misleading "listening elsewhere" events.
			return err
		}

		// The configured port never bound within the timeout. See what the app
		// actually listened on: route to it if it bound a single other port, or
		// fail with a message that names the real port.
		routable, loopback, ok := c.diagnoseListening(wp.ID)
		if alt, single := singleAlternativePort(routable, wp.Port); ok && single {
			if remapped {
				// A second configured port diverged. Routing only follows one
				// observed port, so don't guess — fail loudly instead.
				msg := fmt.Sprintf("more than one configured port came up on a different port; "+
					"can't safely auto-route (latest was :%d)", alt)
				c.EmitSandboxEvent(co, meta.ShortId(), msg)
				return fmt.Errorf("sandbox failed network health check: %s", msg)
			}
			remapped = true
			c.Log.Warn("app bound a port other than the configured one; routing to it",
				"id", co.ID, "configured_port", wp.Port, "observed_port", alt)
			c.EmitSandboxEvent(co, meta.ShortId(), fmt.Sprintf(
				"app is listening on :%d, not the configured :%d; routing to :%d. "+
					"Set $PORT or [services.web] port to :%d to silence this.",
				alt, wp.Port, alt, alt))
			co.BoundPort = append(co.BoundPort, compute.BoundPort{Port: int64(alt)})
			continue
		}

		msg := describePortFailure(wp.Port, routable, loopback)
		c.EmitSandboxEvent(co, meta.ShortId(), msg)
		return fmt.Errorf("sandbox failed network health check: %s", msg)
	}

	// If we're doing a recreate, then we know it's safe to set it to running.
	if recreate {
		co.Status = compute.RUNNING
	} else {
		// Only set status to RUNNING if it hasn't already been marked STOPPED or DEAD
		// (The monitoring goroutine may have already detected a crash)
		// Fetch current status to avoid race condition
		resp, err := c.EAC.Get(ctx, co.ID.String())
		if err != nil {
			c.Log.Warn("failed to fetch current sandbox status before update", "id", co.ID, "error", err)
			// Fallthrough to set RUNNING anyway
			co.Status = compute.RUNNING
		} else {
			var currentSandbox compute.Sandbox
			currentSandbox.Decode(resp.Entity().Entity())
			if currentSandbox.Status == compute.DEAD || currentSandbox.Status == compute.STOPPED {
				c.Log.Info("sandbox already in terminal state, not overwriting to RUNNING",
					"id", co.ID, "current_status", currentSandbox.Status)
				return nil
			}
			co.Status = compute.RUNNING
		}
	}

	// The controller will detect the updates and sync them back
	if err := meta.Update(co.Encode()); err != nil {
		return fmt.Errorf("failed to update entity metadata: %w", err)
	}

	err = c.UpdateServices(ctx, co, meta, ep)
	if err != nil {
		return fmt.Errorf("failed to update services: %w", err)
	}

	return nil
}

func (c *SandboxController) UpdateServices(
	ctx context.Context,
	co *compute.Sandbox,
	meta *entity.Meta,
	ep *network.EndpointConfig,
) error {
	sresp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	if err != nil {
		return err
	}

	md := core_v1alpha.MD(meta.Entity)

	c.Log.Debug("updating services", "id", co.ID, "labels", md.Labels, "services", len(sresp.Values()))

	// addEndpoint skips endpoints this sandbox already registered, so a
	// resumed create-sandbox saga does not mint duplicates.
	for _, ent := range sresp.Values() {
		var srv network_v1alpha.Service
		srv.Decode(ent.Entity())

		if !srv.Match.SubsetOf(md.Labels) {
			c.Log.Debug("skipping service, labels do not match", "service", srv.ID, "labels", srv.Match, "entity", md.Labels)
			continue
		}

		err = c.addEndpoint(ctx, co, ep, &srv)
		if err != nil {
			return fmt.Errorf("failed to add endpoint: %w", err)
		}
	}

	return nil
}

// serviceEndpointKeys returns the keys already registered against srv for this
// sandbox, identified by IP: each sandbox owns a distinct subnet IP. Scoped to
// one service because endpoints.service is indexed and this runs on every
// sandbox boot; a full-kind scan would cost the cluster's endpoint count each
// time.
func (c *SandboxController) serviceEndpointKeys(
	ctx context.Context,
	srv *network_v1alpha.Service,
	ips map[string]bool,
) (map[string]bool, error) {
	keys := make(map[string]bool)
	if len(ips) == 0 {
		return keys, nil
	}

	resp, err := c.EAC.List(ctx, entity.Ref(network_v1alpha.EndpointsServiceId, srv.ID))
	if err != nil {
		return nil, err
	}
	for _, ent := range resp.Values() {
		var eps network_v1alpha.Endpoints
		eps.Decode(ent.Entity())
		for _, e := range eps.Endpoint {
			if !ips[e.Ip] {
				continue
			}
			keys[endpointKey(eps.Service, e.Ip, e.Port)] = true
		}
	}
	return keys, nil
}

// endpointKey is the (service, sandbox-ip, port) identity of an Endpoints entry.
func endpointKey(service entity.Id, ip string, port int64) string {
	return service.String() + "|" + ip + "|" + strconv.FormatInt(port, 10)
}

func (c *SandboxController) addEndpoint(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	srv *network_v1alpha.Service,
) error {
	if len(ep.Addresses) == 0 {
		// Not a state any caller sets up on purpose, so say so.
		c.Log.Warn("skipping endpoint registration, sandbox has no addresses",
			"service", srv.ID, "sandbox", sb.ID)
		return nil
	}
	ip := ep.Addresses[0].Addr().String()

	ips := make(map[string]bool, len(ep.Addresses))
	for _, a := range ep.Addresses {
		ips[a.Addr().String()] = true
	}
	existing, err := c.serviceEndpointKeys(ctx, srv, ips)
	if err != nil {
		return fmt.Errorf("listing existing endpoints for service %s: %w", srv.ID, err)
	}

	c.Log.Debug("adding endpoint to service", "service", srv.ID, "sandbox", sb.ID, "containers", len(sb.Spec.Container))

	for _, co := range sb.Spec.Container {
		for _, p := range co.Port {
			var add bool
			for _, sp := range srv.Port {
				if (sp.TargetPort != 0 && p.Port == sp.TargetPort) || p.Port == sp.Port {
					add = true
					break
				}
			}

			if !add {
				c.Log.Debug("skipping port, not in service", "port", p.Port, "service", srv.ID)
				continue
			}

			key := endpointKey(srv.ID, ip, p.Port)
			if existing[key] {
				c.Log.Debug("skipping endpoint, already registered",
					"service", srv.ID, "sandbox", sb.ID, "ip", ip, "port", p.Port)
				continue
			}

			var eps network_v1alpha.Endpoints

			eps.Service = srv.ID
			eps.Endpoint = append(eps.Endpoint, network_v1alpha.Endpoint{
				Ip:   ip,
				Port: p.Port,
			})

			// TODO add metadata and probably use higher level entityclient
			pr, err := c.EAC.Create(ctx, entity.New(
				eps.Encode(),
			).Attrs())
			if err != nil {
				return fmt.Errorf("failed to update service: %w", err)
			}

			// So a later port matching the same service port does not duplicate.
			existing[key] = true

			c.Log.Debug("updated service", "id", pr.Id(), "service", eps.Service)
		}
	}

	return nil
}

func (c *SandboxController) deleteEndpoints(ctx context.Context, id entity.Id, sandboxIPs map[string]bool) error {
	// If no IPs found, nothing to delete
	if len(sandboxIPs) == 0 {
		c.Log.Debug("no sandbox IPs found, skipping endpoint deletion", "sandbox_id", id)
		return nil
	}

	// Get all endpoints
	endpoints, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
	if err != nil {
		return fmt.Errorf("failed to list endpoints: %w", err)
	}

	c.Log.Debug("considering endpoints for deletion", "sandbox_id", id, "endpoints_count", len(endpoints.Values()), "sandbox_ips", sandboxIPs)

	// Delete any endpoints that contain our sandbox's IPs
	for _, epEntity := range endpoints.Values() {
		var ep network_v1alpha.Endpoints
		ep.Decode(epEntity.Entity())

		// Check if any endpoint IPs match our sandbox IPs
		shouldDelete := false
		for _, endpoint := range ep.Endpoint {
			if sandboxIPs[endpoint.Ip] {
				shouldDelete = true
				break
			}
		}

		if shouldDelete {
			c.Log.Info("deleting endpoints for sandbox", "sandbox_id", id, "endpoint_id", ep.ID)
			_, err = c.EAC.Delete(ctx, ep.ID.String())
			if err != nil {
				c.Log.Error("failed to delete endpoint", "id", ep.ID, "error", err)
			}
		}
	}

	return nil
}

// deallocateNetwork releases the network resources allocated for a sandbox
func (c *SandboxController) deallocateNetwork(ctx context.Context, ep *network.EndpointConfig) {
	if ep == nil {
		return
	}

	for _, addr := range ep.Addresses {
		if err := c.Subnet.ReleaseAddr(addr.Addr()); err != nil {
			c.Log.Error("failed to release IP address during cleanup", "addr", addr.Addr(), "err", err)
		} else {
			c.Log.Debug("released IP address during cleanup", "addr", addr.Addr())
		}
	}
}

func (c *SandboxController) AllocateNetwork(
	ctx context.Context,
	co *compute.Sandbox,
) (*network.EndpointConfig, error) {
	if c.Bridge == "" {
		return nil, fmt.Errorf("bridge name not configured")
	}

	if c.Subnet == nil {
		return nil, fmt.Errorf("subnet not configured")
	}

	var (
		ep  *network.EndpointConfig
		err error
	)

	if len(co.Network) > 0 {
		var prefixes []netip.Prefix

		for _, net := range co.Network {
			// Parse address (handles both CIDR and plain IP formats)
			ipStr, err := netutil.ParseNetworkAddress(net.Address)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %s (%w)", net.Address, err)
			}

			// Convert to netip.Addr
			addr, err := netip.ParseAddr(ipStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP: %s (%w)", ipStr, err)
			}

			// Convert to prefix (assume /32 for IPv4, /128 for IPv6)
			prefix := netip.PrefixFrom(addr, addr.BitLen())
			prefixes = append(prefixes, prefix)
		}

		ep, err = network.SetupOnBridge(c.Bridge, c.Subnet, prefixes)
		if err != nil {
			return nil, err
		}

	} else {
		ep, err = network.AllocateOnBridge(c.Log, c.Bridge, c.Subnet, func() (map[netip.Addr]bool, error) {
			return c.liveBridgeIPs(ctx)
		})
		if err != nil {
			return nil, err
		}

		co.Network = append(co.Network, compute.Network{
			Address: ep.Addresses[0].String(),
			Subnet:  c.Bridge,
		})
	}

	c.Log.Debug("allocated network endpoint", "bridge", c.Bridge, "addresses", ep.Addresses)

	return ep, nil
}

// liveBridgeIPs returns the set of bridge IP addresses currently assigned to
// sandbox containers, read from their containerd labels. It is the in-use set
// used by AllocateOnBridge to avoid handing out an address that netdb's lease
// bookkeeping has lost track of but a sandbox is still using (MIR-1238). The
// labels are the same source the watchdog trusts and require no netns entry.
//
// It does not inspect task state, so a container whose task has exited but whose
// containerd record still exists (e.g. between SIGKILL and removeContainer)
// still contributes its IP. This is deliberately conservative: counting a
// not-quite-gone address as live at worst keeps it reserved for one extra
// reconciler cycle, whereas missing a live address risks a duplicate assignment.
func (c *SandboxController) liveBridgeIPs(ctx context.Context) (map[netip.Addr]bool, error) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	containerList, err := c.CC.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	live := make(map[netip.Addr]bool)
	for _, cont := range containerList {
		labels, err := cont.Labels(ctx)
		if err != nil {
			c.Log.Warn("failed to read container labels while enumerating live IPs", "id", cont.ID(), "error", err)
			continue
		}
		for label, value := range labels {
			if !strings.HasPrefix(label, "runtime.computer/ip") {
				continue
			}
			addr, err := netip.ParseAddr(value)
			if err != nil {
				c.Log.Warn("failed to parse IP from container label", "id", cont.ID(), "label", label, "value", value, "error", err)
				continue
			}
			live[addr] = true
		}
	}

	return live, nil
}

func (c *SandboxController) setupHosts(sb *compute.Sandbox, name string, ep *network.EndpointConfig) error {
	if ep == nil || len(ep.Addresses) == 0 {
		return fmt.Errorf("no addresses allocated for sandbox %s", sb.ID)
	}

	var lines []string

	lines = append(lines, "# The following lines are managed by runtime.computer")
	lines = append(lines, "127.0.0.1\tlocalhost localhost.localdomain")
	lines = append(lines, "::1\tlocalhost localhost.localdomain")

	for _, prefix := range ep.Addresses {
		lines = append(lines, fmt.Sprintf("%s\t%s", prefix.Addr(), name))
	}

	for _, addr := range sb.Spec.StaticHost {
		lines = append(lines, fmt.Sprintf("%s\t%s", addr.Ip, addr.Host))
	}
	lines = append(lines, "")

	path := c.sandboxPath(sb, "hosts")

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func (c *SandboxController) resolver() remotes.Resolver {
	return docker.NewResolver(docker.ResolverOptions{
		Hosts: func(host string) ([]docker.RegistryHost, error) {
			switch host {
			case "cluster.local", ocireg.Host:
				config, err := c.localRegistryHost()
				if err != nil {
					return nil, err
				}
				return []docker.RegistryHost{config}, nil
			default:
				config := containerdx.DefaultRegistryHost(host)
				return []docker.RegistryHost{config}, nil
			}
		},
	})
}

func (c *SandboxController) localRegistryHost() (docker.RegistryHost, error) {
	addr, err := c.Resolver.LookupHost("cluster.local")
	if err != nil {
		return docker.RegistryHost{}, fmt.Errorf("failed to resolve cluster.local: %w", err)
	}

	config := docker.RegistryHost{
		Client:       http.DefaultClient,
		Host:         addr.String() + ":5000",
		Scheme:       "http",
		Path:         "/v2",
		Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve,
	}
	if c.WorkloadIssuer != nil {
		token, err := c.WorkloadIssuer.IssueSystemWorkloadToken(
			workloadidentity.SystemWorkloadSandboxController,
			workloadidentity.TokenOptions{Audience: []string{ocireg.Audience}},
		)
		if err != nil {
			return docker.RegistryHost{}, fmt.Errorf("issuing registry token: %w", err)
		}
		config.Header = http.Header{"Authorization": []string{"Bearer " + token}}
	}

	return config, nil
}

func (c *SandboxController) BuildSpec(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	meta *entity.Meta,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, sandboxImage)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, sandboxImage, containerd.WithPullUnpack, containerd.WithResolver(c.resolver()))
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", sandboxImage, err)
		}

		img, err = c.CC.GetImage(ctx, sandboxImage)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", sandboxImage, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	lbls := map[string]string{}

	for _, lbl := range sb.Labels {
		if key, val, ok := strings.Cut(lbl, "="); ok {
			lbls[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}

	lbls[sandboxVersionLabel] = strconv.FormatInt(meta.Revision, 10)
	lbls[sandboxEntityLabel] = sb.ID.String()
	lbls[sandboxKindLabel] = "sandbox"

	if sb.Spec.Version != "" {
		lbls[sandboxVerEntityLabel] = sb.Spec.Version.String()
	}

	// Store LogEntity in label for retrieval during cleanup
	// This allows stopSandbox to work with only the ID
	le := sb.Spec.LogEntity
	if le == "" {
		le = sb.ID.String()
	}
	lbls["runtime.computer/log-entity"] = le

	// Add IP addresses from endpoint configuration
	for i, addr := range ep.Addresses {
		if i == 0 {
			lbls["runtime.computer/ip"] = addr.Addr().String()
		} else {
			lbls[fmt.Sprintf("runtime.computer/ip%d", i)] = addr.Addr().String()
		}
	}

	//if config.StaticDir != "" {
	//lbls["runtime.computer/static_dir"] = config.StaticDir
	//}

	tmpDir := filepath.Join(c.Tempdir, "containerd", sb.ID.PathSafe())
	os.MkdirAll(tmpDir, 0755)

	resolvePath := c.sandboxPath(sb, "resolv.conf")
	err = c.writeResolve(resolvePath, ep)
	if err != nil {
		return nil, err
	}

	err = c.setupHosts(sb, sandboxHostname(sb.ID), ep)
	if err != nil {
		return nil, err
	}

	mounts := []specs.Mount{
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/etc/hosts",
			Type:        "bind",
			Source:      c.sandboxPath(sb, "hosts"),
			Options:     []string{"rbind", "rw"},
		},
	}

	// Create unique cgroup path for this sandbox
	cgroupPath := fmt.Sprintf("/miren/sandbox-%s", sb.ID.PathSafe())

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd("/"),
		oci.WithHostname(sandboxHostname(sb.ID)),
		oci.WithAnnotations(map[string]string{
			"io.kubernetes.cri.container-type": "sandbox",
		}),
		func(ctx context.Context, c1 oci.Client, c2 *containers.Container, s *oci.Spec) error {
			s.Linux.CgroupsPath = cgroupPath
			return nil
		},
		containerdx.WithOOMScoreAdj(defaultSandboxOOMAdj, false),
	}

	if sb.Spec.HostNetwork {
		specOpts = append(specOpts, oci.WithHostNamespace(specs.NetworkNamespace))
	}

	snapshotId := pauseContainerId(sb.ID)

	opts = append(opts,
		containerd.WithNewSnapshot(snapshotId, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *SandboxController) writeResolve(path string, ep *network.EndpointConfig) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(ep.Bridge.Addresses) == 0 {
		return fmt.Errorf("no nameservers available in bridge config")
	}

	// Add search domain for app.miren
	fmt.Fprintf(f, "search app.miren\n")
	fmt.Fprintf(f, "options timeout:2 attempts:3\n")

	for _, addr := range ep.Bridge.Addresses {
		if !addr.Addr().IsValid() {
			return fmt.Errorf("invalid nameserver address: %v", addr)
		}
		fmt.Fprintf(f, "nameserver %s\n", addr.Addr().String())
	}

	return nil
}

// sandboxLogMeta returns the log entity and attribute map used when
// writing log entries for a sandbox. Both container stdout/stderr
// streaming (via logConsumer) and runtime lifecycle events (via
// EmitSandboxEvent) share this so the two sources appear with the
// same identity and metadata in the log stream.
func entityShortID(e entity.AttrGetter) string {
	if attr, ok := e.Get(entity.DBShortId); ok {
		return attr.Value.String()
	}
	return ""
}

func sandboxLogMeta(sb *compute.Sandbox, container, shortID string) (string, map[string]string) {
	le := sb.Spec.LogEntity
	if le == "" {
		le = sb.ID.String()
	}

	attrs := map[string]string{
		"miren.sandbox": sb.ID.String(),
		"source":        strings.TrimPrefix(sb.ID.String(), "sandbox/"),
	}

	if shortID != "" {
		attrs["miren.short_id"] = shortID
	}

	if container != "" {
		attrs["miren.container"] = container
	}

	if sb.Spec.Version != "" {
		attrs["miren.version"] = sb.Spec.Version.String()
	}

	for _, lbl := range sb.Spec.LogAttribute {
		attrs[lbl.Key] = lbl.Value
	}

	return le, attrs
}

func (c *SandboxController) logConsumer(sb *compute.Sandbox, container, shortID string) *SandboxLogs {
	le, attrs := sandboxLogMeta(sb, container, shortID)
	return NewSandboxLogs(c.Log, le, attrs, c.LogWriter)
}

// EmitSandboxEvent writes a single runtime lifecycle line to the
// sandbox's log stream, using the same entity and attrs the container
// stdio pipeline uses. Events go on the Stderr stream so operators see
// them in `miren logs sandbox <id>` and can distinguish them from
// application output via the [miren] prefix.
func (c *SandboxController) EmitSandboxEvent(sb *compute.Sandbox, shortID, line string) {
	le, attrs := sandboxLogMeta(sb, "", shortID)
	err := c.LogWriter.WriteEntry(le, observability.LogEntry{
		Timestamp:  time.Now(),
		Stream:     observability.Stderr,
		Body:       "[miren] " + line,
		Attributes: attrs,
	})
	if err != nil {
		c.Log.Error("failed to write sandbox lifecycle event",
			"sandbox", sb.ID, "error", err)
	}
}

func (c *SandboxController) BootInitialTask(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	container containerd.Container,
	shortID string,
) (containerd.Task, error) {
	c.Log.Info("booting sandbox task")

	sl := c.logConsumer(sb, "", shortID)

	task, err := container.NewTask(ctx, cio.NewCreator(
		cio.WithStreams(nil, sl, sl.Stderr())))
	if err != nil {
		return nil, err
	}

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), ep)
	if err != nil {
		return nil, err
	}

	err = c.configureFirewall(sb, ep)
	if err != nil {
		return nil, err
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, err
	}

	return task, nil
}

// WaitPort describes a container port to wait for during sandbox creation.
type WaitPort struct {
	ID   string
	Port int
}

const defaultPortWaitTimeout = 15 * time.Second

// resolvePortWaitTimeout parses a user-supplied duration string from
// SandboxSpec.PortWaitTimeout, falling back to the default on empty, invalid,
// or non-positive values so a typo doesn't brick a pool.
func resolvePortWaitTimeout(spec string) time.Duration {
	if spec == "" {
		return defaultPortWaitTimeout
	}
	d, err := time.ParseDuration(spec)
	if err != nil || d <= 0 {
		return defaultPortWaitTimeout
	}
	return d
}

// CleanupContainer removes a container and its snapshot during failure scenarios
func (c *SandboxController) CleanupContainer(ctx context.Context, cont containerd.Container) {
	if cont == nil {
		return
	}

	containerID := cont.ID()

	c.Log.Debug("cleaning up container", "id", containerID)

	// Stop port monitoring for this container
	if c.portMonitor != nil {
		c.portMonitor.StopMonitoring(containerID)
	}

	task, err := cont.Task(ctx, cleanupAttach())
	if err == nil && task != nil {
		task.Kill(ctx, unix.SIGKILL)
		_, err = task.Delete(ctx, containerd.WithProcessKill)
		if err != nil {
			c.Log.Debug("failed to delete task during cleanup", "id", containerID, "err", err)
		}
	}

	// Get the snapshotter info from the container before deleting it
	var snapshotKey string
	var snapshotterName string
	if info, ierr := cont.Info(ctx); ierr == nil {
		snapshotKey = info.SnapshotKey
		snapshotterName = info.Snapshotter
	}

	// Delete the container with snapshot cleanup
	err = cont.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil && !errdefs.IsNotFound(err) {
		c.Log.Error("failed to cleanup container", "id", containerID, "err", err)
	} else {
		c.Log.Debug("cleaned up container", "id", containerID)
	}

	// Always try to explicitly delete the snapshot to be absolutely sure
	if snapshotterName != "" && snapshotKey != "" {
		snapshotter := c.CC.SnapshotService(snapshotterName)
		if snapshotter != nil {
			if err := snapshotter.Remove(ctx, snapshotKey); err != nil && !errdefs.IsNotFound(err) {
				c.Log.Debug("failed to explicitly delete snapshot", "id", containerID, "snapshot_key", snapshotKey, "err", err)
			} else if err == nil {
				c.Log.Debug("explicitly deleted snapshot", "id", containerID, "snapshot_key", snapshotKey)
			}
		}
	}
}

// cleanupContainers removes containers and their snapshots during failure scenarios
func (c *SandboxController) cleanupContainers(ctx context.Context, containers []containerd.Container) {
	for _, cont := range containers {
		if cont != nil {
			c.CleanupContainer(ctx, cont)
		}
	}
}

func (c *SandboxController) BootContainers(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	sbPid int,
	cgroups map[string]string,
	meta *entity.Meta,
	volumeMounts map[string]string,
) ([]WaitPort, error) {
	c.Log.Info("booting containers", "count", len(sb.Spec.Container))

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	var ret []WaitPort
	var createdContainers []containerd.Container

	// Clean up any created containers on failure
	defer func() {
		if err := recover(); err != nil {
			c.Log.Error("panic during container boot, cleaning up", "error", err)
			c.cleanupContainers(ctx, createdContainers)
			panic(err)
		}
	}()

	c.registerSandboxDNS(ctx, sb, ep, meta)

	for _, container := range sb.Spec.Container {
		opts, err := c.buildSubContainerSpec(ctx, sb, &container, ep, sbPid, meta, volumeMounts)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, fmt.Errorf("failed to build container spec: %w", err)
		}

		id := fmt.Sprintf("%s-%s", containerPrefix(sb.ID), container.Name)

		var ports []int
		for _, port := range container.Port {
			ports = append(ports, int(port.Port))
			ret = append(ret, WaitPort{
				ID:   id,
				Port: int(port.Port),
			})
		}

		c.Log.Info("creating container", "id", id)

		cc, err := c.CC.NewContainer(ctx, id, opts...)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, errors.Wrapf(err, "failed to create container %s", sb.ID)
		}
		createdContainers = append(createdContainers, cc)

		spec, err := cc.Spec(ctx)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, fmt.Errorf("failed to get container spec: %w", err)
		}

		cgroups[container.Name] = spec.Linux.CgroupsPath

		sl := c.logConsumer(sb, container.Name, meta.ShortId())

		// Build cio options based on container spec
		var cioOpts []cio.Opt
		if container.Tty {
			cioOpts = append(cioOpts, cio.WithTerminal)
		}

		// An attachable container gets a Hub: its stdin FIFO exists from the
		// start (containerd only wires one up when a reader is supplied here,
		// and it cannot be added later), and its output is teed to whoever is
		// attached as well as to the logs.
		var hub *Hub
		if container.Stdin {
			hub = c.hubs.GetOrCreate(sb.ID, container.Name)
		}
		if hub != nil {
			cioOpts = append(cioOpts, cio.WithStreams(
				hub.Stdin(),
				io.MultiWriter(sl, hub),
				io.MultiWriter(sl.Stderr(), hub),
			))
		} else {
			cioOpts = append(cioOpts, cio.WithStreams(nil, sl, sl.Stderr()))
		}

		task, err := cc.NewTask(ctx, cio.NewCreator(cioOpts...))
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, err
		}

		err = task.Start(ctx)
		if err != nil {
			// Try to delete the task first if it was created but not started
			task.Delete(ctx, containerd.WithProcessKill)
			c.cleanupContainers(ctx, createdContainers)
			return nil, err
		}

		c.Log.Info("container started", "id", cc.ID())

		if hub != nil {
			hub.SetResizer(task)
		}

		// Monitor task for process exit to update sandbox status
		exitCh, err := task.Wait(ctx)
		if err != nil {
			c.Log.Warn("failed to set up task wait", "id", cc.ID(), "error", err)
		} else {
			// Launch goroutine to monitor process exit
			go c.monitorTaskExit(sb, cc.ID(), container.Name, task, exitCh)
		}

		// Start port monitoring for this container if it has ports
		if len(ports) > 0 && len(ep.Addresses) > 0 {
			ip := ep.Addresses[0].Addr().String()
			c.portMonitor.MonitorContainer(id, ip, sbPid, ports)
		}
	}

	return ret, nil
}

func (c *SandboxController) monitorTaskExit(
	sb *compute.Sandbox,
	containerID string,
	containerName string,
	task containerd.Task,
	exitCh <-chan containerd.ExitStatus,
) {
	c.Log.Debug("monitoring task for exit", "sandbox", sb.ID, "container", containerID)

	for {
		select {
		case exitStatus := <-exitCh:
			// Check if the exit status contains an error. Per containerd docs:
			// "ExitCode() is only valid if Error() returns nil" and
			// "If an error is returned, the process may still be running."
			// This can happen when re-establishing task monitoring after server restart,
			// where containerd returns an ExitStatus with UnknownExitStatus (255) and
			// zero exit time because the task state is uncertain.
			if err := exitStatus.Error(); err != nil {
				c.Log.Warn("received exit status with error, attempting to re-establish monitoring",
					"sandbox", sb.ID,
					"container", containerID,
					"error", err,
				)
				// Try to re-establish monitoring since the process may still be running
				newExitCh, waitErr := task.Wait(c.topCtx)
				if waitErr != nil {
					c.Log.Warn("failed to re-establish task monitoring after error status",
						"sandbox", sb.ID,
						"container", containerID,
						"error", waitErr,
					)
					return
				}
				exitCh = newExitCh
				continue
			}

			c.Log.Info("container process exited",
				"sandbox", sb.ID,
				"container", containerID,
				"exit_code", exitStatus.ExitCode(),
				"exit_time", exitStatus.ExitTime(),
			)

			// We don't delete the task here so that our destroySubContainers function
			// has a consistent view of the state of containers and tasks.

			// Record the exit in the same patch as the STOPPED transition, so
			// anything woken by the stop already has the code. Splitting them
			// would leave a window where a run's sandbox reads stopped with no
			// result to report.
			//
			// ExitTime can be zero even on a clean exit, and an Exit carrying a
			// zero code and a zero time is Empty() -- it would encode to nothing
			// and the code would silently vanish. Fall back to now.
			exitAt := exitStatus.ExitTime()
			if exitAt.IsZero() {
				exitAt = time.Now()
			}

			// Update sandbox status to STOPPED using Patch (only updating the
			// fields set here). STOPPED triggers cleanup in reconciliation
			// (stopSandbox), which:
			// - Releases IPs immediately
			// - Cleans up containers
			// - Marks as DEAD afterward
			//
			// The DEAD patch that follows leaves Exit intact: it is a struct
			// literal with an empty Exit, which the generated encoder skips.
			ctx := context.Background()
			result, err := c.recordExit(c.topCtx, sb.ID, compute.Exit{
				Code:      int64(exitStatus.ExitCode()),
				At:        exitAt,
				Container: containerName,
			})
			if err != nil {
				if !errors.Is(err, cond.ErrNotFound{}) {
					c.Log.Error("failed to update sandbox status to STOPPED",
						"sandbox", sb.ID,
						"error", err,
					)
				}
				return
			}
			if c.writeTracker != nil && result != nil && result.HasRevision() {
				c.writeTracker.RecordWrite(result.Revision())
			}

			// If this sandbox belongs to a task run, report the exit to the run
			// as well. That is not redundancy: the sandbox entity has several
			// writers, so the exit recorded there can lose a write to a
			// concurrent stale patch, while the run has exactly one writer plus
			// this input and nothing to contend with.
			c.reportRunExit(ctx, sb, exitStatus.ExitCode(), exitAt)

			c.Log.Info("marked sandbox as STOPPED due to process exit, cleanup will be triggered by reconciliation", "sandbox", sb.ID)
			return

		case <-c.topCtx.Done():
			c.Log.Debug("task monitoring cancelled", "sandbox", sb.ID, "container", containerID)
			return
		}
	}
}

// recordExit writes a container's exit alongside the STOPPED transition, under
// optimistic concurrency.
//
// The revision guard is the whole point. PatchEntity is read-modify-write, and
// the rest of the controller patches with revision 0, so two writers computed
// from the same snapshot do not merge -- the second simply overwrites the first
// wholesale. The boot path is exactly such a writer: it decides to mark a
// sandbox RUNNING from a snapshot taken before the task started, and for a
// command that exits in milliseconds that write can land after the exit. The
// result is not a clobbered field but a lost update, taking the exit code with
// it and restoring RUNNING on a sandbox whose process is already gone.
//
// Retrying against the current revision makes the exit the last write instead.
// The cap is high rather than small: losing a task's exit code is not something
// to give up on after a couple of attempts, and contention here is bounded by
// the handful of writers a single sandbox has.
func (c *SandboxController) recordExit(
	ctx context.Context,
	id entity.Id,
	exit compute.Exit,
) (*entityserver_v1alpha.EntityAccessClientPatchResults, error) {
	const maxAttempts = 100

	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			// Back off rather than hammering the store. Without this a
			// contended sandbox issues up to 200 immediate round trips, and a
			// shutdown could not interrupt them.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(min(time.Duration(attempt)*10*time.Millisecond, 500*time.Millisecond)):
			}
		}

		resp, err := c.EAC.Get(ctx, id.String())
		if err != nil {
			return nil, err
		}

		patchAttrs := entity.New(
			entity.Ref(entity.DBId, id),
			(&compute.Sandbox{Status: compute.STOPPED, Exit: exit}).Encode,
		)

		result, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), resp.Entity().Revision())
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return nil, err
		}

		lastErr = err
		c.Log.Debug("retrying exit record after a concurrent sandbox write",
			"sandbox", id, "attempt", attempt+1)
	}

	return nil, fmt.Errorf("recording exit for %s: %w", id, lastErr)
}

// reportRunExit records a container's exit against the task run that owns it.
//
// RFD-97 models the reported exit as an *input* to the run controller rather
// than a state the reporter owns, with a source saying where it came from. This
// is that input for the ordinary case where the command is the container's own
// process; the exec path supplies the same field when a client is attached.
//
// Reporting here rather than leaving the run to read it off the sandbox is what
// makes the exit code reliable. The sandbox entity is written by the boot path,
// the exit monitor, and teardown, all with revision 0 -- so a read-modify-write
// computed from a pre-exit snapshot can land afterwards and take the exit with
// it. The run entity has one writer and this input, so there is no race to lose.
func (c *SandboxController) reportRunExit(
	ctx context.Context,
	sb *compute.Sandbox,
	code uint32,
	at time.Time,
) {
	var runID entity.Id
	for _, l := range sb.Spec.LogAttribute {
		if l.Key == "miren.run" {
			runID = entity.Id(l.Value)
			break
		}
	}
	if runID == "" {
		return
	}

	_, err := c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, runID),
		(&run_v1alpha.Run{
			Result: run_v1alpha.Result{
				Code:   int64(code),
				At:     at,
				Source: run_v1alpha.TASK,
			},
		}).Encode,
	).Attrs(), 0)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Warn("failed to report exit to run", "run", runID, "sandbox", sb.ID, "error", err)
		}
		return
	}

	c.Log.Debug("reported exit to run", "run", runID, "sandbox", sb.ID, "exit_code", code)
}

func (c *SandboxController) sandboxPath(sb *compute.Sandbox, sub ...string) string {
	parts := append(
		[]string{c.Tempdir, "containerd", sb.ID.PathSafe()},
		sub...,
	)

	return filepath.Join(parts...)
}

func (c *SandboxController) buildSubContainerSpec(
	ctx context.Context,
	sb *compute.Sandbox,
	co *compute.SandboxSpecContainer,
	ep *network.EndpointConfig,
	sbPid int,
	meta *entity.Meta,
	volumeMounts map[string]string,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, co.Image)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, co.Image, containerd.WithPullUnpack, containerd.WithResolver(c.resolver()))
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", co.Image, err)
		}

		img, err = c.CC.GetImage(ctx, co.Image)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", co.Image, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	resolvePath := c.sandboxPath(sb, "resolv.conf")

	mounts := []specs.Mount{
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/etc/hosts",
			Type:        "bind",
			Source:      c.sandboxPath(sb, "hosts"),
			Options:     []string{"rbind", "rw"},
		},
	}

	// Look up volume specs by name so disk mounts can be made writable by the
	// container's run user (MIR-1428). Only miren-provider disks participate;
	// host/local volumes manage their own ownership.
	volByName := make(map[string]compute.SandboxSpecVolume, len(sb.Spec.Volume))
	for _, v := range sb.Spec.Volume {
		volByName[v.Name] = v
	}
	var diskMounts []diskMount

	for _, m := range co.Mount {
		var rawPath string
		var ok bool

		// First try to get the volume from the volumeMounts map (for configured volumes)
		rawPath, ok = volumeMounts[m.Source]
		if !ok {
			// If not in configured volumes, look for it in the sandbox volumes directory
			// This supports the old-style volume mounts
			rawPath = c.sandboxPath(sb, "volumes", m.Source)
			st, err := os.Lstat(rawPath)
			if err != nil {
				return nil, fmt.Errorf("volume %s does not exist", rawPath)
			}

			// Follow symlinks to get the real path
			for st.Mode().Type() == os.ModeSymlink {
				tgt, err := os.Readlink(rawPath)
				if err != nil {
					return nil, fmt.Errorf("failed to read symlink %s: %w", rawPath, err)
				}

				rawPath = tgt
				st, err = os.Stat(rawPath)
				if err != nil {
					return nil, fmt.Errorf("volume %s does not exist", rawPath)
				}
			}
		}

		c.Log.Debug("adding container mount",
			"volume", m.Source,
			"source_path", rawPath,
			"container_dest", m.Destination)

		mounts = append(mounts, specs.Mount{
			Destination: m.Destination,
			Type:        "bind",
			Source:      rawPath,
			Options:     []string{"rbind", "rw"},
		})

		// A miren- or sqlite-provider disk should be writable by the run user:
		// both are created by the runner as root, and a sqlite disk in
		// particular needs the directory writable so SQLite can manage its
		// -wal and -shm sidecars. Skip read-only mounts (chowning a read-only
		// filesystem would fail) and other providers, which manage their own
		// ownership.
		if v, ok := volByName[m.Source]; ok && (v.Provider == "miren" || v.Provider == "" || v.Provider == "sqlite") && !v.ReadOnly {
			diskMounts = append(diskMounts, diskMount{hostPath: rawPath, owner: v.Owner})
		}
	}

	for _, cf := range co.ConfigFile {
		h, _ := blake2b.New256(nil)
		fmt.Fprint(h, cf.Path)
		fmt.Fprint(h, cf.Data)

		id := base58.Encode(h.Sum(nil))

		rawPath := c.sandboxPath(sb, id)

		var mode os.FileMode = 0644

		if cf.Mode != "" {
			m, err := strconv.ParseInt(cf.Mode, 8, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse file mode %s: %w", cf.Mode, err)
			}
			mode = os.FileMode(m)
		}

		err = os.WriteFile(rawPath, []byte(cf.Data), mode)
		if err != nil {
			return nil, fmt.Errorf("failed to write config file %s: %w", rawPath, err)
		}

		c.Log.Debug("created config file", "path", rawPath, "dest", cf.Path, "mode", mode)

		mounts = append(mounts, specs.Mount{
			Destination: cf.Path,
			Type:        "bind",
			Source:      rawPath,
			Options:     []string{"rbind", "rw"},
		})
	}

	// Inject workload identity token
	if c.WorkloadIssuer != nil {
		appName, role := c.resolveAppAndRole(ctx, sb)
		token, tokenErr := c.WorkloadIssuer.IssueTokenWithOptions(appName, sb.ID.String(), workloadidentity.TokenOptions{Role: role})
		if tokenErr != nil {
			c.Log.Warn("failed to generate workload identity token", "sandbox", sb.ID, "error", tokenErr)
		} else {
			tokenPath := c.sandboxPath(sb, "identity-token")
			if writeErr := atomicWriteFile(tokenPath, []byte(token), 0644); writeErr != nil {
				c.Log.Warn("failed to write workload identity token", "sandbox", sb.ID, "error", writeErr)
			} else {
				mounts = append(mounts, specs.Mount{
					Destination: "/var/run/miren/identity-token",
					Type:        "bind",
					Source:      tokenPath,
					Options:     []string{"rbind", "ro"},
				})
				if c.tokenRefresher != nil {
					c.tokenRefresher.register(sb.ID.String(), tokenPath, appName, role)
				}
			}
		}

		// Mount the cluster CA so a sandbox dialing the API can verify its
		// certificate. Without it the only way to connect would be to skip
		// verification, which on a bridge shared with other sandboxes means
		// trusting whoever answers.
		if len(c.CACert) > 0 {
			caPath := c.sandboxPath(sb, "ca.crt")
			if writeErr := atomicWriteFile(caPath, c.CACert, 0644); writeErr != nil {
				c.Log.Warn("failed to write cluster CA for sandbox", "sandbox", sb.ID, "error", writeErr)
			} else {
				mounts = append(mounts, specs.Mount{
					Destination: "/var/run/miren/ca.crt",
					Type:        "bind",
					Source:      caPath,
					Options:     []string{"rbind", "ro"},
				})
			}
		}
	}

	// Materialize any secret the spec references. The spec carries references
	// rather than values precisely so nothing sensitive is at rest in the
	// entity store; the value comes into being here, in memory, and goes
	// straight into the container's environment below.
	//
	// A failure stops the container from starting. An app handed a reference
	// where its credential belongs does not fail in a way anyone can read — it
	// fails much later, inside whatever the credential was for.
	envVars, err := secret.MaterializeEnv(ctx, c.Secrets, co.Env)
	if err != nil {
		return nil, fmt.Errorf("sandbox %s container %s: %w", sb.ID, co.Name, err)
	}

	// Extract instance number from metadata labels and inject the instance env var
	var md core_v1alpha.Metadata
	md.Decode(meta)

	if instanceStr, ok := md.Labels.Get("instance"); ok {
		instanceEnv := appclient.RuntimeEnvWithAlias(appclient.EnvRuntimeInstanceNum, instanceStr)
		envVars = append(instanceEnv, envVars...)
		c.Log.Debug("injected instance number into container env", "sandbox_id", sb.ID, "container", co.Name, "instance", instanceStr)
	}

	if c.WorkloadIssuer != nil {
		envVars = append(envVars,
			"MIREN_IDENTITY_TOKEN_PATH=/var/run/miren/identity-token",
			fmt.Sprintf("MIREN_OIDC_ISSUER_URL=%s", c.WorkloadIssuer.IssuerURL()),
			fmt.Sprintf("MIREN_IDENTITY_TOKEN_URL=http://%s:%d/v1/token", c.Subnet.Router().Addr(), tokenServerPort),
		)

		// Point the client at the cluster API. MIREN_API_ADDRESS rather than
		// MIREN_SERVER_ADDRESS: the latter already means the address the server
		// binds to, so a miren CLI run inside the sandbox would read it as a
		// listen address.
		if c.ApiAddress != "" && len(c.CACert) > 0 {
			envVars = append(envVars,
				"MIREN_IN_CLUSTER=1",
				fmt.Sprintf("MIREN_API_ADDRESS=%s", c.ApiAddress),
				"MIREN_CA_CERT_PATH=/var/run/miren/ca.crt",
			)
		}
		if c.tokenSecrets != nil && len(ep.Addresses) > 0 {
			secret, secretErr := generateTokenSecret()
			if secretErr != nil {
				c.Log.Warn("failed to generate token request secret", "sandbox", sb.ID, "error", secretErr)
			} else {
				c.tokenSecrets.register(sb.ID.String(), secret)
				envVars = append(envVars, fmt.Sprintf("MIREN_IDENTITY_TOKEN_SECRET=%s", secret))

				// Persist the secret host-side so it can be re-registered after a
				// controller/token-server restart. Without this the running sandbox's
				// token requests 403 forever once the in-memory registry is lost.
				secretPath := c.sandboxPath(sb, tokenSecretFilename)
				if writeErr := writeTokenSecret(secretPath, secret); writeErr != nil {
					c.Log.Warn("failed to persist token request secret", "sandbox", sb.ID, "error", writeErr)
				}
			}
		}
	}

	imageConfigOpt := oci.WithImageConfig(img)
	if len(co.Args) > 0 {
		imageConfigOpt = oci.WithImageConfigArgs(img, co.Args)
	}

	specOpts := []oci.SpecOpts{
		imageConfigOpt,
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/net", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.IPCNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/ipc", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.UTSNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/uts", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.TimeNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/time", sbPid),
		}),
		oci.WithEnv(envVars),
	}

	cwd := co.Directory
	if cwd == "" {
		cwd = "/app"
	}
	specOpts = append(specOpts, oci.WithProcessCwd(cwd))

	if co.Command != "" {
		specOpts = append(specOpts, oci.WithProcessArgs("/bin/sh", "-c", co.Command))
	}

	if co.OomScore != 0 {
		specOpts = append(specOpts, containerdx.WithOOMScoreAdj(int(co.OomScore), false))
	}

	if co.Privileged {
		specOpts = append(specOpts,
			oci.WithPrivileged,
			oci.WithAllDevicesAllowed,
			oci.WithWriteableCgroupfs,
			oci.WithAddedCapabilities([]string{"CAP_SYS_ADMIN"}),
		)
	}

	if co.Tty {
		specOpts = append(specOpts, oci.WithTTY)
	}

	// Make miren disk mounts writable by the run user (MIR-1428). Appended
	// last so it observes the fully-resolved spec.Process.User from
	// WithImageConfig above (and any future user override).
	specOpts = append(specOpts, c.withDiskOwnership(diskMounts))

	lbls := map[string]string{}
	lbls[sandboxEntityLabel] = sb.ID.String()

	if sb.Spec.Version != "" {
		lbls[sandboxVerEntityLabel] = sb.Spec.Version.String()
	}

	if co.ShutdownTimeout != "" {
		lbls[shutdownTimeoutLabel] = co.ShutdownTimeout
	}

	snapshotId := fmt.Sprintf("%s-%s", containerPrefix(sb.ID), co.Name)

	opts = append(opts,
		containerd.WithNewSnapshot(snapshotId, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

// containerShutdownInfo tracks a container during graceful shutdown
type containerShutdownInfo struct {
	container containerd.Container
	task      containerd.Task
	timeout   time.Duration
	id        string
}

// getShutdownTimeout extracts shutdown timeout from container labels, falling back to default
func getShutdownTimeout(ctx context.Context, cont containerd.Container) time.Duration {
	labels, err := cont.Labels(ctx)
	if err != nil {
		return defaultShutdownTimeout
	}

	timeoutStr := labels[shutdownTimeoutLabel]
	if timeoutStr == "" {
		return defaultShutdownTimeout
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return defaultShutdownTimeout
	}

	return timeout
}

func (c *SandboxController) DestroySubContainers(ctx context.Context, id entity.Id) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// Discover subcontainers from containerd
	containerList, err := c.CC.Containers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	prefix := containerPrefix(id)
	var containers []containerShutdownInfo
	var maxTimeout time.Duration

	for _, cont := range containerList {
		containerID := cont.ID()
		if strings.HasPrefix(containerID, prefix+"-") {
			timeout := getShutdownTimeout(ctx, cont)
			if timeout > maxTimeout {
				maxTimeout = timeout
			}
			containers = append(containers, containerShutdownInfo{
				container: cont,
				id:        containerID,
				timeout:   timeout,
			})
		}
	}

	if len(containers) == 0 {
		c.Log.Debug("no subcontainers found to destroy", "id", id)
		return nil
	}

	// Set up timeout for the entire operation (max shutdown timeout + buffer)
	overallTimeout := maxTimeout + 30*time.Second
	ctx, cancel := context.WithTimeout(ctx, overallTimeout)
	defer cancel()

	c.Log.Info("starting graceful shutdown of subcontainers",
		"sandbox_id", id,
		"containers", len(containers),
		"max_shutdown_timeout", maxTimeout,
		"overall_timeout", overallTimeout)

	// Stop port monitoring for all containers
	for _, info := range containers {
		if c.portMonitor != nil {
			c.portMonitor.StopMonitoring(info.id)
		}
	}

	// Phase 1: Send SIGTERM to all containers and set up exit detection
	startTime := time.Now()

	// Channel for aggregated exit events
	type exitEvent struct {
		id     string
		status containerd.ExitStatus
	}
	exitedChan := make(chan exitEvent, len(containers))

	// Track containers waiting for shutdown
	tasksByID := make(map[string]*containerShutdownInfo)

	for i := range containers {
		info := &containers[i]
		task, err := info.container.Task(ctx, cleanupAttach())
		if err != nil {
			c.Log.Debug("no task found for container", "id", info.id)
			continue
		}
		info.task = task

		// Set up exit channel before sending SIGTERM
		exitCh, err := task.Wait(ctx)
		if err != nil {
			c.Log.Debug("failed to get wait channel, process may already be gone", "id", info.id, "err", err)
			continue
		}

		// Spawn goroutine to detect exit
		go func(id string, ch <-chan containerd.ExitStatus) {
			select {
			case status := <-ch:
				exitedChan <- exitEvent{id: id, status: status}
			case <-ctx.Done():
			}
		}(info.id, exitCh)

		if err := task.Kill(ctx, unix.SIGTERM); err != nil {
			c.Log.Debug("failed to send SIGTERM", "id", info.id, "err", err)
			continue
		}

		c.Log.Debug("sent SIGTERM to task", "id", info.id, "shutdown_timeout", info.timeout)
		tasksByID[info.id] = info
	}

	// Phase 2: Wait for graceful exit, respecting per-container timeouts
	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()

	for len(tasksByID) > 0 {
		select {
		case <-ctx.Done():
			c.Log.Warn("context cancelled during graceful shutdown", "sandbox_id", id, "remaining", len(tasksByID))
			goto forceKill

		case ev := <-exitedChan:
			info, ok := tasksByID[ev.id]
			if !ok {
				continue // Already processed
			}
			c.Log.Debug("task exited gracefully", "id", info.id, "exit_code", ev.status.ExitCode())
			if _, err := info.task.Delete(ctx); err != nil {
				c.Log.Debug("failed to delete exited task", "id", info.id, "err", err)
			}
			delete(tasksByID, ev.id)

		case <-ticker.C:
			// Check timeouts for remaining tasks
			for id, info := range tasksByID {
				if time.Since(startTime) >= info.timeout {
					c.Log.Info("shutdown timeout expired, force killing",
						"id", info.id,
						"elapsed", time.Since(startTime),
						"timeout", info.timeout)
					if err := info.task.Kill(ctx, unix.SIGKILL); err != nil {
						c.Log.Debug("failed to send SIGKILL", "id", info.id, "err", err)
					}
					if _, err := info.task.Delete(ctx, containerd.WithProcessKill); err != nil {
						c.Log.Debug("failed to delete task after SIGKILL", "id", info.id, "err", err)
					}
					delete(tasksByID, id)
				}
			}
		}
	}

	c.Log.Info("all tasks exited", "sandbox_id", id, "elapsed", time.Since(startTime))
	goto cleanup

forceKill:
	// Force kill any remaining tasks
	for _, info := range tasksByID {
		c.Log.Info("force killing task", "id", info.id)
		if err := info.task.Kill(ctx, unix.SIGKILL); err != nil {
			c.Log.Debug("failed to send SIGKILL", "id", info.id, "err", err)
		}
		if _, err := info.task.Delete(ctx, containerd.WithProcessKill); err != nil {
			c.Log.Debug("failed to delete task", "id", info.id, "err", err)
		}
	}

cleanup:
	// Delete all containers.
	// We must delete the task before the container — containerd requires this.
	// Tasks may still exist here if the process had already exited when we
	// tried Kill(SIGTERM), since those containers were skipped in the
	// graceful-shutdown loop above.
	for _, info := range containers {
		if info.task != nil {
			if _, err := info.task.Delete(ctx, containerd.WithProcessKill); err != nil {
				if !errdefs.IsNotFound(err) {
					c.Log.Debug("failed to delete task during cleanup", "id", info.id, "err", err)
				}
			}
		}
		if err := info.container.Delete(ctx, containerd.WithSnapshotCleanup); err != nil {
			if !errdefs.IsNotFound(err) {
				c.Log.Debug("failed to delete container", "id", info.id, "err", err)
			}
		} else {
			c.Log.Debug("deleted container", "id", info.id)
		}
	}

	c.Log.Info("subcontainer destruction complete", "sandbox_id", id, "elapsed", time.Since(startTime))
	return nil
}

func (c *SandboxController) Delete(ctx context.Context, id entity.Id, sb *compute.Sandbox) error {
	c.Log.Debug("delete callback received, cleaning up sandbox", "id", id)
	return c.StopSandbox(ctx, id, sb)
}

// entityFallbackIPs returns the addresses recorded on a sandbox entity, for use
// when the pause container's labels couldn't supply them.
//
// A DEAD sandbox yields nothing. DEAD is only reached by way of a teardown that
// already released the sandbox's addresses, and nothing ever clears Network on
// the entity, so those addresses are stale by construction. Releasing them again
// would be a no-op at best; once the netdb cooldown has passed and another
// sandbox has reserved one of them, ReleaseAddr updates the row by IP with no
// ownership check and hands a live address back to the pool.
//
// The delete callback is the path that makes this reachable, since it carries
// the prior entity and fires from the periodic sweep an hour after teardown.
func entityFallbackIPs(sb *compute.Sandbox) map[string]bool {
	ips := make(map[string]bool)
	if sb.Status == compute.DEAD {
		return ips
	}

	for _, net := range sb.Network {
		addr := net.Address
		if strings.Contains(addr, "/") {
			if prefix, err := netip.ParsePrefix(addr); err == nil {
				addr = prefix.Addr().String()
			}
		}
		if addr != "" {
			ips[addr] = true
		}
	}
	return ips
}

// StopSandbox tears down a sandbox and releases every resource it holds. sb is
// optional: callers that already have the entity pass it so cleanup still works
// once the entity has been deleted from the store, and it is fetched here
// otherwise.
func (c *SandboxController) StopSandbox(ctx context.Context, id entity.Id, sb *compute.Sandbox) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	c.Log.Debug("stopping sandbox", "id", id)

	// Drop any stdio fan-out for this sandbox. This is the one place allowed to
	// close a Hub's stdin: the container is going away, so there is no longer a
	// shell for an EOF to kill. Attached clients see their stream end, which is
	// what "the run finished" should look like from a terminal.
	c.hubs.RemoveAll(id)

	// Release in-memory token state first. Container teardown below can be slow or
	// fail partway, and a sandbox left registered keeps getting fresh tokens minted
	// for a file that is about to disappear.
	c.ReleaseTokenState(id)

	if c.NetServ != nil {
		// Drop the address claim here rather than leaving it to the watcher's delete
		// event, so a recycled address is free the moment the sandbox holding it goes.
		// Teardown is that moment, and it runs well ahead of the entity delete.
		c.NetServ.RemoveSandboxMapping(id.String())
	}

	// Best-effort removal of the persisted secret. The whole sandbox dir is wiped
	// further down, but removing the sensitive secret up front ensures it doesn't
	// linger if teardown errors out before reaching that cleanup.
	secretPath := filepath.Join(c.Tempdir, "containerd", id.PathSafe(), tokenSecretFilename)
	if err := os.Remove(secretPath); err != nil && !os.IsNotExist(err) {
		c.Log.Warn("failed to remove persisted token secret", "sandbox", id, "error", err)
	}

	// Get LogEntity from pause container labels for metrics cleanup
	var le string
	var sandboxIPs map[string]bool

	pauseID := pauseContainerId(id)
	container, err := c.CC.LoadContainer(ctx, pauseID)
	if err == nil {
		labels, err := container.Labels(ctx)
		if err != nil {
			c.Log.Warn("failed to get container labels", "id", id, "err", err)
		} else {
			// Read LogEntity from labels
			le = labels["runtime.computer/log-entity"]
			if le == "" {
				le = id.String()
			}

			// Collect IPs for cleanup
			sandboxIPs = make(map[string]bool)
			for l, v := range labels {
				if strings.HasPrefix(l, "runtime.computer/ip") {
					sandboxIPs[v] = true
				}
			}
		}
	} else if !errdefs.IsNotFound(err) {
		c.Log.Warn("failed to load pause container", "id", id, "err", err)
	}

	// Fetch the sandbox entity for firewall cleanup and as IP fallback, unless the
	// caller already handed us one. Delete callbacks fire after the entity is gone,
	// so their copy is the only one we will ever see.
	if sb == nil {
		resp, entityErr := c.EAC.Get(ctx, id.String())
		switch {
		case entityErr == nil:
			var fetched compute.Sandbox
			fetched.Decode(resp.Entity().Entity())
			sb = &fetched
		case !errors.Is(entityErr, cond.ErrNotFound{}):
			c.Log.Warn("failed to get sandbox entity for cleanup", "id", id, "err", entityErr)
		default:
			c.Log.Debug("sandbox entity already deleted and no sandbox supplied, skipping firewall cleanup", "id", id)
		}
	}

	if sb != nil {
		// Clean up iptables DNAT rules before destroying containers
		c.UnconfigureFirewall(sb)

		// Use the entity's IPs as fallback if container labels didn't have them
		if len(sandboxIPs) == 0 {
			sandboxIPs = entityFallbackIPs(sb)
			if len(sandboxIPs) > 0 {
				c.Log.Debug("retrieved IPs from entity for cleanup", "id", id, "ips", sandboxIPs)
			}
		}
	}

	// Fallback if we couldn't get LogEntity from labels
	if le == "" {
		le = id.String()
	}

	c.Log.Debug("removing monitoring metrics", "id", id, "log_entity", le)
	err = c.Metrics.Remove(le)
	if err != nil {
		c.Log.Error("failed to remove monitoring metrics", "id", id, "error", err)
	}

	// Release disk leases early so replacement sandboxes can acquire them
	// while we wait for container shutdown
	if err := c.ReleaseDiskLeases(ctx, id); err != nil {
		c.Log.Error("failed to release disk leases", "id", id, "error", err)
		// Continue with cleanup even if this fails
	}

	// Destroy subcontainers - this will discover them from containerd
	c.Log.Debug("destroying subcontainers", "id", id)
	err = c.DestroySubContainers(ctx, id)
	if err != nil {
		c.Log.Error("failed to destroy subcontainers", "id", id, "err", err)
		// Continue with cleanup even if this fails
	}

	// Stop replicating sqlite disks only once the containers are gone. Doing it
	// alongside the lease release above would end replication while the app is
	// still shutting down gracefully, so anything it wrote in that window —
	// which is as long as the shutdown timeout — would never reach the
	// coordinator. Registrations are refcounted by sandbox, so a replacement
	// starting meanwhile simply becomes another owner rather than conflicting.
	c.releaseSqliteDisks(ctx, id)

	// Delete pause container
	c.Log.Debug("deleting pause container", "id", id)
	if container != nil {
		task, err := container.Task(ctx, cleanupAttach())
		if err != nil {
			if !errdefs.IsNotFound(err) {
				c.Log.Error("failed to get pause task", "id", id, "err", err)
			} else {
				c.Log.Debug("pause task not found, continuing with container deletion", "id", id)
			}
		} else if task != nil {
			_, err = task.Delete(ctx, containerd.WithProcessKill)
			if err != nil {
				c.Log.Error("failed to delete pause task", "id", id, "err", err)
			}
		}

		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			c.Log.Error("failed to delete pause container", "id", id, "err", err)
		}

		c.Log.Info("container stopped", "id", id)
	}

	// Release IPs — runs even if pause container was already gone,
	// since sandboxIPs may have been recovered from the entity store.
	for ipStr := range sandboxIPs {
		addr, err := netip.ParseAddr(ipStr)
		if err == nil {
			err = c.Subnet.ReleaseAddr(addr)
			if err != nil {
				c.Log.Error("failed to release IP", "addr", addr, "err", err)
			} else {
				c.Log.Debug("released IP", "addr", addr)
			}
		} else {
			c.Log.Error("failed to parse IP", "addr", ipStr, "err", err)
		}
	}

	// Clean up temp directory
	tmpDir := filepath.Join(c.Tempdir, "containerd", id.PathSafe())
	_ = os.RemoveAll(tmpDir)

	// Mark sandbox as DEAD in entity store
	result, err := c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{
			Status: compute.DEAD,
		}).Encode,
	).Attrs(), 0)
	if err != nil {
		// We ignore if the entity is not found as we run this code path when detecting
		// the sandbox entity has already been deleted.
		if !errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Error("failed to mark sandbox as DEAD", "id", id, "error", err)
		}
	} else if c.writeTracker != nil && result.HasRevision() {
		c.writeTracker.RecordWrite(result.Revision())
	}

	c.Log.Info("sandbox retired", "id", id, "status", compute.DEAD)

	// Clean up endpoints associated with this sandbox
	err = c.deleteEndpoints(ctx, id, sandboxIPs)
	if err != nil {
		c.Log.Error("failed to delete endpoints for sandbox", "id", id, "error", err)
	}

	return nil
}

// reregisterSqliteDisks restores replication for a sandbox that outlived the
// runner process. It mirrors what configureSqliteVolume did when the sandbox
// first started, minus creating anything: the directory and database are
// already there.
//
// Failures are logged rather than returned. A sandbox whose database cannot be
// re-registered is still running and still serving; losing its backups is bad,
// but marking it unhealthy over it would be worse.
func (c *SandboxController) reregisterSqliteDisks(ctx context.Context, sb *compute.Sandbox) {
	if c.SqliteDisks == nil {
		// Returning quietly would strand a sandbox that is still serving: it
		// outlived the runner and is writing to a database this process has no
		// way to replicate. Only say so when the sandbox actually has one,
		// otherwise every restart logs for every sandbox on the node.
		for _, volume := range sb.Spec.Volume {
			if volume.Provider == "sqlite" {
				c.Log.Error("sqlite disk left unreplicated after restart; backups are off for this runner",
					"sandbox", sb.ID, "volume", volume.Name)
			}
		}
		return
	}

	for _, volume := range sb.Spec.Volume {
		if volume.Provider != "sqlite" {
			continue
		}

		dir, dbFile, sqliteId, appKey, err := c.sqliteVolumeLayout(ctx, sb, volume)
		if err != nil {
			c.Log.Error("failed to resolve sqlite volume during boot reconciliation",
				"sandbox", sb.ID, "volume", volume.Name, "error", err)
			continue
		}

		key := sqlitedisk.BackupKey(appKey, sqliteId)
		if err := c.SqliteDisks.Register(ctx, sb.ID.String(), key, filepath.Join(dir, dbFile)); err != nil {
			c.Log.Error("failed to resume replicating sqlite disk after restart",
				"sandbox", sb.ID, "volume", volume.Name, "error", err)
			continue
		}

		c.Log.Info("resumed replicating sqlite disk after restart",
			"sandbox", sb.ID, "volume", volume.Name, "key", key)
	}
}

// releaseSqliteDisks stops replicating the sandbox's sqlite-provider disks,
// flushing a final sync so the last transactions reach the coordinator before
// the disk is handed to a replacement sandbox.
//
// Keys are looked up by sandbox id rather than re-derived from the spec: this
// runs during cleanup, by which point the app version entity the key was built
// from may already be deleted. Failures are logged rather than returned, since
// giving up here would strand the rest of the teardown.
func (c *SandboxController) releaseSqliteDisks(ctx context.Context, id entity.Id) {
	if c.SqliteDisks == nil {
		return
	}

	if err := c.SqliteDisks.DeregisterOwner(ctx, id.String()); err != nil {
		c.Log.Error("failed to stop replicating sqlite disks", "sandbox", id, "error", err)
	}
}

// ReleaseDiskLeases releases all disk leases owned by the given sandbox.
// This transitions leases to RELEASED status, which triggers the disk lease
// controller to unmount the volumes and release the underlying resources.
func (c *SandboxController) ReleaseDiskLeases(ctx context.Context, sandboxID entity.Id) error {
	// Find all disk leases owned by this sandbox
	listResp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, storage.KindDiskLease))
	if err != nil {
		return fmt.Errorf("failed to list disk leases: %w", err)
	}

	for _, e := range listResp.Values() {
		var lease storage.DiskLease
		lease.Decode(e.Entity())

		if lease.SandboxId != sandboxID {
			continue
		}

		// Skip if already released
		if lease.Status == storage.RELEASED {
			continue
		}

		c.Log.Info("releasing disk lease",
			"lease", lease.ID,
			"disk", lease.DiskId,
			"sandbox", sandboxID)

		_, err := c.EAC.Patch(ctx, entity.New(
			entity.DBId, lease.ID,
			(&storage.DiskLease{
				Status: storage.RELEASED,
			}).Encode,
		).Attrs(), 0)
		if err != nil {
			c.Log.Error("failed to release disk lease",
				"lease", lease.ID,
				"error", err)
			// Continue releasing other leases
			continue
		}

		c.Log.Info("disk lease released",
			"lease", lease.ID,
			"disk", lease.DiskId)
	}

	return nil
}

// Periodic cleans up dead sandboxes that are older than the specified time horizon
func (c *SandboxController) Periodic(ctx context.Context, timeHorizon time.Duration) error {
	c.Log.Info("running periodic cleanup of dead sandboxes", "time_horizon", timeHorizon)

	// List sandboxes scheduled to this node only so we don't delete
	// sandbox entities that belong to other runners.
	resp, err := c.EAC.List(ctx, compute.Index(compute.KindSandbox, c.NodeId.Id()))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	now := time.Now()
	cutoffTime := now.Add(-timeHorizon)

	var deleted int
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Check if sandbox is DEAD and UpdatedAt is older than time horizon
		if sb.Status == compute.DEAD {
			updatedAt := e.Entity().GetUpdatedAt()

			if updatedAt.Before(cutoffTime) {
				c.Log.Info("deleting old dead sandbox",
					"id", sb.ID,
					"updated_at", updatedAt.Format(time.RFC3339),
					"age", now.Sub(updatedAt).String())

				if err := c.ReleaseDiskLeases(ctx, sb.ID); err != nil {
					c.Log.Error("failed to release disk leases during periodic cleanup", "id", sb.ID, "error", err)
					continue
				}

				// Catches sqlite disks belonging to sandboxes that died without
				// a graceful stop.
				c.releaseSqliteDisks(ctx, sb.ID)

				_, err := c.EAC.Delete(ctx, sb.ID.String())
				if err != nil {
					c.Log.Error("failed to delete dead sandbox", "id", sb.ID, "error", err)
					continue
				}
				deleted++
			}
		}
	}

	if deleted > 0 {
		c.Log.Info("periodic cleanup completed", "deleted_sandboxes", deleted)
	}

	return nil
}

// registerSandboxDNS publishes the sandbox's address to the DNS servers from the side
// that actually knows it: this controller allocated the address, so it can register the
// mapping before the container starts instead of waiting for the entity watcher to
// notice. The watcher still reconciles, but it is no longer the only writer.
//
// This matters beyond DNS. The workload identity token server resolves a caller's
// identity by looking its source address up in the same map, so a recycled address that
// still named its previous owner meant the new sandbox could never present a secret that
// matched — a permanent 403 for anything deployed onto that address (MIR-1511).
func (c *SandboxController) registerSandboxDNS(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, meta *entity.Meta) {
	if c.NetServ == nil || ep == nil || len(ep.Addresses) == 0 || meta == nil {
		return
	}

	var md core_v1alpha.Metadata
	md.Decode(meta)

	// DNS entries are keyed by app+service; a sandbox without a service label has no
	// name to answer to, which matches what the watcher does with one.
	service, _ := md.Labels.Get("service")
	if service == "" {
		return
	}

	appName := c.resolveAppName(ctx, sb)
	if appName == "" {
		c.Log.Warn("could not resolve app name for sandbox DNS mapping", "sandbox", sb.ID)
		return
	}

	ip := ep.Addresses[0].Addr().String()
	c.NetServ.AddSandboxMapping(sb.ID.String(), ip, appName, service)
	c.Log.Debug("registered sandbox DNS mapping", "sandbox", sb.ID, "ip", ip, "app", appName, "service", service)
}

func (c *SandboxController) resolveAppName(ctx context.Context, sb *compute.Sandbox) string {
	name, _ := c.resolveAppAndRole(ctx, sb)
	return name
}

// resolveAppAndRole derives both the app name and the workload role for a
// sandbox from the entity graph. The role is a property of the app, resolved
// here so it is always server-derived and never taken from the workload. An
// empty role means the default (applied by the issuer at mint time).
func (c *SandboxController) resolveAppAndRole(ctx context.Context, sb *compute.Sandbox) (name, role string) {
	// An empty result means the minted token carries no app, which the validator
	// rejects — so the workload silently loses cluster-API access. The initial
	// no-version case is a normal shape for a sandbox not backed by an app
	// version; the rest are genuine failures, logged so an operator can see why
	// a workload's identity stopped working rather than debugging a token that
	// simply won't authenticate.
	if sb.Spec.Version == "" {
		return "", ""
	}

	versionResp, err := c.EAC.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		c.Log.Warn("workload identity: failed to load app version, sandbox will get no app/role",
			"sandbox", sb.ID, "version", sb.Spec.Version, "error", err)
		return "", ""
	}

	var version core_v1alpha.AppVersion
	version.Decode(versionResp.Entity().Entity())

	if version.App == "" {
		c.Log.Warn("workload identity: app version has no app ref, sandbox will get no app/role",
			"sandbox", sb.ID, "version", sb.Spec.Version)
		return "", ""
	}

	appResp, err := c.EAC.Get(ctx, version.App.String())
	if err != nil {
		c.Log.Warn("workload identity: failed to load app, sandbox will get no app/role",
			"sandbox", sb.ID, "app", version.App, "error", err)
		return "", ""
	}

	appEnt := appResp.Entity().Entity()

	var appMeta core_v1alpha.Metadata
	appMeta.Decode(appEnt)

	var app core_v1alpha.App
	app.Decode(appEnt)

	return appMeta.Name, app.WorkloadRole
}
