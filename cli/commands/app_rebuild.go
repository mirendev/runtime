package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/ui"
)

func AppRebuild(ctx *Context, opts struct {
	AppCentric
	FormatOptions
	Message string `short:"m" long:"message" description:"Description of this rebuild"`
}) error {
	stdout := ctx.Stdout
	if opts.IsJSON() {
		defer ctx.ProgressToStderr()()
	}
	cl, err := ctx.RPCClient("dev.miren.runtime/build")
	if err != nil {
		return err
	}
	if !cl.HasMethodParam(ctx, "rebuild", "application") {
		return fmt.Errorf("rebuilding requires an updated server; upgrade the server first")
	}
	printer, err := progresswriter.NewPrinter(ctx, os.Stderr, "plain")
	if err != nil {
		return err
	}
	status := newSafeStatusCh(printer.Status())
	defer func() {
		status.Close()
		<-printer.Done()
	}()
	cb := stream.Callback(func(s *build_v1alpha.Status) error {
		u := s.Update()
		switch u.Which() {
		case "message":
			fmt.Fprintln(os.Stderr, u.Message())
		case "error":
			fmt.Fprintln(os.Stderr, u.Error())
		case "log":
			if entry := u.Log(); entry != nil {
				fmt.Fprintln(os.Stderr, entry.Text())
			}
		case "buildkit":
			var solve client.SolveStatus
			if err := json.Unmarshal(u.Buildkit(), &solve); err != nil {
				return err
			}
			return status.Send(ctx, &solve)
		case "deployment":
			if dep := u.Deployment(); dep != nil {
				fmt.Fprintf(os.Stderr, "Rebuild %s: %s\n", dep.DeploymentId(), dep.Phase())
			}
		}
		return nil
	})
	req := &build_v1alpha.DeployRequest{}
	req.SetClusterId(ctx.ClusterName)
	req.SetMessage(opts.Message)
	result, err := build_v1alpha.NewBuilderClient(cl).Rebuild(ctx, opts.App, cb, req)
	status.Close()
	<-printer.Done()
	if err != nil {
		return err
	}
	if err := printer.Err(); err != nil {
		return err
	}
	if result.Version() == "" {
		return fmt.Errorf("rebuild returned no version")
	}
	display := ui.DisplayShortID(result.VersionShortId(), result.Version())
	if err := awaitHealthy(ctx, opts.App, result.Version(), display); err != nil {
		return err
	}
	if opts.IsJSON() {
		return PrintJSONTo(stdout, struct {
			App     string `json:"app"`
			Version string `json:"app_version"`
		}{App: opts.App, Version: result.Version()})
	}
	ctx.Printf("Rebuilt app %s as version %s\n", opts.App, display)
	if result.HasAccessInfo() && result.AccessInfo() != nil {
		displayAccessInfo(ctx, opts.App, result)
	}
	return nil
}
