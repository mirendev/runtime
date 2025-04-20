package main

import (
	"net"

	"miren.dev/runtime/controllers/service"
)

func main() {
	src := net.ParseIP("10.10.0.1")
	dst := net.ParseIP("10.8.0.2")
	err := service.SetupPortForwarding(src, 80, dst, 8080)
	if err != nil {
		panic(err)
	}
}
