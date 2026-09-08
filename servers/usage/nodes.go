package usage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
)

// nodeListing is the answer both surfaces return.
type nodeListing struct {
	rows     []*usage_v1alpha.NodeUsage
	cluster  totals
	capacity *usage_v1alpha.NodeCapacity
	warnings []string
}

// listNodes is the one implementation behind ListNodes (RPC), HttpListNodes
// and HttpGetNode (REST).
//
// The sandbox-versus-system split is what makes this worth having over a plain
// host metric. Miren's own moving parts -- the container runtime, the image
// builder, the registry, the log pipeline, the coordinator itself -- run
// outside every sandbox cgroup, so they are invisible in the per-sandbox view.
// Subtracting the sandboxes' total from the host's total is what surfaces them,
// and answers the question a per-sandbox listing cannot: the node is hot but no
// app is.
func (s *Server) listNodes(ctx context.Context, f filter, w window, ord ordering) (*nodeListing, error) {
	// One directory load serves both the node list and the per-node sandbox
	// counts, and passing the node filter through lets it use the indexed
	// schedule lookup rather than scanning every sandbox in the cluster.
	dir, err := s.loadDirectory(ctx, filter{node: f.node, includeSystem: true})
	if err != nil {
		return nil, err
	}

	nodes := dir.nodes
	counts := countSandboxesByNode(dir)

	if f.node != "" {
		match := matchNode(nodes, f.node)
		nodes = map[entity.Id]*nodeInfo{}
		if match != nil {
			nodes[match.id] = match
		}
	}

	samples, warnings := s.nodeSamples(ctx, w)

	out := &nodeListing{warnings: warnings}

	rows := make([]*usage_v1alpha.NodeUsage, 0, len(nodes))
	var clusterCapacity struct {
		cores   float64
		memory  int64
		storage int64
	}

	// Node ids are sorted before rows are built so a stable sort has a stable
	// input. Ranging a map directly would give ties a different order on every
	// call, which a refreshing view renders as rows swapping places.
	for _, id := range sortedNodeIds(nodes) {
		n := nodes[id]
		key := string(id)

		var row usage_v1alpha.NodeUsage
		row.SetNode(key)
		row.SetNodeName(n.displayName())
		row.SetRunnerId(n.runnerID)
		row.SetRole(n.role)
		row.SetStatus(n.status)
		row.SetScheduling(n.scheduling)
		row.SetMeasuredAt(standard.ToTimestamp(w.end))

		cores := samples[metricNodeCPUCoresTotal][key]
		memTotal := samples[metricNodeMemTotal][key]
		storageTotal := samples[metricNodeStorageTotal][key]

		var capacity usage_v1alpha.NodeCapacity
		capacity.SetCpuCores(cores)
		capacity.SetMemoryBytes(int64(memTotal))
		capacity.SetStorageBytes(int64(storageTotal))
		row.SetCapacity(&capacity)

		// A node that reported nothing in the window still gets a row. A runner
		// that has gone quiet is exactly what someone is looking for, and
		// dropping it would leave the cluster looking smaller and healthier
		// than it is.
		_, reported := samples[metricNodeCPUCoresTotal][key]
		row.SetStale(!reported)

		usedCores := samples[metricNodeCPUCoresUsed][key]
		usedMem := samples[metricNodeMemUsed][key]
		sandboxCores := samples[sandboxCoresKey][key]
		sandboxMem := samples[sandboxBytesKey][key]

		row.SetTotal(nodeTotals(usedCores, int64(usedMem), cores, memTotal))
		row.SetSandboxes(nodeTotals(sandboxCores, int64(sandboxMem), cores, memTotal))

		// Floored at zero deliberately. Page-cache accounting means the host
		// total and the sum of cgroups do not reconcile exactly, and a small
		// negative remainder is an artifact of that, not a finding. Reporting
		// it as negative would be worse than rounding it away.
		row.SetSystem(nodeTotals(
			max0(usedCores-sandboxCores),
			max0i(int64(usedMem)-int64(sandboxMem)),
			cores, memTotal,
		))

		row.SetLoad1(samples[metricNodeLoad1][key])
		row.SetLoad5(samples[metricNodeLoad5][key])
		row.SetLoad15(samples[metricNodeLoad15][key])
		row.SetStorageUsedBytes(int64(samples[metricNodeStorageUsed][key]))

		row.SetSandboxCount(counts[id].total)
		row.SetRunningSandboxCount(counts[id].running)

		out.cluster.cpuCores += usedCores
		out.cluster.memoryBytes += int64(usedMem)
		clusterCapacity.cores += cores
		clusterCapacity.memory += int64(memTotal)
		clusterCapacity.storage += int64(storageTotal)

		rows = append(rows, &row)
	}

	sortNodes(rows, ord)
	out.rows = rows[:ord.truncate(len(rows))]

	if clusterCapacity.cores > 0 {
		out.cluster.cpuPercent = out.cluster.cpuCores / clusterCapacity.cores * 100
	}
	if clusterCapacity.memory > 0 {
		out.cluster.memPercent = float64(out.cluster.memoryBytes) / float64(clusterCapacity.memory) * 100
	}

	var capacity usage_v1alpha.NodeCapacity
	capacity.SetCpuCores(clusterCapacity.cores)
	capacity.SetMemoryBytes(clusterCapacity.memory)
	capacity.SetStorageBytes(clusterCapacity.storage)
	out.capacity = &capacity

	return out, nil
}

