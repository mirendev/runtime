package commands

import "miren.dev/runtime/components/runner"

func RunnerRun(ctx *Context, opts struct {
	Id     string `long:"id" description:"Runner ID" default:"miren-runner"`
	Server string `long:"server" description:"Server address to connect to"`
}) error {
	r := runner.NewRunner(ctx.Log, ctx.Server, runner.RunnerConfig{})
	return r.Start(ctx)
}
