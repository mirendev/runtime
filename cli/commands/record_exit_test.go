//go:build linux

package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/exitrecord"
)

func TestExitRecordFromEnv(t *testing.T) {
	now := time.Date(2026, 9, 11, 17, 4, 11, 0, time.UTC)

	tests := []struct {
		name string
		env  map[string]string

		wantAbnormal bool
		want         exitrecord.Record
	}{
		{
			name: "clean stop records nothing",
			env: map[string]string{
				"SERVICE_RESULT": "success",
				"EXIT_CODE":      "exited",
				"EXIT_STATUS":    "0",
			},
			wantAbnormal: false,
		},
		{
			// Running the hook by hand is not evidence that anything went wrong.
			name:         "no SERVICE_RESULT records nothing",
			env:          map[string]string{},
			wantAbnormal: false,
		},
		{
			name: "out-of-memory kill",
			env: map[string]string{
				"SERVICE_RESULT": "oom-kill",
				"EXIT_CODE":      "killed",
				"EXIT_STATUS":    "9",
			},
			wantAbnormal: true,
			want: exitrecord.Record{
				At:         now,
				Unit:       "miren.service",
				Result:     "oom-kill",
				ExitCode:   "killed",
				ExitStatus: "9",
			},
		},
		{
			name: "non-zero exit",
			env: map[string]string{
				"SERVICE_RESULT": "exit-code",
				"EXIT_CODE":      "exited",
				"EXIT_STATUS":    "1",
			},
			wantAbnormal: true,
			want: exitrecord.Record{
				At:         now,
				Unit:       "miren.service",
				Result:     "exit-code",
				ExitCode:   "exited",
				ExitStatus: "1",
			},
		},
		{
			name: "stop timeout with no exit detail",
			env: map[string]string{
				"SERVICE_RESULT": "timeout",
			},
			wantAbnormal: true,
			want: exitrecord.Record{
				At:     now,
				Unit:   "miren.service",
				Result: "timeout",
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			env: map[string]string{
				"SERVICE_RESULT": " oom-kill\n",
				"EXIT_CODE":      " killed ",
				"EXIT_STATUS":    " 9 ",
			},
			wantAbnormal: true,
			want: exitrecord.Record{
				At:         now,
				Unit:       "miren.service",
				Result:     "oom-kill",
				ExitCode:   "killed",
				ExitStatus: "9",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"SERVICE_RESULT", "EXIT_CODE", "EXIT_STATUS"} {
				t.Setenv(k, tt.env[k])
			}

			got, abnormal := exitRecordFromEnv("miren.service", now)

			assert.Equal(t, tt.wantAbnormal, abnormal)
			if tt.wantAbnormal {
				assert.Equal(t, tt.want, got)
				assert.Equal(t, tt.want.Result == "oom-kill", got.OOMKilled())
			}
		})
	}
}

func TestParseUnitProperties(t *testing.T) {
	// Verbatim `systemctl show -p MemoryPeak -p MemoryMax -p NRestarts` output.
	// Note the order: systemd returns properties in its own internal order, not
	// the order they were asked for. Reading these lines positionally assigns
	// every value to the wrong field, which is what this test exists to prevent.
	const out = `NRestarts=3
MemoryPeak=4294967296
MemoryMax=8589934592
`

	props := parseUnitProperties(out)

	assert.Equal(t, "3", props["NRestarts"])
	assert.Equal(t, "4294967296", props["MemoryPeak"])
	assert.Equal(t, "8589934592", props["MemoryMax"])
}

func TestMemoryFactsFromShow(t *testing.T) {
	// Verbatim output, captured from `systemctl show -p MemoryPeak -p MemoryMax
	// -p NRestarts <unit>`. Reading these three lines in the order they were
	// requested — which an earlier version of this code did — puts NRestarts in
	// memory_peak, the peak in memory_max, and "infinity" in restarts. Every
	// number is wrong and every number still looks plausible.
	peak, limit, restarts := memoryFactsFromShow(`NRestarts=3
MemoryPeak=4294967296
MemoryMax=8589934592
`)

	assert.Equal(t, int64(4294967296), peak)
	assert.Equal(t, int64(8589934592), limit)
	assert.Equal(t, 3, restarts)
}

func TestMemoryFactsFromShowMissingValues(t *testing.T) {
	t.Run("no limit set", func(t *testing.T) {
		peak, limit, restarts := memoryFactsFromShow("NRestarts=0\nMemoryPeak=78077952\nMemoryMax=infinity\n")

		assert.Equal(t, int64(78077952), peak)
		assert.Equal(t, int64(0), limit, "infinity means no limit, not a huge one")
		assert.Equal(t, 0, restarts)
	})

	t.Run("systemd too old for MemoryPeak", func(t *testing.T) {
		// MemoryPeak arrived in systemd 254; before that the property is absent.
		peak, limit, restarts := memoryFactsFromShow("NRestarts=2\nMemoryMax=8589934592\n")

		assert.Equal(t, int64(0), peak)
		assert.Equal(t, int64(8589934592), limit, "a missing peak must not cost us the limit")
		assert.Equal(t, 2, restarts)
	})

	t.Run("empty output", func(t *testing.T) {
		peak, limit, restarts := memoryFactsFromShow("")

		assert.Equal(t, int64(0), peak)
		assert.Equal(t, int64(0), limit)
		assert.Equal(t, 0, restarts)
	})
}

func TestParseUnitPropertiesEdgeCases(t *testing.T) {
	props := parseUnitProperties(`MemoryMax=infinity
NRestarts=0
ExecStopPost={ path=/usr/bin/x ; argv[]=/usr/bin/x --flag=1 ; }
MalformedLineWithNoSeparator

MemoryPeak=12345`)

	assert.Equal(t, "infinity", props["MemoryMax"])
	assert.Equal(t, "0", props["NRestarts"])

	// A value can contain "=" itself, so only the first one separates the name.
	assert.Equal(t, "{ path=/usr/bin/x ; argv[]=/usr/bin/x --flag=1 ; }", props["ExecStopPost"])

	// Blank and separator-less lines are skipped rather than poisoning the map.
	assert.NotContains(t, props, "MalformedLineWithNoSeparator")
	assert.NotContains(t, props, "")

	// A property that survives a malformed line above it is still readable.
	assert.Equal(t, "12345", props["MemoryPeak"])
}

func TestParseUnitBytes(t *testing.T) {
	assert.Equal(t, int64(4294967296), parseUnitBytes("4294967296"))
	assert.Equal(t, int64(4294967296), parseUnitBytes(" 4294967296 \n"))

	// systemd's ways of saying "no value". Neither should become a bogus number
	// in a record an operator reads.
	assert.Equal(t, int64(0), parseUnitBytes("infinity"))
	assert.Equal(t, int64(0), parseUnitBytes("[not set]"))
	assert.Equal(t, int64(0), parseUnitBytes(""))
}
