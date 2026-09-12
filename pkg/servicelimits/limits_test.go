package servicelimits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompute(t *testing.T) {
	tests := []struct {
		name     string
		ram      int64
		wantMax  int64
		wantHigh int64
	}{
		{
			name:     "unknown ram yields no limit",
			ram:      0,
			wantMax:  0,
			wantHigh: 0,
		},
		{
			name:     "negative ram yields no limit",
			ram:      -1,
			wantMax:  0,
			wantHigh: 0,
		},
		{
			// A quarter of 4 GB is 1 GiB, which is under the floor.
			name:     "minimum supported host takes the floor",
			ram:      4 * gib,
			wantMax:  2 * gib,
			wantHigh: 1740 * mib,
		},
		{
			name:     "recommended host still takes the floor",
			ram:      8 * gib,
			wantMax:  2 * gib,
			wantHigh: 1740 * mib,
		},
		{
			name:     "medium host scales with ram",
			ram:      16 * gib,
			wantMax:  4 * gib,
			wantHigh: 3481 * mib,
		},
		{
			name:     "large host scales with ram",
			ram:      32 * gib,
			wantMax:  8 * gib,
			wantHigh: 6963 * mib,
		},
		{
			name:     "cap is reached at 64 GB",
			ram:      64 * gib,
			wantMax:  16 * gib,
			wantHigh: 13926 * mib,
		},
		{
			// The cap, not a quarter of RAM, so a leak is still bounded well
			// short of the host's memory.
			name:     "very large host stays at the cap",
			ram:      256 * gib,
			wantMax:  16 * gib,
			wantHigh: 13926 * mib,
		},
		{
			// Below the floor the headroom clamp takes over, so the limit
			// always leaves the rest of the system somewhere to run.
			name:     "tiny host is clamped to leave headroom",
			ram:      1 * gib,
			wantMax:  1 * gib * 90 / 100,
			wantHigh: 782 * mib,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compute(tt.ram)

			assert.Equal(t, tt.ram, got.SystemRAMBytes)
			assert.Equal(t, tt.wantMax, got.MemoryMaxBytes)
			assert.Equal(t, tt.wantHigh, got.MemoryHighBytes)
			assert.Equal(t, tt.wantMax > 0, got.Known())

			if got.Known() {
				assert.Less(t, got.MemoryHighBytes, got.MemoryMaxBytes,
					"MemoryHigh must sit below MemoryMax so reclaim happens before the kill")
				assert.LessOrEqual(t, got.MemoryMaxBytes, tt.ram,
					"the limit must never exceed physical memory")
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "2G", formatBytes(2*gib))
	assert.Equal(t, "16G", formatBytes(16*gib))
	assert.Equal(t, "1740M", formatBytes(1740*mib))
	assert.Equal(t, "966367641", formatBytes(966367641))
}

func TestRender(t *testing.T) {
	t.Run("server unit with a known limit", func(t *testing.T) {
		got := Render("miren.service", "/var/lib/miren/server", Compute(16*gib))

		assert.Contains(t, got, "[Service]\n")
		assert.Contains(t, got, "MemoryMax=4G\n")
		assert.Contains(t, got, "MemoryHigh=3481M\n")
		assert.Contains(t, got, "MemorySwapMax=0\n")
		// The leading "-" is load-bearing: without it a hook that cannot run —
		// after a rollback to a build without the subcommand, or mid-upgrade —
		// puts the unit into a failed state on every stop.
		assert.Contains(t, got,
			"ExecStopPost=-/var/lib/miren/release/miren internal record-exit --unit=miren.service --output=/var/lib/miren/server\n")

		// The override hint has to name the unit the operator would actually
		// type, not the full filename.
		assert.Contains(t, got, "sudo systemctl edit miren`")

		// OOMPolicy=kill would take the containerd shims down with the
		// coordinator, disrupting the apps this cap is meant to spare.
		assert.NotContains(t, got, "OOMPolicy")
	})

	t.Run("runner unit", func(t *testing.T) {
		got := Render("miren-runner.service", "/var/lib/miren/runner", Compute(16*gib))

		assert.Contains(t, got, "sudo systemctl edit miren-runner`")
		assert.Contains(t, got,
			"ExecStopPost=-/var/lib/miren/release/miren internal record-exit --unit=miren-runner.service --output=/var/lib/miren/runner\n")
	})

	t.Run("unknown ram omits the memory directives", func(t *testing.T) {
		got := Render("miren.service", "/var/lib/miren/server", Compute(0))

		assert.NotContains(t, got, "MemoryMax=")
		assert.NotContains(t, got, "MemoryHigh=")
		assert.Contains(t, got, "# Host memory could not be determined")

		// The parts that don't depend on knowing the host's size still apply.
		assert.Contains(t, got, "MemorySwapMax=0\n")
		assert.Contains(t, got, "ExecStopPost=")
	})

	t.Run("empty state path omits the hook", func(t *testing.T) {
		got := Render("miren.service", "", Compute(16*gib))

		assert.NotContains(t, got, "ExecStopPost")
		assert.Contains(t, got, "MemoryMax=4G\n")
	})
}

func TestWriteAndRemove(t *testing.T) {
	dir := t.TempDir()
	prev := unitDir
	unitDir = dir
	t.Cleanup(func() { unitDir = prev })

	path := filepath.Join(dir, "miren.service.d", DropInFileName)

	wrote, err := Write("miren.service", "/var/lib/miren/server", Compute(16*gib))
	require.NoError(t, err)
	require.True(t, wrote)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "MemoryMax=4G\n")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	t.Run("rewriting replaces the previous version", func(t *testing.T) {
		wrote, err := Write("miren.service", "/var/lib/miren/server", Compute(64*gib))
		require.NoError(t, err)
		require.True(t, wrote)

		body, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(body), "MemoryMax=16G\n")
		assert.NotContains(t, string(body), "MemoryMax=4G\n")

		// The atomic write must not leave its temp file behind for systemd to
		// pick up as a second drop-in.
		entries, err := os.ReadDir(filepath.Dir(path))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, DropInFileName, entries[0].Name())
	})

	t.Run("remove clears the file and the directory", func(t *testing.T) {
		require.NoError(t, Remove("miren.service"))

		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Dir(path))
		assert.True(t, os.IsNotExist(err), "an empty drop-in directory should not be left behind")
	})

	t.Run("remove is idempotent", func(t *testing.T) {
		assert.NoError(t, Remove("miren.service"))
	})

	t.Run("remove keeps a directory holding an operator override", func(t *testing.T) {
		_, err := Write("miren.service", "/var/lib/miren/server", Compute(16*gib))
		require.NoError(t, err)
		override := filepath.Join(dir, "miren.service.d", "override.conf")
		require.NoError(t, os.WriteFile(override, []byte("[Service]\nMemoryMax=32G\n"), 0o644))

		require.NoError(t, Remove("miren.service"))

		_, err = os.Stat(override)
		assert.NoError(t, err, "an operator's own drop-in must survive uninstall")
	})
}

