package harness

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	defaultCloudPort     = "18080"
	defaultPopListenPort = "18443"
	defaultPopH3Port     = "19443"
	defaultPopAdminPort  = "19090"
	adminToken           = "test-admin-token-for-pop"
	certEncKey           = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	dbURL                = "postgres://cloud:cloud@postgres:5432/cloud?sslmode=disable"
	valkeyAddr           = "valkey:6379"
	metricsPassword      = "test-metrics-password"
)

// CloudEnv manages a cloud+POP test environment for Miren Anywhere blackbox tests.
type CloudEnv struct {
	CloudURL      string // e.g. http://localhost:18080
	PopListenPort string
	PopH3Port     string
	ClusterID     string
	OrgID         string
	CertXID       string
	PopXID        string

	cloudPort     string
	clusterName   string
	privateKeyPEM string
	publicKeyPEM  string
	popAuthToken  string
	cloudProc     *BackgroundProcess
	popProc       *BackgroundProcess

	t *testing.T
	m *Miren
}

// NewCloudEnv builds a full cloud+POP environment for testing Miren Anywhere.
// It builds cloud/POP binaries, starts cloud, registers a POP and cluster,
// starts POP, and restarts the miren server so it picks up the new
// registration.json. The environment is torn down via t.Cleanup.
func NewCloudEnv(t *testing.T, m *Miren) *CloudEnv {
	t.Helper()

	cloudRepo := detectCloudRepo(t, m)

	env := &CloudEnv{
		cloudPort:     defaultCloudPort,
		CloudURL:      fmt.Sprintf("http://localhost:%s", defaultCloudPort),
		PopListenPort: defaultPopListenPort,
		PopH3Port:     defaultPopH3Port,
		t:             t,
		m:             m,
	}

	// Kill any leftover processes from a previous test run
	env.cleanupStaleProcesses(t)

	// Generate ED25519 keypair for cluster registration
	env.generateKeyPair(t)

	// Build cloud and POP binaries
	env.buildBinaries(t, cloudRepo)

	// Apply database migrations
	env.applyMigrations(t, cloudRepo)

	// Start cloud
	env.startCloud(t)

	// Register POP with cloud
	env.registerPOP(t)

	// Generate and upload TLS certs
	env.setupCerts(t)

	// Register cluster with cloud
	env.registerCluster(t)

	// Write registration.json for the miren server
	env.writeRegistration(t)

	// Start POP
	env.startPOP(t)

	// Restart miren server so it picks up the registration
	env.restartServerWithRegistration(t)

	// Wait for Miren Anywhere to connect
	env.waitForConnection(t)

	return env
}

// BindAppHostname adds a POP hostname entry routing the given hostname to
// the cluster, with the test TLS certificate.
func (env *CloudEnv) BindAppHostname(t *testing.T, hostname string) {
	t.Helper()

	// First set up the route in miren
	// (The app must already have a route for this hostname in miren's ingress)
	result, err := env.adminCall("pop.hostname.add", map[string]string{
		"hostname":        hostname,
		"cluster_xid":     env.ClusterID,
		"certificate_xid": env.CertXID,
	})
	if err != nil {
		t.Fatalf("pop.hostname.add failed: %v", err)
	}
	t.Logf("hostname bound: %v", result)
}

// viaCloudConfigPath is where SetupViaCloudCluster writes its config inside the
// container. MIREN_CONFIG pointing at a file disables clientconfig.d, so this
// config is self-contained and cannot accidentally reach the dev cluster.
const viaCloudConfigPath = "/tmp/miren-viacloud.yaml"

