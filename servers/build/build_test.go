package build

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
)

func TestSanitizeNameForID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple lowercase", "myapp", "myapp"},
		{"uppercase converted", "MyApp", "myapp"},
		{"spaces to hyphens", "my app", "my-app"},
		{"underscores to hyphens", "my_app", "my-app"},
		{"slashes to hyphens", "my/app", "my-app"},
		{"colons to hyphens", "my:app", "my-app"},
		{"multiple separators collapsed", "my--app", "my-app"},
		{"mixed separators", "My App_Name/Test:123", "my-app-name-test-123"},
		{"leading separator stripped", "/myapp", "myapp"},
		{"trailing separator stripped", "myapp/", "myapp"},
		{"numbers preserved", "app123", "app123"},
		{"special chars removed", "my@app#name!", "myappname"},
		{"empty string", "", ""},
		{"only spaces", "   ", ""},
		{"only special chars", "@#$%", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeNameForID(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildVariablesFromAppConfig(t *testing.T) {
	tests := []struct {
		name          string
		appConfig     *appconfig.AppConfig
		wantVariables []core_v1alpha.Variable
	}{
		{
			name:          "nil app config",
			appConfig:     nil,
			wantVariables: nil,
		},
		{
			name: "empty env vars",
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{},
			},
			wantVariables: nil,
		},
		{
			name: "single env var",
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
				},
			},
			wantVariables: []core_v1alpha.Variable{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
			},
		},
		{
			name: "multiple env vars",
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
					{Key: "API_KEY", Value: "secret123"},
					{Key: "PORT", Value: "8080"},
				},
			},
			wantVariables: []core_v1alpha.Variable{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
				{Key: "API_KEY", Value: "secret123"},
				{Key: "PORT", Value: "8080"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildVariablesFromAppConfig(tt.appConfig)
			if tt.wantVariables == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Len(t, result, len(tt.wantVariables))
				for i, want := range tt.wantVariables {
					assert.Equal(t, want.Key, result[i].Key, "variable %d key mismatch", i)
					assert.Equal(t, want.Value, result[i].Value, "variable %d value mismatch", i)
				}
			}
		})
	}
}

