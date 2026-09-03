package coordinate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/clientconfig"
	deploymentattemptsctrl "miren.dev/runtime/controllers/deploymentattempts"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/entity"
	entityexport "miren.dev/runtime/pkg/entity/export"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/entityserver"
	"miren.dev/runtime/servers/runnertelemetry"
)

// NewFoundation constructs the cluster authority and API foundation shared by
// the control plane and local runner-facing boot components.
func NewFoundation(log *slog.Logger, cfg CoordinatorConfig) *Foundation {
	return &Foundation{
		CoordinatorConfig: cfg,
		Log:               log.With("module", "coordinator"),
	}
}

// Foundation owns the cluster authority, RPC state, and entity API. It is a
// real early readiness boundary: consumers can use it without access to the
// later control-plane controllers and handlers.
type Foundation struct {
	CoordinatorConfig

	Log *slog.Logger

	state      *rpc.State
	etcdClient *clientv3.Client
	eac        *esv1.EntityAccessClient // Entity access client for querying entities
	store      entity.Store
	etcdStore  *entity.EtcdStore

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte

	authClient        *cloudauth.AuthClient // For status reporting to cloud
	oidcAuthenticator *oidcauth.OIDCAuthenticator

	netcheckMu        sync.RWMutex
	netcheckResult    *cloudauth.NetcheckDualStackResult
	netcheckCheckedAt time.Time

	logAddressesOnce sync.Once
}

// NewDeploymentAttemptController returns the migration controller after the
// coordinator has opened its entity store. The caller owns its lifecycle.
func (c *Foundation) NewDeploymentAttemptController() (*deploymentattemptsctrl.Controller, error) {
	if c.store == nil || c.eac == nil {
		return nil, errors.New("cluster foundation entity store is not ready")
	}
	return deploymentattemptsctrl.New(c.Log, c.store, c.eac), nil
}

// BackfillCloudExportMarker is the contract-wide safety net for exported kinds
// that no specialized migration covers. The deployment-attempt controller has
// already marked the apps, app versions, and deployments in its clean sweep;
// excluding deployments here preserves its ownership of validating old shapes.
func (c *Foundation) BackfillCloudExportMarker(ctx context.Context) error {
	if c.store == nil {
		return errors.New("cluster foundation entity store is not ready")
	}
	_, err := entityexport.BackfillMarker(
		ctx, c.Log, c.store, core_v1alpha.CloudExportContract, 0,
		entityexport.ExcludingKinds(core_v1alpha.KindDeployment),
	)
	return err
}

// Stop drains RPC after all dependent components stop, then releases etcd.
func (c *Foundation) Stop(ctx context.Context) error {
	var errs []error
	if c.state != nil {
		errs = append(errs, c.state.Shutdown(ctx))
	}
	if c.etcdClient != nil {
		errs = append(errs, c.etcdClient.Close())
	}
	c.state = nil
	c.etcdClient = nil
	c.store = nil
	c.etcdStore = nil
	c.eac = nil
	return errors.Join(errs...)
}

const (
	day  = 24 * time.Hour
	year = 365 * day
)

func validateAPICertificate(cert *x509.Certificate, expectedNames []string, expectedIPs []net.IP) error {
	horizon := time.Now().Add(48 * time.Hour)
	if cert.NotAfter.Before(horizon) {
		return fmt.Errorf("certificate expired on %v (horizon: %v)", cert.NotAfter, horizon)
	}

	if !slices.Equal(cert.DNSNames, expectedNames) {
		return fmt.Errorf("certificate DNS names %v do not match expected %v", cert.DNSNames, expectedNames)
	}

	if !slices.EqualFunc(cert.IPAddresses, expectedIPs, func(a, b net.IP) bool {
		return a.Equal(b)
	}) {
		return fmt.Errorf("certificate IP addresses %v do not match expected %v", cert.IPAddresses, expectedIPs)
	}

	return nil
}

