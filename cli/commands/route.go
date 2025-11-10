package commands

// Route is the default command for the route group - shows the list
func Route(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	return RouteList(ctx, opts)
}
