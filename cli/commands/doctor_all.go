package commands

// DoctorAll prints all info information
func DoctorAll(ctx *Context, opts struct {
	ConfigCentric
}) error {
	if err := DoctorConfig(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := DoctorServer(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := DoctorAuth(ctx, opts); err != nil {
		return err
	}
	ctx.Printf("\n")

	if err := DoctorApps(ctx, opts); err != nil {
		return err
	}

	return nil
}
