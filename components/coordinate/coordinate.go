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
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	"miren.dev/runtime/servers/deployment"
	"miren.dev/runtime/servers/entityserver"
	execproxy "miren.dev/runtime/servers/exec_proxy"
	"miren.dev/runtime/servers/logs"
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

	// Cloud authentication configuration
	CloudAuth CloudAuthConfig `json:"cloud_auth" yaml:"cloud_auth"`

	Mem  *metrics.MemoryUsage
	Cpu  *metrics.CPUUsage
	HTTP *metrics.HTTPMetrics
	Logs *observability.LogReader
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

	aa activator.AppActivator

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte

	authClient *cloudauth.AuthClient // For status reporting to cloud
}

func (c *Coordinator) Activator() activator.AppActivator {
	return c.aa
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

	aa := activator.NewLocalActivator(ctx, c.Log, eac)
	c.aa = aa

	eps := execproxy.NewServer(c.Log, eac, rs, aa)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(eps))

	// Create app client for the builder
	appClient := appclient.NewClient(c.Log, loopback)

	bs := build.NewBuilder(c.Log, eac, appClient, c.Resolver, c.TempDir)
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

	// Build status report
	status := &cloudauth.StatusReport{
		ClusterID: c.CloudAuth.ClusterID,
		State:     "active", // TODO: Determine actual state based on health checks
		// TODO: Add more fields as they become available:
		// - Version (from build info)
		// - NodeCount (from entity store)
		// - WorkloadCount (from entity store)
		// - ResourceUsage (from metrics)
		// - HealthChecks (from component health)
		// - RBACRulesVersion (from RBAC system)
		// - LastRBACSync (from RBAC system)
	}

	return c.authClient.ReportClusterStatus(ctx, status)
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