// sortedNodeIds returns the map's keys in a fixed order.
func sortedNodeIds(nodes map[entity.Id]*nodeInfo) []entity.Id {
	ids := make([]entity.Id, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// ListNodes is the RPC surface.
func (s *Server) ListNodes(ctx context.Context, state *usage_v1alpha.ResourceUsageListNodes) error {
	args := state.Args()
	w := windowFrom(args.Window())

	listing, err := s.listNodes(ctx, selectorToFilter(args.Selector()), w, orderingFrom(args.Ordering()))
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetNodes(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetCapacity(listing.capacity)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpListNodes serves GET /api/v1/usage/nodes.
func (s *Server) HttpListNodes(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpListNodes) error {
	args := state.Args()

	w := restWindow(args.Since(), args.Until(), args.Aggregate())
	ord := ordering{sort: args.Sort(), order: args.Order(), limit: int(args.Limit())}

	listing, err := s.listNodes(ctx, filter{}, w, ord)
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetNodes(listing.rows)
	res.SetCluster(listing.cluster.encode())
	res.SetCapacity(listing.capacity)
	res.SetWindow(w.encode())
	res.SetCollectedAt(standard.ToTimestamp(time.Now()))
	res.SetWarnings(listing.warnings)

	return nil
}

// HttpGetNode serves GET /api/v1/usage/nodes/{node}, addressing one host.
func (s *Server) HttpGetNode(ctx context.Context, state *usage_v1alpha.ResourceUsageHttpGetNode) error {
	args := state.Args()

	query := args.Node()
	if query == "" {
		return fmt.Errorf("a node is required")
	}

	w := restWindow(args.Since(), args.Until(), args.Aggregate())

	listing, err := s.listNodes(ctx, filter{node: query}, w, ordering{})
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetWindow(w.encode())
	res.SetWarnings(listing.warnings)

	if len(listing.rows) == 0 {
		return fmt.Errorf("node %q not found", query)
	}

	res.SetUsage(listing.rows[0])

	return nil
}

// Keys under which the per-node rollups of sandbox usage are stashed alongside
// the genuine node metrics. They are not metric names, so they are spelled
// differently on purpose.
const (
	sandboxCoresKey = "@sandbox_cores"
	sandboxBytesKey = "@sandbox_bytes"
)

// nodeSamples runs every node-level query in one pass, keyed by metric name.
func (s *Server) nodeSamples(ctx context.Context, w window) (map[string]map[string]float64, []string) {
	out := map[string]map[string]float64{}

	if s.Reader == nil {
		return out, []string{"no metrics backend configured; usage figures are unavailable"}
	}

	dur := w.duration()
	var warnings []string

	// Capacity gauges are read as last rather than averaged: a host's core
	// count has no meaningful average, and averaging across a resize would
	// report a machine size that never existed.
	gauges := map[string]string{
		metricNodeCPUCoresTotal: aggregateLast,
		metricNodeMemTotal:      aggregateLast,
		metricNodeStorageTotal:  aggregateLast,
		metricNodeCPUCoresUsed:  w.aggregate,
		metricNodeMemUsed:       w.aggregate,
		metricNodeStorageUsed:   w.aggregate,
		metricNodeLoad1:         w.aggregate,
		metricNodeLoad5:         w.aggregate,
		metricNodeLoad15:        w.aggregate,
	}

	for metric, agg := range gauges {
		vals, err := s.instantByLabel(ctx, nodeGaugeQuery(metric, "", dur, agg), labelNode, w.end)
		if err != nil {
			warnings = append(warnings, metric+" unavailable: "+err.Error())
			continue
		}
		out[metric] = vals
	}

	// The same sandbox series the listing reads, grouped by node
	// instead of by sandbox. This is the subtrahend that isolates platform
	// overhead.
	if vals, err := s.instantByLabel(ctx, cpuCoresQuery(labelNode, "", dur, w.aggregate), labelNode, w.end); err != nil {
		warnings = append(warnings, "sandbox cpu rollup unavailable: "+err.Error())
	} else {
		out[sandboxCoresKey] = vals
	}

	if vals, err := s.instantByLabel(ctx, memoryBytesQuery(labelNode, "", dur, w.aggregate), labelNode, w.end); err != nil {
		warnings = append(warnings, "sandbox memory rollup unavailable: "+err.Error())
	} else {
		out[sandboxBytesKey] = vals
	}

	sort.Strings(warnings)

	return out, warnings
}

type sandboxCount struct {
	total   int64
	running int64
}

func countSandboxesByNode(dir *directory) map[entity.Id]sandboxCount {
	counts := map[entity.Id]sandboxCount{}
	for _, wl := range dir.sandboxes {
		id := entity.Id(wl.ref.Node())
		c := counts[id]
		c.total++
		if strings.EqualFold(wl.ref.Status(), "running") {
			c.running++
		}
		counts[id] = c
	}

	return counts
}

func nodeTotals(cores float64, bytes int64, capacityCores, capacityBytes float64) *usage_v1alpha.ResourceTotals {
	t := totals{cpuCores: cores, memoryBytes: bytes}
	if capacityCores > 0 {
		t.cpuPercent = cores / capacityCores * 100
	}
	if capacityBytes > 0 {
		t.memPercent = float64(bytes) / capacityBytes * 100
	}
	return t.encode()
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func max0i(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func sortNodes(rows []*usage_v1alpha.NodeUsage, ord ordering) {
	var (
		less      func(a, b *usage_v1alpha.NodeUsage) bool
		ascending bool
	)

	switch strings.ToLower(strings.TrimSpace(ord.sort)) {
	case "memory", "mem":
		less = func(a, b *usage_v1alpha.NodeUsage) bool { return a.Total().MemoryBytes() < b.Total().MemoryBytes() }
	case "load":
		less = func(a, b *usage_v1alpha.NodeUsage) bool { return a.Load1() < b.Load1() }
	case "sandboxes":
		less = func(a, b *usage_v1alpha.NodeUsage) bool { return a.SandboxCount() < b.SandboxCount() }
	case "name", "node", "runner":
		less, ascending = func(a, b *usage_v1alpha.NodeUsage) bool { return a.NodeName() < b.NodeName() }, true
	default:
		less = func(a, b *usage_v1alpha.NodeUsage) bool { return a.Total().CpuCores() < b.Total().CpuCores() }
	}

	desc := ord.direction(ascending)

	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}
