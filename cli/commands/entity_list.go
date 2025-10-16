package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func EntityList(ctx *Context, opts struct {
	ConfigCentric
	Attribute string `short:"a" long:"attribute" description:"Attribute to filter by"`
	Value     string `short:"v" long:"value" description:"Value to filter by"`
	Kind      string `short:"k" long:"kind" description:"Kind of entity to filter by"`
	Address   string `long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	var index entity.Attr

	if opts.Kind != "" {
		res, err := eac.LookupKind(ctx, opts.Kind)
		if err != nil {
			return err
		}

		index = res.Attr()
	} else {
		indexres, err := eac.MakeAttr(ctx, opts.Attribute, opts.Value)
		if err != nil {
			return err
		}

		index = indexres.Attr()
	}

	res, err := eac.List(ctx, index)
	if err != nil {
		return err
	}

	for i, e := range res.Values() {
		if i > 0 {
			_, _ = os.Stdout.Write([]byte("---\n"))
		}
		fres, err := eac.Format(ctx, e)
		if err != nil {
			// Print warning but continue with raw attrs
			fmt.Fprintf(os.Stderr, "Warning: failed to format entity %s: %v\n", e.Entity().Id(), err)

			// Fall back to printing raw attrs
			fmt.Printf("id: %s\n", e.Entity().Id())
			fmt.Printf("attrs:\n")
			for k, v := range e.Entity().Attrs() {
				fmt.Printf("  %d: %v\n", k, v)
			}
		} else {
			_, _ = os.Stdout.Write(fres.Data())
		}
	}

	return nil
}
