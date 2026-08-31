package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "valid auto mode config",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 50
scale_down_delay = "5m"
`,
			wantErr: "",
		},
		{
			name: "valid fixed mode config",
			config: `
name = "test-app"

[services.worker.concurrency]
mode = "fixed"
num_instances = 3
`,
			wantErr: "",
		},
		{
			name: "invalid mode",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "invalid"
`,
			wantErr: `service web: invalid concurrency mode "invalid", must be "auto" or "fixed"`,
		},
		{
			name: "negative requests_per_instance",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = -5
`,
			wantErr: "service web: requests_per_instance must be non-negative",
		},
		{
			name: "invalid scale_down_delay",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "auto"
scale_down_delay = "invalid"
`,
			wantErr: "service web: invalid scale_down_delay",
		},
		{
			name: "num_instances in auto mode",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "auto"
num_instances = 2
`,
			wantErr: "service web: num_instances cannot be set in auto mode",
		},
		{
			name: "requests_per_instance in fixed mode",
			config: `
name = "test-app"

[services.worker.concurrency]
mode = "fixed"
num_instances = 1
requests_per_instance = 10
`,
			wantErr: "service worker: requests_per_instance cannot be set in fixed mode",
		},
		{
			name: "scale_down_delay in fixed mode",
			config: `
name = "test-app"

[services.worker.concurrency]
mode = "fixed"
num_instances = 1
scale_down_delay = "2m"
`,
			wantErr: "service worker: scale_down_delay cannot be set in fixed mode",
		},
		{
			name: "negative num_instances",
			config: `
name = "test-app"

[services.worker.concurrency]
mode = "fixed"
num_instances = -1
`,
			wantErr: "service worker: num_instances must be at least 1 for fixed mode",
		},
		{
			name: "zero num_instances in fixed mode",
			config: `
name = "test-app"

[services.worker.concurrency]
mode = "fixed"
num_instances = 0
`,
			wantErr: "service worker: num_instances must be at least 1 for fixed mode",
		},
		{
			name: "empty mode defaults to auto",
			config: `
name = "test-app"

[services.web.concurrency]
requests_per_instance = 100
scale_down_delay = "10m"
`,
			wantErr: "",
		},
		{
			name: "multiple services with mixed modes",
			config: `
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 80
scale_down_delay = "2m"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2

[services.cron.concurrency]
mode = "fixed"
num_instances = 1
`,
			wantErr: "",
		},
		{
			name: "valid aliases",
			config: `
name = "test-app"

[aliases]
console = "app exec -i bin/rails console"
logs = "app logs -f"
`,
			wantErr: "",
		},
		{
			name: "valid multi-word alias",
			config: `
name = "test-app"

[aliases]
"x logs" = "app logs -f"
"x console" = "app exec -i bin/rails console"
`,
			wantErr: "",
		},
		{
			name: "invalid alias name with uppercase",
			config: `
name = "test-app"

[aliases]
Console = "app exec"
`,
			wantErr: "each word must start with a lowercase letter",
		},
		{
			name: "invalid alias name starting with number",
			config: `
name = "test-app"

[aliases]
"1console" = "app exec"
`,
			wantErr: "each word must start with a lowercase letter",
		},
		{
			name: "empty alias target",
			config: `
name = "test-app"

[aliases]
console = ""
`,
			wantErr: `alias "console": command must not be empty`,
		},
		{
			name: "whitespace-only alias target",
			config: `
name = "test-app"

[aliases]
console = "   "
`,
			wantErr: `alias "console": command must not be empty`,
		},
		{
			name: "valid build secret",
			config: `
name = "test-app"

[build]
dockerfile = "Dockerfile"

[[build.secrets]]
id = "npm_token"
backend = "cluster"
ref = "registry/npm-token"
`,
			wantErr: "",
		},
		{
			name: "build secret missing id",
			config: `
name = "test-app"

[[build.secrets]]
backend = "cluster"
ref = "registry/npm-token"
`,
			wantErr: "build.secrets[0]: id is required",
		},
		{
			name: "build secret without backend defaults to cluster",
			config: `
name = "test-app"

[[build.secrets]]
id = "npm_token"
ref = "registry/npm-token"
`,
			wantErr: "",
		},
		{
			name: "build secret invalid id",
			config: `
name = "test-app"

[[build.secrets]]
id = "npm/token"
backend = "cluster"
ref = "registry/npm-token"
`,
			wantErr: `build.secrets[0]: id "npm/token" must contain only letters, digits, and _.-`,
		},
		{
			name: "build secret missing ref",
			config: `
name = "test-app"

[[build.secrets]]
id = "npm_token"
backend = "cluster"
`,
			wantErr: "build.secrets[0]: ref is required",
		},
		{
			name: "build secret duplicate id",
			config: `
name = "test-app"

[[build.secrets]]
id = "npm_token"
backend = "cluster"
ref = "registry/npm-token"

[[build.secrets]]
id = "npm_token"
backend = "cluster"
ref = "registry/other-token"
`,
			wantErr: `build.secrets[1]: duplicate id "npm_token"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, err := Parse([]byte(tt.config))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, ac)
			}
		})
	}
}

