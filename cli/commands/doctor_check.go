package commands

import (
	"context"
	"errors"
	"sync"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/version"
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

// check is one diagnostic in the sweep.
type check struct {
	Name string
	Run  func(*doctorEnv) checkResult
}

// doctorEnv is the evidence every check reads from.
//
// It is gathered once, up front, and concurrently. Checks are pure functions
// over it, which is what makes the verdict logic testable without a cluster:
// the probes are data, not calls.
//
// The evidence comes in scopes, gathered independently so that none of them
// depends on the others having worked. What this binary is doesn't depend on
// which cluster is selected, and neither does whether a server is running on
// this machine.
type doctorEnv struct {
	ctx *Context

	// CLI scope: this binary, and the newest release it could become.

	cliVersion version.Info

	// latest is the metadata of the "latest" channel, the target `miren
	// upgrade` installs by default. latestErr means the asset service could
	// not be asked, which is a fact about the network rather than about the
	// install.
	latest    *release.Metadata
	latestErr error

	// Cluster scope: the selected cluster.

	cfg          *clientconfig.Config
	cluster      *clientconfig.ClusterConfig
	clusterName  string
	clusterCount int
	configErr    error

	// clusterErr is why the selected cluster couldn't be loaded, and
	// requestedCluster the name that was asked for. Both are needed to tell
	// "you haven't picked a cluster" apart from "the one you picked is broken".
	clusterErr       error
	requestedCluster string

	// connErr is the result of actually trying to use the cluster. nil means
	// the connection worked.
	connErr error

	// tcp and udp are independent reachability probes. Their value is the
	// cross product with connErr: TCP answering while UDP stays silent is a
	// blocked API port, whereas TCP refusing means nothing is running at all.
	tcp probe
	udp probe

	// serverVersionErr distinguishes "could not ask" from "asked, and the
	// server is too old to answer".
	serverVersion    *serverVersion
	serverVersionErr error

	// auth is only attempted when the cluster names an identity.
	auth authResult

	resources    *doctorResources
	resourcesErr error
	orphans      int64
	indexErr     error
}

// local reports whether the active cluster runs on this machine, which decides
// whether "start the server" advice makes any sense.
func (e *doctorEnv) local() bool {
	return e.cluster != nil && isLocalCluster(e.cluster.Hostname)
}

func (e *doctorEnv) configured() bool {
	return e.cfg != nil && e.clusterCount > 0 && e.cluster != nil
}

// gatherDoctorEnv collects everything the checks need. The scopes are
// independent and so are most of the probes within them, so everything that
// touches the network or a subprocess runs concurrently: in sequence, the
// all-down case took the sum of every timeout instead of the longest one.
func gatherDoctorEnv(ctx *Context, opts ConfigCentric) *doctorEnv {
	env := &doctorEnv{ctx: ctx}

	var wg sync.WaitGroup
	wg.Go(func() { gatherCLI(ctx, env) })
	wg.Go(func() { gatherCluster(ctx, opts, env) })
	wg.Wait()

	return env
}

func gatherCLI(ctx context.Context, env *doctorEnv) {
	env.cliVersion = version.GetInfo()

	// The downloader's own timeout is sized for pulling a release, not for a
	// health sweep. The probe timeout keeps an unreachable asset service from
	// holding up every other row.
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	env.latest, env.latestErr = release.NewDownloader().GetVersionMetadata(ctx, "latest")
}

func gatherCluster(ctx *Context, opts ConfigCentric, env *doctorEnv) {
	env.cfg, env.configErr = opts.LoadConfig()
	if env.configErr != nil && !errors.Is(env.configErr, clientconfig.ErrNoConfig) {
		return
	}

	if env.cfg != nil {
		env.cfg.IterateClusters(func(string, *clientconfig.ClusterConfig) error {
			env.clusterCount++
			return nil
		})
		// The error matters as much as the cluster. Without it, a cluster that
		// exists but can't be loaded looks identical to no cluster at all, and
		// doctor reports "no cluster is selected" while hiding the actual
		// reason — the exact failure mode this whole change is about.
		env.cluster, env.clusterName, env.clusterErr = opts.LoadCluster()
		env.requestedCluster = opts.RequestedCluster()
	}

	if env.cluster == nil {
		return
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		client, err := ctx.RPCClient("entities")
		if err == nil && client != nil {
			defer client.Close()
		}
		env.connErr = err
	})

	wg.Go(func() {
		env.serverVersion, env.serverVersionErr = fetchServerVersion(ctx)
	})
	wg.Go(func() {
		env.resources, env.resourcesErr = gatherDoctorResources(ctx)
	})
	wg.Go(func() {
		env.orphans, env.indexErr = gatherDoctorIndex(ctx)
	})

	wg.Go(func() {
		env.tcp = probeTCP(env.cluster.Hostname)
	})

	wg.Go(func() {
		env.udp = probeQUIC(env.cluster.Hostname)
	})

	if env.cluster.Identity != "" {
		wg.Go(func() {
			env.auth = tryAuthenticate(ctx, env.cfg, env.cluster)
		})
	}

	wg.Wait()
}

// doctorChecks is the registry, in the order the rows are printed.
func doctorChecks() []check {
	return []check{
		{Name: "Configuration", Run: checkConfiguration},
		{Name: "Server", Run: checkServer},
		{Name: "Version", Run: checkVersion},
		{Name: "Authentication", Run: checkAuthentication},
		{Name: "Entity indexes", Run: checkEntityIndexes},
		{Name: "Sandboxes", Run: checkSandboxes},
		{Name: "Pools", Run: checkPools},
		{Name: "Disks and volumes", Run: checkDisks},
	}
}
