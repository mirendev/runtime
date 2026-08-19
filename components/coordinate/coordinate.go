package coordinate

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
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
	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/admin/admin_v1alpha"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/debug/debug_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/oidcbinding/oidcbinding_v1alpha"
	"miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/api/telemetry/telemetry_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/components/netresolve"
	addonctrl "miren.dev/runtime/controllers/addon"
	artifactctrl "miren.dev/runtime/controllers/artifact"
	certctrl "miren.dev/runtime/controllers/certificate"
	deploymentctrl "miren.dev/runtime/controllers/deployment"
	ephemeralctrl "miren.dev/runtime/controllers/ephemeral"
	indexgcctrl "miren.dev/runtime/controllers/indexgc"
	keyrotationctrl "miren.dev/runtime/controllers/keyrotation"
	nodehealthctrl "miren.dev/runtime/controllers/nodehealth"
	runctrl "miren.dev/runtime/controllers/run"
	sagagcctrl "miren.dev/runtime/controllers/sagagc"
	"miren.dev/runtime/controllers/sandboxpool"
	schedulerctrl "miren.dev/runtime/controllers/scheduler"
	schemareindexctrl "miren.dev/runtime/controllers/schemareindex"
	versionctrl "miren.dev/runtime/controllers/version"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/memcache"
	"miren.dev/runtime/pkg/addon/mysql"
	"miren.dev/runtime/pkg/addon/postgresql"
	"miren.dev/runtime/pkg/addon/rabbitmq"
	"miren.dev/runtime/pkg/addon/valkey"
	"miren.dev/runtime/pkg/anywhere"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/containerenv"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/secret"
	secretcluster "miren.dev/runtime/pkg/secret/cluster"
	"miren.dev/runtime/pkg/secret/keyring"
	"miren.dev/runtime/pkg/sysstats"
	"miren.dev/runtime/pkg/uplink"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/admin"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	debugsrv "miren.dev/runtime/servers/debug"
	"miren.dev/runtime/servers/deployment"
	"miren.dev/runtime/servers/entityserver"
	execproxy "miren.dev/runtime/servers/exec_proxy"
	"miren.dev/runtime/servers/httpingress"
	"miren.dev/runtime/servers/logs"
	oidcbindingsrv "miren.dev/runtime/servers/oidcbinding"
	runnerserver "miren.dev/runtime/servers/runner"
	"miren.dev/runtime/servers/runnertelemetry"
	secretsrv "miren.dev/runtime/servers/secret"
	telemetrysrv "miren.dev/runtime/servers/telemetry"
	"miren.dev/runtime/version"
)

// EtcdTLSConfig holds TLS configuration for connecting to etcd with mTLS.
type EtcdTLSConfig struct {
	CertPEM []byte // Client certificate PEM
	KeyPEM  []byte // Client private key PEM
	CACert  []byte // CA certificate PEM for verifying server
}