func TestAppConfigParsing(t *testing.T) {
	config := `
name = "my-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 80
scale_down_delay = "15m"

[services.worker.concurrency]
mode = "fixed"
num_instances = 1
`

	ac, err := Parse([]byte(config))
	require.NoError(t, err)
	require.NotNil(t, ac)

	assert.Equal(t, "my-app", ac.Name)
	require.NotNil(t, ac.Services)
	require.Len(t, ac.Services, 2)

	// Check web service
	webSvc, ok := ac.Services["web"]
	require.True(t, ok)
	require.NotNil(t, webSvc.Concurrency)
	assert.Equal(t, "auto", webSvc.Concurrency.Mode)
	assert.Equal(t, 80, webSvc.Concurrency.RequestsPerInstance)
	assert.Equal(t, "15m", webSvc.Concurrency.ScaleDownDelay)
	assert.Equal(t, 0, webSvc.Concurrency.NumInstances)

	// Check worker service
	workerSvc, ok := ac.Services["worker"]
	require.True(t, ok)
	require.NotNil(t, workerSvc.Concurrency)
	assert.Equal(t, "fixed", workerSvc.Concurrency.Mode)
	assert.Equal(t, 0, workerSvc.Concurrency.RequestsPerInstance)
	assert.Equal(t, "", workerSvc.Concurrency.ScaleDownDelay)
	assert.Equal(t, 1, workerSvc.Concurrency.NumInstances)
}

func TestResolveDefaults_WebService(t *testing.T) {
	ac := &AppConfig{}
	ac.ResolveDefaults([]string{"web"})

	require.NotNil(t, ac.Services)
	webSvc, ok := ac.Services["web"]
	require.True(t, ok, "web service should be created")
	require.NotNil(t, webSvc.Concurrency)
	assert.Equal(t, "auto", webSvc.Concurrency.Mode)
	assert.Equal(t, 10, webSvc.Concurrency.RequestsPerInstance)
	assert.Equal(t, "15m", webSvc.Concurrency.ScaleDownDelay)
}

func TestResolveDefaults_OtherService(t *testing.T) {
	ac := &AppConfig{}
	ac.ResolveDefaults([]string{"worker"})

	require.NotNil(t, ac.Services)
	workerSvc, ok := ac.Services["worker"]
	require.True(t, ok, "worker service should be created")
	require.NotNil(t, workerSvc.Concurrency)
	assert.Equal(t, "fixed", workerSvc.Concurrency.Mode)
	assert.Equal(t, 1, workerSvc.Concurrency.NumInstances)
}

func TestResolveDefaults_PreservesExistingConfig(t *testing.T) {
	ac := &AppConfig{
		Services: map[string]*ServiceConfig{
			"web": {
				Concurrency: &ServiceConcurrencyConfig{
					Mode:                "auto",
					RequestsPerInstance: 20, // Custom value
					ScaleDownDelay:      "30m",
				},
			},
		},
	}

	ac.ResolveDefaults([]string{"web", "worker"})

	// Existing config preserved
	webSvc := ac.Services["web"]
	assert.Equal(t, 20, webSvc.Concurrency.RequestsPerInstance)
	assert.Equal(t, "30m", webSvc.Concurrency.ScaleDownDelay)

	// New service gets defaults
	workerSvc, ok := ac.Services["worker"]
	require.True(t, ok, "worker service should be created")
	require.NotNil(t, workerSvc.Concurrency)
	assert.Equal(t, "fixed", workerSvc.Concurrency.Mode)
	assert.Equal(t, 1, workerSvc.Concurrency.NumInstances)
}

func TestResolveDefaults_MultipleServices(t *testing.T) {
	ac := &AppConfig{}
	ac.ResolveDefaults([]string{"web", "worker", "scheduler"})

	require.Len(t, ac.Services, 3)

	// web gets auto mode
	assert.Equal(t, "auto", ac.Services["web"].Concurrency.Mode)
	assert.Equal(t, 10, ac.Services["web"].Concurrency.RequestsPerInstance)

	// Others get fixed mode
	assert.Equal(t, "fixed", ac.Services["worker"].Concurrency.Mode)
	assert.Equal(t, 1, ac.Services["worker"].Concurrency.NumInstances)

	assert.Equal(t, "fixed", ac.Services["scheduler"].Concurrency.Mode)
	assert.Equal(t, 1, ac.Services["scheduler"].Concurrency.NumInstances)
}

func TestResolveDefaults_EmptyServicesList(t *testing.T) {
	ac := &AppConfig{}
	ac.ResolveDefaults([]string{})

	require.NotNil(t, ac.Services, "Services map should be initialized")
	assert.Len(t, ac.Services, 0, "No services should be created")
}

func TestResolveDefaults_NilAppConfig(t *testing.T) {
	// Verify it doesn't panic with nil config
	var ac *AppConfig
	assert.NotPanics(t, func() {
		if ac != nil {
			ac.ResolveDefaults([]string{"web"})
		}
	})
}

