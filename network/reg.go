package network

import "miren.dev/runtime/pkg/asm"

var testPool IPPool

func init() {
	testPool.Init("172.16.8.0/24", true)
}

func TestInject(reg *asm.Registry) {
	reg.Provide(func() *IPPool {
		return &testPool
	})
}
