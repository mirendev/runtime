package run

import "miren.dev/runtime/pkg/asm"

func TestInject(reg *asm.Registry) {
	reg.Provide(func() (*ContainerRunner, error) {
		return &ContainerRunner{}, nil
	})
}