func TestBuildServicesConfig(t *testing.T) {
	tests := []struct {
		name             string
		appConfig        *appconfig.AppConfig
		procfileServices map[string]string
		validateServices func(t *testing.T, services []core_v1alpha.Services)
	}{
		{
			name: "service with only concurrency config (no command) - uptime-kuma case",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						// No Command field - relies on container default
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1, "should have one service")
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "fixed", services[0].ServiceConcurrency.Mode)
				assert.Equal(t, int64(1), services[0].ServiceConcurrency.NumInstances)
			},
		},
		{
			name: "service with both command and concurrency",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Command: "node server.js",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
							ScaleDownDelay:      "15m",
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "auto", services[0].ServiceConcurrency.Mode)
				assert.Equal(t, int64(10), services[0].ServiceConcurrency.RequestsPerInstance)
				assert.Equal(t, "15m", services[0].ServiceConcurrency.ScaleDownDelay)
			},
		},
		{
			name:      "procfile only - gets default concurrency",
			appConfig: nil,
			procfileServices: map[string]string{
				"web": "npm start",
			},
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				// Web service should get auto mode defaults
				assert.Equal(t, "auto", services[0].ServiceConcurrency.Mode)
				assert.Equal(t, int64(10), services[0].ServiceConcurrency.RequestsPerInstance)
				assert.Equal(t, "15m", services[0].ServiceConcurrency.ScaleDownDelay)
			},
		},
		{
			name: "multiple services with mixed configs",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						// Only concurrency, no command
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
					"worker": {
						Command: "node worker.js",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 2,
						},
					},
				},
			},
			procfileServices: map[string]string{
				"cron": "node cron.js",
			},
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 3, "should have three services")

				// Find each service and validate
				serviceMap := make(map[string]core_v1alpha.Services)
				for _, svc := range services {
					serviceMap[svc.Name] = svc
				}

				// Web service
				require.Contains(t, serviceMap, "web")
				assert.Equal(t, "fixed", serviceMap["web"].ServiceConcurrency.Mode)
				assert.Equal(t, int64(1), serviceMap["web"].ServiceConcurrency.NumInstances)

				// Worker service
				require.Contains(t, serviceMap, "worker")
				assert.Equal(t, "fixed", serviceMap["worker"].ServiceConcurrency.Mode)
				assert.Equal(t, int64(2), serviceMap["worker"].ServiceConcurrency.NumInstances)

				// Cron service (from procfile, gets default fixed mode)
				require.Contains(t, serviceMap, "cron")
				assert.Equal(t, "fixed", serviceMap["cron"].ServiceConcurrency.Mode)
				assert.Equal(t, int64(1), serviceMap["cron"].ServiceConcurrency.NumInstances)
			},
		},
		{
			name: "app config command overrides procfile",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Command: "npm run prod",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 20,
							ScaleDownDelay:      "5m",
						},
					},
				},
			},
			procfileServices: map[string]string{
				"web": "npm start",
			},
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				// Should get app config concurrency, not defaults
				assert.Equal(t, "auto", services[0].ServiceConcurrency.Mode)
				assert.Equal(t, int64(20), services[0].ServiceConcurrency.RequestsPerInstance)
				assert.Equal(t, "5m", services[0].ServiceConcurrency.ScaleDownDelay)
			},
		},
		{
			name: "service with command but no concurrency - empty concurrency",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"worker": {
						Command: "node worker.js",
						// No concurrency specified - ResolveDefaults skips existing services
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "worker", services[0].Name)
				// No concurrency will be empty since ResolveDefaults skips existing services
				assert.Equal(t, "", services[0].ServiceConcurrency.Mode)
				assert.Equal(t, int64(0), services[0].ServiceConcurrency.NumInstances)
			},
		},
		{
			name: "service with custom image",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"postgres": {
						Image: "oci.miren.cloud/postgres:15",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "postgres", services[0].Name)
				assert.Equal(t, "oci.miren.cloud/postgres:15", services[0].Image)
			},
		},
		{
			name: "service without custom image",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "", services[0].Image, "image should be empty when not specified")
			},
		},
		{
			name: "multiple services with mixed image configs",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"postgres": {
						Image: "oci.miren.cloud/postgres:15",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
					"redis": {
						Image: "oci.miren.cloud/redis:7",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
					"web": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 3)

				serviceMap := make(map[string]core_v1alpha.Services)
				for _, svc := range services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "postgres")
				assert.Equal(t, "oci.miren.cloud/postgres:15", serviceMap["postgres"].Image)

				require.Contains(t, serviceMap, "redis")
				assert.Equal(t, "oci.miren.cloud/redis:7", serviceMap["redis"].Image)

				require.Contains(t, serviceMap, "web")
				assert.Equal(t, "", serviceMap["web"].Image, "web service should not have custom image")
			},
		},
		{
			name: "service with disk configuration",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"postgres": {
						Image: "oci.miren.cloud/postgres:15",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Disks: []appconfig.DiskConfig{
							{
								Name:         "postgres-data",
								MountPath:    "/var/lib/postgresql/data",
								SizeGB:       100,
								Filesystem:   "ext4",
								LeaseTimeout: "5m",
							},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				svc := services[0]
				assert.Equal(t, "postgres", svc.Name)
				assert.Equal(t, "oci.miren.cloud/postgres:15", svc.Image)

				require.Len(t, svc.Disks, 1, "should have one disk")
				disk := svc.Disks[0]
				assert.Equal(t, "postgres-data", disk.Name)
				assert.Equal(t, "/var/lib/postgresql/data", disk.MountPath)
				assert.Equal(t, int64(100), disk.SizeGb)
				assert.Equal(t, "ext4", disk.Filesystem)
				assert.Equal(t, "5m", disk.LeaseTimeout)
				assert.False(t, disk.ReadOnly)
			},
		},
		{
			name: "service with multiple disks",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"database": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 2,
						},
						Disks: []appconfig.DiskConfig{
							{
								Name:       "db-data",
								MountPath:  "/data",
								SizeGB:     200,
								Filesystem: "ext4",
							},
							{
								Name:       "db-wal",
								MountPath:  "/wal",
								SizeGB:     50,
								Filesystem: "xfs",
								ReadOnly:   false,
							},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				svc := services[0]
				assert.Equal(t, "database", svc.Name)

				require.Len(t, svc.Disks, 2, "should have two disks")

				disk1 := svc.Disks[0]
				assert.Equal(t, "db-data", disk1.Name)
				assert.Equal(t, "/data", disk1.MountPath)
				assert.Equal(t, int64(200), disk1.SizeGb)
				assert.Equal(t, "ext4", disk1.Filesystem)

				disk2 := svc.Disks[1]
				assert.Equal(t, "db-wal", disk2.Name)
				assert.Equal(t, "/wal", disk2.MountPath)
				assert.Equal(t, int64(50), disk2.SizeGb)
				assert.Equal(t, "xfs", disk2.Filesystem)
			},
		},
		{
			name: "service with read-only disk",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"reader": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Disks: []appconfig.DiskConfig{
							{
								Name:      "shared-data",
								MountPath: "/data",
								ReadOnly:  true,
								SizeGB:    50,
							},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				svc := services[0]

				require.Len(t, svc.Disks, 1)
				disk := svc.Disks[0]
				assert.Equal(t, "shared-data", disk.Name)
				assert.True(t, disk.ReadOnly, "disk should be read-only")
			},
		},
		{
			name: "service with environment variables",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Command: "npm start",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
						},
						EnvVars: []appconfig.AppEnvVar{
							{Key: "NODE_ENV", Value: "production"},
							{Key: "PORT", Value: "3000"},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 1)
				svc := services[0]
				assert.Equal(t, "web", svc.Name)

				require.Len(t, svc.Env, 2, "should have two environment variables")
				assert.Equal(t, "NODE_ENV", svc.Env[0].Key)
				assert.Equal(t, "production", svc.Env[0].Value)
				assert.Equal(t, "PORT", svc.Env[1].Key)
				assert.Equal(t, "3000", svc.Env[1].Value)
			},
		},
		{
			name: "multiple services with different environment variables",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
						},
						EnvVars: []appconfig.AppEnvVar{
							{Key: "NODE_ENV", Value: "production"},
						},
					},
					"worker": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 2,
						},
						EnvVars: []appconfig.AppEnvVar{
							{Key: "WORKER_THREADS", Value: "4"},
							{Key: "QUEUE_NAME", Value: "default"},
						},
					},
					"scheduler": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				require.Len(t, services, 3)

				serviceMap := make(map[string]core_v1alpha.Services)
				for _, svc := range services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "web")
				webSvc := serviceMap["web"]
				require.Len(t, webSvc.Env, 1)
				assert.Equal(t, "NODE_ENV", webSvc.Env[0].Key)
				assert.Equal(t, "production", webSvc.Env[0].Value)

				require.Contains(t, serviceMap, "worker")
				workerSvc := serviceMap["worker"]
				require.Len(t, workerSvc.Env, 2)
				assert.Equal(t, "WORKER_THREADS", workerSvc.Env[0].Key)
				assert.Equal(t, "4", workerSvc.Env[0].Value)
				assert.Equal(t, "QUEUE_NAME", workerSvc.Env[1].Key)
				assert.Equal(t, "default", workerSvc.Env[1].Value)

				require.Contains(t, serviceMap, "scheduler")
				schedulerSvc := serviceMap["scheduler"]
				assert.Len(t, schedulerSvc.Env, 0, "scheduler should have no env vars")
			},
		},
		{
			name:             "no config no procfile",
			appConfig:        nil,
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				assert.Len(t, services, 0, "should have no services")
			},
		},
		{
			name: "empty app config",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.Services) {
				assert.Len(t, services, 0, "should have no services")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services := buildServicesConfig(tt.appConfig, tt.procfileServices)
			tt.validateServices(t, services)
		})
	}
}

