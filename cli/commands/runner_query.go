package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/internal/runnerquery"
	query "miren.dev/runtime/pkg/portalquery"
	"miren.dev/runtime/pkg/rpc"
)

func RunnerQuery(ctx *Context, opts struct {
	ConfigCentric

	Node       string `position:"0" usage:"Runner to query (name, ID, or short ID)"`
	Expression string `position:"1" usage:"Live host query expression (not SQL); quote expressions containing spaces"`
	Reference  bool   `long:"reference" description:"Print the offline query syntax and source reference"`
}) error {
	if opts.Reference {
		reference, err := runnerquery.Reference()
		if err != nil {
			return err
		}
		_, err = io.WriteString(ctx.Stdout, reference)
		return err
	}

	if strings.TrimSpace(opts.Node) == "" || strings.TrimSpace(opts.Expression) == "" {
		return fmt.Errorf("runner and query expression are required (or use --reference)")
	}

	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	queryCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	res, err := runner_v1alpha.NewRunnerRegistrationClient(client).Query(queryCtx, opts.Node, opts.Expression)
	if err != nil {
		return err
	}
	if res.Error() != "" {
		if res.EngineRevision() != "" {
			if res.EngineRevision() != query.Revision {
				return fmt.Errorf("%s (runner query engine %s; CLI reference %s — revisions differ)", res.Error(), res.EngineRevision(), query.Revision)
			}
			return fmt.Errorf("%s (runner query engine %s)", res.Error(), res.EngineRevision())
		}
		return fmt.Errorf("%s", res.Error())
	}

	// Keep full-width integers intact rather than decoding through float64.
	return PrintJSONTo(ctx.Stdout, json.RawMessage(res.Data()))
}