type CoordinatorConfig struct {
	Address         string              `json:"address" yaml:"address"`
	EtcdEndpoints   []string            `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix          string              `json:"prefix" yaml:"prefix"`
	Resolver        netresolve.Resolver `json:"resolver" yaml:"resolver"`
	TempDir         string              `json:"temp_dir" yaml:"temp_dir"`
	DataPath        string              `json:"data_path" yaml:"data_path"`
	AdditionalNames []string            `json:"additional_names" yaml:"additional_names"`
	IPs             *IPSet              `json:"ips" yaml:"ips"`

	// ACME certificate configuration
	AcmeEmail       string `json:"acme_email" yaml:"acme_email"`
	AcmeDNSProvider string `json:"acme_dns_provider" yaml:"acme_dns_provider"`

	// Cloud authentication configuration
	CloudAuth CloudAuthConfig `json:"cloud_auth" yaml:"cloud_auth"`

	// NoAuth disables authentication entirely (for testing only)
	NoAuth bool `json:"no_auth" yaml:"no_auth"`

	// EtcdTLS holds mTLS configuration for etcd connections (optional).
	// When set, the coordinator will use mTLS to connect to etcd.
	EtcdTLS *EtcdTLSConfig `json:"etcd_tls" yaml:"etcd_tls"`

	Mem       *metrics.MemoryUsage
	Cpu       *metrics.CPUUsage
	HTTP      *metrics.HTTPMetrics
	Logs      *observability.LogReader
	LogWriter observability.LogWriter

	// Observability addresses for distributed runners
	VictoriametricsAddress string
	VictorialogsAddress    string

	// BuildKit is the persistent BuildKit component for container image builds
	BuildKit *buildkit.Component

	// HTTPRequestTimeout is the timeout for HTTP requests to app sandboxes
	HTTPRequestTimeout time.Duration

	// AppVersionRetentionCount and AppVersionRetentionPeriod tune the version
	// retention GC. Values <= 0 fall back to the controller defaults.
	AppVersionRetentionCount  int
	AppVersionRetentionPeriod time.Duration

	// SecretKeyRotationPeriod is how old the cluster key may get before it
	// rotates on its own. Zero means rotate only when asked; negative means the
	// operator's value did not parse, so fall back to the default rather than
	// reading a typo as "never rotate".
	SecretKeyRotationPeriod time.Duration

	// SagaRetentionPeriod is how long a finished saga execution is kept.
	// Unlike the app-version knobs above, zero is meaningful here: it keeps
	// executions indefinitely, which is the escape hatch for an operator who
	// wants saga history frozen during an investigation. A negative value
	// falls back to the controller default.
	SagaRetentionPeriod time.Duration

	// WorkloadIssuer signs workload identity tokens for sandbox containers
	WorkloadIssuer *workloadidentity.Issuer

	// Secrets holds the registered secret backends. The caller builds it so the
	// runner sharing this process materializes through the same registry the
	// coordinator pins and serves with. Nil leaves the cluster without a secret
	// store, in which case a config referencing one fails rather than deploying
	// without it.
	Secrets *secret.Registry
}

// CloudAuthConfig contains cloud authentication settings
type CloudAuthConfig struct {
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	CloudURL    string            `json:"cloud_url" yaml:"cloud_url"`       // URL of miren.cloud (default: https://api.miren.cloud)
	PrivateKey  string            `json:"private_key" yaml:"private_key"`   // Required: Path to service account private key when enabled
	Tags        map[string]string `json:"tags" yaml:"tags"`                 // Tags from registration for RBAC evaluation
	ClusterID   string            `json:"cluster_id" yaml:"cluster_id"`     // Cluster ID for status reporting
	DNSHostname string            `json:"dns_hostname" yaml:"dns_hostname"` // Cloud-provisioned DNS hostname for the cluster

	// IdentityIssuerURL is the workload identity anchor cloud assigned this
	// cluster, when it has one. Empty means the cluster anchors identity at its
	// own hostname and serves its own discovery.
	IdentityIssuerURL string `json:"identity_issuer_url" yaml:"identity_issuer_url"`
}

const (
	DefaultProjectOwner = "miren.system@miren.dev"
	DefaultCloudURL     = "https://api.miren.cloud"
)

// EtcdTLSSetupResult contains the results of setting up etcd TLS.
type EtcdTLSSetupResult struct {
	// CertsDir is the directory containing etcd server certs (ca.crt, server.crt, server.key)
	CertsDir string
	// ClientTLS is the TLS config for clients connecting to etcd
	ClientTLS *EtcdTLSConfig
	// ClientCertFile is the path to the client certificate on disk
	ClientCertFile string
	// ClientKeyFile is the path to the client private key on disk
	ClientKeyFile string
	// CAFile is the path to the CA certificate on disk
	CAFile string
}

// SetupEtcdTLS loads the existing CA and ensures valid etcd mTLS certificates.
// Existing certificates are reused if their SANs match and they aren't near
// expiry; otherwise they are regenerated.
// The dataPath should be the same path used for CoordinatorConfig.DataPath.
// The CA must already exist (created by the coordinator's LoadCA).
// Additional DNS names and IPs are included in the server certificate SANs
// so that distributed runners can connect to etcd over the network.
func SetupEtcdTLS(log *slog.Logger, dataPath string, extraDNSNames []string, extraIPs []net.IP) (*EtcdTLSSetupResult, error) {
	certPath := filepath.Join(dataPath, "server", "ca.crt")
	keyPath := filepath.Join(dataPath, "server", "ca.key")

	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("CA certificate not found at %s: %w (CA must be created before setting up etcd TLS)", certPath, err)
	}

	log.Info("loading existing CA for etcd TLS", "path", certPath)

	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("missing key for CA: %w", err)
	}

	ca, err := caauth.LoadFromPEM(data, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA: %w", err)
	}

	// Create etcd certs directory
	certsDir := filepath.Join(dataPath, "etcd-certs")
	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create etcd certs directory: %w", err)
	}

	// Build expected server cert SANs
	dnsNames := []string{"localhost"}
	dnsNames = append(dnsNames, extraDNSNames...)

	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	ips = append(ips, extraIPs...)

	serverCertFile := filepath.Join(certsDir, "server.crt")
	serverKeyFile := filepath.Join(certsDir, "server.key")
	clientCertFile := filepath.Join(certsDir, "client.crt")
	clientKeyFile := filepath.Join(certsDir, "client.key")
	caFile := filepath.Join(certsDir, "ca.crt")

	// Check if existing certs are still valid
	if existing, err := loadX509Cert(serverCertFile); err == nil {
		if err := validateAPICertificate(existing, dnsNames, ips); err == nil {
			// Also check client cert exists and isn't expired
			if clientExisting, err := loadX509Cert(clientCertFile); err == nil {
				horizon := time.Now().Add(48 * time.Hour)
				if clientExisting.NotAfter.After(horizon) {
					log.Info("etcd TLS certificates valid, reusing", "certs_dir", certsDir,
						"server_expires", existing.NotAfter.Format(time.RFC3339),
						"sans_ips", existing.IPAddresses)

					clientPEM, err := os.ReadFile(clientCertFile)
					if err != nil {
						log.Info("etcd client cert unreadable, regenerating", "error", err)
						goto regenerate
					}
					clientKey, err := os.ReadFile(clientKeyFile)
					if err != nil {
						log.Info("etcd client key unreadable, regenerating", "error", err)
						goto regenerate
					}
					caCert := ca.GetCACertificate()

					return &EtcdTLSSetupResult{
						CertsDir: certsDir,
						ClientTLS: &EtcdTLSConfig{
							CertPEM: clientPEM,
							KeyPEM:  clientKey,
							CACert:  caCert,
						},
						ClientCertFile: clientCertFile,
						ClientKeyFile:  clientKeyFile,
						CAFile:         caFile,
					}, nil
				}
			}
		} else {
			log.Info("etcd server cert needs regeneration", "reason", err)
		}
	}

regenerate:
	log.Info("generating etcd TLS certificates", "dns_names", dnsNames, "ips", ips)

	// Issue etcd server certificate
	serverCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "etcd-server",
		Organization: "miren",
		ValidFor:     1 * year,
		DNSNames:     dnsNames,
		IPs:          ips,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to issue etcd server certificate: %w", err)
	}

	// Write etcd server certs
	if err := os.WriteFile(caFile, ca.GetCACertificate(), 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd CA cert: %w", err)
	}
	if err := os.WriteFile(serverCertFile, serverCert.CertPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd server cert: %w", err)
	}
	if err := os.WriteFile(serverKeyFile, serverCert.KeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write etcd server key: %w", err)
	}

	// Issue coordinator client certificate
	clientCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "etcd-client-coordinator",
		Organization: "miren",
		ValidFor:     1 * year,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to issue etcd client certificate: %w", err)
	}

	if err := os.WriteFile(clientCertFile, clientCert.CertPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd client cert: %w", err)
	}
	if err := os.WriteFile(clientKeyFile, clientCert.KeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write etcd client key: %w", err)
	}

	log.Info("etcd TLS certificates generated", "certs_dir", certsDir)

	return &EtcdTLSSetupResult{
		CertsDir: certsDir,
		ClientTLS: &EtcdTLSConfig{
			CertPEM: clientCert.CertPEM,
			KeyPEM:  clientCert.KeyPEM,
			CACert:  ca.GetCACertificate(),
		},
		ClientCertFile: clientCertFile,
		ClientKeyFile:  clientKeyFile,
		CAFile:         caFile,
	}, nil
}

func loadX509Cert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

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

	aa            activator.AppActivator
	spm           *sandboxpool.Manager
	cm            *controller.ControllerManager
	certProvider  autotls.CertificateProvider
	autocertReady func() // nil when DNS-01 path is used
	artifactGC    *artifactctrl.GCController
	runScheduler  *runctrl.Scheduler
	runGC         *runctrl.GCController
	ephemeralGC   *ephemeralctrl.GCController
	versionGC     *versionctrl.GCController
	indexGC       *indexgcctrl.GCController
	sagaGC        *sagagcctrl.GCController
	schemaReindex *schemareindexctrl.Controller
	keyRotation   *keyrotationctrl.Controller
	hs            *httpingress.Server

	authority *caauth.Authority

	apiCert []byte
	apiKey  []byte

	authClient        *cloudauth.AuthClient // For status reporting to cloud
	oidcAuthenticator *oidcauth.OIDCAuthenticator

	netcheckMu        sync.RWMutex
	netcheckResult    *cloudauth.NetcheckDualStackResult
	netcheckCheckedAt time.Time

	// publishedKeyFingerprint is the workload identity key set most recently
	// accepted by cloud, so an unchanged set isn't republished on every status
	// cycle. Guarded because the startup publish and the periodic one are
	// different goroutines.
	publishedKeysMu         sync.Mutex
	publishedKeyFingerprint string

	logAddressesOnce sync.Once

	debugServer *debugsrv.Server

	// sagaBuilder is retained so build saga recovery can be driven from
	// the boot sequence (RecoverBuildSagas) after the build's runtime
	// dependencies — the registry and the cluster.local mapping — are
	// ready. nil when sagas are disabled. See MIR-1285.
	sagaBuilder *build.SagaBuilder

	// appInfo is retained so app state can be reported up to cloud for
	// visibility (MIR-1558). It is the same instance backing the app RPC
	// surface, so cloud sees the health `miren app list` sees. nil when cloud
	// auth is not configured, which is the disconnected case reporting must
	// tolerate.
	appInfo *app.AppInfo
}

func (c *Coordinator) Activator() activator.AppActivator {
	return c.aa
}

func (c *Coordinator) SandboxPoolManager() *sandboxpool.Manager {
	return c.spm
}

func (c *Coordinator) HttpIngress() *httpingress.Server {
	return c.hs
}

// RecoverBuildSagas resumes in-flight build sagas left by a previous
// process. It must be called from the boot sequence AFTER the runtime
// dependencies a resumed build needs — the cluster registry and the
// cluster.local name mapping — are ready. Recovery is deliberately not
// run during Start, where it would resume an image push before those
// exist (MIR-1285). No-op when sagas are disabled; recovery errors are
// logged, not fatal.
func (c *Coordinator) RecoverBuildSagas(ctx context.Context) {
	if c.sagaBuilder == nil {
		return
	}
	if err := c.sagaBuilder.Recover(ctx); err != nil {
		c.Log.Error("build saga recovery completed with errors", "error", err)
	}
}

// Stop stops the coordinator and all managed controllers
func (c *Coordinator) Stop() {
	if c.cm != nil {
		c.cm.Stop()
	}
	if c.artifactGC != nil {
		c.artifactGC.Stop()
	}
	if c.ephemeralGC != nil {
		c.ephemeralGC.Stop()
	}
	if c.runScheduler != nil {
		c.runScheduler.Stop()
	}
	if c.runGC != nil {
		c.runGC.Stop()
	}
	if c.versionGC != nil {
		c.versionGC.Stop()
	}
	if c.indexGC != nil {
		c.indexGC.Stop()
	}
	if c.sagaGC != nil {
		c.sagaGC.Stop()
	}
	if c.schemaReindex != nil {
		c.schemaReindex.Stop()
	}
	if c.debugServer != nil {
		if err := c.debugServer.Close(); err != nil {
			c.Log.Error("failed to close debug server", "error", err)
		}
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

func (c *Coordinator) LoadCA(ctx context.Context) error {
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

func (c *Coordinator) LoadAPICert(ctx context.Context) error {
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
func (c *Coordinator) runnerTelemetryOptions() []rpc.StateOption {
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
func (c *Coordinator) buildEtcdTLSConfig() (*tls.Config, error) {
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

// RunnerConfig returns a client config for a runner service with proper TLS certificate SANs.
// The certificate will be valid for localhost and the runner's listen address.
func (c *Coordinator) RunnerConfig(listenAddress string) (*clientconfig.Config, error) {
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

// CACertificate returns the cluster CA in PEM form, or nil before LoadCA has
// run. Callers hand it to clients that must verify the cluster's certificates —
// sandboxes reaching the API with a workload identity token, for one.
func (c *Coordinator) CACertificate() []byte {
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
func (c *Coordinator) workloadAuthenticator() rpc.Authenticator {
	if c.WorkloadIssuer == nil {
		c.Log.Info("workload identity authentication disabled (no issuer configured)")
		return nil
	}

	c.Log.Info("workload identity authentication enabled",
		"issuer", c.WorkloadIssuer.IssuerURL())
	return workloadidentity.NewAuthenticator(c.WorkloadIssuer, c.Log)
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

	rs, err := rpc.NewState(ctx, rpcOpts...)
	if err != nil {
		c.Log.Error("failed to create RPC server", "error", err)
		return err
	}
	c.state = rs

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

	// Report associations left stale by a rotation that predates resolving
	// bindings from them. This only reports; see ReportStaleAssociationVariables
	// for why repairing it here would be worse.
	//
	// Bounded by the same startup-maintenance deadline as the migrations above, so
	// a large or slow store delays boot by at most that budget. A sweep cut short
	// reports what it reached and boot continues: an unread association means a
	// missing warning, never a missing binding.
	assocStale, assocChecked, assocErr := addonctrl.ReportStaleAssociationVariables(maintCtx, c.Log, ec, eac)
	if assocErr != nil {
		c.Log.Warn("addon association variable check did not complete",
			"stale", assocStale, "checked", assocChecked, "error", assocErr)
	} else if assocStale > 0 {
		c.Log.Warn("addon associations disagree with their apps' stored config",
			"stale", assocStale, "checked", assocChecked)
	}

	// Set up addon registry and register providers.
	addonRegistry := addon.NewRegistry()
	addonFw := addon.NewProviderFramework(c.Log, ec, eac, saga.NewEntityStorage(etcdStore, c.Log))
	addonRegistry.Register(postgresql.AddonName, postgresql.NewProvider(addonFw), postgresql.Definition())
	addonRegistry.Register(mysql.AddonName, mysql.NewProvider(addonFw), mysql.Definition())
	addonRegistry.Register(valkey.AddonName, valkey.NewProvider(addonFw), valkey.Definition())
	addonRegistry.Register(rabbitmq.AddonName, rabbitmq.NewProvider(addonFw), rabbitmq.Definition())
	addonRegistry.Register(memcache.AddonName, memcache.NewProvider(addonFw), memcache.Definition())

	if err := addonRegistry.EnsureEntities(ctx, ec); err != nil {
		c.Log.Error("failed to ensure addon entities", "error", err)
		return err
	}

	// The in-cluster backend needs the entity store, which only exists here, so
	// the caller supplies an empty registry and this fills it in. Externally
	// configured instances would register alongside it.
	secretRegistry := c.Secrets
	if secretRegistry == nil {
		secretRegistry = secret.NewRegistry()
	}
	secretKeyring, err := keyring.Ensure(c.Log, c.DataPath)
	if err != nil {
		c.Log.Error("failed to open secret keyring", "error", err)
		return err
	}
	secretBackend := secretcluster.NewBackend(c.Log, ec, secretKeyring)
	if err := secretRegistry.Register(secretBackend); err != nil {
		c.Log.Error("failed to register the in-cluster secret backend", "error", err)
		return err
	}

	// Rotation owns the keyring from here: it is the only thing that swaps the
	// backend's ring, and it persists each new ring before the backend seals
	// anything with it.
	keyRotationConfig := keyrotationctrl.DefaultConfig()
	if c.SecretKeyRotationPeriod >= 0 {
		keyRotationConfig.MaxKeyAge = c.SecretKeyRotationPeriod
	}
	keyRotation := &keyrotationctrl.Controller{
		Log:      c.Log.With("module", "key-rotation"),
		EC:       ec,
		Backend:  secretBackend,
		DataPath: c.DataPath,
		Config:   keyRotationConfig,
	}
	keyRotation.Start(ctx)
	c.keyRotation = keyRotation

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
	launcher.DataPath = c.DataPath
	if err := launcher.Init(ctx); err != nil {
		c.Log.Error("failed to initialize deployment launcher", "error", err)
		return err
	}

	// Register the launcher as a pool creator for the activator so ephemeral
	// versions can create pools on demand (they bypass the normal Launcher
	// reconciliation triggered by ActiveVersion changes).
	aa.SetPoolCreator(launcher)

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

	// Watch AddonAssociation changes to re-trigger launcher when addons become ready
	addonLauncherController := controller.NewReconcileController(
		"deploymentlauncher-addons",
		c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindAddonAssociation),
		eac,
		launcher.AddonAssociationHandler(),
		0, // No resync — driven entirely by watch events
		1,
	)
	c.cm.AddController(addonLauncherController)

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

	// Add node health controller (marks sandboxes DEAD when their runner
	// has been non-READY for longer than the grace period)
	nodeHealth := nodehealthctrl.NewController(c.Log, eac)
	if err := nodeHealth.Init(ctx); err != nil {
		c.Log.Error("failed to initialize node health controller", "error", err)
		return err
	}

	nodeHealthRC := controller.NewReconcileController(
		"nodehealth",
		c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindNode),
		eac,
		controller.AdaptReconcileController[compute_v1alpha.Node](nodeHealth),
		30*time.Second, // Resync to check grace period expiry
		1,
	)
	c.cm.AddController(nodeHealthRC)

	// Collect cluster-level hostnames for TLS cert provisioning (e.g., cloud-provisioned DNS).
	var clusterHostnames []string
	if c.CloudAuth.DNSHostname != "" {
		clusterHostnames = append(clusterHostnames, c.CloudAuth.DNSHostname)
	}

	// Add certificate controller — DNS-01 when a DNS provider is configured,
	// otherwise HTTP-01 via autocert for eager cert provisioning on route set.
	if c.AcmeDNSProvider != "" {
		c.Log.Info("enabling ACME DNS challenge certificate controller", "provider", c.AcmeDNSProvider)
		dnsController := certctrl.NewController(c.Log, c.DataPath, c.AcmeEmail, c.AcmeDNSProvider, clusterHostnames)
		if err := dnsController.Init(ctx); err != nil {
			c.Log.Error("failed to initialize certificate controller", "error", err)
			return err
		}
		c.certProvider = dnsController

		certRC := controller.NewReconcileController(
			"certificate",
			c.Log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute),
			eac,
			controller.AdaptReconcileController[ingress_v1alpha.HttpRoute](dnsController),
			time.Hour, // Resync hourly to handle dropped watches and check renewals
			1,         // Single worker to avoid duplicate cert requests
		)
		c.cm.AddController(certRC)
	} else {
		c.Log.Info("enabling ACME HTTP-01 certificate controller (autocert)")
		autocertController := certctrl.NewAutocertController(certctrl.AutocertControllerOpts{
			Log:              c.Log,
			EAC:              eac,
			DataPath:         c.DataPath,
			Email:            c.AcmeEmail,
			PublicIPs:        c.PublicIPs,
			ClusterHostnames: clusterHostnames,
		})
		if err := autocertController.Init(ctx); err != nil {
			c.Log.Error("failed to initialize autocert controller", "error", err)
			return err
		}
		c.certProvider = autocertController
		c.autocertReady = autocertController.SetReady

		certRC := controller.NewReconcileController(
			"certificate",
			c.Log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute),
			eac,
			controller.AdaptReconcileController[ingress_v1alpha.HttpRoute](autocertController),
			time.Hour, // Resync hourly to handle dropped watches and check renewals
			1,         // Single worker to avoid duplicate cert requests
		)
		c.cm.AddController(certRC)
	}

	// Add addon controller (reconciles addon associations for provisioning/deprovisioning)
	addonController := addonctrl.NewController(c.Log, ec, eac, addonRegistry, addonFw.Storage)
	if err := addonController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize addon controller", "error", err)
		return err
	}

	addonReconciler := controller.NewReconcileController(
		"addon",
		c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindAddonAssociation),
		eac,
		controller.AdaptReconcileController[addon_v1alpha.AddonAssociation](addonController),
		time.Minute,
		// Multiple workers so a long-running provisioning saga for one
		// association does not block reconciliation of others. Same-entity
		// concurrency is already prevented by ReconcileController.inFlight.
		4,
	)
	c.cm.AddController(addonReconciler)

	// Add addon rotation controller (reconciles rotation requests: apply a new
	// secret to the live engine, update the stored value, redeploy consumers).
	rotationController := addonctrl.NewRotationController(c.Log, ec, eac, addonRegistry)
	if err := rotationController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize addon rotation controller", "error", err)
		return err
	}

	rotationReconciler := controller.NewReconcileController(
		"addon-rotation",
		c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindRotationRequest),
		eac,
		controller.AdaptReconcileController[addon_v1alpha.RotationRequest](rotationController),
		time.Minute,
		4,
	)
	c.cm.AddController(rotationReconciler)

	// Run controller: one sandbox, one command, one exit code, then teardown.
	runController := runctrl.NewController(c.Log, ec, eac)
	if err := runController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize run controller", "error", err)
		return err
	}

	runReconciler := controller.NewReconcileController(
		"run",
		c.Log,
		entity.Ref(entity.EntityKind, run_v1alpha.KindRun),
		eac,
		controller.AdaptReconcileController[run_v1alpha.Run](runController),
		time.Minute,
		4,
	)
	// The controller needs a handle on its own queue: the deadline sweep and the
	// sandbox bridge enqueue work rather than transitioning runs themselves, so
	// every status change stays inside the reconcile the framework serializes
	// per entity.
	runController.RC = runReconciler
	runReconciler.SetPeriodic(runctrl.SweepInterval, runController.SweepDeadlines)
	c.cm.AddController(runReconciler)

	// A sandbox reaching STOPPED produces no event on the run index, so without
	// this bridge a finished run would wait for the sweep to notice it.
	runSandboxWatch := runctrl.NewSandboxWatchController(c.Log, eac, runReconciler)
	runSandboxReconciler := controller.NewReconcileController(
		"run-sandbox-watch",
		c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		eac,
		controller.AdaptController(runSandboxWatch),
		0,
		1,
	)
	c.cm.AddController(runSandboxReconciler)

	eps := execproxy.NewServer(c.Log, eac, rs)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(eps))

	// Give the addon framework an exec client so providers whose credential lives
	// inside the container (RabbitMQ) can rotate it via rabbitmqctl. This must be
	// wired before the controller manager starts, since the rotation controller
	// can reconcile a pending request (and call fw.Exec) the moment it's running.
	//
	// Assumption worth pinning: this loopback binds fw.Exec to whatever THIS
	// process exposes as the exec service. On the coordinator that's the
	// node-routing exec proxy (the eps ExposeValue just above), so ExecInPool
	// reaches an addon sandbox on whatever node holds it. That only holds while
	// addon reconciliation runs on the coordinator. A runner exposes the
	// containerd-local exec server under the same name, which sees only its own
	// node's sandboxes, so if the addon controllers are ever distributed onto
	// runners this must become a real RPC to a node-router rather than a loopback.
	execLoopback, err := rs.Connect(rs.LoopbackAddr(), "dev.miren.runtime/exec")
	if err != nil {
		c.Log.Error("failed to connect to exec RPC service", "error", err)
		return err
	}
	addonFw.Exec = exec_v1alpha.NewSandboxExecClient(execLoopback)

	// Start the controller manager
	if err := c.cm.Start(ctx); err != nil {
		c.Log.Error("failed to start controller manager", "error", err)
		return err
	}

	// Scheduled tasks. Deliberately not a reconcile controller: ticks come from
	// config on a timer, not from an entity changing, and dedup is the
	// create-if-absent on the tick-derived name rather than any lock.
	//
	// Started here with the other background controllers rather than at
	// construction, so it does not begin firing runs before the reconcile
	// manager -- including the run controller that would execute them -- is up.
	c.runScheduler = runctrl.NewScheduler(c.Log, ec, eac)
	c.runScheduler.Start(ctx)

	// Run retention. Not a reconcile controller either: it deletes rather than
	// transitions, on a cadence of minutes.
	c.runGC = runctrl.NewGCController(c.Log, ec, eac)
	c.runGC.Start(ctx)

	// Start the artifact GC controller
	c.artifactGC = &artifactctrl.GCController{
		Log:    c.Log.With("module", "artifact-gc"),
		EAC:    eac,
		Config: artifactctrl.DefaultGCConfig(),
	}
	c.artifactGC.Start(ctx)

	// Start the ephemeral version GC controller
	c.ephemeralGC = &ephemeralctrl.GCController{
		Log:    c.Log.With("module", "ephemeral-gc"),
		EAC:    eac,
		Config: ephemeralctrl.DefaultGCConfig(),
	}
	c.ephemeralGC.Start(ctx)

	// Start the version retention GC controller
	versionGCConfig := versionctrl.DefaultGCConfig()
	if c.AppVersionRetentionCount > 0 {
		versionGCConfig.RetentionCount = c.AppVersionRetentionCount
	}
	if c.AppVersionRetentionPeriod > 0 {
		versionGCConfig.RetentionPeriod = c.AppVersionRetentionPeriod
	}
	c.versionGC = &versionctrl.GCController{
		Log:      c.Log.With("module", "version-gc"),
		EAC:      eac,
		Config:   versionGCConfig,
		DataPath: c.DataPath,
	}
	c.versionGC.Start(ctx)

	// Start the stale index GC controller. It deletes stale index entries in the
	// background so clusters self-heal; the manual `miren debug reindex` stays
	// available as the immediate big hammer.
	c.indexGC = &indexgcctrl.GCController{
		Log:    c.Log.With("module", "index-gc"),
		Store:  etcdStore,
		Config: indexgcctrl.DefaultGCConfig(),
	}
	c.indexGC.Start(ctx)

	// Start the saga retention GC controller. It runs regardless of the sagas
	// feature flag: a cluster that had the flag on and then turned it off still
	// has a backlog to drain, and on a cluster that never enabled it the sweep
	// costs two empty index lookups.
	sagaGCConfig := sagagcctrl.DefaultGCConfig()
	if c.SagaRetentionPeriod >= 0 {
		sagaGCConfig.Retention = c.SagaRetentionPeriod
	}
	c.sagaGC = &sagagcctrl.GCController{
		Log:     c.Log.With("module", "saga-gc"),
		Storage: saga.NewEntityStorage(etcdStore, c.Log),
		Config:  sagaGCConfig,
	}
	c.sagaGC.Start(ctx)

	// Start the schema reindex controller. It backfills index entries after an
	// index schema change, checkpointing between bounded passes so a store too
	// large to reindex in one go still converges. This deliberately runs in the
	// background rather than during startup: under the old startup-time reindex,
	// a store that exceeded the maintenance deadline restarted the scan from the
	// first entity every boot and never reached the tail.
	c.schemaReindex = &schemareindexctrl.Controller{
		Log:         c.Log.With("module", "schema-reindex"),
		Store:       etcdStore,
		CurrentHash: schema.IndexHash,
		Config:      schemareindexctrl.DefaultConfig(),
	}
	c.schemaReindex.Start(ctx)

	ai := app.NewAppInfo(c.Log, ec, c.Cpu, c.Mem, c.HTTP, secretRegistry)
	c.appInfo = ai
	server.ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(ai))
	server.ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(ai))
	server.ExposeValue("dev.miren.runtime/app-runs", app_v1alpha.AdaptRuns(ai))

	addonsServer := app.NewAddonsServer(c.Log, ec, addonRegistry, addon.NewRegistryImageChecker())
	server.ExposeValue("dev.miren.runtime/addons", app_v1alpha.AdaptAddons(addonsServer))

	secretsServer := secretsrv.NewServer(c.Log, secretRegistry, keyRotation)
	server.ExposeValue("dev.miren.runtime/secrets", secret_v1alpha.AdaptSecrets(secretsServer))

	addonsLoopback, err := rs.Connect(rs.LoopbackAddr(), "dev.miren.runtime/addons")
	if err != nil {
		c.Log.Error("failed to connect to addons RPC service", "error", err)
		return err
	}
	addonsClient := app_v1alpha.NewAddonsClient(addonsLoopback)

	// Create app client for the builder
	appClient := appclient.NewClient(c.Log, loopback)

	bs := build.NewBuilder(c.Log, eac, appClient, addonsClient, c.Resolver, c.TempDir, c.LogWriter, c.CloudAuth.DNSHostname, c.BuildKit, c.DataPath)
	bs.WorkloadIssuer = c.WorkloadIssuer
	bs.Secrets = secretRegistry

	var buildHandler build_v1alpha.Builder = bs
	if labs.Sagas() {
		sagaStorage := saga.NewEntityStorage(etcdStore, c.Log)
		sagaBuilder := build.NewSagaBuilder(bs, sagaStorage, c.Log)
		if err := sagaBuilder.Init(); err != nil {
			c.Log.Error("failed to initialize saga builder", "error", err)
			return err
		}
		// Retain for RecoverBuildSagas, driven from the boot sequence
		// after the registry and cluster.local mapping are ready. Running
		// recovery here would resume an image push before they exist
		// (MIR-1285).
		c.sagaBuilder = sagaBuilder
		buildHandler = sagaBuilder
	}
	server.ExposeValue("dev.miren.runtime/build", build_v1alpha.AdaptBuilder(buildHandler))

	ls := logs.NewServer(c.Log, ec, c.Logs)
	server.ExposeValue("dev.miren.runtime/logs", app_v1alpha.AdaptLogs(ls))

	ds, err := deployment.NewDeploymentServer(c.Log, eac, ec, appClient, c.CloudAuth.DNSHostname, secretRegistry)
	if err != nil {
		c.Log.Error("failed to create deployment server", "error", err)
		return err
	}
	server.ExposeValue("dev.miren.runtime/deployment", deployment_v1alpha.AdaptDeployment(ds))

	oidcServer := oidcbindingsrv.NewServer(c.Log, ec, eac)
	server.ExposeValue("dev.miren.runtime/oidc-bindings", oidcbinding_v1alpha.AdaptOidcBindings(oidcServer))

	c.debugServer, err = debugsrv.NewServer(c.Log, filepath.Join(c.DataPath, "net.db"), eac)
	if err != nil {
		c.Log.Error("failed to create debug server", "error", err)
		return err
	}
	server.ExposeValue("dev.miren.runtime/debug-netdb", debug_v1alpha.AdaptNetDB(c.debugServer))

	// Create httpingress server for internal HTTP requests
	ingressConfig := httpingress.IngressConfig{
		RequestTimeout: c.HTTPRequestTimeout,
		DataPath:       c.DataPath,
		WorkloadIssuer: c.WorkloadIssuer,
	}
	c.hs = httpingress.NewServer(ctx, c.Log, ingressConfig, loopback, aa, c.HTTP, c.LogWriter)

	adminServer := admin.NewServer(c.Log, ec, c.hs, c.LogWriter)
	server.ExposeValue("dev.miren.runtime/admin", admin_v1alpha.AdaptAdmin(adminServer))

	runnerReg := runnerserver.NewRegistrationServer(runnerserver.RegistrationServerConfig{
		Log:                    c.Log,
		Authority:              c.authority,
		EAC:                    eac,
		CoordinatorAddr:        c.Address,
		EtcdEndpoints:          c.EtcdEndpoints,
		EtcdPrefix:             c.Prefix,
		VictoriametricsAddress: c.VictoriametricsAddress,
		VictorialogsAddress:    c.VictorialogsAddress,
		WorkloadIssuer:         c.WorkloadIssuer,
	})
	server.ExposeValue(rpc.ServiceRunner, runner_v1alpha.AdaptRunnerRegistration(runnerReg))

	ts := telemetrysrv.NewServer(c.Log)
	server.ExposeValue("dev.miren.runtime/telemetry", telemetry_v1alpha.AdaptTelemetry(ts))

	c.Log.Info("started RPC server")

	// Report initial cluster status if cloud auth is enabled
	if c.CloudAuth.Enabled && c.authClient != nil && c.CloudAuth.ClusterID != "" {
		// Publish the public half of the workload identity key set before the
		// status loop, so cloud can serve discovery for this cluster from the
		// moment it starts handing out tokens. The signing key stays here.
		c.publishSigningKeysAtStartup(ctx)

		err = c.ReportStartupStatus(ctx)
		if err != nil {
			c.Log.Error("failed to report initial cluster status", "error", err)
		}

		go c.reportStatusPeriodically(ctx)
	}

	// Bring up the control-plane link to cloud, then attach its tenants. The
	// link is owned here rather than by any one feature, because several share
	// it: Miren Anywhere uses it to learn when to dial a POP, and app reporting
	// uses it to push state up. Adding a tenant means registering against the
	// link, not wrapping it.
	if c.CloudAuth.Enabled && c.authClient != nil {
		cloudURL := c.CloudAuth.CloudURL
		if cloudURL == "" {
			cloudURL = DefaultCloudURL
		}

		link := uplink.NewClient(
			cloudURL,
			c.authClient,
			uplink.NewMessageRouter(),
			c.Log.With("component", "uplink"),
		)
		anywhereConn := anywhere.New(anywhere.Config{
			ClusterXID: c.CloudAuth.ClusterID,
			Ingress:    c.hs,
			Log:        c.Log.With("component", "anywhere"),
			Uplink:     link,
		})
		c.startAppReporter(link)
		c.startDeployReporter(link)

		go func() {
			// POP connections outlive individual reconnects but not the link
			// itself, which is why this is tied to Run returning rather than to
			// this function, which returns as soon as everything is wired.
			defer anywhereConn.Close()

			if err := link.Run(ctx); err != nil && ctx.Err() == nil {
				c.Log.Error("cloud uplink exited with error", "error", err)
			}
		}()
	}

	return nil
}

// runNetcheck calls the cloud's netcheck endpoint over both IPv4 and IPv6
// to determine public reachability on each address family.
func (c *Coordinator) runNetcheck(ctx context.Context) {
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

// PublicIPs returns the cluster's known public IP addresses, applying the
// same filtering rules as the advertised API addresses. Routes through
// ComputeAdvertise so the AutocertController's DNS sanity check honors
// per-family netcheck state (no leaking the source IP when its family has
// zero reachable ports) and the CGNAT filter (no advertising tailnet
// addresses as "public").
func (c *Coordinator) PublicIPs() []net.IP {
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

// apiAddresses builds the list of API addresses the server should advertise.
// The heavy lifting lives in ComputeAdvertise so the same rules can be
// exercised by the 'miren debug advertise' command.
func (c *Coordinator) apiAddresses() []string {
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
func (c *Coordinator) reachabilityVerdict() *cloudauth.ReachabilityVerdict {
	c.netcheckMu.RLock()
	netcheck := c.netcheckResult
	c.netcheckMu.RUnlock()

	return netcheck.ReachabilityVerdict()
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

			// Republish only when the key set actually changed, which makes
			// this the path a rotation propagates through — and the retry for
			// a startup publish that failed.
			if _, err := c.publishSigningKeys(ctx); err != nil {
				c.Log.Error("failed to publish workload identity signing keys", "error", err)
			}
		}
	}
}

func (c *Coordinator) Server() *rpc.Server {
	return c.state.Server()
}

// CertificateProvider returns the certificate provider for use by autotls.
func (c *Coordinator) CertificateProvider() autotls.CertificateProvider {
	return c.certProvider
}

// AutocertReadySignal returns a function that signals the autocert controller
// that the port-80 ACME challenge server is ready. Returns nil when the DNS-01
// path is used (which doesn't need port 80).
func (c *Coordinator) AutocertReadySignal() func() {
	return c.autocertReady
}
