package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
)

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
					{Name: "DATABASE_URL", Value: "postgres://localhost/db"},
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
					{Name: "DATABASE_URL", Value: "postgres://localhost/db"},
					{Name: "API_KEY", Value: "secret123"},
					{Name: "PORT", Value: "8080"},
				},
			},
			wantVariables: []core_v1alpha.Variable{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
				{Key: "API_KEY", Value: "secret123"},
				{Key: "PORT", Value: "8080"},
			},
		},
		{
			name: "env var with generator field (ignored)",
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Name: "SECRET_KEY", Value: "default", Generator: "random"},
				},
			},
			wantVariables: []core_v1alpha.Variable{
				{Key: "SECRET_KEY", Value: "default"},
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
						Image: "postgres:15",
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
				assert.Equal(t, "postgres:15", services[0].Image)
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
						Image: "postgres:15",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
					},
					"redis": {
						Image: "redis:7-alpine",
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
				assert.Equal(t, "postgres:15", serviceMap["postgres"].Image)

				require.Contains(t, serviceMap, "redis")
				assert.Equal(t, "redis:7-alpine", serviceMap["redis"].Image)

				require.Contains(t, serviceMap, "web")
				assert.Equal(t, "", serviceMap["web"].Image, "web service should not have custom image")
			},
		},
		{
			name: "service with disk configuration",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"postgres": {
						Image: "postgres:15",
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
				assert.Equal(t, "postgres:15", svc.Image)

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
							{Name: "NODE_ENV", Value: "production"},
							{Name: "PORT", Value: "3000"},
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
							{Name: "NODE_ENV", Value: "production"},
						},
					},
					"worker": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 2,
						},
						EnvVars: []appconfig.AppEnvVar{
							{Name: "WORKER_THREADS", Value: "4"},
							{Name: "QUEUE_NAME", Value: "default"},
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
				{Key: "API_KEY", Value: "secret123"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
			},
			appConfig: nil,
			wantVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db"},
			},
		},
		{
			name: "preserve existing vars when app.toml has empty env section",
			existingVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "API_KEY", Value: "secret123"},
			},
		},
		{
			name: "replace vars when app.toml has new env vars",
			existingVars: []core_v1alpha.Variable{
				{Key: "OLD_VAR", Value: "old_value"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Name: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "NEW_VAR", Value: "new_value"},
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
					{Name: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.Variable{
				{Key: "NEW_VAR", Value: "new_value"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeVariablesFromAppConfig(tt.existingVars, tt.appConfig)
			if tt.wantVars == nil {
				assert.Nil(t, result)
			} else {
				require.Equal(t, len(tt.wantVars), len(result))
				for i, want := range tt.wantVars {
					assert.Equal(t, want.Key, result[i].Key, "variable %d key mismatch", i)
					assert.Equal(t, want.Value, result[i].Value, "variable %d value mismatch", i)
				}
			}
		})
	}
}
