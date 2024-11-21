package network

import "miren.dev/runtime/pkg/asm"

func TestInject(reg *asm.Registry) {
	var p IPPool
	p.Init("172.16.8.0/24", true)

	reg.Provide(func() *IPPool {
		return &p
	})
}
