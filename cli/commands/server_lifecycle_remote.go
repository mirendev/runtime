package commands

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverlifecycle"
	lifecyclesrv "miren.dev/runtime/servers/serverlifecycle"
)

// Mirrors components/coordinate.ServerLifecycleService; that package is
// linux-only.
const serverLifecycleService = "dev.miren.runtime/server-lifecycle"

// errServerLifecycleUnsupported: the server answered but predates the
// lifecycle RPC.
var errServerLifecycleUnsupported = errors.New("server does not serve lifecycle operations")

// lifecycleClient connects to the server's lifecycle RPC. The returned close
// releases the connection.
func lifecycleClient(ctx *Context) (*server_v1alpha.ServerLifecycleClient, func(), error) {
	client, err := ctx.RPCClient(serverLifecycleService)
	if err != nil {
		if re, ok := errors.AsType[*rpc.ResolveError](err); ok && re.Kind == rpc.ResolveLookupError {
			return nil, nil, fmt.Errorf("%w: %w", errServerLifecycleUnsupported, err)
		}
		return nil, nil, err
	}
	return server_v1alpha.NewServerLifecycleClient(client), func() { client.Close() }, nil
}

// targetsLocalServer reports whether the server the CLI would connect to is
// on this host: the default address when no cluster is configured, or a
// configured cluster whose hostname resolves to loopback. A cluster reached
// through cloud has no hostname at all, and is not this host.
func (c *Context) targetsLocalServer() bool {
	address := c.Config.ServerAddress
	if c.ClusterConfig != nil {
		if c.ClusterConfig.ViaCloud || c.ClusterConfig.Hostname == "" {
			return false
		}
		address = c.ClusterConfig.Hostname
	} else if c.ClientConfig != nil && c.ClientConfig.ActiveCluster() != "" {
		// A cluster is active but its config did not load; nothing here
		// says where it is, so it is not known to be local.
		return false
	}
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// answered reports whether err is the server's answer rather than a failure
// to reach it. Application errors cross the RPC as cond errors; connection
// and resolution failures do not.
func answered(err error) bool {
	if errors.Is(err, cond.ErrNotFound{}) || errors.Is(err, cond.ErrConflict{}) {
		return true
	}
	if _, ok := errors.AsType[cond.ErrRemote](err); ok {
		return true
	}
	if _, ok := errors.AsType[cond.ErrValidationFailure](err); ok {
		return true
	}
	return false
}

func listRemoteOperations(ctx *Context) ([]*serverlifecycle.Operation, error) {
	client, closeClient, err := lifecycleClient(ctx)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	results, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	ops := make([]*serverlifecycle.Operation, 0, len(results.Operations()))
	for _, op := range results.Operations() {
		if converted := lifecyclesrv.FromRPC(op); converted != nil {
			ops = append(ops, converted)
		}
	}
	return ops, nil
}

func getRemoteOperation(ctx *Context, id string) (*serverlifecycle.Operation, error) {
	client, closeClient, err := lifecycleClient(ctx)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	results, err := client.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !results.HasOperation() {
		return nil, fmt.Errorf("%w: %s", serverlifecycle.ErrNotFound, id)
	}
	return lifecyclesrv.FromRPC(results.Operation()), nil
}

// startRemoteOperation asks the server to record and launch op and follows
// it to a terminal phase. The server answering the start is the one the
// operation replaces, so the follow expects to lose it and reconnects to
// whatever comes up in its place.
func startRemoteOperation(ctx *Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	client, closeClient, err := lifecycleClient(ctx)
	if err != nil {
		return nil, err
	}
	results, err := client.Start(ctx, op.ID, string(op.Action), op.TargetVersion, op.ArtifactType, op.NoRollback, int32(op.ReadyTimeoutSeconds))
	closeClient()
	if err != nil {
		return nil, err
	}
	if !results.HasOperation() {
		return nil, fmt.Errorf("server did not return the started operation")
	}
	started := lifecyclesrv.FromRPC(results.Operation())
	ctx.Info("Started %s operation %s", started.Action, started.ID)
	return followRemoteOperation(ctx, started)
}

// followRemoteOperation polls the operation until it finishes. Unreachable
// stretches are expected: the server is restarting. The patience is the
// operation's own ready timeout, since after that the executor has given up
// too, plus room for a download and the restart itself.
func followRemoteOperation(ctx *Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	readyTimeout := serverlifecycle.DefaultOptions().ReadyTimeout
	if op.ReadyTimeoutSeconds > 0 {
		readyTimeout = time.Duration(op.ReadyTimeoutSeconds) * time.Second
	}
	patience := readyTimeout + 5*time.Minute

	reporter := &operationReporter{ctx: ctx, daemon: serverDaemon}
	reporter.report(op)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var unreachableSince time.Time
	for !op.Done() {
		select {
		case <-ctx.Done():
			return op, fmt.Errorf("stopped following operation %s; it continues on the server (miren server operations show %s)", op.ID, op.ID)
		case <-ticker.C:
		}
		latest, err := getRemoteOperation(ctx, op.ID)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			// The server answered; waiting for a different answer is not
			// going to help. Only an unreachable server is worth outlasting.
			if answered(err) {
				return op, err
			}
			if unreachableSince.IsZero() {
				unreachableSince = time.Now()
				ctx.Info("  server unreachable, waiting for it to come back")
			} else if time.Since(unreachableSince) > patience {
				return op, fmt.Errorf("server has been unreachable for %s while operation %s was %s; last error: %w", patience.Round(time.Second), op.ID, op.Phase, err)
			}
			continue
		}
		if !unreachableSince.IsZero() {
			ctx.Info("  server is back")
			unreachableSince = time.Time{}
		}
		op = latest
		reporter.report(op)
	}
	return op, nil
}