// EnsureCA makes sure the server CA certificate and key exist on disk under
// <dataPath>/server, creating them if they're missing, and returns the loaded
// authority. It's safe to call before the coordinator starts.
//
// This is what lets the server bootstrap its own etcd mTLS: SetupEtcdTLS reads
// the CA from disk and requires it to already exist, so the CA must be
// materialized before that runs. Historically the only create-if-missing path
// was Coordinator.LoadCA, which doesn't run until Coordinator.Start, well after
// the etcd TLS block, so a fresh install with distributed runners enabled had
// no CA yet. Calling EnsureCA early closes that gap.
func EnsureCA(log *slog.Logger, dataPath string) (*caauth.Authority, error) {
	cert := filepath.Join(dataPath, "server", "ca.crt")
	keyPath := filepath.Join(dataPath, "server", "ca.key")

	data, err := os.ReadFile(cert)
	switch {
	case err == nil:
		log.Info("loading existing CA", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("missing key for CA: %w", err)
		}

		ca, err := caauth.LoadFromPEM(data, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA: %w", err)
		}

		return ca, nil
	case !errors.Is(err, os.ErrNotExist):
		// The CA cert exists but couldn't be read (permissions, transient IO,
		// etc). Bail rather than fall through and regenerate, which would
		// overwrite a valid CA and invalidate every cert it ever issued.
		return nil, fmt.Errorf("failed to read CA cert at %s: %w", cert, err)
	}

	log.Info("generating new CA", "path", cert)

	ca, err := caauth.New(caauth.Options{
		CommonName:   "miren-server",
		Organization: "miren",
		ValidFor:     10 * year,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cert), 0755); err != nil {
		return nil, fmt.Errorf("failed to create CA directory: %w", err)
	}

	cd, kd, err := ca.ExportPEM()
	if err != nil {
		return nil, fmt.Errorf("failed to export CA: %w", err)
	}

	if err := os.WriteFile(cert, cd, 0644); err != nil {
		return nil, fmt.Errorf("failed to write CA cert: %w", err)
	}

	if err := os.WriteFile(keyPath, kd, 0600); err != nil {
		return nil, fmt.Errorf("failed to write CA key: %w", err)
	}

	return ca, nil
}

func (c *Foundation) LoadCA(ctx context.Context) error {
	ca, err := EnsureCA(c.Log, c.DataPath)
	if err != nil {
		return err
	}

	c.authority = ca
	return nil
}

// APIServerName is the stable name in-cluster clients verify the API
// certificate against.
//
// A sandbox reaches the API through its bridge router address, which cannot be
// a certificate SAN: the subnet is leased after the certificate has been
// issued, and on a first boot there is no prior lease to anticipate. Clients
// therefore dial by address and pass this as the TLS server name. Nothing
// resolves it — no DNS record backs it, and none is needed — but it lets a
// sandbox verify the certificate against the cluster CA rather than skipping
// verification, which matters on a bridge shared with other sandboxes.
const APIServerName = "api.miren"

func (c *Foundation) LoadAPICert(ctx context.Context) error {
	names := []string{
		"localhost",
		APIServerName,
	}

	names = append(names, c.AdditionalNames...)

	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}

	ips = append(ips, c.IPs.RawIPs()...)

	cert := filepath.Join(c.DataPath, "server", "api.crt")
	keyPath := filepath.Join(c.DataPath, "server", "api.key")

	if data, err := os.ReadFile(cert); err == nil {
		c.Log.Info("loading existing API cert", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for API cert: %w", err)
		}

		x509Cert, err := caauth.LoadCertificate(data)
		if err == nil {
			if err := validateAPICertificate(x509Cert, names, ips); err != nil {
				c.Log.Info("API cert validation failed", "error", err)
				goto regen
			}

			c.apiCert = data
			c.apiKey = key
			return nil
		}
	}