func TestServiceMetricsDefaultsAndValidation(t *testing.T) {
	t.Run("web defaults", func(t *testing.T) {
		ac, err := Parse([]byte(`
[services.web.metrics]
enabled = true
`))
		require.NoError(t, err)

		ac.ResolveDefaults([]string{"web"})
		metrics := ac.Services["web"].Metrics
		require.NotNil(t, metrics)
		assert.True(t, metrics.Enabled)
		assert.Equal(t, "/metrics", metrics.Path)
		assert.Equal(t, 3000, metrics.Port)
		assert.Equal(t, "30s", metrics.Interval)
		assert.False(t, metrics.Public)
	})

	t.Run("first http port", func(t *testing.T) {
		ac, err := Parse([]byte(`
[[services.api.ports]]
port = 7000
name = "rpc"
type = "tcp"

[[services.api.ports]]
port = 8080
name = "http"
type = "http"

[services.api.metrics]
enabled = true
`))
		require.NoError(t, err)
		ac.ResolveDefaults([]string{"api"})
		assert.Equal(t, 8080, ac.Services["api"].Metrics.Port)
	})

	t.Run("port with omitted type defaults to http", func(t *testing.T) {
		ac, err := Parse([]byte(`
[[services.api.ports]]
port = 8080
name = "http"

[services.api.metrics]
enabled = true
`))
		require.NoError(t, err)
		ac.ResolveDefaults([]string{"api"})
		assert.Equal(t, 8080, ac.Services["api"].Metrics.Port)
	})

	t.Run("explicit port list has no implicit web port", func(t *testing.T) {
		_, err := Parse([]byte(`
[[services.web.ports]]
port = 7000
name = "rpc"
type = "tcp"

[services.web.metrics]
enabled = true
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metrics port is required")
	})

	t.Run("explicit tcp port has no implicit web port", func(t *testing.T) {
		_, err := Parse([]byte(`
[services.web]
port = 7000
port_type = "tcp"

[services.web.metrics]
enabled = true
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metrics port is required")
	})

	tests := []struct {
		name    string
		metrics string
		wantErr string
	}{
		{"short interval", `interval = "15s"`, "must be at least 30s"},
		{"invalid interval", `interval = "sometimes"`, "invalid metrics interval"},
		{"relative path", `path = "metrics"`, "absolute URL path"},
		{"query in path", `path = "/metrics?format=prom"`, "absolute URL path"},
		{"undeclared port", `port = 9090`, "must be a declared HTTP or TCP service port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(`
[services.web]
port = 8080

[services.web.metrics]
enabled = true
` + tt.metrics + "\n"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("explicit tcp port", func(t *testing.T) {
		ac, err := Parse([]byte(`
[[services.worker.ports]]
port = 9090
name = "prometheus"
type = "tcp"

[services.worker.metrics]
enabled = true
port = 9090
path = "/internal/metrics"
interval = "1m"
public = true
`))
		require.NoError(t, err)
		ac.ResolveDefaults([]string{"worker"})
		assert.Equal(t, "/internal/metrics", ac.Services["worker"].Metrics.Path)
		assert.Equal(t, "1m", ac.Services["worker"].Metrics.Interval)
	})
}

func TestParseAppConfigWithEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		wantVars []AppEnvVar
	}{
		{
			name: "single env var",
			config: `
name = "test-app"

[[env]]
key = "DATABASE_URL"
value = "postgres://localhost/db"
`,
			wantVars: []AppEnvVar{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
			},
		},
		{
			name: "multiple env vars",
			config: `
name = "test-app"

[[env]]
key = "DATABASE_URL"
value = "postgres://localhost/db"

[[env]]
key = "API_KEY"
value = "secret123"

[[env]]
key = "PORT"
value = "8080"
`,
			wantVars: []AppEnvVar{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
				{Key: "API_KEY", Value: "secret123"},
				{Key: "PORT", Value: "8080"},
			},
		},
		{
			name: "no env vars",
			config: `
name = "test-app"
`,
			wantVars: nil,
		},
		{
			name: "env vars with services",
			config: `
name = "test-app"

[[env]]
key = "DATABASE_URL"
value = "postgres://localhost/db"

[services.postgres]
image = "oci.miren.cloud/postgres:15"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
`,
			wantVars: []AppEnvVar{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, err := Parse([]byte(tt.config))
			require.NoError(t, err)
			require.NotNil(t, ac)

			if tt.wantVars == nil {
				assert.Nil(t, ac.EnvVars)
			} else {
				require.Len(t, ac.EnvVars, len(tt.wantVars))
				for i, want := range tt.wantVars {
					assert.Equal(t, want.Key, ac.EnvVars[i].Key, "env var %d key mismatch", i)
					assert.Equal(t, want.Value, ac.EnvVars[i].Value, "env var %d value mismatch", i)
				}
			}
		})
	}
}

func TestGetDefaultsForServices(t *testing.T) {
	tests := []struct {
		name         string
		serviceNames []string
		validate     func(t *testing.T, ac *AppConfig)
	}{
		{
			name:         "web service gets auto mode defaults",
			serviceNames: []string{"web"},
			validate: func(t *testing.T, ac *AppConfig) {
				require.Contains(t, ac.Services, "web")
				svc := ac.Services["web"]
				require.NotNil(t, svc)
				require.NotNil(t, svc.Concurrency)
				assert.Equal(t, "auto", svc.Concurrency.Mode)
				assert.Equal(t, 10, svc.Concurrency.RequestsPerInstance)
				assert.Equal(t, "15m", svc.Concurrency.ScaleDownDelay)
				assert.Equal(t, 0, svc.Concurrency.NumInstances)
			},
		},
		{
			name:         "non-web service gets fixed mode defaults",
			serviceNames: []string{"worker"},
			validate: func(t *testing.T, ac *AppConfig) {
				require.Contains(t, ac.Services, "worker")
				svc := ac.Services["worker"]
				require.NotNil(t, svc)
				require.NotNil(t, svc.Concurrency)
				assert.Equal(t, "fixed", svc.Concurrency.Mode)
				assert.Equal(t, 1, svc.Concurrency.NumInstances)
				assert.Equal(t, 0, svc.Concurrency.RequestsPerInstance)
				assert.Equal(t, "", svc.Concurrency.ScaleDownDelay)
			},
		},
		{
			name:         "multiple services",
			serviceNames: []string{"web", "worker", "cron"},
			validate: func(t *testing.T, ac *AppConfig) {
				require.Len(t, ac.Services, 3)

				// web should be auto
				require.Contains(t, ac.Services, "web")
				webSvc := ac.Services["web"]
				require.NotNil(t, webSvc.Concurrency)
				assert.Equal(t, "auto", webSvc.Concurrency.Mode)

				// worker should be fixed
				require.Contains(t, ac.Services, "worker")
				workerSvc := ac.Services["worker"]
				require.NotNil(t, workerSvc.Concurrency)
				assert.Equal(t, "fixed", workerSvc.Concurrency.Mode)

				// cron should be fixed
				require.Contains(t, ac.Services, "cron")
				cronSvc := ac.Services["cron"]
				require.NotNil(t, cronSvc.Concurrency)
				assert.Equal(t, "fixed", cronSvc.Concurrency.Mode)
			},
		},
		{
			name:         "empty service list",
			serviceNames: []string{},
			validate: func(t *testing.T, ac *AppConfig) {
				assert.Empty(t, ac.Services)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := GetDefaultsForServices(tt.serviceNames)
			require.NotNil(t, ac)
			tt.validate(t, ac)
		})
	}
}

func TestValidateEnvVarKey(t *testing.T) {
	t.Run("env var with key succeeds", func(t *testing.T) {
		config := `
name = "test-app"

[[env]]
key = "DATABASE_URL"
value = "postgres://localhost/db"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
		require.Len(t, ac.EnvVars, 1)
		assert.Equal(t, "DATABASE_URL", ac.EnvVars[0].Key)
	})

	t.Run("env var with empty key fails", func(t *testing.T) {
		config := `
name = "test-app"

[[env]]
value = "postgres://localhost/db"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "env[0]: key is required")
	})

	t.Run("service env var with key succeeds", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
command = "server"

[[services.web.env]]
key = "PORT"
value = "8080"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
	})

	t.Run("service env var with empty key fails", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
command = "server"

[[services.web.env]]
value = "8080"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "service web: env[0] key is required")
	})

	t.Run("env var with empty value succeeds (secrets stored server-side)", func(t *testing.T) {
		config := `
name = "test-app"

[[env]]
key = "DATABASE_URL"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
		assert.Len(t, ac.EnvVars, 1)
		assert.Equal(t, "DATABASE_URL", ac.EnvVars[0].Key)
		assert.Empty(t, ac.EnvVars[0].Value)
	})

	t.Run("service env var with empty value succeeds (secrets stored server-side)", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
command = "server"

[[services.web.env]]
key = "API_KEY"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
		assert.Len(t, ac.Services["web"].EnvVars, 1)
		assert.Equal(t, "API_KEY", ac.Services["web"].EnvVars[0].Key)
		assert.Empty(t, ac.Services["web"].EnvVars[0].Value)
	})
}

func TestValidateDiskConcurrencyRequirement(t *testing.T) {
	t.Run("fixed mode service with disk succeeds", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
mount_path = "/data"
size_gb = 100
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
	})

	t.Run("auto mode service with disk fails", func(t *testing.T) {
		config := `
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10

[[services.web.disks]]
name = "data"
mount_path = "/data"
size_gb = 100
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "disks can only be attached to services with fixed concurrency mode")
	})

	t.Run("service with disk but no concurrency config fails", func(t *testing.T) {
		config := `
name = "test-app"

[[services.app.disks]]
name = "data"
mount_path = "/data"
size_gb = 100
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "disks can only be attached to services with fixed concurrency mode")
	})

	t.Run("service without disks can be auto mode", func(t *testing.T) {
		config := `
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
	})

	t.Run("multiple disks on fixed service succeeds", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
mount_path = "/data"
size_gb = 100

[[services.database.disks]]
name = "wal"
mount_path = "/wal"
size_gb = 50
filesystem = "xfs"
owner = "keep"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
		require.Len(t, ac.Services["database"].Disks, 2)
		assert.Equal(t, "", ac.Services["database"].Disks[0].Owner)
		assert.Equal(t, "keep", ac.Services["database"].Disks[1].Owner)
	})

	t.Run("invalid filesystem type fails", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
mount_path = "/data"
size_gb = 100
filesystem = "ntfs"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid filesystem")
	})

	t.Run("missing disk name fails", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
mount_path = "/data"
size_gb = 100
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must have a name")
	})

	t.Run("missing mount_path fails", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
size_gb = 100
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must have a mount_path")
	})
}

func TestParseAppConfigWithEnvVarMetadata(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		wantVars []AppEnvVar
	}{
		{
			name: "env var with required and description",
			config: `
name = "test-app"

[[env]]
key = "DATABASE_URL"
value = ""
required = true
description = "PostgreSQL connection string"
`,
			wantVars: []AppEnvVar{
				{Key: "DATABASE_URL", Value: "", Required: true, Description: "PostgreSQL connection string"},
			},
		},
		{
			name: "env var with sensitive flag",
			config: `
name = "test-app"

[[env]]
key = "API_KEY"
value = "secret123"
sensitive = true
description = "Third-party API key"
`,
			wantVars: []AppEnvVar{
				{Key: "API_KEY", Value: "secret123", Sensitive: true, Description: "Third-party API key"},
			},
		},
		{
			name: "mix of env vars with and without metadata",
			config: `
name = "test-app"

[[env]]
key = "DATABASE_URL"
required = true
sensitive = true
description = "Database connection URL"

[[env]]
key = "LOG_LEVEL"
value = "info"

[[env]]
key = "SECRET_KEY"
value = ""
required = true
sensitive = true
`,
			wantVars: []AppEnvVar{
				{Key: "DATABASE_URL", Required: true, Sensitive: true, Description: "Database connection URL"},
				{Key: "LOG_LEVEL", Value: "info"},
				{Key: "SECRET_KEY", Required: true, Sensitive: true},
			},
		},
		{
			name: "service-level env vars with metadata",
			config: `
name = "test-app"

[services.web]
command = "server"

[[services.web.env]]
key = "PORT"
value = "3000"
description = "HTTP port"

[[services.web.env]]
key = "TLS_CERT"
value = ""
required = true
sensitive = true
description = "TLS certificate contents"
`,
			wantVars: nil, // global vars are nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, err := Parse([]byte(tt.config))
			require.NoError(t, err)
			require.NotNil(t, ac)

			if tt.wantVars == nil {
				assert.Nil(t, ac.EnvVars)
			} else {
				require.Len(t, ac.EnvVars, len(tt.wantVars))
				for i, want := range tt.wantVars {
					assert.Equal(t, want.Key, ac.EnvVars[i].Key, "env var %d key", i)
					assert.Equal(t, want.Value, ac.EnvVars[i].Value, "env var %d value", i)
					assert.Equal(t, want.Required, ac.EnvVars[i].Required, "env var %d required", i)
					assert.Equal(t, want.Sensitive, ac.EnvVars[i].Sensitive, "env var %d sensitive", i)
					assert.Equal(t, want.Description, ac.EnvVars[i].Description, "env var %d description", i)
				}
			}

			// For the service-level test, verify the service env vars
			if tt.name == "service-level env vars with metadata" {
				require.NotNil(t, ac.Services["web"])
				require.Len(t, ac.Services["web"].EnvVars, 2)
				assert.Equal(t, "PORT", ac.Services["web"].EnvVars[0].Key)
				assert.Equal(t, "HTTP port", ac.Services["web"].EnvVars[0].Description)
				assert.Equal(t, "TLS_CERT", ac.Services["web"].EnvVars[1].Key)
				assert.True(t, ac.Services["web"].EnvVars[1].Required)
				assert.True(t, ac.Services["web"].EnvVars[1].Sensitive)
				assert.Equal(t, "TLS certificate contents", ac.Services["web"].EnvVars[1].Description)
			}
		})
	}
}

func TestParseAppConfigWithAddons(t *testing.T) {
	t.Run("parse addon with variant", func(t *testing.T) {
		config := `
name = "my-app"

[addons.miren-postgresql]
variant = "small"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac.Addons)
		require.Contains(t, ac.Addons, "miren-postgresql")
		assert.Equal(t, "small", ac.Addons["miren-postgresql"].Variant)
	})

	t.Run("parse multiple addons", func(t *testing.T) {
		config := `
name = "my-app"

[addons.miren-postgresql]
variant = "small"

[addons.miren-redis]
variant = "shared"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.Len(t, ac.Addons, 2)
		assert.Equal(t, "small", ac.Addons["miren-postgresql"].Variant)
		assert.Equal(t, "shared", ac.Addons["miren-redis"].Variant)
	})

	t.Run("parse addon without variant uses default", func(t *testing.T) {
		config := `
name = "my-app"

[addons.miren-postgresql]
variant = ""
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac.Addons)
		require.Contains(t, ac.Addons, "miren-postgresql")
		assert.Equal(t, "", ac.Addons["miren-postgresql"].Variant)
	})

	t.Run("no addons section", func(t *testing.T) {
		config := `
name = "my-app"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.Nil(t, ac.Addons)
	})
}

func TestParseAppConfigWithPorts(t *testing.T) {
	t.Run("multi-port parsing", func(t *testing.T) {
		config := `
name = "irc-app"

[services.irc]
command = "./ircd"

[[services.irc.ports]]
port = 6667
name = "irc"
type = "tcp"

[[services.irc.ports]]
port = 6697
name = "irc-tls"
type = "tcp"
node_port = 6697

[services.irc.concurrency]
mode = "fixed"
num_instances = 1
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac)

		ircSvc := ac.Services["irc"]
		require.NotNil(t, ircSvc)
		require.Len(t, ircSvc.Ports, 2)

		assert.Equal(t, 6667, ircSvc.Ports[0].Port)
		assert.Equal(t, "irc", ircSvc.Ports[0].Name)
		assert.Equal(t, "tcp", ircSvc.Ports[0].Type)
		assert.Equal(t, 0, ircSvc.Ports[0].NodePort)

		assert.Equal(t, 6697, ircSvc.Ports[1].Port)
		assert.Equal(t, "irc-tls", ircSvc.Ports[1].Name)
		assert.Equal(t, "tcp", ircSvc.Ports[1].Type)
		assert.Equal(t, 6697, ircSvc.Ports[1].NodePort)
	})

	t.Run("port with udp type", func(t *testing.T) {
		config := `
name = "test-app"

[services.dns]
command = "./dns-server"

[[services.dns.ports]]
port = 53
name = "dns-udp"
type = "udp"

[[services.dns.ports]]
port = 53
name = "dns-tcp"
type = "tcp"

[services.dns.concurrency]
mode = "fixed"
num_instances = 1
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac)

		dnsSvc := ac.Services["dns"]
		require.Len(t, dnsSvc.Ports, 2)
		assert.Equal(t, "udp", dnsSvc.Ports[0].Type)
		assert.Equal(t, "tcp", dnsSvc.Ports[1].Type)
	})
}

func TestValidatePortsConfig(t *testing.T) {
	t.Run("mutual exclusion with scalar port", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
port = 8080

[[services.web.ports]]
port = 8080
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both 'ports' array and scalar port/port_name/port_type fields")
	})

	t.Run("mutual exclusion with scalar port_name", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
port_name = "http"

[[services.web.ports]]
port = 8080
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both 'ports' array and scalar port/port_name/port_type fields")
	})

	t.Run("mutual exclusion with scalar port_type", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
port_type = "http"

[[services.web.ports]]
port = 8080
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both 'ports' array and scalar port/port_name/port_type fields")
	})

	t.Run("mutual exclusion with negative scalar port", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
port = -1

[[services.web.ports]]
port = 8080
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both 'ports' array and scalar port/port_name/port_type fields")
	})

	t.Run("port out of range", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 70000
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "port must be between 1 and 65535")
	})

	t.Run("port zero", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 0
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "port must be between 1 and 65535")
	})

	t.Run("missing name", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("invalid type", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
name = "http"
type = "sctp"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `type must be "http", "tcp", or "udp"`)
	})

	t.Run("duplicate port name", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
name = "http"

[[services.web.ports]]
port = 8443
name = "http"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `duplicate port name "http"`)
	})

	t.Run("duplicate port number same type", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
name = "http"
type = "tcp"

[[services.web.ports]]
port = 8080
name = "https"
type = "tcp"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate port number 8080")
	})

	t.Run("duplicate port http type treated as tcp protocol", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
name = "http"
type = "http"

[[services.web.ports]]
port = 8080
name = "also-http"
type = "tcp"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate port number 8080")
	})

	t.Run("same port different type is valid", func(t *testing.T) {
		config := `
name = "test-app"

[[services.dns.ports]]
port = 53
name = "dns-udp"
type = "udp"

[[services.dns.ports]]
port = 53
name = "dns-tcp"
type = "tcp"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac)
		require.Len(t, ac.Services["dns"].Ports, 2)
	})

	t.Run("valid multi-port config", func(t *testing.T) {
		config := `
name = "test-app"

[[services.web.ports]]
port = 8080
name = "http"
type = "http"

[[services.web.ports]]
port = 8443
name = "https"
type = "http"
node_port = 443
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac)
		require.Len(t, ac.Services["web"].Ports, 2)
	})
}

func TestRejectUnknownFields(t *testing.T) {
	t.Run("unknown top-level field", func(t *testing.T) {
		config := `
name = "test-app"
unknown_field = "value"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
		assert.Contains(t, err.Error(), "unknown_field")
	})

	t.Run("size instead of size_gb in disk config", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
mount_path = "/data"
size = 20
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
		assert.Contains(t, err.Error(), `did you mean "size_gb"`)
	})

	t.Run("unknown field in service config", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
command = "server"
bogus = true
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
		assert.Contains(t, err.Error(), "bogus")
	})

	// A newer CLI may send fields that an older server doesn't recognize.
	// The server must reject the config rather than silently dropping
	// services.
	t.Run("unknown top-level section rejects cleanly (version skew)", func(t *testing.T) {
		config := `
name = "test-app"

[services.web]
command = "bin/server"

[services.valkey]
image = "oci.miren.cloud/valkey:8"

[future_feature]
setting = "value"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
		assert.Contains(t, err.Error(), "future_feature")
	})

	t.Run("valid config still works", func(t *testing.T) {
		config := `
name = "test-app"

[services.database.concurrency]
mode = "fixed"
num_instances = 1

[[services.database.disks]]
name = "data"
mount_path = "/data"
size_gb = 20
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		require.NotNil(t, ac)
		assert.Equal(t, 20, ac.Services["database"].Disks[0].SizeGB)
	})
}

