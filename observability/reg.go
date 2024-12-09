package observability

import "miren.dev/runtime/pkg/asm"

func TestInject(reg *asm.Registry) {
	reg.Provide(func() LogWriter {
		return &DebugLogWriter{}
	})

	reg.Provide(func() *StatusMonitor {
		return &StatusMonitor{
			entities: make(map[string]*EntityStatus),
		}
	})
}
