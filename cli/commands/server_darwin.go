//go:build darwin

package commands

import "fmt"

func Server(ctx *Context, opts struct{}) error {
	return fmt.Errorf("server command is not supported on macOS/Darwin")
}