regen:

	c.Log.Info("generating new API cert", "path", cert)

	cc, err := c.authority.IssueCertificate(caauth.Options{
		CommonName:   "miren-api",
		Organization: "miren",
		ValidFor:     1 * year,
		IPs:          ips,
		DNSNames:     names,
	})
	if err != nil {
		return fmt.Errorf("failed to generate API cert: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(cert), 0755)
	if err != nil {
		return fmt.Errorf("failed to create API directory: %w", err)
	}

	err = os.WriteFile(cert, cc.CertPEM, 0644)
	if err != nil {
		return fmt.Errorf("failed to write API cert: %w", err)
	}

	err = os.WriteFile(keyPath, cc.KeyPEM, 0600)
	if err != nil {
		return fmt.Errorf("failed to write API key: %w", err)
	}

	c.apiCert = cc.CertPEM
	c.apiKey = cc.KeyPEM

	return nil
}

// runnerTelemetryOptions mounts the ingest endpoints distributed runners ship
// their metrics and logs to, so VictoriaMetrics and VictoriaLogs never need to
// listen anywhere a runner can reach.
//
// Nothing is mounted without a workload issuer, since the issuer is what
// verifies the runner's token; a route that could not check its caller would be
// exactly the unauthenticated opening this replaces. The same goes for a
// backend whose address we do not know.
func (c *Foundation) runnerTelemetryOptions() []rpc.StateOption {
	if c.WorkloadIssuer == nil {
		c.Log.Warn("no workload identity issuer; runner telemetry ingest disabled")
		return nil
	}

	var opts []rpc.StateOption

	if c.VictoriametricsAddress != "" {
		opts = append(opts, rpc.WithHTTPHandler(runnertelemetry.MetricsPattern,
			runnertelemetry.NewMetricsHandler(c.Log, c.WorkloadIssuer, c.VictoriametricsAddress)))
	} else {
		c.Log.Warn("no victoriametrics address; runner metrics ingest disabled")
	}

	if c.VictorialogsAddress != "" {
		opts = append(opts, rpc.WithHTTPHandler(runnertelemetry.LogsPattern,
			runnertelemetry.NewLogsHandler(c.Log, c.WorkloadIssuer, c.VictorialogsAddress)))
	} else {
		c.Log.Warn("no victorialogs address; runner log ingest disabled")
	}

	return opts
}

// buildEtcdTLSConfig creates a tls.Config from the EtcdTLS configuration.
func (c *Foundation) buildEtcdTLSConfig() (*tls.Config, error) {
	if c.EtcdTLS == nil {
		return nil, fmt.Errorf("etcd TLS config not set")
	}

	// Load client certificate
	cert, err := tls.X509KeyPair(c.EtcdTLS.CertPEM, c.EtcdTLS.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load etcd client certificate: %w", err)
	}

	// Create CA cert pool
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(c.EtcdTLS.CACert) {
		return nil, fmt.Errorf("failed to parse etcd CA certificate")
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}, nil
}

func (c *Foundation) LocalConfig() (*clientconfig.Config, error) {
	return c.NamedConfig("miren-user")
}

func (c *Foundation) ServiceConfig() (*clientconfig.Config, error) {
	return c.NamedConfig("miren-services")
}

func (c *Foundation) NamedConfig(name string) (*clientconfig.Config, error) {
	cc, err := c.authority.IssueCertificate(caauth.Options{
		CommonName:   name,
		Organization: "miren",
		ValidFor:     1 * year,
	})

	if err != nil {
		return nil, err
	}

	return clientconfig.Local(cc, c.Address), nil
}

// RunnerConfig returns a client config for a runner service with proper TLS certificate SANs.
// The certificate will be valid for localhost and the runner's listen address.
func (c *Foundation) RunnerConfig(listenAddress string) (*clientconfig.Config, error) {
	// Build list of IPs and DNS names for the certificate
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}
	names := []string{"localhost"}

	// Parse the listen address to extract host/IP
	if listenAddress != "" {
		host, _, err := net.SplitHostPort(listenAddress)
		if err == nil && host != "" {
			// Check if host is an IP address
			if ip := net.ParseIP(host); ip != nil {
				// Add to IPs if not already present
				found := false
				for _, existing := range ips {
					if existing.Equal(ip) {
						found = true
						break
					}
				}
				if !found {
					ips = append(ips, ip)
				}
			} else {
				// It's a hostname, add to DNS names if not already present
				if host != "localhost" {
					names = append(names, host)
				}
			}
		}
	}

	cc, err := c.authority.IssueCertificate(caauth.Options{
		CommonName:   "miren-runner",
		Organization: "miren",
		ValidFor:     1 * year,
		IPs:          ips,
		DNSNames:     names,
	})
	if err != nil {
		return nil, err
	}

	return clientconfig.Local(cc, c.Address), nil
}

func (c *Foundation) IssueCertificate(name string) (*caauth.ClientCertificate, error) {
	if c.authority == nil {
		return nil, fmt.Errorf("CA authority not initialized")
	}

	return c.authority.IssueCertificate(caauth.Options{
		CommonName:   name,
		Organization: "miren",
		ValidFor:     1 * year,
	})
}

