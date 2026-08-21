package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

func TestBuildVariablesFromAppConfig(t *testing.T) {
	tests := []struct {
		name          string
		appConfig     *appconfig.AppConfig
		wantVariables []core_v1alpha.ConfigSpecVariables
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
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
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
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
				{Key: "API_KEY", Value: "secret123", Source: "config"},
				{Key: "PORT", Value: "8080", Source: "config"},
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
		ensureWeb        bool
		webDefault       string
		validateServices func(t *testing.T, services []core_v1alpha.ConfigSpecServices)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1, "should have one service")
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "fixed", services[0].Concurrency.Mode)
				assert.Equal(t, int64(1), services[0].Concurrency.NumInstances)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "auto", services[0].Concurrency.Mode)
				assert.Equal(t, int64(10), services[0].Concurrency.RequestsPerInstance)
				assert.Equal(t, "15m", services[0].Concurrency.ScaleDownDelay)
			},
		},
		{
			name:      "procfile only - gets default concurrency",
			appConfig: nil,
			procfileServices: map[string]string{
				"web": "npm start",
			},
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				// Web service should get auto mode defaults
				assert.Equal(t, "auto", services[0].Concurrency.Mode)
				assert.Equal(t, int64(10), services[0].Concurrency.RequestsPerInstance)
				assert.Equal(t, "15m", services[0].Concurrency.ScaleDownDelay)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 3, "should have three services")

				// Find each service and validate
				serviceMap := make(map[string]core_v1alpha.ConfigSpecServices)
				for _, svc := range services {
					serviceMap[svc.Name] = svc
				}

				// Web service
				require.Contains(t, serviceMap, "web")
				assert.Equal(t, "fixed", serviceMap["web"].Concurrency.Mode)
				assert.Equal(t, int64(1), serviceMap["web"].Concurrency.NumInstances)

				// Worker service
				require.Contains(t, serviceMap, "worker")
				assert.Equal(t, "fixed", serviceMap["worker"].Concurrency.Mode)
				assert.Equal(t, int64(2), serviceMap["worker"].Concurrency.NumInstances)

				// Cron service (from procfile, gets default fixed mode)
				require.Contains(t, serviceMap, "cron")
				assert.Equal(t, "fixed", serviceMap["cron"].Concurrency.Mode)
				assert.Equal(t, int64(1), serviceMap["cron"].Concurrency.NumInstances)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				// Should get app config concurrency, not defaults
				assert.Equal(t, "auto", services[0].Concurrency.Mode)
				assert.Equal(t, int64(20), services[0].Concurrency.RequestsPerInstance)
				assert.Equal(t, "5m", services[0].Concurrency.ScaleDownDelay)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "worker", services[0].Name)
				// No concurrency will be empty since ResolveDefaults skips existing services
				assert.Equal(t, "", services[0].Concurrency.Mode)
				assert.Equal(t, int64(0), services[0].Concurrency.NumInstances)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 3)

				serviceMap := make(map[string]core_v1alpha.ConfigSpecServices)
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
				assert.Equal(t, core_v1alpha.ConfigSpecServicesDisksMIREN, disk.Provider, "default provider should be miren")
			},
		},
		{
			name: "service with local disk provider",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Disks: []appconfig.DiskConfig{
							{
								Name:      "cache",
								MountPath: "/cache",
								Provider:  "local",
							},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				svc := services[0]

				require.Len(t, svc.Disks, 1)
				disk := svc.Disks[0]
				assert.Equal(t, "cache", disk.Name)
				assert.Equal(t, "/cache", disk.MountPath)
				assert.Equal(t, core_v1alpha.ConfigSpecServicesDisksLOCAL, disk.Provider, "local provider should be mapped")
			},
		},
		{
			name: "service with explicit miren disk provider",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"db": {
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Disks: []appconfig.DiskConfig{
							{
								Name:      "data",
								MountPath: "/data",
								SizeGB:    100,
								Provider:  "miren",
							},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				svc := services[0]

				require.Len(t, svc.Disks, 1)
				disk := svc.Disks[0]
				assert.Equal(t, "data", disk.Name)
				assert.Equal(t, core_v1alpha.ConfigSpecServicesDisksMIREN, disk.Provider, "explicit miren provider should be mapped")
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
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
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 3)

				serviceMap := make(map[string]core_v1alpha.ConfigSpecServices)
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
			name: "service with ports array",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"irc": {
						Command: "./ircd",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Ports: []appconfig.PortConfig{
							{Port: 6667, Name: "irc", Type: "tcp"},
							{Port: 6697, Name: "irc-tls", Type: "tcp", NodePort: 6697},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				svc := services[0]
				assert.Equal(t, "irc", svc.Name)

				// Should use ports array, not scalar fields
				require.Len(t, svc.Ports, 2, "should have two ports")
				assert.Equal(t, int64(6667), svc.Ports[0].Port)
				assert.Equal(t, "irc", svc.Ports[0].Name)
				assert.Equal(t, "tcp", svc.Ports[0].Type)
				assert.Equal(t, int64(0), svc.Ports[0].NodePort)

				assert.Equal(t, int64(6697), svc.Ports[1].Port)
				assert.Equal(t, "irc-tls", svc.Ports[1].Name)
				assert.Equal(t, "tcp", svc.Ports[1].Type)
				assert.Equal(t, int64(6697), svc.Ports[1].NodePort)

				// Scalar fields should be zero
				assert.Equal(t, int64(0), svc.Port)
				assert.Equal(t, "", svc.PortName)
				assert.Equal(t, "", svc.PortType)
			},
		},
		{
			name: "service with ports array and udp type",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"dns": {
						Command: "./dns-server",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:         "fixed",
							NumInstances: 1,
						},
						Ports: []appconfig.PortConfig{
							{Port: 53, Name: "dns-udp", Type: "udp"},
							{Port: 53, Name: "dns-tcp", Type: "tcp"},
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				svc := services[0]
				require.Len(t, svc.Ports, 2)
				assert.Equal(t, core_v1alpha.ConfigSpecServicesPortsUDP, svc.Ports[0].Protocol)
				assert.Equal(t, core_v1alpha.ConfigSpecServicesPortsTCP, svc.Ports[1].Protocol)
			},
		},
		{
			name: "service with scalar port (backward compat)",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Port:     8080,
						PortName: "http",
						PortType: "http",
						Concurrency: &appconfig.ServiceConcurrencyConfig{
							Mode:                "auto",
							RequestsPerInstance: 10,
						},
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				svc := services[0]
				assert.Equal(t, int64(8080), svc.Port)
				assert.Equal(t, "http", svc.PortName)
				assert.Equal(t, "http", svc.PortType)
				assert.Len(t, svc.Ports, 0, "ports array should be empty when using scalar fields")
			},
		},
		{
			name:             "no config no procfile",
			appConfig:        nil,
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				assert.Len(t, services, 0, "should have no services")
			},
		},
		{
			name: "empty app config",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				assert.Len(t, services, 0, "should have no services")
			},
		},
		{
			name:             "ensureWeb defaults web when nothing supplies a command",
			appConfig:        nil,
			procfileServices: nil,
			ensureWeb:        true,
			webDefault:       "",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				assert.Empty(t, services[0].Command, "empty default is the image exec-form case")
			},
		},
		{
			name:             "ensureWeb does not override a web command from the Procfile",
			appConfig:        nil,
			procfileServices: map[string]string{"web": "npm start"},
			ensureWeb:        true,
			webDefault:       "should-not-win",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
				assert.Equal(t, "npm start", services[0].Command)
			},
		},
		{
			name: "web = false suppresses the synthesized web service",
			appConfig: &appconfig.AppConfig{
				Web: ptr(false),
				Tasks: map[string]*appconfig.TaskConfig{
					"session": {Command: "/bin/bash"},
				},
			},
			procfileServices: nil,
			ensureWeb:        true,
			webDefault:       "would-have-been-synthesized",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				assert.Empty(t, services, "a task-only app runs nothing between invocations")
			},
		},
		{
			name: "web = false does not remove an explicitly declared web service",
			appConfig: &appconfig.AppConfig{
				Web: ptr(false),
				Services: map[string]*appconfig.ServiceConfig{
					"web": {Command: "bin/server"},
				},
			},
			ensureWeb:  true,
			webDefault: "unused",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "bin/server", services[0].Command,
					"the opt-out suppresses synthesis, it does not delete config the user wrote")
			},
		},
		{
			name: "web = true is the same as leaving it unset",
			appConfig: &appconfig.AppConfig{
				Web: ptr(true),
			},
			ensureWeb:  true,
			webDefault: "bin/server",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 1)
				assert.Equal(t, "web", services[0].Name)
			},
		},
		{
			// The historical default has to survive: an app declaring only a
			// worker still gets a web, or its pool would drain on next deploy.
			name: "unset web still synthesizes alongside a declared worker",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"worker": {Command: "bin/worker"},
				},
			},
			ensureWeb:  true,
			webDefault: "bin/server",
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				names := make([]string, 0, len(services))
				for _, s := range services {
					names = append(names, s.Name)
				}
				assert.ElementsMatch(t, []string{"web", "worker"}, names)
			},
		},
		{
			name: "service with port_timeout copies through to ConfigSpec",
			appConfig: &appconfig.AppConfig{
				Services: map[string]*appconfig.ServiceConfig{
					"web": {
						Command:     "bin/start",
						Port:        4000,
						PortTimeout: "120s",
					},
					"worker": {
						Command: "bin/worker",
					},
				},
			},
			procfileServices: nil,
			validateServices: func(t *testing.T, services []core_v1alpha.ConfigSpecServices) {
				require.Len(t, services, 2)
				byName := make(map[string]core_v1alpha.ConfigSpecServices, len(services))
				for _, s := range services {
					byName[s.Name] = s
				}
				require.Contains(t, byName, "web")
				assert.Equal(t, "120s", byName["web"].PortTimeout)
				require.Contains(t, byName, "worker")
				assert.Empty(t, byName["worker"].PortTimeout, "unset field stays empty so resolvePortWaitTimeout falls back to default")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services := buildServicesConfig(tt.appConfig, tt.procfileServices, tt.ensureWeb, tt.webDefault)
			tt.validateServices(t, services)
		})
	}
}

