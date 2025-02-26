package lsvd

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestNATS(t *testing.T) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		t.Skip("nats url not specified")
	}

	log := slog.Default()

	gctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctx := NewContext(gctx)

	t.Run("can connect and deliver events", func(t *testing.T) {
		r := require.New(t)
		d, err := NewDisk(ctx, log, t.TempDir())
		r.NoError(err)
		defer d.Close(ctx)

		nc, err := NewNATSConnector(log, d, url, "test")
		r.NoError(err)

		conn, err := nats.Connect(url)
		r.NoError(err)

		defer conn.Close()

		done := make(chan struct{})

		b := time.Now()
		var got bool
		_, err = conn.Subscribe("lsvd.disk.test.stats", func(msg *nats.Msg) {
			defer close(done)

			got = true
			var stats StatsMessage

			err = json.Unmarshal(msg.Data, &stats)
			r.NoError(err)

			r.InDelta(0, stats.PublishTime.Sub(b).Seconds(), 1)
		})
		r.NoError(err)

		time.Sleep(100 * time.Millisecond)

		nc.publishStats()

		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case <-done:
			// ok
		}

		r.True(got)
	})
}