func (c *Foundation) ListenAddress() string {
	return c.state.ListenAddr()
}

// CACertificate returns the cluster CA in PEM form, or nil before LoadCA has
// run. Callers hand it to clients that must verify the cluster's certificates —
// sandboxes reaching the API with a workload identity token, for one.
func (c *Foundation) CACertificate() []byte {
	if c.authority == nil {
		return nil
	}
	return c.authority.GetCACertificate()
}

// workloadAuthenticator returns the authenticator for workload identity tokens
// presented by code running inside a sandbox, or nil when no issuer was wired in
// — issuer construction failed, or a caller left WorkloadIssuer unset. (Startup
// now falls back to a cluster-local issuer URL, so a plain unregistered cluster
// still gets one.) A nil link is skipped when the chain is built, leaving
// authentication exactly as it was before workload identity.
//
// Verification is coordinator-only: this needs the signing key, which only the
// coordinator holds. Sandboxes on a distributed runner dial the coordinator's
// API, so their tokens are verified here too.
func (c *Foundation) workloadAuthenticator() rpc.Authenticator {
	if c.WorkloadIssuer == nil {
		c.Log.Info("workload identity authentication disabled (no issuer configured)")
		return nil
	}

	c.Log.Info("workload identity authentication enabled",
		"issuer", c.WorkloadIssuer.IssuerURL())
	return workloadidentity.NewAuthenticator(c.WorkloadIssuer, c.Log)
}