func TestAliasLineNumbers(t *testing.T) {
	config := `name = "test-app"

[aliases]
console = "app run bin/rails console"
tail = "logs app -f"
"x console" = "app exec -i bin/rails console"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	require.NoError(t, os.WriteFile(path, []byte(config), 0644))

	lines := AliasLineNumbers(path)
	require.NotNil(t, lines)

	assert.Equal(t, 4, lines["console"])
	assert.Equal(t, 5, lines["tail"])
	assert.Equal(t, 6, lines["x console"])
}

func TestServicePortTimeoutParsing(t *testing.T) {
	config := `name = "test-app"

[services.web]
command = "bin/start"
port = 4000
port_timeout = "120s"

[services.worker]
command = "bin/worker"
`
	ac, err := Parse([]byte(config))
	require.NoError(t, err)
	require.NotNil(t, ac.Services["web"])
	assert.Equal(t, "120s", ac.Services["web"].PortTimeout)

	require.NotNil(t, ac.Services["worker"])
	assert.Empty(t, ac.Services["worker"].PortTimeout, "unspecified field should be empty, not defaulted at parse time")
}

func TestServicePortTimeoutValidation(t *testing.T) {
	t.Run("invalid duration is rejected at parse time", func(t *testing.T) {
		// "120" is missing a unit suffix — the kind of typo we want to catch
		// at deploy time rather than silently falling back to the 15s default.
		config := `name = "test-app"

[services.web]
command = "bin/start"
port = 4000
port_timeout = "120"
`
		_, err := Parse([]byte(config))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `service web: invalid port_timeout "120"`)
	})

	t.Run("valid duration parses cleanly", func(t *testing.T) {
		config := `name = "test-app"

[services.web]
command = "bin/start"
port = 4000
port_timeout = "2m"
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.Equal(t, "2m", ac.Services["web"].PortTimeout)
	})
}

