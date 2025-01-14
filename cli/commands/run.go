package commands

import (
	"time"
)

func Run(ctx *Context, opts struct {
	App string `short:"a" long:"app" description:"Application to run"`
}) error {
	ctx.Log.DebugContext(ctx, "running application")

	t := time.NewTimer(5 * time.Minute)

	select {
	case <-ctx.Done():
		return nil
	case <-t.C:
		return nil
	}
}
