package ingress

import "miren.dev/runtime/pkg/asm"

func TestInject(reg *asm.Registry) {
	reg.Register("http_domain", "miren.test")
}
