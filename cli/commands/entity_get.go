package commands

import (
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/model"
)

func EntityGet(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Id     string `short:"i" long:"id" description:"Entity ID" required:"true"`
	Expand bool   `long:"expand" description:"Show nested components in full"`
	// Bounded even though you named this entity: you cannot know it holds a
	// megabyte blob until it is already scrolling past. Roomier than the
	// listing's cap, since you did ask for this one.
	MaxValue int    `long:"max-value-len" description:"Elide values longer than this, 0 for no limit" default:"512"`
	Address  string `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	maxValue := int64(opts.MaxValue)
	if opts.IsJSON() {
		maxValue = 0
	}

	res, err := eac.GetDocument(ctx, opts.Id, maxValue)
	if err != nil {
		return err
	}

	raw := res.Document()

	if opts.IsJSON() {
		_, err := ctx.Stdout.Write(append(raw, '\n'))
		return err
	}

	var doc model.Document
	if err := decodeExact(raw, &doc); err != nil {
		return err
	}

	model.RenderCard(ctx.Stdout, &doc, model.RenderOptions{Expand: opts.Expand})

	printElisionHint(ctx.Stderr, doc.Elided())

	return nil
}
