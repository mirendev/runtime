package commands

func EntityCheck(ctx *Context, opts struct {
	Path string `short:"p" long:"path" description:"Path to check"`
}) error {
	return nil
}