func TestMergeVariablesFromAppConfig(t *testing.T) {
	tests := []struct {
		name         string
		existingVars []core_v1alpha.Variable
		appConfig    *appconfig.AppConfig
		wantVars     []core_v1alpha.Variable
	}{
		{
			name: "preserve existing vars when app.toml has no env section",
			existingVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
			},
			appConfig: nil,
			wantVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
			},
		},
		{
			name: "preserve existing vars when app.toml has empty env section",
			existingVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
			},
		},
		{
			name: "manual vars persist when removed from app.toml",
			existingVars: []core_v1alpha.Variable{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "CONFIG_VAR", Value: "config_value", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "config vars removed when removed from app.toml",
			existingVars: []core_v1alpha.Variable{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
				{Key: "CONFIG_VAR_2", Value: "value2", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "CONFIG_VAR_1", Value: "value1"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
		},
		{
			name: "app.toml vars override config vars per-key",
			existingVars: []core_v1alpha.Variable{
				{Key: "VAR1", Value: "old_value", Source: "config"},
				{Key: "VAR2", Value: "keep_value", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "VAR1", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "VAR1", Value: "new_value", Source: "config"},
				{Key: "VAR2", Value: "keep_value", Source: "manual"},
			},
		},
		{
			name: "backward compatibility - empty source treated as config",
			existingVars: []core_v1alpha.Variable{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "manual var shadows config var with same key",
			existingVars: []core_v1alpha.Variable{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "DATABASE_URL", Value: "from_config"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				// Manual wins - user intent takes precedence over config
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
		},
		{
			name: "config cannot override existing manual var",
			existingVars: []core_v1alpha.Variable{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "debug", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "SECRET", Value: "default_secret"}, // Should NOT override manual
					{Key: "LOG_LEVEL", Value: "info"},        // Should override config
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "SECRET", Value: "user_secret", Source: "manual"}, // Manual preserved
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},     // Config updated
			},
		},
		{
			name: "complex mix of manual and config vars",
			existingVars: []core_v1alpha.Variable{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1", Source: "config"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_2", Value: "c2", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "CONFIG_1", Value: "c1_updated"},
					{Key: "CONFIG_3", Value: "c3"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
		},
		{
			name:         "handle nil existing vars with no app config",
			existingVars: nil,
			appConfig:    nil,
			wantVars:     nil,
		},
		{
			name:         "handle empty existing vars with no app config",
			existingVars: []core_v1alpha.Variable{},
			appConfig:    nil,
			wantVars:     []core_v1alpha.Variable{},
		},
		{
			name:         "set new vars when there are no existing vars",
			existingVars: nil,
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeVariablesFromAppConfig(tt.existingVars, tt.appConfig)
			if tt.wantVars == nil {
				assert.Nil(t, result)
			} else {
				// Sort both slices by key for consistent comparison
				sortVarsByKey := func(vars []core_v1alpha.Variable) {
					sort.Slice(vars, func(i, j int) bool {
						return vars[i].Key < vars[j].Key
					})
				}
				sortVarsByKey(result)
				wantSorted := make([]core_v1alpha.Variable, len(tt.wantVars))
				copy(wantSorted, tt.wantVars)
				sortVarsByKey(wantSorted)

				require.Equal(t, len(wantSorted), len(result), "variable count mismatch")
				for i, want := range wantSorted {
					assert.Equal(t, want.Key, result[i].Key, "variable %d key mismatch", i)
					assert.Equal(t, want.Value, result[i].Value, "variable %d value mismatch", i)
					assert.Equal(t, want.Source, result[i].Source, "variable %d source mismatch", i)
				}
			}
		})
	}
}

