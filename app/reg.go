package app

import "miren.dev/runtime/pkg/asm"

func Inject(reg *asm.Registry) {
	reg.ProvideName("", func() *AppAccess {
		return &AppAccess{}
	})
}

func TestInject(reg *asm.Registry) {
	Inject(reg)
}
