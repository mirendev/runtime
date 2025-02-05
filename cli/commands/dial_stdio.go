package commands

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
)

func DialStdio(ctx *Context, opts struct {
	Addr string `long:"addr" description:"address to dial" required:"true"`
}) error {
	c, err := net.Dial("unix", opts.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to dial: %v\n", err)
		os.Exit(1)
	}

	defer c.Close()

	fmt.Fprintf(os.Stderr, "connected to %s\n", opts.Addr)

	var wg sync.WaitGroup

	sctx, cancel := context.WithCancel(ctx)

	go func() {
		<-sctx.Done()
		c.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		io.Copy(c, os.Stdin)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		io.Copy(os.Stdout, c)
	}()

	wg.Wait()

	return nil
}
