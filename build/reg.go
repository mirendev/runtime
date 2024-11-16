package build

import "miren.dev/runtime/pkg/asm"

func Inject(reg *asm.Registry) {
	reg.ProvideName("", func() *Buildkit {
		return &Buildkit{}
	})
}

func TestInject(reg *asm.Registry) {
	Inject(reg)
}