// SetupViaCloudCluster provisions a cloud user who can reach this cluster and
// writes a client config that routes to it through cloud rather than dialing
// it. It returns the config path, for MIREN_CONFIG, and the user's XID, which
// is what the cluster should resolve a relayed caller to.
//
// The user is a real one as far as cloud is concerned — created through the dev
// login endpoint, made a member of the organization that owns the cluster, and
// granted access by an RBAC rule — because the whole point of the relay is that
// the cluster judges the caller's own credential. A synthetic token would prove
// nothing.
func (env *CloudEnv) SetupViaCloudCluster(t *testing.T, clusterName string) (configPath, userXID string) {
	t.Helper()

	setupToken, userXID := env.devLogin(t)
	configPath = viaCloudConfigPath

	if _, err := env.adminCall("org.member.add", map[string]string{
		"organization_id": env.OrgID,
		"user_xid":        userXID,
		"role":            "admin",
	}); err != nil {
		t.Fatalf("org.member.add failed: %v", err)
	}
	t.Logf("cloud user %s added to org %s", userXID, env.OrgID)

	env.grantClusterAccess(t, setupToken, userXID)

	// A second login, after the group exists: a token carries the groups its
	// user had when it was minted, and the first one predates the grant.
	token, _ := env.devLogin(t)

	cfg := fmt.Sprintf(`active_cluster: %s
identities:
  cloud:
    type: token
    issuer: %s
    token: %s
clusters:
  %s:
    via_cloud: true
    xid: %s
    identity: cloud
`, clusterName, env.CloudURL, token, clusterName, env.ClusterID)

	env.m.RunCmdAsRoot("bash", "-c",
		fmt.Sprintf("cat > %s << 'CFGEOF'\n%s\nCFGEOF", viaCloudConfigPath, cfg))

	t.Cleanup(func() {
		env.m.RunCmdAsRoot("rm", "-f", viaCloudConfigPath)
	})

	return configPath, userXID
}

// grantClusterAccess puts the user in a group and writes an RBAC rule giving
// that group everything on this org's clusters.
//
// The cluster authorizes each call against these rules, so without them a
// relayed command is refused — correctly, but for a reason that has nothing to
// do with the relay. Going through cloud's real APIs rather than the database
// keeps the grant the same one a person would make.
func (env *CloudEnv) grantClusterAccess(t *testing.T, token, userXID string) {
	t.Helper()

	group := env.cloudAPI(t, token, "POST",
		"/api/v1/organizations/"+env.OrgID+"/groups",
		map[string]string{
			"name":        "blackbox",
			"description": "blackbox test access",
		})

	// The handler serializes the group's XID as "id"; "xid" is the fallback in
	// case that response ever grows the field under its own name.
	created := nested(group, "group")
	groupXID := getString(created, "id")
	if groupXID == "" {
		groupXID = getString(created, "xid")
	}
	if groupXID == "" {
		t.Fatalf("group creation returned no identifier: %v", group)
	}

	env.cloudAPI(t, token, "POST", "/api/v1/groups/"+groupXID+"/members",
		map[string]string{"user_xid": userXID})

	// An empty tag selector matches every cluster in the org, and "*" on both
	// resource and action is what an operator's own access looks like. The
	// relay is not what this test is narrowing down.
	env.cloudAPI(t, token, "POST",
		"/api/v1/organizations/"+env.OrgID+"/rbac-rules",
		map[string]any{
			"name":         "blackbox-full-access",
			"tag_selector": map[string]any{"expressions": []any{}},
			"groups":       []string{groupXID},
			"permissions":  []any{map[string]any{"resource": "*", "actions": []string{"*"}}},
		})

	t.Logf("granted cluster access to %s via group %s", userXID, groupXID)
}

// cloudAPI calls a cloud HTTP endpoint as an authenticated user and returns the
// decoded response.
func (env *CloudEnv) cloudAPI(t *testing.T, token, method, path string, body any) map[string]any {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal %s %s body: %v", method, path, err)
	}

	r := env.m.RunCmd("curl", "-sf", "-X", method, env.CloudURL+path,
		"-H", "Authorization: Bearer "+token,
		"-H", "Content-Type: application/json",
		"-d", string(payload))
	if !r.Success() {
		t.Fatalf("%s %s failed (exit %d): %s", method, path, r.ExitCode, r.Stderr)
	}

	if strings.TrimSpace(r.Stdout) == "" {
		return nil
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("failed to parse %s %s response: %v\nraw: %s", method, path, err, r.Stdout)
	}
	return out
}

// nested returns a nested object, or the original when the key is absent, so a
// response that is already the object works too.
func nested(m map[string]any, key string) map[string]any {
	if inner, ok := m[key].(map[string]any); ok {
		return inner
	}
	return m
}

