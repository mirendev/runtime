package observability

import "miren.dev/runtime/pkg/asm"

func TestInject(reg *asm.Registry) {
	reg.ProvideName("", func() LogWriter {
		return &DebugLogWriter{}
	})
}
