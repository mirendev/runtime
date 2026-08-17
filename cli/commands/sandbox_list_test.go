package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSandboxFilterKeep(t *testing.T) {
	tests := []struct {
		name    string
		filter  sandboxFilter
		app     string
		service string
		want    bool
	}{
		{name: "no filter keeps everything", app: "myapp", service: "web", want: true},
		{
			name:   "app matches",
			filter: sandboxFilter{app: "myapp"},
			app:    "myapp", service: "worker", want: true,
		},
		{
			name:   "another app is dropped",
			filter: sandboxFilter{app: "myapp"},
			app:    "otherapp", service: "web", want: false,
		},
		{
			name:   "service matches",
			filter: sandboxFilter{service: "worker"},
			app:    "myapp", service: "worker", want: true,
		},
		{
			name:   "app and service must both match",
			filter: sandboxFilter{app: "myapp", service: "worker"},
			app:    "myapp", service: "web", want: false,
		},
		{
			// A sandbox with no pool has no service to speak of, and the table
			// renders that as "-". Asking for a service should exclude it
			// rather than sweep it in.
			name:   "a sandbox with no service is dropped by a service filter",
			filter: sandboxFilter{service: "web"},
			app:    "myapp", service: "", want: false,
		},
		{
			name:   "a sandbox with no app is dropped by an app filter",
			filter: sandboxFilter{app: "myapp"},
			app:    "", service: "web", want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.filter.keep(tt.app, tt.service))
		})
	}
}

func TestSandboxFilterDescribe(t *testing.T) {
	require.Empty(t, sandboxFilter{}.describe())
	require.Equal(t, ` for app "myapp"`, sandboxFilter{app: "myapp"}.describe())
	require.Equal(t, ` for service "web"`, sandboxFilter{service: "web"}.describe())
	require.Equal(t,
		` for app "myapp", service "web"`,
		sandboxFilter{app: "myapp", service: "web"}.describe())
}
