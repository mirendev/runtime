package discovery

import (
	"time"

	"miren.dev/runtime/pkg/asm"
)

func TestInject(reg *asm.Registry) {
	reg.Provide(func() Lookup {
		return &Memory{
			endpoints: map[string]Endpoint{},
		}
	})

	reg.Provide(func() *Containerd {
		return &Containerd{}
	})

	reg.Register("lookup_timeout", 5*time.Second)
}