func TestMergeVariablesFromAppConfig(t *testing.T) {
	tests := []struct {
		name         string
		existingVars []core_v1alpha.ConfigSpecVariables
		appConfig    *appconfig.AppConfig
		wantVars     []core_v1alpha.ConfigSpecVariables
	}{
		{
			name: "preserve existing vars when app.toml has no env section",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
			},
			appConfig: nil,
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "config"},
			},
		},
		{
			name: "preserve existing vars when app.toml has empty env section",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "API_KEY", Value: "secret123", Source: "manual"},
			},
		},
		{
			name: "manual vars persist when removed from app.toml",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "CONFIG_VAR", Value: "config_value", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "config vars removed when removed from app.toml",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
				{Key: "CONFIG_VAR_2", Value: "value2", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "CONFIG_VAR_1", Value: "value1"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
		},
		{
			name: "app.toml vars override config vars per-key",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "VAR1", Value: "old_value", Source: "config"},
				{Key: "VAR2", Value: "keep_value", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "VAR1", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "VAR1", Value: "new_value", Source: "config"},
				{Key: "VAR2", Value: "keep_value", Source: "manual"},
			},
		},
		{
			name: "backward compatibility - empty source preserved as manual",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "manual var shadows config var with same key",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "DATABASE_URL", Value: "from_config"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
		},
		{
			name: "config cannot override existing manual var",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "debug", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "SECRET", Value: "default_secret"},
					{Key: "LOG_LEVEL", Value: "info"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},
			},
		},
		{
			name: "complex mix of manual and config vars",
			existingVars: []core_v1alpha.ConfigSpecVariables{
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
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
		},
		{
			name: "empty source vars preserved when app.toml adds env",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "LEGACY_DB_URL", Value: "postgres://old/db", Source: ""},
				{Key: "LEGACY_API_KEY", Value: "key123", Source: ""},
				{Key: "MANUAL_SECRET", Value: "secret", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_CONFIG_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "LEGACY_DB_URL", Value: "postgres://old/db", Source: ""},
				{Key: "LEGACY_API_KEY", Value: "key123", Source: ""},
				{Key: "MANUAL_SECRET", Value: "secret", Source: "manual"},
				{Key: "NEW_CONFIG_VAR", Value: "new_value", Source: "config"},
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
			existingVars: []core_v1alpha.ConfigSpecVariables{},
			appConfig:    nil,
			wantVars:     []core_v1alpha.ConfigSpecVariables{},
		},
		{
			name:         "set new vars when there are no existing vars",
			existingVars: nil,
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "NEW_VAR", Value: "new_value"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			// Addon bindings are resolved from the association when a version
			// runs, so a build must not copy them into the version it stores.
			// The app still receives them; see ResolveRuntimeConfig.
			name: "addon vars are not carried into a build",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon", Sensitive: true},
				{Key: "PGHOST", Value: "host.addon.app.miren", Source: "addon"},
				{Key: "CONFIG_VAR", Value: "old", Source: "config"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "CONFIG_VAR", Value: "new"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "CONFIG_VAR", Value: "new", Source: "config"},
			},
		},
		{
			// The stored version keeps the app.toml declaration. The addon's
			// value still wins at runtime, because ResolveRuntimeConfig lets a
			// binding displace a config-sourced variable.
			name: "app.toml declaration is stored where an addon var was dropped",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "DATABASE_URL", Value: "postgres://manual/db"},
				},
			},
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://manual/db", Source: "config"},
			},
		},
		{
			name: "operator vars preserved when app.toml has no env section",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
				{Key: "MANUAL_VAR", Value: "val", Source: "manual"},
			},
			appConfig: nil,
			wantVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "MANUAL_VAR", Value: "val", Source: "manual"},
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
				sortVarsByKey := func(vars []core_v1alpha.ConfigSpecVariables) {
					sort.Slice(vars, func(i, j int) bool {
						return vars[i].Key < vars[j].Key
					})
				}
				sortVarsByKey(result)
				wantSorted := make([]core_v1alpha.ConfigSpecVariables, len(tt.wantVars))
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
		existingEnvs []core_v1alpha.ConfigSpecServicesEnv
		newEnvs      []core_v1alpha.ConfigSpecServicesEnv
		wantEnvs     []core_v1alpha.ConfigSpecServicesEnv
	}{
		{
			name: "manual vars persist when removed from app.toml",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "CONFIG_VAR", Value: "config_value", Source: "config"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "config vars removed when removed from app.toml",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
				{Key: "CONFIG_VAR_2", Value: "value2", Source: "config"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "CONFIG_VAR_1", Value: "value1", Source: "config"},
			},
		},
		{
			name: "backward compatibility - empty source preserved as manual",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "OLD_VAR", Value: "old_value", Source: ""},
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "manual var shadows config var with same key",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "from_config", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "manually_set", Source: "manual"},
			},
		},
		{
			name: "config cannot override existing manual var",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "debug", Source: "config"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "SECRET", Value: "default_secret", Source: "config"},
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "SECRET", Value: "user_secret", Source: "manual"},
				{Key: "LOG_LEVEL", Value: "info", Source: "config"},
			},
		},
		{
			name: "complex mix of manual and config vars",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1", Source: "config"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_2", Value: "c2", Source: "config"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_1", Value: "m1", Source: "manual"},
				{Key: "MANUAL_2", Value: "m2", Source: "manual"},
				{Key: "CONFIG_1", Value: "c1_updated", Source: "config"},
				{Key: "CONFIG_3", Value: "c3", Source: "config"},
			},
		},
		{
			name: "empty source vars preserved when app.toml adds env",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "LEGACY_DB_URL", Value: "postgres://old/db", Source: ""},
				{Key: "LEGACY_API_KEY", Value: "key123", Source: ""},
				{Key: "MANUAL_SECRET", Value: "secret", Source: "manual"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "NEW_CONFIG_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "LEGACY_DB_URL", Value: "postgres://old/db", Source: ""},
				{Key: "LEGACY_API_KEY", Value: "key123", Source: ""},
				{Key: "MANUAL_SECRET", Value: "secret", Source: "manual"},
				{Key: "NEW_CONFIG_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name:         "nil existing envs",
			existingEnvs: nil,
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "NEW_VAR", Value: "new_value", Source: "config"},
			},
		},
		{
			name: "nil new envs preserves manual vars",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
			},
			newEnvs: nil,
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
			},
		},
		{
			name: "addon vars persist across deploys",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
				{Key: "CONFIG_VAR", Value: "old", Source: "config"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "CONFIG_VAR", Value: "new", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
				{Key: "CONFIG_VAR", Value: "new", Source: "config"},
			},
		},
		{
			name: "addon vars not overridden by config vars",
			existingEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
			},
			newEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "postgres://config/db", Source: "config"},
			},
			wantEnvs: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeServiceEnvVars(tt.existingEnvs, tt.newEnvs)

			// Sort both slices by key for consistent comparison
			sortEnvsByKey := func(envs []core_v1alpha.ConfigSpecServicesEnv) {
				sort.Slice(envs, func(i, j int) bool {
					return envs[i].Key < envs[j].Key
				})
			}
			sortEnvsByKey(result)
			wantSorted := make([]core_v1alpha.ConfigSpecServicesEnv, len(tt.wantEnvs))
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

