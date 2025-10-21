package observability

import (
	"log/slog"

	"miren.dev/runtime/pkg/asm"
)

func TestInject(reg *asm.Registry) {
	reg.Provide(func(opts struct {
		Log *slog.Logger
	}) LogWriter {
		return &DebugLogWriter{
			Log: opts.Log,
		}
	})

	reg.Provide(func(opts struct {
		Log *slog.Logger
	}) *StatusMonitor {
		return &StatusMonitor{
			Log:      opts.Log,
			entities: make(map[string]*EntityStatus),
		}
	})
}
