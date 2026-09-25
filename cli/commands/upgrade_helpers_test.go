package commands

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/release"
)

func TestResolveVersionChannel(t *testing.T) {
	testCases := []struct {
		name    string
		version string
		channel string
		want    string
		wantErr bool
	}{
		{
			name: "neither set defaults to latest",
			want: "latest",
		},
		{
			name:    "channel main resolves to main",
			channel: "main",
			want:    "main",
		},
		{
			name:    "channel latest resolves to latest",
			channel: "latest",
			want:    "latest",
		},
		{
			name:    "version only is passed through",
			version: "v0.2.0",
			want:    "v0.2.0",
		},
		{
			name:    "both version and channel is an error",
			version: "v0.2.0",
			channel: "main",
			wantErr: true,
		},
		{
			name:    "version with channel=latest is still an error",
			version: "v0.2.0",
			channel: "latest",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveVersionChannel(tc.version, tc.channel)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPrintVersionComparisonBuildDatesUTC(t *testing.T) {
	stdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	defer r.Close()

	PrintVersionComparison(
		release.VersionInfo{Version: "v1", BuildDate: time.Date(2026, 9, 14, 12, 48, 18, 0, time.FixedZone("CDT", -5*60*60))},
		release.VersionInfo{Version: "v2", BuildDate: time.Date(2026, 9, 15, 4, 10, 17, 0, time.FixedZone("JST", 9*60*60))},
	)
	require.NoError(t, w.Close())
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "Current version: v1\nCurrent build:   2026-09-14 17:48:18 UTC\n\nLatest version:  v2\nLatest build:    2026-09-14 19:10:17 UTC\n", string(output))
}
