package usage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/compute"

	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

// appListing is the answer both surfaces return.
type appListing struct {
	rows     []*usage_v1alpha.AppUsage
	cluster  totals
	total    int32
	warnings []string
}

// listApps is the one implementation behind ListApps (RPC) and HttpListApps
// and HttpGetApp (REST).
//
// A caller could add up the sandbox listing itself, but only by knowing that an
// app's database is a separate sandbox with a different label spelling and a
// different kind. Doing it here means "what is app X using" has one answer that
// does not depend on the caller reconstructing Miren's model.
func (s *Server) listApps(ctx context.Context, f filter, w window, ord ordering) (*appListing, error) {
	// Every kind is loaded regardless of the addon setting: addons are needed
	// either to be counted or to be reported as the zero they were excluded at.
	// The app filter is applied per row rather than in the directory, since an
	// addon's app comes from a different label than a service's.
	dir, err := s.loadDirectory(ctx, filter{node: f.node, includeSystem: true})
	if err != nil {
		return nil, err
	}

	cpu, memory, warnings := s.appSamples(ctx, f.node, w, dir)

	rows, cluster := buildAppRows(dir, cpu, memory, f.app, f.includeAddons)

	out := &appListing{cluster: cluster, warnings: warnings, total: int32(len(rows))}

	sortApps(rows, ord)
	out.rows = rows[:ord.truncate(len(rows))]

	return out, nil
}