func TestBuildVersionConfig(t *testing.T) {
	tests := []struct {
		name     string
		inputs   ConfigInputs
		validate func(t *testing.T, spec core_v1alpha.ConfigSpec)
	}{
		{
			name: "build result entrypoint creates web service when no services configured",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node server.js",
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1, "should have one service")
				assert.Equal(t, "web", spec.Services[0].Name)
				assert.Equal(t, "node server.js", spec.Services[0].Command)
				assert.Equal(t, "/app", spec.StartDirectory)
			},
		},
		{
			name: "build result entrypoint creates web service when only worker in procfile",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "npm start",
					WorkingDir: "/myapp",
				},
				AppConfig: nil,
				ProcfileServices: map[string]string{
					"worker": "node worker.js",
				},
				ExistingConfig: core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 2, "should have two services")

				// Find services by name
				serviceMap := make(map[string]core_v1alpha.ConfigSpecServices)
				for _, svc := range spec.Services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "web")
				require.Contains(t, serviceMap, "worker")

				assert.Equal(t, "npm start", serviceMap["web"].Command)
				assert.Equal(t, "node worker.js", serviceMap["worker"].Command)
				assert.Equal(t, "/myapp", spec.StartDirectory)
			},
		},
		{
			name: "procfile web takes precedence over build result entrypoint",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node default.js",
					WorkingDir: "/app",
				},
				AppConfig: nil,
				ProcfileServices: map[string]string{
					"web": "npm run production",
				},
				ExistingConfig: core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1)
				assert.Equal(t, "web", spec.Services[0].Name)
				assert.Equal(t, "npm run production", spec.Services[0].Command)
			},
		},
		{
			name: "app config web command takes precedence over build result entrypoint",
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
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1)
				assert.Equal(t, "web", spec.Services[0].Name)
				assert.Equal(t, "npm run app-config", spec.Services[0].Command)
			},
		},
		{
			// A build that produced an image but carries no command is the
			// image/Dockerfile case: we still materialize a web service, with an
			// empty command, so the image's own ENTRYPOINT+CMD run in exec form
			// at launch (MIR-1444) instead of the app silently having no service.
			name: "image build with no command still yields an empty-command web service",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1, "web service should materialize so the image entrypoint runs")
				assert.Equal(t, "web", spec.Services[0].Name)
				assert.Empty(t, spec.Services[0].Command, "command stays empty so the image's ENTRYPOINT+CMD run in exec form")
				assert.Equal(t, "/app", spec.StartDirectory)
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
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				assert.Equal(t, "/app/bin/start", spec.Entrypoint)
			},
		},
		{
			name: "default start directory is /app when not specified",
			inputs: ConfigInputs{
				BuildResult:      &BuildResult{},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				assert.Equal(t, "/app", spec.StartDirectory)
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
				ExistingConfig: core_v1alpha.ConfigSpec{
					Services: []core_v1alpha.ConfigSpecServices{
						{
							Name: "web",
							Env: []core_v1alpha.ConfigSpecServicesEnv{
								{Key: "MANUAL_VAR", Value: "manual_value", Source: "manual"},
								{Key: "CONFIG_VAR", Value: "old_config", Source: "config"},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1)
				assert.Equal(t, "web", spec.Services[0].Name)

				// Should preserve manual var
				envMap := make(map[string]core_v1alpha.ConfigSpecServicesEnv)
				for _, env := range spec.Services[0].Env {
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
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 2, "should have two services")

				serviceMap := make(map[string]core_v1alpha.ConfigSpecServices)
				for _, svc := range spec.Services {
					serviceMap[svc.Name] = svc
				}

				require.Contains(t, serviceMap, "web")
				require.Contains(t, serviceMap, "worker")

				assert.Equal(t, "python app.py", serviceMap["web"].Command)
				assert.Equal(t, "celery worker", serviceMap["worker"].Command)
			},
		},
		{
			name: "command takes precedence over entrypoint for web service",
			inputs: ConfigInputs{
				BuildResult: &BuildResult{
					Entrypoint: "node",
					Command:    "server.js",
					WorkingDir: "/app",
				},
				AppConfig:        nil,
				ProcfileServices: nil,
				ExistingConfig:   core_v1alpha.ConfigSpec{},
			},
			validate: func(t *testing.T, spec core_v1alpha.ConfigSpec) {
				require.Len(t, spec.Services, 1, "should have one service")
				assert.Equal(t, "web", spec.Services[0].Name)
				assert.Equal(t, "server.js", spec.Services[0].Command)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := buildVersionConfig(tt.inputs)
			tt.validate(t, spec)
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

// Helper to create a build_v1alpha.EnvironmentVariable for testing
func makeEnvVar(key, value string, sensitive bool) *build_v1alpha.EnvironmentVariable {
	ev := &build_v1alpha.EnvironmentVariable{}
	ev.SetKey(key)
	ev.SetValue(value)
	ev.SetSensitive(sensitive)
	return ev
}

func TestMergeCliEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		existingVars  []core_v1alpha.ConfigSpecVariables
		cliVars       []*build_v1alpha.EnvironmentVariable
		wantVariables []core_v1alpha.ConfigSpecVariables
	}{
		{
			name:          "empty CLI vars returns existing unchanged",
			existingVars:  []core_v1alpha.ConfigSpecVariables{{Key: "FOO", Value: "bar", Source: "config"}},
			cliVars:       nil,
			wantVariables: []core_v1alpha.ConfigSpecVariables{{Key: "FOO", Value: "bar", Source: "config"}},
		},
		{
			name:         "CLI vars added to empty existing",
			existingVars: nil,
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("FOO", "bar", false),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "bar", Sensitive: false, Source: "manual"},
			},
		},
		{
			name: "CLI vars override existing config vars",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "old", Source: "config"},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("FOO", "new", false),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "new", Sensitive: false, Source: "manual"},
			},
		},
		{
			name: "CLI vars override existing manual vars",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "old-manual", Source: "manual"},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("FOO", "new-manual", false),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "new-manual", Sensitive: false, Source: "manual"},
			},
		},
		{
			name: "CLI vars merge with existing - different keys",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "EXISTING", Value: "value", Source: "config"},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("NEW", "cli-value", false),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "EXISTING", Value: "value", Source: "config"},
				{Key: "NEW", Value: "cli-value", Sensitive: false, Source: "manual"},
			},
		},
		{
			name: "sensitive flag preserved from CLI",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "SECRET", Value: "old", Sensitive: false, Source: "config"},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("SECRET", "new-secret", true),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "SECRET", Value: "new-secret", Sensitive: true, Source: "manual"},
			},
		},
		{
			name:         "multiple CLI vars",
			existingVars: nil,
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("FOO", "foo-val", false),
				makeEnvVar("BAR", "bar-val", false),
				makeEnvVar("SECRET", "secret-val", true),
			},
			wantVariables: []core_v1alpha.ConfigSpecVariables{
				{Key: "FOO", Value: "foo-val", Sensitive: false, Source: "manual"},
				{Key: "BAR", Value: "bar-val", Sensitive: false, Source: "manual"},
				{Key: "SECRET", Value: "secret-val", Sensitive: true, Source: "manual"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeCliEnvVars(tt.existingVars, tt.cliVars)

			// Sort both for comparison since map iteration is non-deterministic
			sort.Slice(result, func(i, j int) bool {
				return result[i].Key < result[j].Key
			})
			sort.Slice(tt.wantVariables, func(i, j int) bool {
				return tt.wantVariables[i].Key < tt.wantVariables[j].Key
			})

			require.Len(t, result, len(tt.wantVariables))
			for i, want := range tt.wantVariables {
				assert.Equal(t, want.Key, result[i].Key, "key mismatch at index %d", i)
				assert.Equal(t, want.Value, result[i].Value, "value mismatch at index %d", i)
				assert.Equal(t, want.Sensitive, result[i].Sensitive, "sensitive mismatch at index %d", i)
				assert.Equal(t, want.Source, result[i].Source, "source mismatch at index %d", i)
			}
		})
	}
}