func TestParseTasks(t *testing.T) {
	config := `
name = "test-app"
web = false

[tasks.migrate]
command = "bin/rails db:migrate"
trigger = "deploy"
timeout = "10m"
retries = 2

[tasks.cleanup]
command = "bin/cleanup-sessions"
trigger = "schedule"
every = "6h"

[tasks.reports]
command = "bin/weekly-report"
trigger = "schedule"
schedule = "Mon *-*-* 09:00:00"

[tasks.reindex]
command = "bin/reindex"
max_concurrent = 4

[[tasks.reindex.env]]
key = "BATCH_SIZE"
value = "500"
`
	ac, err := Parse([]byte(config))
	require.NoError(t, err)
	require.Len(t, ac.Tasks, 4)

	migrate := ac.Tasks["migrate"]
	assert.Equal(t, "bin/rails db:migrate", migrate.Command)
	assert.Equal(t, TriggerDeploy, migrate.ResolvedTrigger())
	assert.Equal(t, "10m", migrate.Timeout)
	assert.Equal(t, 2, migrate.Retries)

	// trigger defaults to manual, max_concurrent to 1
	reindex := ac.Tasks["reindex"]
	assert.Equal(t, TriggerManual, reindex.ResolvedTrigger())
	assert.Equal(t, 4, reindex.ResolvedMaxConcurrent())
	require.Len(t, reindex.EnvVars, 1)
	assert.Equal(t, "BATCH_SIZE", reindex.EnvVars[0].Key)

	assert.Equal(t, DefaultTaskMaxConcurrent, migrate.ResolvedMaxConcurrent())

	// every is sugar: it desugars to the same mechanism schedule uses.
	cleanup, err := ac.Tasks["cleanup"].ResolvedSchedule()
	require.NoError(t, err)
	assert.Equal(t, "*-*-* 00/6:00:00", cleanup)

	reports, err := ac.Tasks["reports"].ResolvedSchedule()
	require.NoError(t, err)
	assert.Equal(t, "Mon *-*-* 09:00:00", reports)

	// A non-scheduled task has no schedule at all.
	none, err := ac.Tasks["reindex"].ResolvedSchedule()
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestWantsWeb(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		wantWeb      bool
		wantExplicit bool
	}{
		{
			name:         "unset preserves the historical default",
			config:       "name = \"a\"\n",
			wantWeb:      true,
			wantExplicit: false,
		},
		{
			name:         "explicit false opts out",
			config:       "name = \"a\"\nweb = false\n",
			wantWeb:      false,
			wantExplicit: true,
		},
		{
			name:         "explicit true is still explicit",
			config:       "name = \"a\"\nweb = true\n",
			wantWeb:      true,
			wantExplicit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, err := Parse([]byte(tt.config))
			require.NoError(t, err)

			want, explicit := ac.WantsWeb()
			assert.Equal(t, tt.wantWeb, want)
			assert.Equal(t, tt.wantExplicit, explicit)
		})
	}
}

