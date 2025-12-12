package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
)

func TestLogs(t *testing.T) {
	t.Run("can write logs", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			lm observability.LogsMaintainer
			pw observability.PersistentLogWriter
			pr observability.PersistentLogReader
		)

		err := reg.Populate(&lm)
		r.NoError(err)

		err = reg.Populate(&pw)
		r.NoError(err)

		err = reg.Populate(&pr)
		r.NoError(err)

		err = lm.Setup(ctx)
		r.NoError(err)

		id := identity.NewID()

		err = pw.WriteEntry(id, observability.LogEntry{
			Timestamp: time.Now(),
			Stream:    observability.Stdout,
			Body:      "this is a log line",
		})
		r.NoError(err)

		// VictoriaLogs may not immediately index the entry, especially under load.
		// Poll until the entry is readable.
		var entries []observability.LogEntry
		r.Eventually(func() bool {
			entries, err = pr.Read(ctx, id)
			return err == nil && len(entries) == 1
		}, 10*time.Second, 100*time.Millisecond, "log entry should be readable")

		r.Equal("this is a log line", entries[0].Body)
	})
}