func TestIsSystemEnvVar(t *testing.T) {
	tests := []struct {
		key      string
		isSystem bool
	}{
		// The entire MIREN_ namespace is reserved, whoever set it.
		{appclient.EnvRuntimeApp, true},
		{appclient.EnvRuntimeVersion, true},
		{appclient.EnvRuntimeInstanceNum, true},
		{"MIREN_CUSTOM", true},
		{"MIREN_", true},
		// Unprefixed system vars, named explicitly.
		{"PORT", true},
		{"ADMIN_TOKEN", true},
		{"DATABASE_URL", false},
		{"API_KEY", false},
		{"NODE_ENV", false},
		{"SECRET_KEY_BASE", false},
		{"MY_PORT", false},
		{"PORTNAME", false},
		{"MIRENISH", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.isSystem, isSystemEnvVar(tt.key))
		})
	}
}

func TestComputeBuildEnvVars(t *testing.T) {
	tests := []struct {
		name         string
		existingVars []core_v1alpha.ConfigSpecVariables
		appConfig    *appconfig.AppConfig
		cliVars      []*build_v1alpha.EnvironmentVariable
		wantVars     map[string]string
	}{
		{
			name:         "empty inputs",
			existingVars: nil,
			appConfig:    nil,
			cliVars:      nil,
			wantVars:     map[string]string{},
		},
		{
			name: "existing config vars are included",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "manual"},
				{Key: "API_KEY", Value: "secret123", Source: "config"},
			},
			appConfig: nil,
			cliVars:   nil,
			wantVars: map[string]string{
				"DATABASE_URL": "postgres://localhost/db",
				"API_KEY":      "secret123",
			},
		},
		{
			name: "app config vars merged in",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "EXISTING", Value: "value", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "FROM_CONFIG", Value: "config_value"},
				},
			},
			cliVars: nil,
			wantVars: map[string]string{
				"EXISTING":    "value",
				"FROM_CONFIG": "config_value",
			},
		},
		{
			name:         "CLI vars override config vars",
			existingVars: nil,
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "FOO", Value: "from-config"},
				},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("FOO", "from-cli", false),
			},
			wantVars: map[string]string{
				"FOO": "from-cli",
			},
		},
		{
			name: "system vars are filtered out",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_URL", Value: "postgres://localhost/db", Source: "manual"},
				{Key: appclient.EnvRuntimeVersion, Value: "v1", Source: "config"},
				{Key: appclient.EnvRuntimeApp, Value: "myapp", Source: "config"},
				{Key: "PORT", Value: "8080", Source: "manual"},
				{Key: "ADMIN_TOKEN", Value: "token123", Source: "manual"},
				{Key: "MIREN_CUSTOM", Value: "custom", Source: "config"},
			},
			appConfig: nil,
			cliVars:   nil,
			wantVars: map[string]string{
				"DATABASE_URL": "postgres://localhost/db",
			},
		},
		{
			name: "sensitive vars are included",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "SECRET_KEY", Value: "super-secret", Sensitive: true, Source: "manual"},
			},
			appConfig: nil,
			cliVars:   nil,
			wantVars: map[string]string{
				"SECRET_KEY": "super-secret",
			},
		},
		{
			name: "full merge: existing + app config + CLI with system filtering",
			existingVars: []core_v1alpha.ConfigSpecVariables{
				{Key: "EXISTING_MANUAL", Value: "m1", Source: "manual"},
				{Key: "EXISTING_CONFIG", Value: "c1", Source: "config"},
				{Key: "PORT", Value: "3000", Source: "manual"},
			},
			appConfig: &appconfig.AppConfig{
				EnvVars: []appconfig.AppEnvVar{
					{Key: "EXISTING_CONFIG", Value: "c1-updated"},
					{Key: "NEW_CONFIG", Value: "new"},
				},
			},
			cliVars: []*build_v1alpha.EnvironmentVariable{
				makeEnvVar("CLI_VAR", "cli-val", false),
			},
			wantVars: map[string]string{
				"EXISTING_MANUAL": "m1",
				"EXISTING_CONFIG": "c1-updated",
				"NEW_CONFIG":      "new",
				"CLI_VAR":         "cli-val",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeBuildEnvVars(tt.existingVars, tt.appConfig, tt.cliVars)
			assert.Equal(t, tt.wantVars, result)
		})
	}
}