func TestMergeServiceEnvVars(t *testing.T) {
	tests := []struct {
		name         string
		existingEnvs []core_v1alpha.Env
		newEnvs      []core_v1alpha.Env
		wantEnvs     []core_v1alpha.Env
	}{
		{
			name: "manual vars persist when removed from app.toml",
			existingEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "CONFIG_VAR", Value: "config_value", Source: "config"},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "config vars removed when removed from app.toml",
			existingEnvs: []core_v1alpha.Env{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
				{Key: "CONFIG_VAR_2", Value: "value2", Source: "config"},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
		},
		{
			name: "backward compatibility - empty source treated as config",
			existingEnvs: []core_v1alpha.Env{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "manual var shadows config var with same key",
			existingEnvs: []core_v1alpha.Env{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "DATABASE_URL", Value: "from_config", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				// Manual wins - user intent takes precedence over config
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
		},
		{
			name: "config cannot override existing manual var",
			existingEnvs: []core_v1alpha.Env{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "debug", Source: "config"},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "SECRET", Value: "default_secret", Source: "config"}, // Should NOT override manual
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},        // Should override config
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "SECRET", Value: "user_secret", Source: "manual"}, // Manual preserved
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},     // Config updated
			},
		},
		{
			name: "complex mix of manual and config vars",
			existingEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1", Source: "config"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_2", Value: "c2", Source: "config"},
			},
			newEnvs: []core_v1alpha.Env{
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
		},
		{
			name:         "nil existing envs",
			existingEnvs: nil,
			newEnvs: []core_v1alpha.Env{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.Env{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "nil new envs preserves manual vars",
			existingEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
			},
			newEnvs: nil,
			wantEnvs: []core_v1alpha.Env{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeServiceEnvVars(tt.existingEnvs, tt.newEnvs)

			// Sort both slices by key for consistent comparison
			sortEnvsByKey := func(envs []core_v1alpha.Env) {
				sort.Slice(envs, func(i, j int) bool {
					return envs[i].Key < envs[j].Key
				})
			}
			sortEnvsByKey(result)
			wantSorted := make([]core_v1alpha.Env, len(tt.wantEnvs))
			copy(wantSorted, tt.wantEnvs)
			sortEnvsByKey(wantSorted)

			require.Equal(t, len(wantSorted), len(result), "env var count mismatch")
			for i, want := range wantSorted {
				assert.Equal(t, want.Key, result[i].Key, "env var %d key mismatch", i)
				assert.Equal(t, want.Value, result[i].Value, "env var %d value mismatch", i)
				assert.Equal(t, want.Source, result[i].Source, "env var %d source mismatch", i)
			}
		})
	}
}