func TestWantsWebNilConfig(t *testing.T) {
	var ac *AppConfig
	want, explicit := ac.WantsWeb()
	assert.True(t, want)
	assert.False(t, explicit)
}

func TestTaskValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "command is required",
			config:  "name = \"a\"\n[tasks.t]\ntrigger = \"manual\"\n",
			wantErr: "command is required",
		},
		{
			name:    "invalid trigger",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"whenever\"\n",
			wantErr: "invalid trigger",
		},
		{
			name:    "every and schedule are mutually exclusive",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nevery = \"6h\"\nschedule = \"daily\"\n",
			wantErr: "cannot set both",
		},
		{
			name:    "schedule trigger needs one of them",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\n",
			wantErr: "requires either",
		},
		{
			name:    "every without a schedule trigger is inert",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\nevery = \"6h\"\n",
			wantErr: "has no effect without",
		},
		{
			name:    "schedule without a schedule trigger is inert",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"deploy\"\nschedule = \"daily\"\n",
			wantErr: "has no effect without",
		},
		{
			name:    "unparseable every",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nevery = \"soon\"\n",
			wantErr: "invalid every",
		},
		{
			name:    "every must tile a day",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nevery = \"7h\"\n",
			wantErr: "does not divide a day evenly",
		},
		{
			name:    "unparseable schedule",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nschedule = \"0 9 * * 1\"\n",
			wantErr: "invalid calendar expression",
		},
		{
			name:    "invalid timeout",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntimeout = \"10\"\n",
			wantErr: "invalid timeout",
		},
		{
			name:    "negative timeout",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntimeout = \"-5m\"\n",
			wantErr: "must not be negative",
		},
		{
			name:    "negative retries",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"deploy\"\nretries = -1\n",
			wantErr: "retries must be non-negative",
		},
		{
			name:    "retries on a manual task",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\nretries = 2\n",
			wantErr: "no effect on a manually-triggered task",
		},
		{
			name:    "negative max_concurrent",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\nmax_concurrent = -1\n",
			wantErr: "max_concurrent must be at least 1",
		},
		{
			name:    "task env key is required",
			config:  "name = \"a\"\n[tasks.t]\ncommand = \"x\"\n[[tasks.t.env]]\nvalue = \"v\"\n",
			wantErr: "env[0] key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.config))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTaskValidationAccepts(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"timeout 0 means unbounded", "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntimeout = \"0s\"\n"},
		{"retries on a scheduled task", "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nevery = \"1h\"\nretries = 3\n"},
		{"keyword schedule", "name = \"a\"\n[tasks.t]\ncommand = \"x\"\ntrigger = \"schedule\"\nschedule = \"daily\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.config))
			assert.NoError(t, err)
		})
	}
}