func TestValidateWorkloadsExist(t *testing.T) {
	tests := []struct {
		name    string
		config  core_v1alpha.ConfigSpec
		wantErr bool
	}{
		{
			name:    "no services returns error",
			config:  core_v1alpha.ConfigSpec{Services: nil},
			wantErr: true,
		},
		{
			name:    "empty services returns error",
			config:  core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{}},
			wantErr: true,
		},
		{
			name: "one service passes",
			config: core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}},
			},
			wantErr: false,
		},
		{
			name: "multiple services passes",
			config: core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}, {Name: "worker"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkloadsExist(tt.config)
			if tt.wantErr {
				assert.ErrorIs(t, err, errNoServices)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuildVariablesFromAppConfig_NewFields(t *testing.T) {
	appConfig := &appconfig.AppConfig{
		EnvVars: []appconfig.AppEnvVar{
			{Key: "DATABASE_URL", Value: "", Required: true, Sensitive: true, Description: "PostgreSQL connection string"},
			{Key: "LOG_LEVEL", Value: "info", Description: "Application log level"},
			{Key: "PLAIN_VAR", Value: "hello"},
		},
	}

	result := buildVariablesFromAppConfig(appConfig)
	require.Len(t, result, 3)

	// DATABASE_URL: required, sensitive, with description
	assert.Equal(t, "DATABASE_URL", result[0].Key)
	assert.True(t, result[0].Required)
	assert.True(t, result[0].Sensitive)
	assert.Equal(t, "PostgreSQL connection string", result[0].Description)
	assert.Equal(t, "config", result[0].Source)

	// LOG_LEVEL: not required, not sensitive, with description
	assert.Equal(t, "LOG_LEVEL", result[1].Key)
	assert.False(t, result[1].Required)
	assert.False(t, result[1].Sensitive)
	assert.Equal(t, "Application log level", result[1].Description)

	// PLAIN_VAR: no metadata
	assert.Equal(t, "PLAIN_VAR", result[2].Key)
	assert.False(t, result[2].Required)
	assert.False(t, result[2].Sensitive)
	assert.Equal(t, "", result[2].Description)
}

func TestMergeVariablesFromAppConfig_MetadataCarried(t *testing.T) {
	t.Run("manual var inherits metadata from config var", func(t *testing.T) {
		existingVars := []core_v1alpha.ConfigSpecVariables{
			{Key: "DATABASE_URL", Value: "manual://db", Source: "manual"},
		}
		appConfig := &appconfig.AppConfig{
			EnvVars: []appconfig.AppEnvVar{
				{Key: "DATABASE_URL", Value: "", Required: true, Description: "PostgreSQL connection string"},
			},
		}

		result := mergeVariablesFromAppConfig(existingVars, appConfig)
		require.Len(t, result, 1)
		assert.Equal(t, "DATABASE_URL", result[0].Key)
		assert.Equal(t, "manual://db", result[0].Value)
		assert.Equal(t, "manual", result[0].Source)
		assert.True(t, result[0].Required, "should carry Required from config var")
		assert.Equal(t, "PostgreSQL connection string", result[0].Description, "should carry Description from config var")
	})

	// An addon var is no longer stored at all, so what a build keeps for this key
	// is the app.toml declaration itself, metadata included. The binding still
	// shadows it when the version runs, and still inherits that metadata there;
	// see TestResolveRuntimeConfigOverridesAppTomlDeclaration.
	t.Run("config metadata survives dropping an addon var", func(t *testing.T) {
		existingVars := []core_v1alpha.ConfigSpecVariables{
			{Key: "DATABASE_URL", Value: "postgres://addon/db", Source: "addon"},
		}
		appConfig := &appconfig.AppConfig{
			EnvVars: []appconfig.AppEnvVar{
				{Key: "DATABASE_URL", Value: "", Required: true, Description: "Database URL"},
			},
		}

		result := mergeVariablesFromAppConfig(existingVars, appConfig)
		require.Len(t, result, 1)
		assert.Equal(t, "config", result[0].Source)
		assert.True(t, result[0].Required)
		assert.Equal(t, "Database URL", result[0].Description)
	})

	t.Run("config var without metadata clears existing metadata", func(t *testing.T) {
		existingVars := []core_v1alpha.ConfigSpecVariables{
			{Key: "VAR", Value: "val", Source: "manual", Required: true, Description: "old desc"},
		}
		appConfig := &appconfig.AppConfig{
			EnvVars: []appconfig.AppEnvVar{
				{Key: "VAR", Value: "new_val"},
			},
		}

		result := mergeVariablesFromAppConfig(existingVars, appConfig)
		require.Len(t, result, 1)
		// Manual var keeps its value, gets metadata from config (which has no metadata)
		assert.Equal(t, "val", result[0].Value)
		assert.False(t, result[0].Required, "metadata replaced by config var's values")
		assert.Equal(t, "", result[0].Description)
	})
}

func TestMergeServiceEnvVars_MetadataCarried(t *testing.T) {
	t.Run("manual var inherits metadata from config var", func(t *testing.T) {
		existing := []core_v1alpha.ConfigSpecServicesEnv{
			{Key: "DB_HOST", Value: "manual-host", Source: "manual"},
		}
		newEnvs := []core_v1alpha.ConfigSpecServicesEnv{
			{Key: "DB_HOST", Value: "", Source: "config", Required: true, Description: "Database hostname"},
		}

		result := mergeServiceEnvVars(existing, newEnvs)
		require.Len(t, result, 1)
		assert.Equal(t, "manual-host", result[0].Value)
		assert.Equal(t, "manual", result[0].Source)
		assert.True(t, result[0].Required)
		assert.Equal(t, "Database hostname", result[0].Description)
	})
}

func TestMergeCliEnvVars_MetadataPreserved(t *testing.T) {
	t.Run("CLI override preserves Required and Description from existing var", func(t *testing.T) {
		existing := []core_v1alpha.ConfigSpecVariables{
			{Key: "DATABASE_URL", Value: "old", Source: "config", Required: true, Description: "DB connection"},
		}
		cliVars := []*build_v1alpha.EnvironmentVariable{
			makeEnvVar("DATABASE_URL", "new-value", true),
		}

		result := mergeCliEnvVars(existing, cliVars)
		require.Len(t, result, 1)
		assert.Equal(t, "new-value", result[0].Value)
		assert.Equal(t, "manual", result[0].Source)
		assert.True(t, result[0].Sensitive)
		assert.True(t, result[0].Required, "should preserve Required from existing")
		assert.Equal(t, "DB connection", result[0].Description, "should preserve Description from existing")
	})

	t.Run("new CLI var without existing has no metadata", func(t *testing.T) {
		existing := []core_v1alpha.ConfigSpecVariables{}
		cliVars := []*build_v1alpha.EnvironmentVariable{
			makeEnvVar("NEW_VAR", "val", false),
		}

		result := mergeCliEnvVars(existing, cliVars)
		require.Len(t, result, 1)
		assert.False(t, result[0].Required)
		assert.Equal(t, "", result[0].Description)
	})
}

func TestValidateRequiredVars(t *testing.T) {
	tests := []struct {
		name    string
		spec    core_v1alpha.ConfigSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "no required vars - passes",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "FOO", Value: "bar"},
					{Key: "BAZ", Value: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "required var with value - passes",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "DATABASE_URL", Value: "postgres://localhost/db", Required: true},
				},
			},
			wantErr: false,
		},
		{
			name: "required var without value - fails",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "DATABASE_URL", Value: "", Required: true, Description: "PostgreSQL connection string"},
				},
			},
			wantErr: true,
			errMsg:  "DATABASE_URL",
		},
		{
			name: "required var description included in error",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "API_KEY", Value: "", Required: true, Description: "Third-party API key"},
				},
			},
			wantErr: true,
			errMsg:  "Third-party API key",
		},
		{
			name: "multiple missing required vars",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "DB_URL", Value: "", Required: true},
					{Key: "HAS_VALUE", Value: "ok", Required: true},
					{Key: "API_KEY", Value: "", Required: true},
				},
			},
			wantErr: true,
			errMsg:  "DB_URL",
		},
		{
			name: "required service env var without value - fails",
			spec: core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{
						Name: "web",
						Env: []core_v1alpha.ConfigSpecServicesEnv{
							{Key: "TLS_CERT", Value: "", Required: true, Description: "TLS certificate"},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "TLS_CERT",
		},
		{
			name: "service name included in error",
			spec: core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{
						Name: "worker",
						Env: []core_v1alpha.ConfigSpecServicesEnv{
							{Key: "QUEUE_URL", Value: "", Required: true},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "service: worker",
		},
		{
			name: "error includes actionable hint",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "SECRET", Value: "", Required: true},
				},
			},
			wantErr: true,
			errMsg:  "miren env set",
		},
		{
			name:    "empty spec - passes",
			spec:    core_v1alpha.ConfigSpec{},
			wantErr: false,
		},
		{
			name: "required var with whitespace-only value - fails",
			spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "DATABASE_URL", Value: "   ", Required: true},
				},
			},
			wantErr: true,
			errMsg:  "DATABASE_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequiredVars(tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNodePortsConflictBetweenApps(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app A with a pool claiming node_port 6697
	appA := &core_v1alpha.App{}
	appAID, err := server.Client.Create(ctx, "app-a", appA)
	require.NoError(t, err)

	pool := &compute_v1alpha.SandboxPool{
		App:              appAID,
		Service:          "irc",
		DesiredInstances: 1,
		SandboxLabels:    types.LabelSet("app", "app-a"),
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Image: "test:latest",
				Port: []compute_v1alpha.SandboxSpecContainerPort{{
					Name:     "irc",
					Port:     6697,
					NodePort: 6697,
				}},
			}},
		},
	}
	_, err = server.Client.Create(ctx, "app-a-irc-pool", pool)
	require.NoError(t, err)

	// Deploy app B claiming the same node_port 6697
	appB := &core_v1alpha.App{}
	appBID, err := server.Client.Create(ctx, "app-b", appB)
	require.NoError(t, err)

	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "chat",
			Ports: []core_v1alpha.ConfigSpecServicesPorts{{
				Name:     "chat",
				Port:     6697,
				NodePort: 6697,
			}},
		}},
	}

	err = validateNodePorts(ctx, server.EAC, appBID, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "6697")
	assert.Contains(t, err.Error(), "app-a")
	assert.Contains(t, err.Error(), "irc")
	assert.Contains(t, err.Error(), "chat")
}

func TestValidateNodePortsNoConflictSameApp(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create an app with a pool claiming node_port 6697
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "my-app", app)
	require.NoError(t, err)

	pool := &compute_v1alpha.SandboxPool{
		App:              appID,
		Service:          "irc",
		DesiredInstances: 1,
		SandboxLabels:    types.LabelSet("app", "my-app"),
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Image: "test:latest",
				Port: []compute_v1alpha.SandboxSpecContainerPort{{
					Name:     "irc",
					Port:     6697,
					NodePort: 6697,
				}},
			}},
		},
	}
	_, err = server.Client.Create(ctx, "my-app-irc-pool", pool)
	require.NoError(t, err)

	// Redeploy the same app with the same node_port — should succeed
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "irc",
			Ports: []core_v1alpha.ConfigSpecServicesPorts{{
				Name:     "irc",
				Port:     6697,
				NodePort: 6697,
			}},
		}},
	}

	err = validateNodePorts(ctx, server.EAC, appID, spec)
	assert.NoError(t, err)
}