func TestBuildImageCommand(t *testing.T) {
	tests := []struct {
		name       string
		entrypoint []string
		cmd        []string
		want       string
	}{
		{
			name:       "nil entrypoint and cmd",
			entrypoint: nil,
			cmd:        nil,
			want:       "",
		},
		{
			name:       "empty entrypoint and cmd",
			entrypoint: []string{},
			cmd:        []string{},
			want:       "",
		},
		{
			name:       "entrypoint only",
			entrypoint: []string{"node", "server.js"},
			cmd:        nil,
			want:       "node server.js",
		},
		{
			name:       "cmd only",
			entrypoint: nil,
			cmd:        []string{"npm", "start"},
			want:       "npm start",
		},
		{
			name:       "entrypoint and cmd combined",
			entrypoint: []string{"node"},
			cmd:        []string{"server.js"},
			want:       "node server.js",
		},
		{
			name:       "shell form entrypoint",
			entrypoint: []string{"/bin/sh", "-c", "exec node server.js"},
			cmd:        nil,
			want:       "/bin/sh -c \"exec node server.js\"",
		},
		{
			name:       "single element shell command",
			entrypoint: []string{"npm start"},
			cmd:        nil,
			want:       "npm start",
		},
		{
			name:       "arguments with spaces",
			entrypoint: []string{"python"},
			cmd:        []string{"-c", "print('hello world')"},
			want:       "python -c \"print('hello world')\"",
		},
		{
			name:       "complex command",
			entrypoint: []string{"./start.sh"},
			cmd:        []string{"--config", "/etc/myapp/config.yaml"},
			want:       "./start.sh --config /etc/myapp/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildImageCommand(tt.entrypoint, tt.cmd)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildVersionConfig(t *testing.T) {
	tests := []struct {
		name     string
		inputs   ConfigInputs
		validate func(t *testing.T, cfg core_v1alpha.Config)
	}{
		{
			name: "image entrypoint creates web service when no services configured",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node server.js",
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 1, "should have one service")
				assert.Equal(t, "web", cfg.Services[0].Name)
				require.Len(t, cfg.Commands, 1, "should have one command")
				assert.Equal(t, "web", cfg.Commands[0].Service)
				assert.Equal(t, "node server.js", cfg.Commands[0].Command)
				assert.Equal(t, "/app", cfg.StartDirectory)
			},
		},
		{
			name: "image entrypoint creates web service when only worker in procfile",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "npm start",
					WorkingDir: "/myapp",
				},
				AppConfig: nil,
				ProcfileServices: map[string]string{
					"worker": "node worker.js",
				},
				ExistingConfig: core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 2, "should have two services")

				// Find services by name
				serviceMap := make(map[string]core_v1alpha.Services)
				for _, svc := range cfg.Services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "web")
				require.Contains(t, serviceMap, "worker")

				// Find commands by service
				cmdMap := make(map[string]string)
				for _, cmd := range cfg.Commands {
					cmdMap[cmd.Service] = cmd.Command
				}

				assert.Equal(t, "npm start", cmdMap["web"])
				assert.Equal(t, "node worker.js", cmdMap["worker"])
				assert.Equal(t, "/myapp", cfg.StartDirectory)
			},
		},
		{
			name: "procfile web takes precedence over image entrypoint",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node default.js",
					WorkingDir: "/app",
				},
				AppConfig: nil,
				ProcfileServices: map[string]string{
					"web": "npm run production",
				},
				ExistingConfig: core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 1)
				assert.Equal(t, "web", cfg.Services[0].Name)
				require.Len(t, cfg.Commands, 1)
				assert.Equal(t, "npm run production", cfg.Commands[0].Command)
			},
		},
		{
			name: "app config web command takes precedence over image entrypoint",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node default.js",
				},
				AppConfig: &appconfig.AppConfig{
					Services: map[string]*appconfig.ServiceConfig{
						"web": {
							Command: "npm run app-config",
						},
					},
				},
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 1)
				assert.Equal(t, "web", cfg.Services[0].Name)
				require.Len(t, cfg.Commands, 1)
				assert.Equal(t, "npm run app-config", cfg.Commands[0].Command)
			},
		},
		{
			name: "no image entrypoint means no web service when no config",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				assert.Len(t, cfg.Services, 0, "should have no services")
				assert.Len(t, cfg.Commands, 0, "should have no commands")
				assert.Equal(t, "/app", cfg.StartDirectory)
			},
		},
		{
			name: "stack entrypoint is set from build result",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "/app/bin/start",
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				assert.Equal(t, "/app/bin/start", cfg.Entrypoint)
			},
		},
		{
			name: "default start directory is /app when not specified",
			inputs: ConfigInputs{
				BuildResult:      &BuildResult{},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				assert.Equal(t, "/app", cfg.StartDirectory)
			},
		},
		{
			name: "preserves manual env vars from existing config",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{},
				AppConfig:   nil,
				ProcfileServices: map[string]string{
					"web": "npm start",
				},
				ExistingConfig: core_v1alpha.Config{
					Services: []core_v1alpha.Services{
						{
							Name: "web",
							Env: []core_v1alpha.Env{
								{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
								{Key: "CONFIG_VAR", Value: "old_config", Source: "config"},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 1)
				assert.Equal(t, "web", cfg.Services[0].Name)

				// Should preserve manual var
				envMap := make(map[string]core_v1alpha.Env)
				for _, env := range cfg.Services[0].Env {
					envMap[env.Key] = env
				}

				require.Contains(t, envMap, "MANUAL_VAR")
				assert.Equal(t, "manual_value", envMap["MANUAL_VAR"].Value)
				assert.Equal(t, "manual", envMap["MANUAL_VAR"].Source)
			},
		},
		{
			name: "app config with only worker creates web from image command",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Command: "python app.py",
				},
				AppConfig: &appconfig.AppConfig{
					Services: map[string]*appconfig.ServiceConfig{
						"worker": {
							Command: "celery worker",
							Concurrency: &appconfig.ServiceConcurrencyConfig{
								Mode:         "fixed",
								NumInstances: 2,
							},
						},
					},
				},
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 2, "should have two services")

				serviceMap := make(map[string]core_v1alpha.Services)
				for _, svc := range cfg.Services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "web")
				require.Contains(t, serviceMap, "worker")

				cmdMap := make(map[string]string)
				for _, cmd := range cfg.Commands {
					cmdMap[cmd.Service] = cmd.Command
				}

				assert.Equal(t, "python app.py", cmdMap["web"])
				assert.Equal(t, "celery worker", cmdMap["worker"])
			},
		},
		{
			name: "entrypoint and command are combined for web service",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node",
					Command:    "server.js",
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.Config{},
			},
			validate: func(t *testing.T, cfg core_v1alpha.Config) {
				require.Len(t, cfg.Services, 1, "should have one service")
				assert.Equal(t, "web", cfg.Services[0].Name)
				require.Len(t, cfg.Commands, 1, "should have one command")
				assert.Equal(t, "node server.js", cfg.Commands[0].Command)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := buildVersionConfig(tt.inputs)
			tt.validate(t, cfg)
		})
	}
}

