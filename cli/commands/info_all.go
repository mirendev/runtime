package commands

// InfoAll prints all info information
func InfoAll(ctx *Context, opts struct {
	ConfigCentric
}) error {
	if err := InfoConfig(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := InfoServer(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := InfoAuth(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := InfoApps(ctx, opts); err != nil {
		return err
	}

	return nil
}