func TestValidateNodePortsIntraAppDuplicate(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	appID := entity.Id("app/test-app")

	// Two services in the same config both claim node_port 6697
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{
			{
				Name: "irc",
				Ports: []core_v1alpha.ConfigSpecServicesPorts{{
					Name:     "irc",
					Port:     6697,
					NodePort: 6697,
				}},
			},
			{
				Name: "chat",
				Ports: []core_v1alpha.ConfigSpecServicesPorts{{
					Name:     "chat",
					Port:     6697,
					NodePort: 6697,
				}},
			},
		},
	}

	err := validateNodePorts(ctx, server.EAC, appID, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "irc")
	assert.Contains(t, err.Error(), "chat")
	assert.Contains(t, err.Error(), "6697")
}

func TestValidateNodePortsNoConflictDifferentPorts(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// App A claims node_port 6697
	appA := &core_v1alpha.App{}
	appAID, err := server.Client.Create(ctx, "app-a", appA)
	require.NoError(t, err)

	pool := &compute_v1alpha.SandboxPool{
		App:              appAID,
		Service:          "irc",
		DesiredInstances: 1,
		SandboxLabels:    types.LabelSet("app", "app-a"),
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Image: "test:latest",
				Port: []compute_v1alpha.SandboxSpecContainerPort{{
					Name:     "irc",
					Port:     6697,
					NodePort: 6697,
				}},
			}},
		},
	}
	_, err = server.Client.Create(ctx, "app-a-irc-pool", pool)
	require.NoError(t, err)

	// App B claims node_port 8080 — different port, no conflict
	appB := &core_v1alpha.App{}
	appBID, err := server.Client.Create(ctx, "app-b", appB)
	require.NoError(t, err)

	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web",
			Ports: []core_v1alpha.ConfigSpecServicesPorts{{
				Name:     "http",
				Port:     8080,
				NodePort: 8080,
			}},
		}},
	}

	err = validateNodePorts(ctx, server.EAC, appBID, spec)
	assert.NoError(t, err)
}

func TestValidateNodePortsScaledDownPoolIgnored(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// App A has a pool with node_port 6697 but scaled to zero
	appA := &core_v1alpha.App{}
	appAID, err := server.Client.Create(ctx, "app-a", appA)
	require.NoError(t, err)

	pool := &compute_v1alpha.SandboxPool{
		App:              appAID,
		Service:          "irc",
		DesiredInstances: 0,
		SandboxLabels:    types.LabelSet("app", "app-a"),
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Image: "test:latest",
				Port: []compute_v1alpha.SandboxSpecContainerPort{{
					Name:     "irc",
					Port:     6697,
					NodePort: 6697,
				}},
			}},
		},
	}
	_, err = server.Client.Create(ctx, "app-a-irc-pool", pool)
	require.NoError(t, err)

	// App B claims node_port 6697 — should succeed since app A's pool is scaled down
	appB := &core_v1alpha.App{}
	appBID, err := server.Client.Create(ctx, "app-b", appB)
	require.NoError(t, err)

	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "chat",
			Ports: []core_v1alpha.ConfigSpecServicesPorts{{
				Name:     "chat",
				Port:     6697,
				NodePort: 6697,
			}},
		}},
	}

	err = validateNodePorts(ctx, server.EAC, appBID, spec)
	assert.NoError(t, err)
}

func TestValidateDiskConfigsMissingDisk(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Deploy with size_gb=0 and no existing disk — should fail
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "db",
			Disks: []core_v1alpha.ConfigSpecServicesDisks{{
				Name:      "data",
				MountPath: "/data",
				SizeGb:    0,
			}},
		}},
	}

	err := validateDiskConfigs(ctx, server.EAC, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `disk "data" does not exist`)
	assert.Contains(t, err.Error(), "size_gb")
}

func TestValidateDiskConfigsExistingDisk(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a disk entity so the lookup succeeds
	disk := &storage.Disk{
		Name:   "data",
		SizeGb: 20,
		Status: storage.PROVISIONED,
	}
	_, err := server.Client.Create(ctx, "data-disk", disk)
	require.NoError(t, err)

	// Deploy with size_gb=0 referencing an existing disk — should succeed
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "db",
			Disks: []core_v1alpha.ConfigSpecServicesDisks{{
				Name:      "data",
				MountPath: "/data",
				SizeGb:    0,
			}},
		}},
	}

	err = validateDiskConfigs(ctx, server.EAC, spec)
	assert.NoError(t, err)
}

func TestValidateDiskConfigsLocalDiskSkipsCheck(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Local disk with size_gb=0 and no existing entity — should succeed
	// because local disks are managed by the node, not the entity store
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web",
			Disks: []core_v1alpha.ConfigSpecServicesDisks{{
				Name:      "data",
				MountPath: "/miren/data/local",
				Provider:  core_v1alpha.ConfigSpecServicesDisksLOCAL,
				SizeGb:    0,
			}},
		}},
	}

	err := validateDiskConfigs(ctx, server.EAC, spec)
	assert.NoError(t, err)
}

// TestShouldWarnLocalStorageMigration covers the migration-warning decision,
// including MIR-1423: an explicit local-provider disk mounted somewhere other
// than the legacy /miren/data/local path must suppress the warning. All local
// volumes share the same per-app host directory keyed on app ID, so the explicit
// disk already exposes the data — the old path-only check missed this and warned
// (and injected a duplicate mount) once the shared directory held data.
func TestShouldWarnLocalStorageMigration(t *testing.T) {
	const appID = entity.Id("app/test")

	localDiskAt := func(mountPath string) core_v1alpha.ConfigSpec {
		return core_v1alpha.ConfigSpec{
			Services: []core_v1alpha.ConfigSpecServices{{
				Name: "web",
				Disks: []core_v1alpha.ConfigSpecServicesDisks{{
					Name:      "chisigns-data",
					MountPath: mountPath,
					Provider:  core_v1alpha.ConfigSpecServicesDisksLOCAL,
				}},
			}},
		}
	}

	tests := []struct {
		name      string
		spec      core_v1alpha.ConfigSpec
		writeData bool
		want      bool
	}{
		{
			name:      "existing data, no disk declared",
			spec:      core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}}},
			writeData: true,
			want:      true,
		},
		{
			name:      "explicit local disk at /data suppresses warning",
			spec:      localDiskAt("/data"),
			writeData: true,
			want:      false,
		},
		{
			name:      "explicit local disk at legacy path suppresses warning",
			spec:      localDiskAt("/miren/data/local"),
			writeData: true,
			want:      false,
		},
		{
			name: "miren-provider disk at legacy path suppresses warning",
			spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
				Name: "web",
				Disks: []core_v1alpha.ConfigSpecServicesDisks{{
					Name:      "legacy",
					MountPath: "/miren/data/local",
					Provider:  core_v1alpha.ConfigSpecServicesDisksMIREN,
				}},
			}}},
			writeData: true,
			want:      false,
		},
		{
			name:      "no existing data, no disk declared",
			spec:      core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}}},
			writeData: false,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataPath := t.TempDir()
			if tt.writeData {
				localDir := filepath.Join(dataPath, "data", "local", appID.String())
				require.NoError(t, os.MkdirAll(localDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("x"), 0644))
			}
			got := shouldWarnLocalStorageMigration(dataPath, appID, tt.spec)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("empty data path never warns", func(t *testing.T) {
		assert.False(t, shouldWarnLocalStorageMigration("", appID, core_v1alpha.ConfigSpec{}))
	})
}

