package observability

import (
	"log/slog"

	"miren.dev/runtime/pkg/asm"
)

func TestInject(reg *asm.Registry) {
	reg.Provide(func(opts struct {
		Log *slog.Logger
	}) *StatusMonitor {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		return &StatusMonitor{
			Log:      log,
			entities: make(map[string]*EntityStatus),
		}
	})
}