// ListApps is the RPC surface.
func (s *Server) ListApps(ctx context.Context, state *usage_v1alpha.ResourceUsageListApps) error {
	args := state.Args()
	w := windowFrom(args.Window())

	listing, err := s.listApps(ctx, selectorToFilter(args.Selector()), w, orderingFrom(args.Ordering()))
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetApps(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpListApps serves GET /api/v1/usage/apps.
func (s *Server) HttpListApps(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpListApps) error {
	args := state.Args()

	w := restWindow(args.Since(), args.Until(), args.Aggregate())
	f := filter{node: args.Node(), includeAddons: !args.HasAddons() || args.Addons()}
	ord := ordering{sort: args.Sort(), order: args.Order(), limit: int(args.Limit())}

	listing, err := s.listApps(ctx, f, w, ord)
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetApps(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetTotalCount(listing.total)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpGetApp serves GET /api/v1/usage/apps/{app}, addressing one app.
func (s *Server) HttpGetApp(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpGetApp) error {
	args := state.Args()

	name := args.App()
	if name == "" {
		return fmt.Errorf("an app name is required")
	}

	w := restWindow(args.Since(), args.Until(), args.Aggregate())
	f := filter{app: name, includeAddons: !args.HasAddons() || args.Addons()}

	listing, err := s.listApps(ctx, f, w, ordering{})
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetWindow(w.encode())
	res.SetWarnings(listing.warnings)

	if len(listing.rows) == 0 {
		return fmt.Errorf("app %q has no running sandboxes", name)
	}

	res.SetUsage(listing.rows[0])

	return nil
}

// appTally accumulates one app's sandboxes before they become a row.
type appTally struct {
	app      string
	appID    string
	services totals
	addons   totals

	serviceCount int64
	addonCount   int64

	// sampled records whether any sandbox of this app reported anything. An app
	// whose every sandbox is silent is reported as stale rather than as idle.
	sampled bool
}

// buildAppRows groups the directory's sandboxes by app and attaches their
// samples.
//
// The entity pass drives the row set here exactly as it does for sandboxes: an
// app is a row because it has sandboxes, not because it has metrics. An app
// whose sandboxes have all stopped reporting still appears, marked stale.
func buildAppRows(
	dir *directory,
	cpu, memory map[string]float64,
	appFilter string,
	includeAddons bool,
) ([]*usage_v1alpha.AppUsage, totals) {
	tallies := map[string]*appTally{}
	order := []string{}

	for _, sb := range dir.sandboxes {
		app := sb.ref.App()
		if app == "" {
			// A sandbox belonging to no app -- a shared addon server, or an
			// unclassifiable one -- has nothing to roll up into.
			continue
		}
		if appFilter != "" && app != appFilter {
			continue
		}

		isAddon := sb.ref.Kind() == string(compute.KindAddon)
		if isAddon && !includeAddons {
			continue
		}

		t, ok := tallies[app]
		if !ok {
			t = &appTally{app: app, appID: sb.ref.AppId()}
			tallies[app] = t
			order = append(order, app)
		}
		if t.appID == "" {
			t.appID = sb.ref.AppId()
		}

		id := sb.ref.Sandbox()
		cores, haveCPU := cpu[id]
		bytes, haveMem := memory[id]
		if haveCPU || haveMem {
			t.sampled = true
		}

		if isAddon {
			t.addons.cpuCores += cores
			t.addons.memoryBytes += int64(bytes)
			t.addonCount++
		} else {
			t.services.cpuCores += cores
			t.services.memoryBytes += int64(bytes)
			t.serviceCount++
		}
	}

	rows := make([]*usage_v1alpha.AppUsage, 0, len(order))
	var cluster totals

	for _, name := range order {
		t := tallies[name]

		total := totals{
			cpuCores:    t.services.cpuCores + t.addons.cpuCores,
			memoryBytes: t.services.memoryBytes + t.addons.memoryBytes,
		}

		var row usage_v1alpha.AppUsage
		row.SetApp(t.app)
		row.SetAppId(t.appID)
		row.SetTotal(total.encode())
		row.SetServices(t.services.encode())
		row.SetAddons(t.addons.encode())
		row.SetSandboxCount(t.serviceCount + t.addonCount)
		row.SetServiceCount(t.serviceCount)
		row.SetAddonCount(t.addonCount)
		row.SetStale(!t.sampled)

		cluster.cpuCores += total.cpuCores
		cluster.memoryBytes += total.memoryBytes

		rows = append(rows, &row)
	}

	return rows, cluster
}

// appSamples fetches per-sandbox usage for the app rollup.
//
// It groups by sandbox rather than by app, even though the answer is per-app,
// because the services-versus-addons split has to be made per sandbox and the
// metric labels do not carry that distinction -- the entity model does. Summing
// in the query would produce a total that could not be broken apart again.
func (s *Server) appSamples(
	ctx context.Context,
	node string,
	w window,
	dir *directory,
) (cpu, memory map[string]float64, warnings []string) {
	cpu = map[string]float64{}
	memory = map[string]float64{}

	if s.Reader == nil {
		return cpu, memory, []string{"no metrics backend configured; usage figures are unavailable"}
	}

	selector := ""
	if node != "" {
		if n := matchNode(dir.nodes, node); n != nil {
			selector = labelSelector(map[string]string{labelNode: string(n.id)})
		}
	}

	dur := w.duration()

	if vals, err := s.instantByLabel(ctx, cpuCoresQuery(labelSandbox, selector, dur, w.aggregate), labelSandbox, w.end); err != nil {
		warnings = append(warnings, "cpu usage unavailable: "+err.Error())
	} else {
		cpu = vals
	}

	if vals, err := s.instantByLabel(ctx, memoryBytesQuery(labelSandbox, selector, dur, w.aggregate), labelSandbox, w.end); err != nil {
		warnings = append(warnings, "memory usage unavailable: "+err.Error())
	} else {
		memory = vals
	}

	return cpu, memory, warnings
}

func sortApps(rows []*usage_v1alpha.AppUsage, ord ordering) {
	key := ord.sort
	desc := ord.descending()

	less := func(a, b *usage_v1alpha.AppUsage) bool {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "memory", "mem":
			return a.Total().MemoryBytes() < b.Total().MemoryBytes()
		case "sandboxes":
			return a.SandboxCount() < b.SandboxCount()
		case "name", "app":
			return a.App() < b.App()
		default:
			return a.Total().CpuCores() < b.Total().CpuCores()
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}