func TestUnitShortName(t *testing.T) {
	assert.Equal(t, "miren", UnitShortName("miren.service"))
	assert.Equal(t, "miren-runner", UnitShortName("miren-runner.service"))
	assert.Equal(t, "miren", UnitShortName("miren"))

	// A record written before the unit name was stored still has to yield a
	// usable instruction.
	assert.Equal(t, "miren", UnitShortName(""))
}

func TestQuoteExecArg(t *testing.T) {
	// The ordinary case has to stay unquoted, so the common drop-in reads plainly.
	assert.Equal(t, "/var/lib/miren/server", quoteExecArg("/var/lib/miren/server"))
	assert.Equal(t, "/var/lib/miren/runner", quoteExecArg("/var/lib/miren/runner"))

	// A space would otherwise split into extra arguments and the hook would
	// silently stop recording.
	assert.Equal(t, `"/var/lib/my miren/server"`, quoteExecArg("/var/lib/my miren/server"))

	// A newline would end the ExecStopPost directive and let whatever follows
	// become its own systemd setting.
	assert.Equal(t, `"/tmp/x\nExecStart=/bin/false"`, quoteExecArg("/tmp/x\nExecStart=/bin/false"))

	assert.Equal(t, `"/tmp/a\"b"`, quoteExecArg(`/tmp/a"b`))
	assert.Equal(t, `"/tmp/a\\b"`, quoteExecArg(`/tmp/a\b`))

	// "$" and "%" are interpreted by systemd whether or not the value is
	// quoted, and each has its own escape rather than a backslash. A path with
	// no other special character therefore stays unquoted but still doubled.
	assert.Equal(t, "/tmp/a$$b", quoteExecArg("/tmp/a$b"))
	assert.Equal(t, "/tmp/a%%nb", quoteExecArg("/tmp/a%nb"))
	assert.Equal(t, "/tmp/$${HOME}/x", quoteExecArg("/tmp/${HOME}/x"))
	assert.Equal(t, `"/tmp/a $$b %%n"`, quoteExecArg("/tmp/a $b %n"))
}

