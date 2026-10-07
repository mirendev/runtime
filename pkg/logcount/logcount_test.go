package logcount

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/slogfmt"
)

func newLogger(level slog.Level) (*slog.Logger, *Counts) {
	counts := &Counts{}
	inner := slogfmt.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: level})
	return slog.New(NewHandler(inner, counts)), counts
}

func TestHandlerCountsByLevel(t *testing.T) {
	log, counts := newLogger(slog.LevelDebug)

	log.Debug("d")
	log.Info("i")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	log.Error("e")
	log.Error("e")

	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 0))
	assert.Equal(t, uint64(2), counts.Load(SourceMiren, 1))
	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 2))
	assert.Equal(t, uint64(3), counts.Load(SourceMiren, 3))
}

func TestHandlerSkipsRecordsBelowLevel(t *testing.T) {
	log, counts := newLogger(slog.LevelInfo)

	log.Debug("never printed")
	log.Info("printed")

	assert.Zero(t, counts.Load(SourceMiren, 0))
	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 1))
}

func TestHandlerChildrenShareCounts(t *testing.T) {
	log, counts := newLogger(slog.LevelInfo)

	log.With("module", "coordinator").Error("a")
	log.WithGroup("g").With("k", "v").Error("b")

	assert.Equal(t, uint64(2), counts.Load(SourceMiren, 3))
}

func TestHandlerBucketsOddLevels(t *testing.T) {
	log, counts := newLogger(slogfmt.Trace)

	log.Log(context.Background(), slogfmt.Trace, "trace")
	log.Log(context.Background(), slog.LevelInfo+2, "between info and warn")
	log.Log(context.Background(), slog.LevelError+4, "past error")

	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 0))
	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 1))
	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 3))
}

func TestHandlerCountsRelayedAtChildLevel(t *testing.T) {
	log, counts := newLogger(slog.LevelInfo)

	// The relay prints the child's error at Info; the count keeps it an error
	// and keeps it out of miren's own numbers.
	ctx := Relayed(context.Background(), SourceVMAgent, slog.LevelError)
	log.Log(ctx, slog.LevelInfo, "cannot send a block to remote storage")

	assert.Equal(t, uint64(1), counts.Load(SourceVMAgent, 3))
	assert.Zero(t, counts.Load(SourceVMAgent, 1))
	assert.Zero(t, counts.Load(SourceMiren, 1))
	assert.Zero(t, counts.Load(SourceMiren, 3))
}

func TestCountAtOverridesCountedLevel(t *testing.T) {
	log, counts := newLogger(slog.LevelInfo)

	log.ErrorContext(CountAt(context.Background(), slog.LevelInfo), "Log leveling changed")

	assert.Equal(t, uint64(1), counts.Load(SourceMiren, 1))
	assert.Zero(t, counts.Load(SourceMiren, 3))
}

func TestSourceFor(t *testing.T) {
	assert.Equal(t, SourceEtcd, SourceFor("etcd"))
	assert.Equal(t, SourceVMAgent, SourceFor("vmagent"))
	assert.Equal(t, SourceOther, SourceFor("something-new"))
	assert.Equal(t, SourceOther, SourceFor("miren"), "miren is never a relay")
	assert.Equal(t, "victoriametrics", SourceVictoriaMetrics.String())
}

func BenchmarkHandle(b *testing.B) {
	plain := slog.New(slogfmt.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	counted, _ := newLogger(slog.LevelInfo)

	for _, bc := range []struct {
		name string
		log  *slog.Logger
	}{{"plain", plain}, {"counted", counted}} {
		b.Run(bc.name+"/printed", func(b *testing.B) {
			for b.Loop() {
				bc.log.Info("request handled", "status", 200)
			}
		})
		b.Run(bc.name+"/filtered", func(b *testing.B) {
			for b.Loop() {
				bc.log.Debug("request handled", "status", 200)
			}
		})
		b.Run(bc.name+"/with", func(b *testing.B) {
			for b.Loop() {
				_ = bc.log.With("module", "httpingress")
			}
		})
	}
}
