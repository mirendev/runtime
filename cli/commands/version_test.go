package commands

import (
	"bytes"
	"testing"
	"time"
)

func TestPrintBuildLinesUTC(t *testing.T) {
	tests := []struct {
		name  string
		built time.Time
		want  string
	}{
		{
			name:  "server build in local timezone",
			built: time.Date(2026, 9, 14, 12, 48, 18, 0, time.FixedZone("CDT", -5*60*60)),
			want:  "  Version:  v1\n  Built:    2026-09-14 17:48:18 UTC\n",
		},
		{
			name:  "CLI build already in UTC",
			built: time.Date(2026, 9, 14, 18, 10, 17, 0, time.UTC),
			want:  "  Version:  v1\n  Built:    2026-09-14 18:10:17 UTC\n",
		},
		{
			name: "unknown build date",
			want: "  Version:  v1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printBuildLines(&Context{Stdout: &out}, "v1", "", tt.built)
			if got := out.String(); got != tt.want {
				t.Fatalf("printBuildLines output = %q, want %q", got, tt.want)
			}
		})
	}
}