func TestRenderEscapesTheStatePath(t *testing.T) {
	t.Run("path with a space", func(t *testing.T) {
		got := Render("miren-runner.service", "/var/lib/my runner", Compute(16*gib))

		assert.Contains(t, got, `--output="/var/lib/my runner"`+"\n")
	})

	t.Run("path with a newline cannot inject a directive", func(t *testing.T) {
		got := Render("miren-runner.service", "/tmp/x\nExecStart=/bin/false", Compute(16*gib))

		// The injected text has to stay inside the quoted argument, on the same
		// line, rather than becoming a setting systemd would act on.
		assert.NotContains(t, got, "\nExecStart=/bin/false")
		assert.Contains(t, got, `\nExecStart=/bin/false"`)

		for line := range strings.SplitSeq(got, "\n") {
			assert.False(t, strings.HasPrefix(line, "ExecStart="),
				"a state path must never introduce its own directive")
		}
	})
}

func TestWriteKeepsAnExistingLimitWhenRAMIsUnknown(t *testing.T) {
	dir := t.TempDir()
	prev := unitDir
	unitDir = dir
	t.Cleanup(func() { unitDir = prev })

	path := filepath.Join(dir, "miren.service.d", DropInFileName)

	wrote, err := Write("miren.service", "/var/lib/miren/server", Compute(16*gib))
	require.NoError(t, err)
	require.True(t, wrote)

	// An upgrade on a host where /proc/meminfo momentarily can't be read must
	// not replace a working cap with a drop-in that has no memory directives.
	// That would silently remove the protection the host already had.
	wrote, err = Write("miren.service", "/var/lib/miren/server", Compute(0))
	require.NoError(t, err)
	assert.False(t, wrote, "an unknown limit must not overwrite a known one")

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "MemoryMax=4G\n", "the existing cap must survive")
}

func TestWriteStillInstallsWhenRAMIsUnknownAndNothingExists(t *testing.T) {
	dir := t.TempDir()
	prev := unitDir
	unitDir = dir
	t.Cleanup(func() { unitDir = prev })

	// A first install with no detectable RAM still gets a drop-in, because even
	// without a limit it carries the ExecStopPost hook.
	wrote, err := Write("miren.service", "/var/lib/miren/server", Compute(0))
	require.NoError(t, err)
	assert.True(t, wrote)

	body, err := os.ReadFile(filepath.Join(dir, "miren.service.d", DropInFileName))
	require.NoError(t, err)
	assert.Contains(t, string(body), "ExecStopPost=")
	assert.NotContains(t, string(body), "MemoryMax=")
}

func TestExistingStatePath(t *testing.T) {
	dir := t.TempDir()
	prev := unitDir
	unitDir = dir
	t.Cleanup(func() { unitDir = prev })

	t.Run("no drop-in yet", func(t *testing.T) {
		_, ok := ExistingStatePath("miren.service")
		assert.False(t, ok)
	})

	t.Run("reads back a plain path", func(t *testing.T) {
		_, err := Write("miren.service", "/data/miren/server", Compute(16*gib))
		require.NoError(t, err)

		got, ok := ExistingStatePath("miren.service")
		require.True(t, ok)

		// Upgrade has no idea the operator moved data_path. Recomputing here
		// would write records to /var/lib/miren/server while the server reads
		// /data/miren/server, and the restart would go unreported.
		assert.Equal(t, "/data/miren/server", got)
	})

	t.Run("reads back a quoted path", func(t *testing.T) {
		_, err := Write("miren-runner.service", "/var/lib/my runner", Compute(16*gib))
		require.NoError(t, err)

		got, ok := ExistingStatePath("miren-runner.service")
		require.True(t, ok)
		assert.Equal(t, "/var/lib/my runner", got)
	})

	t.Run("ignores a drop-in with no hook", func(t *testing.T) {
		_, err := Write("miren-nohook.service", "", Compute(16*gib))
		require.NoError(t, err)

		_, ok := ExistingStatePath("miren-nohook.service")
		assert.False(t, ok)
	})
}

func TestUnquoteExecArgRoundTrips(t *testing.T) {
	for _, path := range []string{
		"/var/lib/miren/server",
		"/var/lib/my runner",
		"/tmp/a\"b",
		`/tmp/a\b`,
		"/tmp/a$b",
		"/tmp/a%nb",
		"/tmp/100%",
		"/tmp/${HOME}/x",
		"/tmp/a $b %n",
		"/tmp/x\nExecStart=/bin/false",
	} {
		assert.Equal(t, path, unquoteExecArg(quoteExecArg(path)), "round trip for %q", path)
	}
}
