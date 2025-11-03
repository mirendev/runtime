package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
)

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
