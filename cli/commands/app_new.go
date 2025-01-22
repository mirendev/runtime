package commands

import (
	"fmt"

	"miren.dev/runtime/app"
)

func AppNew(ctx *Context, opts struct {
	Name string `short:"n" long:"name" description:"Name of the app"`
}) error {

	/*
		ht := &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}

		req, err := http.NewRequest("POST", "https://127.0.0.1:8443/_rpc/lookup/foo", nil)
		if err != nil {
			panic(err)
		}

		resp, err := ht.RoundTrip(req)
		if err != nil {
			panic(err)

		}

		spew.Dump(resp.Status)

		return nil
	*/

	if opts.Name == "" {
		return fmt.Errorf("name is required")
	}

	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	ac := app.CrudClient{Client: cl}

	results, err := ac.New(ctx, opts.Name)
	if err != nil {
		return err
	}

	ctx.Printf("app id: %s\n", results.Id())

	return nil
}
