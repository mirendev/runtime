package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func AppUndelete(ctx *Context, opts struct {
	AppCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return err
	}

	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	var foundApp *core_v1alpha.App
	var foundRevision int64

	for _, e := range res.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())

		var md core_v1alpha.Metadata
		md.Decode(e.Entity())

		if md.Name == opts.App {
			foundApp = &app
			foundRevision = e.Revision()
			break
		}
	}

	if foundApp == nil {
		return fmt.Errorf("app '%s' not found", opts.App)
	}

	if foundApp.DeletedAt.IsZero() {
		ctx.Printf("App '%s' is not deleted\n", opts.App)
		return nil
	}

	attrs := []entity.Attr{
		{
			ID:    entity.DBId,
			Value: entity.AnyValue(foundApp.ID),
		},
		{
			ID:    core_v1alpha.AppDeletedAtId,
			Value: entity.AnyValue(time.Time{}),
		},
	}

	_, err = eac.Patch(ctx, attrs, foundRevision)
	if err != nil {
		return fmt.Errorf("failed to undelete app: %w", err)
	}

	ctx.Printf("App '%s' has been undeleted\n", opts.App)
	return nil
}