func TestValidateDiskConfigsAutoCreate(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Deploy with size_gb > 0 — should always succeed (will auto-create)
	spec := core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "db",
			Disks: []core_v1alpha.ConfigSpecServicesDisks{{
				Name:      "new-disk",
				MountPath: "/data",
				SizeGb:    20,
			}},
		}},
	}

	err := validateDiskConfigs(ctx, server.EAC, spec)
	assert.NoError(t, err)
}

func ptr[T any](v T) *T { return &v }

func TestBuildTasksConfig(t *testing.T) {
	t.Run("nil and empty produce no tasks", func(t *testing.T) {
		assert.Nil(t, buildTasksConfig(nil, nil))
		assert.Nil(t, buildTasksConfig(&appconfig.AppConfig{}, nil))
	})

	t.Run("resolves defaults and desugars every", func(t *testing.T) {
		ac := &appconfig.AppConfig{
			Tasks: map[string]*appconfig.TaskConfig{
				"migrate": {
					Command: "bin/rails db:migrate",
					Trigger: appconfig.TriggerDeploy,
					Timeout: "10m",
					Retries: 2,
				},
				"cleanup": {
					Command: "bin/cleanup",
					Trigger: appconfig.TriggerSchedule,
					Every:   "6h",
				},
				"reindex": {
					Command:       "bin/reindex",
					MaxConcurrent: 4,
					EnvVars: []appconfig.AppEnvVar{
						{Key: "BATCH", Value: "500"},
					},
				},
			},
		}

		tasks := buildTasksConfig(ac, nil)
		require.Len(t, tasks, 3)

		byName := make(map[string]core_v1alpha.ConfigSpecTasks, len(tasks))
		for _, tk := range tasks {
			byName[tk.Name] = tk
		}

		migrate := byName["migrate"]
		assert.Equal(t, "bin/rails db:migrate", migrate.Command)
		assert.Equal(t, "deploy", migrate.Trigger)
		assert.Equal(t, "10m", migrate.Timeout)
		assert.Equal(t, int64(2), migrate.Retries)
		assert.Equal(t, int64(1), migrate.MaxConcurrent, "max_concurrent defaults to 1")
		assert.Empty(t, migrate.Schedule)

		// every is sugar: only the calendar form reaches ConfigSpec, so every
		// consumer has exactly one scheduling mechanism to understand.
		assert.Equal(t, "*-*-* 00/6:00:00", byName["cleanup"].Schedule)
		assert.Empty(t, byName["cleanup"].Timeout)

		reindex := byName["reindex"]
		assert.Equal(t, "manual", reindex.Trigger, "trigger defaults to manual")
		assert.Equal(t, int64(4), reindex.MaxConcurrent)
		require.Len(t, reindex.Env, 1)
		assert.Equal(t, "BATCH", reindex.Env[0].Key)
		assert.Equal(t, "config", reindex.Env[0].Source)
	})

	t.Run("output order is stable across builds", func(t *testing.T) {
		ac := &appconfig.AppConfig{
			Tasks: map[string]*appconfig.TaskConfig{
				"zulu": {Command: "z"}, "alpha": {Command: "a"}, "mike": {Command: "m"},
			},
		}
		// Map iteration order varies per run; the stored config must not, or
		// identical input would produce spurious version churn.
		want := []string{"alpha", "mike", "zulu"}
		for range 20 {
			var got []string
			for _, tk := range buildTasksConfig(ac, nil) {
				got = append(got, tk.Name)
			}
			require.Equal(t, want, got)
		}
	})
}

