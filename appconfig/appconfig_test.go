package appconfig

import (
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
`
		ac, err := Parse([]byte(config))
		require.NoError(t, err)
		assert.NotNil(t, ac)
		require.Len(t, ac.Services["database"].Disks, 2)
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