func TestExtractWorkingDirFromImageConfig(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantDir    string
	}{
		{
			name: "standard Dockerfile with WORKDIR /app",
			configJSON: `{
				"config": {
					"WorkingDir": "/app",
					"Env": ["PATH=/usr/local/bin:/usr/bin:/bin"],
					"Cmd": ["node", "server.js"]
				}
			}`,
			wantDir: "/app",
		},
		{
			name: "custom working directory",
			configJSON: `{
				"config": {
					"WorkingDir": "/home/myuser/application",
					"User": "myuser"
				}
			}`,
			wantDir: "/home/myuser/application",
		},
		{
			name: "empty working directory",
			configJSON: `{
				"config": {
					"WorkingDir": "",
					"Cmd": ["./start.sh"]
				}
			}`,
			wantDir: "",
		},
		{
			name: "no working directory field",
			configJSON: `{
				"config": {
					"Cmd": ["./start.sh"]
				}
			}`,
			wantDir: "",
		},
		{
			name: "root working directory",
			configJSON: `{
				"config": {
					"WorkingDir": "/",
					"Cmd": ["./app"]
				}
			}`,
			wantDir: "/",
		},
		{
			name: "deeply nested working directory",
			configJSON: `{
				"config": {
					"WorkingDir": "/var/www/html/app/current",
					"Env": ["NODE_ENV=production"]
				}
			}`,
			wantDir: "/var/www/html/app/current",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mimics the parsing logic used in BuildImage for Dockerfile builds
			var imgConfig struct {
				Config struct {
					WorkingDir string `json:"WorkingDir"`
				} `json:"config"`
			}
			err := json.Unmarshal([]byte(tt.configJSON), &imgConfig)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDir, imgConfig.Config.WorkingDir)
		})
	}
}
