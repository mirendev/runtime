package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRouteTimeoutArgs(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		isDefault   bool
		clear       bool
		timeout     string
		wantHost    string
		wantTimeout string
		wantErr     string
	}{
		{name: "host and duration", host: "example.com", timeout: "10m",
			wantHost: "example.com", wantTimeout: "10m"},
		{name: "host alone shows current", host: "example.com",
			wantHost: "example.com"},
		{name: "seconds", host: "example.com", timeout: "300s",
			wantHost: "example.com", wantTimeout: "300s"},
		{name: "clear a host route", host: "example.com", clear: true,
			wantHost: "example.com"},

		// With --default the sole positional is the duration, not a hostname.
		{name: "default with duration", isDefault: true, host: "5m",
			wantTimeout: "5m"},
		{name: "default alone shows current", isDefault: true},
		{name: "clear the default route", isDefault: true, clear: true},

		{name: "no target", wantErr: "either a hostname or --default"},
		{name: "both targets", host: "example.com", isDefault: true, timeout: "10m",
			wantErr: "--default cannot be used"},
		{name: "clear with a value", host: "example.com", clear: true, timeout: "10m",
			wantErr: "--clear cannot be used"},
		{name: "bare number", host: "example.com", timeout: "10", wantErr: "invalid timeout"},
		{name: "garbage", host: "example.com", timeout: "forever", wantErr: "invalid timeout"},
		{name: "garbage on default", isDefault: true, host: "forever", wantErr: "invalid timeout"},
		{name: "zero", host: "example.com", timeout: "0s", wantErr: "must be positive"},
		{name: "negative", host: "example.com", timeout: "-5m", wantErr: "must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, timeout, err := resolveRouteTimeoutArgs(tt.host, tt.isDefault, tt.clear, tt.timeout)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantHost, host)
			require.Equal(t, tt.wantTimeout, timeout)
		})
	}
}
