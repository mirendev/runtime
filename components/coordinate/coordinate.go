package coordinate

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/components/netresolve"
	certctrl "miren.dev/runtime/controllers/certificate"
	deploymentctrl "miren.dev/runtime/controllers/deployment"
	"miren.dev/runtime/controllers/sandboxpool"
	schedulerctrl "miren.dev/runtime/controllers/scheduler"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/sysstats"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	"miren.dev/runtime/servers/deployment"
	"miren.dev/runtime/servers/entityserver"
	execproxy "miren.dev/runtime/servers/exec_proxy"
	"miren.dev/runtime/servers/logs"
	"miren.dev/runtime/version"
)

type CoordinatorConfig struct {
	Address         string              `json:"address" yaml:"address"`
	EtcdEndpoints   []string            `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix          string              `json:"prefix" yaml:"prefix"`
	Resolver        netresolve.Resolver `json:"resolver" yaml:"resolver"`
	TempDir         string              `json:"temp_dir" yaml:"temp_dir"`
	DataPath        string              `json:"data_path" yaml:"data_path"`
	AdditionalNames []string            `json:"additional_names" yaml:"additional_names"`
	AdditionalIPs   []net.IP            `json:"additional_ips" yaml:"additional_ips"`

	// ACME certificate configuration
	AcmeEmail       string `json:"acme_email" yaml:"acme_email"`
	AcmeDNSProvider string `json:"acme_dns_provider" yaml:"acme_dns_provider"`

	// Cloud authentication configuration
	CloudAuth CloudAuthConfig `json:"cloud_auth" yaml:"cloud_auth"`

	Mem       *metrics.MemoryUsage
	Cpu       *metrics.CPUUsage
	HTTP      *metrics.HTTPMetrics
	Logs      *observability.LogReader
	LogWriter *observability.PersistentLogWriter
}

// CloudAuthConfig contains cloud authentication settings
type CloudAuthConfig struct {
	Enabled    bool              `json:"enabled" yaml:"enabled"`
	CloudURL   string            `json:"cloud_url" yaml:"cloud_url"`     // URL of miren.cloud (default: https://api.miren.cloud)
	PrivateKey string            `json:"private_key" yaml:"private_key"` // Required: Path to service account private key when enabled
	Tags       map[string]string `json:"tags" yaml:"tags"`               // Tags from registration for RBAC evaluation
	ClusterID  string            `json:"cluster_id" yaml:"cluster_id"`   // Cluster ID for status reporting
}

const (
	DefaultProjectOwner = "miren.system@miren.dev"
	DefaultCloudURL     = "https://api.miren.cloud"
)

func NewCoordinator(log *slog.Logger, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{
		CoordinatorConfig: cfg,
		Log:               log.With("module", "coordinator"),
	}
}

type Coordinator struct {
	CoordinatorConfig

	Log *slog.Logger

	state *rpc.State
	eac   *esv1.EntityAccessClient // Entity access client for querying entities

	aa             activator.AppActivator
	spm            *sandboxpool.Manager
	cm             *controller.ControllerManager
	certController *certctrl.Controller

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte

	authClient *cloudauth.AuthClient // For status reporting to cloud
}

func (c *Coordinator) Activator() activator.AppActivator {
	return c.aa
}

func (c *Coordinator) SandboxPoolManager() *sandboxpool.Manager {
	return c.spm
}

// Stop stops the coordinator and all managed controllers
func (c *Coordinator) Stop() {
	if c.cm != nil {
		c.cm.Stop()
	}
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

func (c *Coordinator) LoadCA(ctx context.Context) error {
	cert := filepath.Join(c.DataPath, "server", "ca.crt")
	keyPath := filepath.Join(c.DataPath, "server", "ca.key")

	if data, err := os.ReadFile(cert); err == nil {
		c.Log.Info("loading existing CA", "path", cert)

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("missing key for CA: %w", err)
		}

		ca, err := caauth.LoadFromPEM(data, key)
		if err != nil {
			return fmt.Errorf("failed to load CA: %w", err)
		}

		c.authority = ca
	} else {
		c.Log.Info("generating new CA", "path", cert)

		ca, err := caauth.New(caauth.Options{
			CommonName:   "miren-server",
			Organization: "miren",
			ValidFor:     10 * year,
		})
		if err != nil {
			return fmt.Errorf("failed to generate CA: %w", err)
		}

		err = os.MkdirAll(filepath.Dir(cert), 0755)
		if err != nil {
			return fmt.Errorf("failed to create CA directory: %w", err)
		}

		cd, kd, err := ca.ExportPEM()
		if err != nil {
			return fmt.Errorf("failed to export CA: %w", err)
		}

		err = os.WriteFile(cert, cd, 0644)
		if err != nil {
			return fmt.Errorf("failed to write CA cert: %w", err)
		}

		err = os.WriteFile(keyPath, kd, 0600)
		if err != nil {
			return fmt.Errorf("failed to write CA key: %w", err)
		}

		c.authority = ca
	}

	return nil
}

func (c *Coordinator) LoadAPICert(ctx context.Context) error {
	names := []string{
		"localhost",
	}

	names = append(names, c.AdditionalNames...)

	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}

	ips = append(ips, c.AdditionalIPs...)

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

func (c *Coordinator) LocalConfig() (*clientconfig.Config, error) {
	return c.NamedConfig("miren-user")
}

func (c *Coordinator) ServiceConfig() (*clientconfig.Config, error) {
	return c.NamedConfig("miren-services")
}

func (c *Coordinator) NamedConfig(name string) (*clientconfig.Config, error) {
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

func (c *Coordinator) IssueCertificate(name string) (*caauth.ClientCertificate, error) {
	if c.authority == nil {
		return nil, fmt.Errorf("CA authority not initialized")
	}

	return c.authority.IssueCertificate(caauth.Options{
		CommonName:   name,
		Organization: "miren",
		ValidFor:     1 * year,
	})
}

func (c *Coordinator) ListenAddress() string {
	return c.state.ListenAddr()
}

func (c *Coordinator) Start(ctx context.Context) error {
	c.Log.Info("starting coordinator", "address", c.Address, "etcd_endpoints", c.EtcdEndpoints, "prefix", c.Prefix)

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

		rpcOpts = append(rpcOpts, rpc.WithAuthenticator(authenticator))
		c.Log.Info("cloud authentication enabled",
			"cloud_url", authCloudURL)
	}

	rs, err := rpc.NewState(ctx, rpcOpts...)
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}
	c.state = rs

	server := rs.Server()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:        c.EtcdEndpoints,
		AutoSyncInterval: time.Minute,
	})
	if err != nil {
		c.Log.Error("failed to create etcd client", "error", err)
		return err
	}

	etcdStore, err := entity.NewEtcdStore(ctx, c.Log, client, c.Prefix)
	if err != nil {
		c.Log.Error("failed to create etcd store", "error", err)
		return err
	}

	err = schema.Apply(ctx, etcdStore)
	if err != nil {
		c.Log.Error("failed to apply schema", "error", err)
		return err
	}

	// Migrate entities from old format to new attribute-based format
	migrated, skipped, err := entity.MigrateEntityStore(ctx, c.Log, client, entity.MigrateOptions{
		Prefix: c.Prefix,
		DryRun: false,
	})
	if err != nil {
		c.Log.Warn("entity migration completed with errors", "migrated", migrated, "skipped", skipped, "error", err)
	} else if migrated > 0 {
		c.Log.Info("entity migration completed", "migrated", migrated, "skipped", skipped)
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

	// Migrate app versions before starting components that depend on them
	migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := core_v1alpha.MigrateAppVersionConcurrency(migrationCtx, c.Log, eac); err != nil {
		c.Log.Error("failed to migrate app versions", "error", err)
		// Continue even if migration fails
	}

	aa := activator.NewLocalActivator(ctx, c.Log, eac)
	c.aa = aa

	spm := sandboxpool.NewManager(c.Log, eac)
	c.spm = spm

	// Initialize the pool manager
	if err := spm.Init(ctx); err != nil {
		c.Log.Error("failed to initialize pool manager", "error", err)
		return err
	}

	// Create DeploymentLauncher to watch App entities and create pools
	launcher := deploymentctrl.NewLauncher(c.Log, eac)
	if err := launcher.Init(ctx); err != nil {
		c.Log.Error("failed to initialize deployment launcher", "error", err)
		return err
	}

	// Create controller manager and add controllers
	c.cm = controller.NewControllerManager()

	// Add deployment launcher controller (watches App entities for ActiveVersion changes)
	launcherController := controller.NewReconcileController(
		"deploymentlauncher",
		c.Log,
		entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
		eac,
		controller.AdaptReconcileController[core_v1alpha.App](launcher),
		time.Minute, // Resync every minute to ensure pools exist
		1,           // Single worker to prevent race conditions
	)
	c.cm.AddController(launcherController)

	// Add sandbox pool controller (reconciles pool desired_instances to actual sandboxes)
	poolController := controller.NewReconcileController(
		"sandboxpool",
		c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool),
		eac,
		controller.AdaptReconcileController[compute_v1alpha.SandboxPool](spm),
		10*time.Second, // Resync every 10 seconds for fast crash detection
		1,              // Single worker to prevent duplicate sandbox creation races
	)
	c.cm.AddController(poolController)

	// Add scheduler controller (assigns sandboxes to nodes)
	scheduler := schedulerctrl.NewController(c.Log, eac)
	if err := scheduler.Init(ctx); err != nil {
		c.Log.Error("failed to initialize scheduler controller", "error", err)
		return err
	}

	schedulerController := controller.NewReconcileController(
		"scheduler",
		c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		eac,
		controller.AdaptReconcileController[compute_v1alpha.Sandbox](scheduler),
		time.Minute, // Resync every minute to catch any missed sandboxes
		1,           // Single worker
	)
	c.cm.AddController(schedulerController)

	// Add certificate controller if DNS provider is configured
	if c.AcmeDNSProvider != "" {
		c.Log.Info("enabling ACME DNS challenge certificate controller", "provider", c.AcmeDNSProvider)
		certController := certctrl.NewController(c.Log, c.DataPath, c.AcmeEmail, c.AcmeDNSProvider)
		if err := certController.Init(ctx); err != nil {
			c.Log.Error("failed to initialize certificate controller", "error", err)
			return err
		}
		c.certController = certController

		certCtrl := controller.NewReconcileController(
			"certificate",
			c.Log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute),
			eac,
			controller.AdaptReconcileController[ingress_v1alpha.HttpRoute](certController),
			time.Hour, // Resync hourly to handle dropped watches and check renewals
			1,         // Single worker to avoid duplicate cert requests
		)
		c.cm.AddController(certCtrl)
	}

	// Start the controller manager
	if err := c.cm.Start(ctx); err != nil {
		c.Log.Error("failed to start controller manager", "error", err)
		return err
	}

	eps := execproxy.NewServer(c.Log, eac, rs, aa)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(eps))

	// Create app client for the builder
	appClient := appclient.NewClient(c.Log, loopback)

	bs := build.NewBuilder(c.Log, eac, appClient, c.Resolver, c.TempDir, c.LogWriter)
	server.ExposeValue("dev.miren.runtime/build", build_v1alpha.AdaptBuilder(bs))

	ai := app.NewAppInfo(c.Log, ec, c.Cpu, c.Mem, c.HTTP)
	server.ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(ai))
	server.ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(ai))

	ls := logs.NewServer(c.Log, ec, c.Logs)
	server.ExposeValue("dev.miren.runtime/logs", app_v1alpha.AdaptLogs(ls))

	ds, err := deployment.NewDeploymentServer(c.Log, eac)
	if err != nil {
		c.Log.Error("failed to create deployment server", "error", err)
		return err
	}
	server.ExposeValue("dev.miren.runtime/deployment", deployment_v1alpha.AdaptDeployment(ds))

	c.Log.Info("started RPC server")

	// Report initial cluster status if cloud auth is enabled
	if c.CloudAuth.Enabled && c.authClient != nil && c.CloudAuth.ClusterID != "" {
		err = c.ReportStartupStatus(ctx)
		if err != nil {
			c.Log.Error("failed to report initial cluster status", "error", err)
		}

		go c.reportStatusPeriodically(ctx)
	}

	return nil
}

// ReportStatus reports the current cluster status to miren.cloud
func (c *Coordinator) ReportStartupStatus(ctx context.Context) error {
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

	// Build list of API addresses
	apiAddresses := []string{c.Address}

	// Add localhost addresses
	apiAddresses = append(apiAddresses, "127.0.0.1:8443", "[::1]:8443")

	// Add additional IPs
	for _, ip := range c.AdditionalIPs {
		// Format the IP address with port
		if ip.To4() != nil {
			apiAddresses = append(apiAddresses, fmt.Sprintf("%s:8443", ip.String()))
		} else {
			apiAddresses = append(apiAddresses, fmt.Sprintf("[%s]:8443", ip.String()))
		}
	}

	// Build status report
	status := &cloudauth.StatusReport{
		ClusterID:         c.CloudAuth.ClusterID,
		APIAddresses:      apiAddresses,
		CACertFingerprint: caFingerprint,
		// TODO: Add more fields as they become available:
		// - Version (from build info)
	}

	return c.authClient.ReportClusterStatus(ctx, status)
}

// ReportStatus reports the current cluster status to miren.cloud
func (c *Coordinator) ReportStatus(ctx context.Context) error {
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
	}

	return c.authClient.ReportClusterStatus(ctx, status)
}

// collectResourceUsage gathers basic host system resource usage metrics
func (c *Coordinator) collectResourceUsage() cloudauth.ResourceUsage {
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
func (c *Coordinator) reportStatusPeriodically(ctx context.Context) {
	// Initial report after a short delay to allow services to start
	time.Sleep(5 * time.Second)

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
		}
	}
}

func (c *Coordinator) Server() *rpc.Server {
	return c.state.Server()
}

// CertificateProvider returns the certificate controller for use by autotls.
// Returns nil if DNS provider is not configured.
func (c *Coordinator) CertificateProvider() autotls.CertificateProvider {
	if c.certController == nil {
		return nil
	}
	return c.certController
}