// A task's schema has no ports, concurrency, image, or disks. They must be
// rejected as unknown fields rather than accepted-and-ignored, and the
// "did you mean?" machinery must not advertise them.
func TestTaskRejectsServiceOnlyFields(t *testing.T) {
	for _, field := range []string{
		"port = 8080",
		"ports = []",
		"image = \"alpine\"",
		"[tasks.t.concurrency]\nmode = \"auto\"",
		"[[tasks.t.disks]]\nname = \"d\"",
	} {
		t.Run(field, func(t *testing.T) {
			config := "name = \"a\"\n[tasks.t]\ncommand = \"x\"\n" + field + "\n"
			_, err := Parse([]byte(config))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown field")
		})
	}
}

func TestTaskUnknownFieldSuggestsTaskField(t *testing.T) {
	config := `
name = "test-app"

[tasks.migrate]
comand = "bin/migrate"
`
	_, err := Parse([]byte(config))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
	assert.Contains(t, err.Error(), `did you mean "command"`)
}

// A SQLite database allows one writer, so a service holding it must run a
// single fixed instance. Catching that here is the difference between a failed
// deploy that says why and a green deploy with no database.
func TestValidateSingleWriterAddonRequiresFixedSingleInstance(t *testing.T) {
	t.Run("fixed single instance is accepted", func(t *testing.T) {
		_, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[addons.miren-sqlite]
variant = "standard"
`))
		require.NoError(t, err)
	})

	t.Run("autoscaling service is rejected", func(t *testing.T) {
		_, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10

[addons.miren-sqlite]
variant = "standard"
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "allows one writer")
	})

	t.Run("more than one instance is rejected", func(t *testing.T) {
		_, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 3

[addons.miren-sqlite]
variant = "standard"
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "num_instances = 1")
	})

	// The scoping knob is what lets the rest of the app scale.
	t.Run("services narrows the constraint", func(t *testing.T) {
		ac, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[services.worker.concurrency]
mode = "fixed"
num_instances = 3

[addons.miren-sqlite]
variant = "standard"
services = ["web"]
`))
		require.NoError(t, err, "worker should be free to scale when it is not named")
		assert.Equal(t, []string{"web"}, ac.Addons["miren-sqlite"].Services)
	})

	// An addon with no single-writer storage constrains nothing.
	t.Run("other addons are unaffected", func(t *testing.T) {
		_, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10

[addons.miren-postgresql]
variant = "small"
`))
		require.NoError(t, err)
	})
}

// provider = "sqlite" is no longer something a user writes; the addon supplies
// the database and attaches its own storage.
func TestValidateSqliteDiskProviderRejected(t *testing.T) {
	_, err := Parse([]byte(`
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[[services.web.disks]]
name = "state"
provider = "sqlite"
mount_path = "/data"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `must be "miren" or "local"`)
}
