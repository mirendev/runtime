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
			wantErr: "service worker: num_instances must be non-negative",
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
