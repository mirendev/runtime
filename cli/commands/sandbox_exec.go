package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/ui"
)

// sandboxExecOptions is a named type rather than an inline struct so the flag
// parsing, which now has rules of its own, can be tested directly.
//
// App deliberately has no env:"MIREN_APP" tag. Inside a sandbox that variable
// is injected as the running app's own name, and picking it up here would
// silently switch the command into --app mode and reinterpret the first
// positional as part of the command instead of a sandbox id.
type sandboxExecOptions struct {
	ConfigCentric

	Id      string `short:"i" long:"id" description:"Sandbox ID"`
	App     string `short:"a" long:"app" description:"Pick a running sandbox belonging to this app"`
	Service string `long:"service" description:"With --app, restrict the choice to one service (default: web)"`

	Args []string `rest:"true"`
}

// validate covers the flag combinations mflags can't express. It is called from
// the command body rather than through OptsValidate, which RunCommand skips.
func (o sandboxExecOptions) validate() error {
	if o.App != "" && o.Id != "" {
		return fmt.Errorf("--app and --id both name a sandbox; pass one or the other")
	}

	if o.Service != "" && o.App == "" {
		return fmt.Errorf("--service selects among an app's sandboxes; it needs --app")
	}

	return nil
}

// execChoiceNotice describes where --app landed the caller, or returns "" when
// there was nothing to report. One qualifying sandbox means we didn't choose
// anything — naming it would be noise in front of every single-instance app.
func execChoiceNotice(chosen sandboxCandidate, considered int) string {
	if considered <= 1 {
		return ""
	}

	return fmt.Sprintf("%s (%s), picked from %d running", chosen.Brief, chosen.Service, considered)
}

func SandboxExec(ctx *Context, opts sandboxExecOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

	id := opts.Id
	args := opts.Args

	// Non-nil only when we picked the sandbox ourselves, which changes how a
	// failure downstream has to be explained.
	var chosen *sandboxCandidate

	switch {
	case opts.App != "":
		// Every positional is part of the command here: there is no sandbox id
		// to consume, because that's the whole point of --app.
		pick, considered, err := selectAppSandbox(ctx, opts.App, opts.Service)
		if err != nil {
			return err
		}
		chosen = &pick

		// Only to a terminal: stdout belongs to the sandbox, and plenty of
		// callers merge stderr into it. This has to happen before setupExecIO
		// puts the terminal into raw mode.
		if msg := execChoiceNotice(pick, considered); msg != "" && isInteractiveOutput(ctx.Stderr) {
			ctx.Begin("%s", msg)
		}

		id = pick.ID.String()
	case id == "":
		if len(args) == 0 {
			return fmt.Errorf("sandbox ID is required (pass as first positional arg, via --id, or use --app)")
		}
		id, args = args[0], args[1:]
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/exec")
	if err != nil {
		return err
	}

	sec := exec_v1alpha.NewSandboxExecClient(cl)

	opt := new(exec_v1alpha.ShellOptions)
	opt.SetCommand(args)

	in, out, winUpdates, cleanup := setupExecIO(ctx, opt)
	defer cleanup()

	res, err := sec.Exec(
		ctx,
		"id", id,
		strings.Join(args, " "),
		opt,
		stream.ServeReader(ctx, in),
		stream.ServeWriter(ctx, out),
		stream.ChanReader(winUpdates),
	)
	if err != nil {
		if chosen != nil {
			return chosenSandboxExecError(opts.App, *chosen, err)
		}
		return err
	}

	ctx.SetExitCode(int(res.Code()))
	return nil
}

// chosenSandboxExecError explains a failed exec against a sandbox the caller
// never named. Selecting and connecting are separate round trips, so the
// sandbox we picked can be gone by the time exec looks for it — scaled down,
// crashed, or reaped along with its pool by a deploy. Left unwrapped, the
// caller gets an entity id they have no way to connect to the -a they typed.
//
// This deliberately doesn't try to tell "it vanished" from any other failure:
// the cause is shown either way, and retrying is the right first move for both.
func chosenSandboxExecError(appName string, chosen sandboxCandidate, cause error) error {
	return &ui.Diagnostic{
		Summary: fmt.Sprintf("couldn't exec into the sandbox picked for %q", appName),
		Detail: fmt.Sprintf("We picked %s (%s) from the sandboxes running at the time. "+
			"If it has gone away since, running the same command again picks from "+
			"whatever is running now.", chosen.Brief, chosen.Service),
		Actions: []ui.Action{
			{Command: "miren sandbox list --app " + appName, Note: "see what's running for it"},
		},
		Cause:     cause,
		ShowCause: true,
	}
}
