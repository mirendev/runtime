package commands

import (
	"errors"
	"sync"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// checkStatus is the outcome of one diagnostic check.
type checkStatus int

const (
	checkOK checkStatus = iota
	checkWarn
	checkFail
	// checkSkip means the check couldn't run, usually because something it
	// depends on already failed. It is not a problem in its own right.
	checkSkip
)

func (s checkStatus) String() string {
	switch s {
	case checkOK:
		return "ok"
	case checkWarn:
		return "warning"
	case checkFail:
		return "failed"
	case checkSkip:
		return "skipped"
	default:
		return "unknown"
	}
}

// checkResult is what a single check reports.
//
// Summary is the one-line roll-up. Problem carries the full explanation and is
// nil unless something is actually wrong.
//
// The rule that keeps doctor trustworthy: a check may only report a problem it
// can name a fix for. If we can't say what to do about something, it isn't a
// check, it's a fact, and facts belong in the summary line. Diagnostics that
// warn about things nobody can act on are how a doctor command teaches people
// to ignore it.
type checkResult struct {
	Status  checkStatus
	Summary string
	Problem *ui.Diagnostic
}

// check is one diagnostic in the sweep. Group lets `miren doctor <group>` run a
// subset without any of them being a separate implementation.
type check struct {
	Name  string
	Group string
	Run   func(*doctorEnv) checkResult
}

const (
	groupConfig = "config"
	groupServer = "server"
	groupAuth   = "auth"
)

// doctorEnv is the evidence every check reads from.
//
// It is gathered once, up front, and concurrently. Checks are pure functions
// over it, which is what makes the verdict logic testable without a cluster:
// the probes are data, not calls.
type doctorEnv struct {
	ctx *Context

	cfg          *clientconfig.Config
	cluster      *clientconfig.ClusterConfig
	clusterName  string
	clusterCount int
	configErr    error

	// connErr is the result of actually trying to use the cluster. nil means
	// the connection worked.
	connErr error

	// tcp and udp are independent reachability probes. Their value is the
	// cross product with connErr: TCP answering while UDP stays silent is a
	// blocked API port, whereas TCP refusing means nothing is running at all.
	tcp probe
	udp probe
}

// local reports whether the active cluster runs on this machine, which decides
// whether "start the server" advice makes any sense.
func (e *doctorEnv) local() bool {
	return e.cluster != nil && isLocalCluster(e.cluster.Hostname)
}

func (e *doctorEnv) configured() bool {
	return e.cfg != nil && e.clusterCount > 0 && e.cluster != nil
}

// gatherDoctorEnv collects everything the checks need. The three network probes
// are independent, so they run concurrently: doing them in sequence made the
// all-down case take twice as long as its slowest probe.
func gatherDoctorEnv(ctx *Context, opts ConfigCentric) *doctorEnv {
	env := &doctorEnv{ctx: ctx}

	env.cfg, env.configErr = opts.LoadConfig()
	if env.configErr != nil && !errors.Is(env.configErr, clientconfig.ErrNoConfig) {
		return env
	}

	if env.cfg != nil {
		env.cfg.IterateClusters(func(string, *clientconfig.ClusterConfig) error {
			env.clusterCount++
			return nil
		})
		env.cluster, env.clusterName, _ = opts.LoadCluster()
	}

	if env.cluster == nil {
		return env
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		client, err := ctx.RPCClient("entities")
		if err == nil && client != nil {
			defer client.Close()
		}
		env.connErr = err
	}()

	go func() {
		defer wg.Done()
		env.tcp = probeTCP(env.cluster.Hostname)
	}()

	go func() {
		defer wg.Done()
		env.udp = probeQUIC(env.cluster.Hostname)
	}()

	wg.Wait()

	return env
}

// doctorChecks is the registry. `miren doctor` runs all of it; the subcommands
// filter by group.
func doctorChecks() []check {
	return []check{
		{Name: "Configuration", Group: groupConfig, Run: checkConfiguration},
		{Name: "Server", Group: groupServer, Run: checkServer},
		{Name: "Authentication", Group: groupAuth, Run: checkAuthentication},
	}
}

func checksForGroup(group string) []check {
	all := doctorChecks()
	if group == "" {
		return all
	}

	var out []check
	for _, c := range all {
		if c.Group == group {
			out = append(out, c)
		}
	}
	return out
}