func TestValidateWebIntent(t *testing.T) {
	tasks := map[string]*appconfig.TaskConfig{"t": {Command: "x"}}

	tests := []struct {
		name     string
		ac       *appconfig.AppConfig
		procfile map[string]string
		wantErr  bool
	}{
		{
			name:    "nil config",
			ac:      nil,
			wantErr: false,
		},
		{
			name:    "no tasks is the pre-existing world",
			ac:      &appconfig.AppConfig{},
			wantErr: false,
		},
		{
			name:    "tasks and no services without saying which",
			ac:      &appconfig.AppConfig{Tasks: tasks},
			wantErr: true,
		},
		{
			name:    "tasks with web = false",
			ac:      &appconfig.AppConfig{Tasks: tasks, Web: ptr(false)},
			wantErr: false,
		},
		{
			name:    "tasks with web = true",
			ac:      &appconfig.AppConfig{Tasks: tasks, Web: ptr(true)},
			wantErr: false,
		},
		{
			name: "tasks with a declared service",
			ac: &appconfig.AppConfig{
				Tasks:    tasks,
				Services: map[string]*appconfig.ServiceConfig{"web": {Command: "bin/s"}},
			},
			wantErr: false,
		},
		{
			// A Procfile answers the question just as well as app.toml does.
			name:     "tasks with a Procfile service",
			ac:       &appconfig.AppConfig{Tasks: tasks},
			procfile: map[string]string{"worker": "bin/worker"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebIntent(tt.ac, tt.procfile)
			if tt.wantErr {
				require.ErrorIs(t, err, errAmbiguousWeb)
				// The message has to carry the fix, not just the complaint.
				assert.Contains(t, err.Error(), "web = false")
				assert.Contains(t, err.Error(), "[services.web]")
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateWorkloadsExistAcceptsTasksOnly(t *testing.T) {
	spec := core_v1alpha.ConfigSpec{
		Tasks: []core_v1alpha.ConfigSpecTasks{{Name: "session", Command: "/bin/bash"}},
	}
	assert.NoError(t, validateWorkloadsExist(spec),
		"an app made entirely of tasks is valid; it just has nothing running between invocations")

	assert.ErrorIs(t, validateWorkloadsExist(core_v1alpha.ConfigSpec{}), errNoServices)
}

// The check has to survive being run in the order the build actually runs
// things. buildServicesConfig calls ResolveDefaults, which mutates ac.Services
// in place to stage the synthesized web service -- so once the config has been
// assembled, every app looks like it declares a service and the question can no
// longer be asked. A test with a fresh struct is exactly where that aliasing is
// invisible, which is why this one goes through buildVersionConfig first.
func TestValidateWebIntentSurvivesConfigAssembly(t *testing.T) {
	ac := &appconfig.AppConfig{
		Tasks: map[string]*appconfig.TaskConfig{
			"session": {Command: "/bin/bash"},
		},
	}

	// Asked before assembly -- the order the build uses -- the ambiguity is caught.
	require.ErrorIs(t, validateWebIntent(ac, nil), errAmbiguousWeb)

	spec := buildVersionConfig(ConfigInputs{
		BuildResult: &BuildResult{Command: "bin/server"},
		AppConfig:   ac,
	})
	require.NotEmpty(t, spec.Services, "assembly stages a synthesized web service")

	// And this is the trap: after assembly the app claims a service it never
	// declared, so asking now can never fail.
	assert.Contains(t, ac.Services, "web",
		"ResolveDefaults mutates the caller's config, which is what makes the ordering load-bearing")
	assert.NoError(t, validateWebIntent(ac, nil),
		"asking after assembly always passes; if this ever starts failing the ordering constraint has changed")
}

// A task's env is declared the same way a service's is, so a required-but-empty
// task variable must fail the deploy too -- otherwise the run starts and falls
// over on a value the platform already knew was missing.
func TestValidateRequiredVarsCoversTaskEnv(t *testing.T) {
	spec := core_v1alpha.ConfigSpec{
		Tasks: []core_v1alpha.ConfigSpecTasks{
			{
				Name: "migrate",
				Env: []core_v1alpha.ConfigSpecTasksEnv{
					{Key: "DATABASE_URL", Required: true},
					{Key: "OPTIONAL", Required: false},
				},
			},
		},
	}

	err := validateRequiredVars(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.Contains(t, err.Error(), "task: migrate", "the message should say where to look")
	assert.NotContains(t, err.Error(), "OPTIONAL")

	spec.Tasks[0].Env[0].Value = "postgres://..."
	assert.NoError(t, validateRequiredVars(spec))
}

// analyze is the deploy preview, so it must not report a web service the deploy
// will refuse to create. A task-only app that never says whether it wants web
// reaches the historical default and gets one synthesized from the image
// entrypoint; showing that as "image default" is an affirmative claim about a
// deploy that is in fact about to fail with errAmbiguousWeb.
func TestValidateWebIntentDrivesWhatAnalyzeCanClaim(t *testing.T) {
	taskOnly := &appconfig.AppConfig{
		Tasks: map[string]*appconfig.TaskConfig{
			"migrate": {Command: "rails db:migrate"},
		},
	}

	// The condition analyze has to detect.
	require.ErrorIs(t, validateWebIntent(taskOnly, nil), errAmbiguousWeb)

	// And the service it would otherwise have described: synthesized, with no
	// command of its own, which is what renders as "image default".
	spec := buildVersionConfig(ConfigInputs{
		BuildResult: &BuildResult{},
		AppConfig:   taskOnly,
	})
	var web *core_v1alpha.ConfigSpecServices
	for i := range spec.Services {
		if spec.Services[i].Name == "web" {
			web = &spec.Services[i]
		}
	}
	require.NotNil(t, web, "the synthesized web is what analyze must not report")
	assert.Empty(t, web.Command)

	// Saying either thing clears it, and then the listing is honest again.
	explicit := &appconfig.AppConfig{
		Tasks: taskOnly.Tasks,
		Web:   func() *bool { b := false; return &b }(),
	}
	assert.NoError(t, validateWebIntent(explicit, nil))

	// A Procfile web counts as saying so, and -- unlike web = false -- leaves a
	// real service behind, which is why the docs call that out.
	assert.NoError(t, validateWebIntent(taskOnly, map[string]string{"web": "bin/server"}))
}

// [services.console] is a real name in the wild, undocumented but used. It gets
// a shim rather than a hard break: it becomes a task, which is the only thing
// anyone declaring it ever wanted, since a console service being kept running
// was never useful.
func TestMigrateConsoleService(t *testing.T) {
	t.Run("moves the block to tasks", func(t *testing.T) {
		ac := &appconfig.AppConfig{
			Services: map[string]*appconfig.ServiceConfig{
				"web":     {Command: "bin/server"},
				"console": {Command: "bin/rails console"},
			},
		}

		assert.True(t, migrateConsoleService(ac))
		assert.NotContains(t, ac.Services, "console",
			"a console service must stop being deployed as a long-running process")
		require.Contains(t, ac.Tasks, "console")
		assert.Equal(t, "bin/rails console", ac.Tasks["console"].Command)
		assert.Equal(t, appconfig.TriggerManual, ac.Tasks["console"].Trigger)
		assert.Contains(t, ac.Services, "web", "other services are untouched")
	})

	t.Run("an author who wrote both meant both", func(t *testing.T) {
		ac := &appconfig.AppConfig{
			Services: map[string]*appconfig.ServiceConfig{"console": {Command: "svc"}},
			Tasks:    map[string]*appconfig.TaskConfig{"console": {Command: "task"}},
		}

		assert.False(t, migrateConsoleService(ac))
		assert.Contains(t, ac.Services, "console", "the service stays an ordinary service")
		assert.Equal(t, "task", ac.Tasks["console"].Command, "the task wins")
	})

	t.Run("nothing to do", func(t *testing.T) {
		assert.False(t, migrateConsoleService(nil))
		assert.False(t, migrateConsoleService(&appconfig.AppConfig{}))
	})

	t.Run("carries the service env across", func(t *testing.T) {
		ac := &appconfig.AppConfig{
			Services: map[string]*appconfig.ServiceConfig{
				"console": {
					Command: "bin/console",
					EnvVars: []appconfig.AppEnvVar{{Key: "RAILS_ENV", Value: "production"}},
				},
			},
		}

		require.True(t, migrateConsoleService(ac))
		require.Len(t, ac.Tasks["console"].EnvVars, 1)
		assert.Equal(t, "RAILS_ENV", ac.Tasks["console"].EnvVars[0].Key)
		// The value matters as much as the key: a migration that carried the
		// name across but dropped the setting would pass a key-only assertion
		// and leave the console running with the wrong environment.
		assert.Equal(t, "production", ac.Tasks["console"].EnvVars[0].Value)
	})
}

// The deploy gate exists to prove every task ran. A skipped run did not, so it
// must not let the version flip -- and that has to be exercised through
// awaitDeployRuns, since asserting the predicate alone would still pass if the
// wait loop treated SKIPPED as done.
func TestDeployGateRejectsASkippedRun(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &Builder{Log: testutils.TestLogger(t), ec: inmem.Client}

	runID := entity.Id("run/demo-migrate-1")
	_, err := inmem.EAC.Put(ctx, runEntity(t, runID, run_v1alpha.SKIPPED))
	require.NoError(t, err)

	err = b.awaitDeployRuns(ctx, map[entity.Id]string{runID: "migrate"}, noopStatusSender{})
	require.Error(t, err, "a skipped run never executed, so it cannot satisfy the gate")
	assert.Contains(t, err.Error(), "migrate")
	assert.Contains(t, err.Error(), "skipped")
}

// And the succeeding case has to actually pass, or the gate would block every
// deploy rather than only the broken ones.
func TestDeployGateAcceptsASucceededRun(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &Builder{Log: testutils.TestLogger(t), ec: inmem.Client}

	runID := entity.Id("run/demo-migrate-2")
	_, err := inmem.EAC.Put(ctx, runEntity(t, runID, run_v1alpha.SUCCEEDED))
	require.NoError(t, err)

	assert.NoError(t, b.awaitDeployRuns(ctx, map[entity.Id]string{runID: "migrate"}, noopStatusSender{}))
}

func TestDeployGateRejectsAFailedRun(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &Builder{Log: testutils.TestLogger(t), ec: inmem.Client}

	runID := entity.Id("run/demo-migrate-3")
	_, err := inmem.EAC.Put(ctx, runEntity(t, runID, run_v1alpha.FAILED))
	require.NoError(t, err)

	err = b.awaitDeployRuns(ctx, map[entity.Id]string{runID: "migrate"}, noopStatusSender{})
	require.Error(t, err)
	// The message has to point at the logs, since a failing migration is a
	// deploy failure and the user is already reading this output.
	assert.Contains(t, err.Error(), "miren logs run")
}

// runEntity builds a terminal run for the gate tests.
func runEntity(t *testing.T, id entity.Id, status run_v1alpha.RunStatus) *entityserver_v1alpha.Entity {
	t.Helper()

	var e entityserver_v1alpha.Entity
	e.SetId(id.String())
	e.SetAttrs(entity.New(entity.DBId, id, (&run_v1alpha.Run{
		Task:    "migrate",
		Trigger: run_v1alpha.DEPLOY,
		Status:  status,
	}).Encode).Attrs())
	return &e
}

// A name in services = [...] that matches nothing is always a mistake, and a
// silent one: the addon's storage is filtered onto no service at all, while its
// variables are not service-filtered and still reach every service, so the app
// deploys green with a connection string pointing at a mount that was never
// created.
func TestValidateAddonServices(t *testing.T) {
	parse := func(t *testing.T, toml string) *appconfig.AppConfig {
		t.Helper()
		ac, err := appconfig.Parse([]byte(toml))
		require.NoError(t, err)
		return ac
	}

	t.Run("rejects a name that is no service", func(t *testing.T) {
		ac := parse(t, `
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[addons.miren-sqlite]
services = ["wroker"]
`)
		err := validateAddonServices(ac, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wroker")
	})

	t.Run("accepts a service declared in app.toml", func(t *testing.T) {
		ac := parse(t, `
name = "test-app"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[addons.miren-sqlite]
services = ["web"]
`)
		require.NoError(t, validateAddonServices(ac, nil))
	})

	// The reason this check cannot live in appconfig.Validate: app.toml alone
	// does not know about a service the Procfile declares, and rejecting it
	// would break a config that is entirely valid.
	t.Run("accepts a service declared only in the Procfile", func(t *testing.T) {
		ac := parse(t, `
name = "test-app"

[addons.miren-postgresql]
services = ["web"]
`)
		require.NoError(t, validateAddonServices(ac, map[string]string{"web": "/bin/app"}))
	})

	t.Run("no services key is unconstrained", func(t *testing.T) {
		ac := parse(t, `
name = "test-app"

[services.web]

[addons.miren-postgresql]
variant = "standard"
`)
		require.NoError(t, validateAddonServices(ac, nil))
	})
}