// devLogin mints a user and a JWT through cloud's dev-only login endpoint,
// which is enabled for this environment (see startCloud).
func (env *CloudEnv) devLogin(t *testing.T) (token, userXID string) {
	t.Helper()

	r := env.m.RunCmd("curl", "-sf", env.CloudURL+"/auth/dev/login?user=blackbox")
	if !r.Success() {
		t.Fatalf("dev login failed (exit %d): %s", r.ExitCode, r.Stderr)
	}

	var resp struct {
		Token   string `json:"token"`
		UserXID string `json:"user_xid"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		t.Fatalf("failed to parse dev login response: %v\nraw: %s", err, r.Stdout)
	}
	if resp.Token == "" || resp.UserXID == "" {
		t.Fatalf("dev login returned no credentials: %s", r.Stdout)
	}

	return resp.Token, resp.UserXID
}

func detectCloudRepo(t *testing.T, m *Miren) string {
	t.Helper()

	cloudRepo := os.Getenv("BLACKBOX_CLOUD_REPO")
	if cloudRepo == "" {
		// Auto-detect: try ../cloud relative to the repo root
		candidate := filepath.Join(m.cluster.RepoRoot, "..", "cloud")
		if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil {
			cloudRepo = candidate
		}
	}
	if cloudRepo == "" {
		t.Skip("BLACKBOX_CLOUD_REPO not set and ../cloud not found; skipping POP test")
	}

	// Verify it's the right repo
	if _, err := os.Stat(filepath.Join(cloudRepo, "cmd", "cloud")); err != nil {
		t.Skipf("cloud repo at %s does not contain cmd/cloud: %v", cloudRepo, err)
	}

	absPath, err := filepath.Abs(cloudRepo)
	if err != nil {
		t.Fatalf("failed to resolve cloud repo path: %v", err)
	}
	return absPath
}

func (env *CloudEnv) generateKeyPair(t *testing.T) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	env.privateKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}))

	pubBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	env.publicKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}))
}

func (env *CloudEnv) buildBinaries(t *testing.T, cloudRepo string) {
	t.Helper()

	binDir := filepath.Join(env.m.cluster.RepoRoot, "bin")

	// GOTOOLCHAIN=auto lets these builds fetch whatever toolchain the cloud
	// repo's go.mod requires, decoupling it from runtime's Go version. CI's
	// setup-go exports GOTOOLCHAIN=local job-wide, which would otherwise pin
	// the cloud build to runtime's toolchain and fail whenever cloud requires
	// a newer Go. Appended last so it wins over the inherited value.
	buildEnv := append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOTOOLCHAIN=auto")

	// Build cloud binary
	t.Log("building cloud binary...")
	cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, "bb-cloud"), "./cmd/cloud")
	cmd.Dir = cloudRepo
	cmd.Env = buildEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cloud binary: %v\n%s", err, out)
	}

	// Build POP binary
	t.Log("building POP binary...")
	cmd = exec.Command("go", "build", "-o", filepath.Join(binDir, "bb-pop"), "./cmd/pop")
	cmd.Dir = cloudRepo
	cmd.Env = buildEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build POP binary: %v\n%s", err, out)
	}

	t.Cleanup(func() {
		os.Remove(filepath.Join(binDir, "bb-cloud"))
		os.Remove(filepath.Join(binDir, "bb-pop"))
	})
}

func (env *CloudEnv) applyMigrations(t *testing.T, cloudRepo string) {
	t.Helper()
	t.Log("applying database migrations...")

	migrationsDir := filepath.Join(cloudRepo, "db", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("failed to read migrations dir: %v", err)
	}

	// Sort migration files by name (they are timestamped)
	var sqlFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, filepath.Join(migrationsDir, e.Name()))
		}
	}
	sort.Strings(sqlFiles)

	// Concatenate all migrations and apply via psql
	var allSQL strings.Builder
	for _, f := range sqlFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", f, err)
		}
		allSQL.Write(data)
		allSQL.WriteString("\n")
	}

	// Write to a temp file accessible in the container
	tmpFile := filepath.Join(env.m.cluster.RepoRoot, "bin", "bb-migrations.sql")
	if err := os.WriteFile(tmpFile, []byte(allSQL.String()), 0644); err != nil {
		t.Fatalf("failed to write migration file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile) })

	containerPath := env.m.ContainerPath(tmpFile)
	r := env.m.RunCmd("bash", "-c",
		fmt.Sprintf("PGPASSWORD=cloud psql -h postgres -U cloud -d cloud -f %s 2>&1 || true", containerPath))
	t.Logf("migrations output (last 500 chars): ...%s", truncate(r.Stdout, 500))
}

func (env *CloudEnv) startCloud(t *testing.T) {
	t.Helper()
	t.Log("starting cloud server...")

	// Generate a random challenge signing key (64 bytes minimum for HMAC-SHA256)
	sigKey := make([]byte, 64)
	if _, err := rand.Read(sigKey); err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	env.cloudProc = env.m.RunCmdBackground(t, map[string]string{
		"PORT":                    env.cloudPort,
		"DATABASE_URL":            dbURL,
		"VALKEY_ADDRESS":          valkeyAddr,
		"ADMIN_TOKEN":             adminToken,
		"POP_CERT_ENCRYPTION_KEY": certEncKey,
		"CHALLENGE_SIGNING_KEY":   base64.StdEncoding.EncodeToString(sigKey),
		"DEV_LOGIN":               "true",
		"METRICS_PASSWORD":        metricsPassword,
	}, "/src/bin/bb-cloud", "-mode=all")

	// Wait for cloud to be ready, with process liveness checks
	Poll(t, "cloud server ready", 30*time.Second, 500*time.Millisecond, func() (bool, string) {
		// Check if the process is still alive (must use root since process runs as root)
		alive := env.m.RunCmdAsRoot("bash", "-c", fmt.Sprintf("kill -0 %s 2>/dev/null && echo alive", env.cloudProc.PID))
		if !strings.Contains(alive.Stdout, "alive") {
			logs := env.cloudProc.Logs()
			t.Fatalf("cloud process died (PID %s). Logs:\n%s", env.cloudProc.PID, logs)
		}

		r := env.m.RunCmd("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			fmt.Sprintf("http://localhost:%s/health", env.cloudPort))
		code := strings.TrimSpace(r.Stdout)
		if code == "200" {
			return true, ""
		}
		return false, fmt.Sprintf("cloud not ready (status: %s)", code)
	})

	t.Log("cloud server ready")
}

func (env *CloudEnv) registerPOP(t *testing.T) {
	t.Helper()
	t.Log("registering POP server...")

	popName := fmt.Sprintf("test-pop-%d", time.Now().UnixNano())
	result, err := env.adminCall("pop.register", map[string]string{
		"name":   popName,
		"region": "local",
	})
	if err != nil {
		t.Fatalf("pop.register failed: %v", err)
	}

	env.PopXID = getString(result, "xid")
	env.popAuthToken = getString(result, "auth_token")
	if env.PopXID == "" || env.popAuthToken == "" {
		t.Fatalf("pop.register returned incomplete result: %v", result)
	}
	t.Logf("POP registered: xid=%s", env.PopXID)
}

func (env *CloudEnv) setupCerts(t *testing.T) {
	t.Helper()
	t.Log("generating TLS certificates...")

	// Generate self-signed cert for POP H3 (cluster-facing)
	env.m.RunCmd("bash", "-c",
		"openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 "+
			"-keyout /tmp/bb-h3.key -out /tmp/bb-h3.crt "+
			"-days 1 -nodes -subj '/CN=pop-h3-test' 2>/dev/null")

	// Generate wildcard cert for hostname testing (client-facing)
	env.m.RunCmd("bash", "-c",
		"openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 "+
			"-keyout /tmp/bb-hostname.key -out /tmp/bb-hostname.crt "+
			"-days 1 -nodes -subj '/CN=*.test.pop' "+
			"-addext 'subjectAltName=DNS:*.test.pop' 2>/dev/null")

	t.Cleanup(func() {
		env.m.RunCmd("bash", "-c", "rm -f /tmp/bb-h3.key /tmp/bb-h3.crt /tmp/bb-hostname.key /tmp/bb-hostname.crt")
	})

	// Read the hostname cert and key to upload via admin API
	certR := env.m.RunCmd("cat", "/tmp/bb-hostname.crt")
	keyR := env.m.RunCmd("cat", "/tmp/bb-hostname.key")

	result, err := env.adminCall("pop.cert.add", map[string]any{
		"hostnames":       []string{"*.test.pop"},
		"certificate_pem": certR.Stdout,
		"private_key_pem": keyR.Stdout,
		"expires_at":      "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("pop.cert.add failed: %v", err)
	}
	env.CertXID = getString(result, "xid")
	t.Logf("cert uploaded: xid=%s", env.CertXID)
}

func (env *CloudEnv) registerCluster(t *testing.T) {
	t.Helper()
	t.Log("registering cluster with cloud...")

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	env.clusterName = "bb-cluster-" + suffix
	result, err := env.adminCall("cluster.register", map[string]string{
		"cluster_name": env.clusterName,
		"org_name":     "bb-org-" + suffix,
		"public_key":   env.publicKeyPEM,
	})
	if err != nil {
		t.Fatalf("cluster.register failed: %v", err)
	}

	env.ClusterID = getString(result, "cluster_id")
	env.OrgID = getString(result, "organization_id")
	if env.ClusterID == "" {
		t.Fatalf("cluster.register returned no cluster_id: %v", result)
	}
	t.Logf("cluster registered: id=%s, org=%s", env.ClusterID, env.OrgID)
}

func (env *CloudEnv) writeRegistration(t *testing.T) {
	t.Helper()

	reg := map[string]any{
		"cluster_id":         env.ClusterID,
		"cluster_name":       env.clusterName,
		"organization_id":    env.OrgID,
		"service_account_id": "", // filled by cloud
		"private_key":        env.privateKeyPEM,
		"cloud_url":          env.CloudURL,
		"registered_at":      time.Now().Format(time.RFC3339),
		"status":             "approved",
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal registration: %v", err)
	}

	// Write to the server data directory inside the container
	env.m.RunCmdAsRoot("bash", "-c",
		fmt.Sprintf("mkdir -p /var/lib/miren/server && cat > /var/lib/miren/server/registration.json << 'REGEOF'\n%s\nREGEOF", string(data)))

	t.Cleanup(func() {
		env.m.RunCmdAsRoot("rm", "-f", "/var/lib/miren/server/registration.json", "/var/lib/miren/server/service-account.key")
	})

	// Also write the private key file
	env.m.RunCmdAsRoot("bash", "-c",
		fmt.Sprintf("cat > /var/lib/miren/server/service-account.key << 'KEYEOF'\n%s\nKEYEOF", env.privateKeyPEM))
}

func (env *CloudEnv) startPOP(t *testing.T) {
	t.Helper()
	t.Log("starting POP server...")

	env.popProc = env.m.RunCmdBackground(t, map[string]string{
		"POP_AUTH_TOKEN":     env.popAuthToken,
		"CLOUD_API_URL":      env.CloudURL,
		"POP_LISTEN_ADDR":    ":" + env.PopListenPort,
		"POP_H3_LISTEN_ADDR": ":" + env.PopH3Port,
		"POP_ADMIN_ADDR":     ":" + defaultPopAdminPort,
		"POP_H3_CERT_FILE":   "/tmp/bb-h3.crt",
		"POP_H3_KEY_FILE":    "/tmp/bb-h3.key",
		"POP_EXTERNAL_ADDR":  "localhost:" + env.PopH3Port,
		"POP_POLL_INTERVAL":  "2s",
	}, "/src/bin/bb-pop")

	// Poll for POP readiness via its admin /health endpoint
	Poll(t, "POP server ready", 30*time.Second, 500*time.Millisecond, func() (bool, string) {
		alive := env.m.RunCmdAsRoot("bash", "-c", fmt.Sprintf("kill -0 %s 2>/dev/null && echo alive", env.popProc.PID))
		if !strings.Contains(alive.Stdout, "alive") {
			logs := env.popProc.Logs()
			t.Fatalf("POP process died (PID %s). Logs:\n%s", env.popProc.PID, logs)
		}

		r := env.m.RunCmd("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			fmt.Sprintf("http://localhost:%s/health", defaultPopAdminPort))
		code := strings.TrimSpace(r.Stdout)
		if code == "200" {
			return true, ""
		}
		return false, fmt.Sprintf("POP not ready (status: %s)", code)
	})

	t.Log("POP server ready")
}

func (env *CloudEnv) restartServerWithRegistration(t *testing.T) {
	t.Helper()
	t.Log("restarting miren server to pick up registration...")

	// Stop current server
	env.m.RunCmdAsRoot("bash", "-c", "hack/dev-server stop")

	// Truncate the log file so `dev-server wait-ready` doesn't match the
	// old "Miren server started" line from the previous startup.
	env.m.RunCmdAsRoot("bash", "-c", ": > /tmp/miren-server.log")

	env.m.RunCmdAsRoot("bash", "-c", "hack/dev-server start")

	// Wait for server to be ready
	r := env.m.RunCmdAsRoot("hack/dev-server", "wait-ready", "60")
	if !r.Success() {
		t.Fatalf("server failed to start: %s", r.Stderr)
	}

	// Also wait for buildkit's hosts file to be updated with the registry IP,
	// otherwise cluster.local resolution will fail during deploys.
	Poll(t, "buildkit registry hosts updated", 30*time.Second, 500*time.Millisecond, func() (bool, string) {
		r := env.m.RunCmdAsRoot("bash", "-c",
			"grep -F 'updated buildkit hosts file with registry IP' /tmp/miren-server.log 2>/dev/null | head -1")
		if strings.Contains(r.Stdout, "registry IP") {
			return true, ""
		}
		return false, "buildkit hosts file not yet updated"
	})

	t.Cleanup(func() {
		// Restart server after registration files are removed to restore original state
		env.m.RunCmdAsRoot("bash", "-c", "hack/dev-server stop")
		env.m.RunCmdAsRoot("bash", "-c", ": > /tmp/miren-server.log")
		env.m.RunCmdAsRoot("bash", "-c", "hack/dev-server start")
		env.m.RunCmdAsRoot("hack/dev-server", "wait-ready", "30")
	})

	t.Log("miren server restarted with registration loaded")
}

func (env *CloudEnv) waitForConnection(t *testing.T) {
	t.Helper()
	t.Log("waiting for Miren Anywhere to connect to cloud...")

	// Look for the definitive "connected to cloud" log line emitted by
	// pkg/uplink/client.go after the WebSocket dial succeeds.
	const readyMarker = "connected to cloud"

	Poll(t, "Miren Anywhere connected", 60*time.Second, 2*time.Second, func() (bool, string) {
		r := env.m.RunCmdAsRoot("bash", "-c",
			fmt.Sprintf("grep -F %q /tmp/miren-server.log 2>/dev/null | head -1", readyMarker))
		if strings.Contains(r.Stdout, readyMarker) {
			return true, ""
		}

		// Surface recent log lines mentioning Miren Anywhere for faster
		// diagnosis when the marker is missing.
		tail := env.m.RunCmdAsRoot("bash", "-c",
			"grep -i 'anywhere\\|cluster.channel\\|cloud' /tmp/miren-server.log 2>/dev/null | tail -5")
		return false, fmt.Sprintf("no %q marker yet; recent Miren Anywhere logs:\n%s", readyMarker, tail.Stdout)
	})

	t.Log("Miren Anywhere connected")
}

// adminCall makes a JSON-RPC call to the cloud admin API.
func (env *CloudEnv) adminCall(method string, params any) (map[string]any, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":"%s","params":%s,"id":1}`, method, string(paramsJSON))

	r := env.m.RunCmd("curl", "-sf", "-X", "POST",
		fmt.Sprintf("http://localhost:%s/.well-known/miren/admin", env.cloudPort),
		"-H", fmt.Sprintf("Authorization: Bearer %s", adminToken),
		"-H", "Content-Type: application/json",
		"-d", body)

	if !r.Success() {
		return nil, fmt.Errorf("admin call %s failed (exit %d): %s", method, r.ExitCode, r.Stderr)
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response for %s: %v\nraw: %s", method, err, r.Stdout)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("admin call %s returned error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result for %s: %v", method, err)
	}

	return result, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (env *CloudEnv) cleanupStaleProcesses(t *testing.T) {
	t.Helper()
	// Use -x for exact name match to avoid killing the bash process running this command
	env.m.RunCmdAsRoot("bash", "-c", "pkill -x bb-cloud 2>/dev/null; pkill -x bb-pop 2>/dev/null; true")
	time.Sleep(500 * time.Millisecond)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[len(s)-maxLen:]
}
