package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/app"
)

func Set(ctx *Context, opts struct {
	App string `short:"a" long:"app" description:"Application to set"`
	Arg struct {
		Rest []string
	} `positional-args:"yes" required:"yes"`
}) error {
	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	ac := app.CrudClient{Client: cl}

	var envvars []*app.NamedValue

	for _, v := range opts.Arg.Rest {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env var: %s", v)
		}

		ctx.Printf("setting %s...\n", parts[0])

		var nv app.NamedValue

		nv.SetKey(parts[0])
		nv.SetValue(parts[1])

		envvars = append(envvars, &nv)
	}

	res, err := ac.AddEnv(ctx, opts.App, envvars)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", res.VersionId())

	return nil
}