// Start prepares the cluster CA, RPC state, entity store, and entity API.
func (c *Foundation) Start(ctx context.Context) (retErr error) {
	c.Log.Info("starting cluster foundation", "address", c.Address, "etcd_endpoints", c.EtcdEndpoints, "prefix", c.Prefix)

	err := c.LoadCA(ctx)
	if err != nil {
		c.Log.Error("failed to load CA", "error", err)
		return err
	}

	err = c.LoadAPICert(ctx)
	if err != nil {
		c.Log.Error("failed to load API cert", "error", err)
		return err
	}

	// Prepare RPC options
	rpcOpts := []rpc.StateOption{
		rpc.WithCertPEMs(c.apiCert, c.apiKey),
		rpc.WithCertificateVerification(c.authority.GetCACertificate()),
		rpc.WithBindAddr(c.Address),
		// Same address, other protocol: QUIC owns udp/<port> and the REST
		// gateway owns tcp/<port>, so an ordinary HTTP client reaches the API
		// on the port the cluster already advertises.
		rpc.WithRESTBindAddr(c.Address),
		rpc.WithLogger(c.Log),
	}

	// Add cloud authenticator if enabled
	if c.CloudAuth.Enabled {
		// Private key is required for cloud authentication
		if c.CloudAuth.PrivateKey == "" {
			c.Log.Error("private key is required when cloud authentication is enabled")
			return fmt.Errorf("cloud_auth.private_key is required when cloud authentication is enabled")
		}

		authConfig := cloudauth.Config{
			CloudURL: c.CloudAuth.CloudURL, // cloudauth will use default if empty
			Logger:   c.Log,
		}

		// Pass through tags from registration for RBAC evaluation
		if c.CloudAuth.Tags != nil {
			// Convert map[string]string to map[string]any
			tags := make(map[string]any)
			for k, v := range c.CloudAuth.Tags {
				tags[k] = v
			}
			authConfig.Tags = tags
		}

		var keyData []byte

		if strings.HasPrefix(c.CloudAuth.PrivateKey, "-----BEGIN PRIVATE KEY----") {
			keyData = []byte(c.CloudAuth.PrivateKey)
		} else {
			// Load the private key and create an AuthClient for the runtime
			keyData, err = os.ReadFile(c.CloudAuth.PrivateKey)
			if err != nil {
				c.Log.Error("failed to load service account private key", "error", err, "path", c.CloudAuth.PrivateKey)
				return fmt.Errorf("failed to load service account private key: %w", err)
			}
		}

		keyPair, err := cloudauth.LoadKeyPairFromPEM(string(keyData))
		if err != nil {
			c.Log.Error("failed to parse service account private key", "error", err)
			return fmt.Errorf("failed to parse service account private key: %w", err)
		}

		// Use CloudURL or default when creating auth client
		authCloudURL := c.CloudAuth.CloudURL
		if authCloudURL == "" {
			authCloudURL = cloudauth.DefaultCloudURL
		}

		authClient, err := cloudauth.NewAuthClient(authCloudURL, keyPair)
		if err != nil {
			c.Log.Error("failed to create auth client", "error", err)
			return fmt.Errorf("failed to create auth client: %w", err)
		}

		authConfig.AuthClient = authClient
		c.authClient = authClient // Store for status reporting
		c.Log.Info("service account authentication configured",
			"fingerprint", keyPair.Fingerprint())

		authenticator, err := cloudauth.NewRPCAuthenticator(ctx, authConfig)
		if err != nil {
			c.Log.Error("failed to create cloud authenticator", "error", err)
			return err
		}

		// Create OIDC authenticator and wrap with composite auth.
		// EAC is set later after entity store initialization.
		c.oidcAuthenticator = oidcauth.NewOIDCAuthenticator(c.Log)
		compositeAuth := oidcauth.NewCompositeAuthenticatorChain(authenticator, c.workloadAuthenticator(), c.oidcAuthenticator)
		compositeAuthz := oidcauth.NewCompositeAuthorizer(authenticator)

		rpcOpts = append(rpcOpts, rpc.WithAuthenticator(compositeAuth), rpc.WithAuthorizer(compositeAuthz))
		c.Log.Info("cloud authentication enabled with OIDC support",
			"cloud_url", authCloudURL)
	} else if c.NoAuth {
		// Use NoOpAuthenticator when explicitly disabled (for testing). Every
		// caller is anonymous and no authorizer is installed, so workload
		// identity has nothing to add here.
		rpcOpts = append(rpcOpts, rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
		c.Log.Warn("authentication disabled (NoOpAuthenticator)")
	} else {
		c.oidcAuthenticator = oidcauth.NewOIDCAuthenticator(c.Log)
		compositeAuth := oidcauth.NewCompositeAuthenticatorChain(&rpc.LocalOnlyAuthenticator{}, c.workloadAuthenticator(), c.oidcAuthenticator)
		compositeAuthz := oidcauth.NewCompositeAuthorizer(nil)
		rpcOpts = append(rpcOpts, rpc.WithAuthenticator(compositeAuth), rpc.WithAuthorizer(compositeAuthz))
		c.Log.Info("local-only authentication enabled with OIDC support")
	}

	rpcOpts = append(rpcOpts, c.runnerTelemetryOptions()...)

	// The boot graph cancels ctx before it enters reverse dependency order.
	// Keep RPC alive across that cancellation so dependents can make their final
	// coordinator calls before the foundation's stop hook drains it.
	rs, err := rpc.NewState(context.WithoutCancel(ctx), rpcOpts...)
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}
	c.state = rs
	defer func() {
		if retErr == nil {
			return
		}
		_ = c.state.Close()
		c.state = nil
	}()

	server := rs.Server()

	// Build etcd client config
	etcdConfig := clientv3.Config{
		Endpoints:        c.EtcdEndpoints,
		AutoSyncInterval: time.Minute,
	}

	// Add TLS config if configured
	if c.EtcdTLS != nil {
		tlsConfig, err := c.buildEtcdTLSConfig()
		if err != nil {
			c.Log.Error("failed to build etcd TLS config", "error", err)
			return err
		}
		etcdConfig.TLS = tlsConfig
		c.Log.Info("etcd client using mTLS")
	}

	client, err := clientv3.New(etcdConfig)
	if err != nil {
		c.Log.Error("failed to create etcd client", "error", err)
		return err
	}
	c.etcdClient = client
	defer func() {
		if retErr == nil {
			return
		}
		_ = client.Close()
		c.etcdClient = nil
	}()

	etcdStore, err := entity.NewEtcdStore(ctx, c.Log, client, c.Prefix)
	if err != nil {
		c.Log.Error("failed to create etcd store", "error", err)
		return err
	}
	c.store = etcdStore
	c.etcdStore = etcdStore

	err = schema.Apply(ctx, etcdStore)
	if err != nil {
		c.Log.Error("failed to apply schema", "error", err)
		return err
	}

	// Best-effort startup maintenance (format migration and short-id backfill)
	// scans the entity store. Bound it with a timeout so that a large or
	// not-recently-compacted store fails fast and lets startup continue, rather
	// than blocking the coordinator — and therefore the edge listener — on an
	// unbounded read. Each step below already treats failure as non-fatal, so a
	// timeout simply defers the work to a later startup once compaction catches up.
	//
	// Schema reindexing used to run here too and was the step most likely to
	// exhaust this budget. It now runs in the background (see the schema reindex
	// controller below), where it can checkpoint and resume instead of losing a
	// partial pass every time the deadline expired.
	const startupMaintenanceTimeout = 2 * time.Minute
	maintCtx, cancelMaint := context.WithTimeout(ctx, startupMaintenanceTimeout)
	defer cancelMaint()

	// Migrate entities from old format to new attribute-based format
	migrated, skipped, err := entity.MigrateEntityStore(maintCtx, c.Log, client, entity.MigrateOptions{
		Prefix: c.Prefix,
		DryRun: false,
	})
	if err != nil {
		c.Log.Warn("entity migration completed with errors", "migrated", migrated, "skipped", skipped, "error", err)
	} else if migrated > 0 {
		c.Log.Info("entity migration completed", "migrated", migrated, "skipped", skipped)
	}

	// Backfill short-ids for entities that don't have one
	sidMigrated, sidSkipped, sidErr := entity.MigrateShortIds(maintCtx, c.Log, client, entity.MigrateShortIdOptions{
		Prefix: c.Prefix,
		DryRun: false,
	})
	if sidErr != nil {
		c.Log.Warn("short-id migration completed with errors", "migrated", sidMigrated, "skipped", sidSkipped, "error", sidErr)
	} else if sidMigrated > 0 {
		c.Log.Info("short-id migration completed", "migrated", sidMigrated, "skipped", sidSkipped)
	}

	ess, err := entityserver.NewEntityServer(c.Log, etcdStore)
	if err != nil {
		c.Log.Error("failed to create entity server", "error", err)
		return err
	}

	server.ExposeValue("entities", esv1.AdaptEntityAccess(ess))

	loopback, err := rs.Connect(rs.LoopbackAddr(), "entities")
	if err != nil {
		c.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := esv1.NewEntityAccessClient(loopback)
	c.eac = eac // Store for use in status reporting and other methods

	// Set EAC on OIDC authenticator now that entity store is ready
	if c.oidcAuthenticator != nil {
		c.oidcAuthenticator.SetEAC(eac)
	}

	ec := aes.NewClient(c.Log, eac)

	defaultProject := &core_v1alpha.Project{
		ID:    entity.Id("default"),
		Owner: DefaultProjectOwner,
	}

	_, err = ec.CreateOrUpdate(ctx, defaultProject.ID.String(), defaultProject)
	if err != nil {
		c.Log.Error("failed to create default project", "error", err)
		return err
	}

	return nil
}

// PrepareAppData finishes the startup-time AppVersion rewrite that must not
// race consumers which update the same entities. Failures remain non-fatal, as
// they were when this work lived inside Start.
func (c *Foundation) PrepareAppData(ctx context.Context) error {
	if c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}

	migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := core_v1alpha.MigrateAppVersionConcurrency(migrationCtx, c.Log, c.eac); err != nil {
		c.Log.Error("failed to migrate app versions", "error", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// PublicIPs returns the cluster's known public IP addresses, applying the
// same filtering rules as the advertised API addresses. Routes through
// ComputeAdvertise so the AutocertController's DNS sanity check honors
// per-family netcheck state (no leaking the source IP when its family has
// zero reachable ports) and the CGNAT filter (no advertising tailnet
// addresses as "public").
func (c *Foundation) PublicIPs() []net.IP {
	c.netcheckMu.RLock()
	netcheck := c.netcheckResult
	c.netcheckMu.RUnlock()

	cands, _ := ComputeAdvertise(AdvertiseInput{
		IPs:      c.IPs.All(),
		Netcheck: netcheck,
	})

	seen := make(map[string]struct{})
	var ips []net.IP
	for _, cand := range cands {
		if !cand.Included || cand.IP == nil {
			continue
		}
		if cand.Classification != "global-unicast" {
			continue
		}
		s := cand.IP.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		ips = append(ips, cand.IP)
	}
	return ips
}

func (c *Foundation) Server() *rpc.Server {
	return c.state.Server()
}
