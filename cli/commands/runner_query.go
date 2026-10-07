package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	query "miren.dev/runtime/pkg/portalquery"
	"miren.dev/runtime/pkg/rpc"
)

// Keep language semantics aligned with the vendored Portal engine. The field
// appendix below is taken from that engine's metadata rather than duplicated.
const runnerQueryReference = `Miren runner query reference — Portal monitoring DSL, not SQL

INVOCATION AND SCOPE
  miren runner query RUNNER 'QUERY'
RUNNER is a name, runner ID, entity ID, or short ID. Quote the whole expression
for the shell. Queries observe the runner host with daemon privileges, not an
app's isolated environment. The coordinator authenticates and authorizes the
caller; the runner accepts queries only from the coordinator certificate.
Output is JSON. Errors exit nonzero without a result on stdout.

Only finite snapshots and aggregates are exposed. Execution and CLI waiting
have a one-minute deadline; choose windows shorter than 1m to leave setup time.
Streaming/registered monitors, capabilities queries, Portal's client-side jq
suffix and folded-output flags are NOT exposed. Pipe CLI stdout to external jq
instead: miren runner query runner1 memory | jq '.memory.used'

COMPACT GRAMMAR
  SOURCE [where CONDITION [and CONDITION ...]]
  SOURCE [where ...] METRIC[, METRIC ...] over DURATION
    [every DURATION] [by FIELD[, FIELD ...]]
  CONDITION: FIELD = VALUE | FIELD == VALUE | FIELD in (VALUE[, VALUE ...])
             FIELD > NUMBER | FIELD >= NUMBER | FIELD < NUMBER | FIELD <= NUMBER
  METRIC: count | sum(FIELD) | avg(FIELD) | min(FIELD) | max(FIELD)
          count_distinct(FIELD) | percentile(FIELD, P) | hist(FIELD) | rate(FIELD)
Use one where clause and AND conditions; no OR, !=, NOT, or arbitrary predicates.
Compact keywords/condition names are case-insensitive; field.NAME is kernel
case-sensitive. Values with spaces or punctuation should be double-quoted.
Built-in name/path filters accept exact strings or ONE leading/trailing *;
interior * and a lone * are not supported. Custom source values are exact,
not glob patterns. Numeric IDs may use decimal or 0x hexadecimal notation.
Numeric comparisons are event-only, on available numeric fields. Missing values
do not match. Thresholds are exact decimal integers/fractions or hex integers.
Only duration_ns additionally accepts durations such as 5ms, 1.5ms or 250us.
Durations use ns, us/µs/μs, ms, s, m, h; positive windows are required.

MIREN SOURCES
  sandboxes — snapshot only; one row per labeled local Miren container,
    including pause containers. JSON: data[]. Fields/filter keys:
    sandbox_id, container_id, app, version, pid, state, cgroup.
    pid is numeric. state is the containerd task state or no_task; pid is zero
    if no task exists. cgroup is the OCI cgroup path. Equality/membership only;
    no numeric inequalities, sampled aggregation, or snapshot block aggregates.
  sandbox_events — event aggregation only; JSON: aggregation/windows/tables.
    Fields/filter/group keys: sandbox_id, container_id, app, version, action,
    pid, exit_status. Numeric: pid, exit_status. action: start, exit, oom.
    pid exists on start/exit; exit_status only on exit. Exec exits are excluded.
Both describe runtime truth, NOT readiness or history. App/version enrichment
is best-effort; app may be empty. Events whose container metadata disappeared
before attribution are omitted. These sources never return environment values.

Example query:
  sandboxes where app = api and state = running

Example query:
  sandbox_events where action in (exit, oom) count over 10s by app,action

INVENTORY-DRIVEN SNAPSHOT CORRELATION (ONE-QUERY APP I/O)
  TARGET using (INVENTORY [where ...]) on TARGET_KEY = INVENTORY_KEY
    [where TARGET_FILTERS] [METRICS over WINDOW [every INTERVAL] [by FIELDS]]
The using clause precedes the target where/aggregate/action block. It selects
custom inventory rows before target collection, not after aggregating a whole
host. on takes one exact string key and requires = (not ==). Supported targets:
  cgroups.path; network.name; cpu.name; sensors.name; containers.id; gpu.uuid.
Inventory can be a registered snapshot-only source such as sandboxes; no custom
event collector or sampler is needed. Inventory filters are equality/membership,
not inequalities. No nested using, aggregate inventories, composite keys, numeric
coercion, event targets, or additional cgroups where path filter are supported.
Other target filters retain normal semantics. Inventory fields become dimensions
inventory.FIELD (grouping/distinct count), not numeric metrics.

Example query:
  cgroups using (sandboxes where app = "my-app" and state = running) on path = cgroup

Example query:
  cgroups using (sandboxes where app = "my-app") on path = cgroup where result.format = rows rate(io.write_bytes), rate(io.write_ios) over 10s every 1s by inventory.app

Example query:
  cgroups using (sandboxes where app = "my-app") on path = cgroup {
    @io[app: inventory.app] = {
      write_bytes_per_second: rate(io.write_bytes),
      write_ios_per_second: rate(io.write_ios)
    }
  } after 10s { emit @io }

Use rate(io.read_bytes)/rate(io.read_ios) for reads, and rate(io.discard_bytes)/
rate(io.discard_ios) for discard. App totals sum concurrent container deltas over
the union of observed intervals, NOT the mean of container rates or lifetime
bytes divided by the window. avg(counter_per_second) averages observations;
sum(counter_per_second) sums over time as well as containers. Neither replaces
app-total rate(counter). Add path or inventory.container_id to by for per-container
rates. Blocks use the default 1s sampler; periodic reports remain event-only.

Without aggregation, correlated snapshots return a flat data array, NOT cgroups.
Keys such as "io.write_bytes" and "inventory.app" are literal dot-containing keys,
not nested objects. Use jq '.data[] | .["io.write_bytes"]'. These are lifetime
counters, not deltas. Missing metrics remain absent. Aggregates keep normal
formats: compact result.format = rows uses a values array aligned with columns;
blocks use named values objects. Example compact group: {"inventory.app":"my-app"}.

Inventory is frozen once per selector inside [start,start+WINDOW); each selector
resolves independently. New sandboxes aren't discovered mid-window. Missing
cgroups contribute no samples; recreated paths, missing observations, resets,
missing device counters or device-set changes require fresh rate baselines.
At least two valid observations are needed. Frozen ownership is attribution at
query start, not a guarantee that a reused path still belongs to the same app.
Exact cgroup paths are read serially; slow reads skip ticks, reads finishing at
the window end are discarded, and there is no mandatory final-boundary sample.
Inventory collection time counts against both the window and Miren's deadline.
No matches: data: [] or empty grouped rows, not a synthetic zero app row;
ungrouped count is zero, rate is null. Caller cancellation propagates, not partial
success; other inventory/target errors fail the entire query.

Identical inventory rows sharing a key deduplicate. Differing rows sharing a key
fail as ambiguous owners. Parent/child cgroup selections fail to prevent double
counting, even across apps; a parent alone includes descendants. Choose disjoint
roots. Inventory cgroup values must be canonical absolute paths relative to the
visible cgroup-v2 mount, not /sys/fs/cgroup filenames, aliases, .., repeated or
trailing slashes, or globs. Missing/null/empty inventory keys are ignored; numeric
keys fail. Inventory data must be flat declared scalar fields with [] when empty.
Limits: 4096 inventory rows BEFORE deduplication, 7 MiB serialized inventory,
4096 target records/snapshot and matched records/tick. Up to 4096 paths are read
serially each tick. Narrow filters on large apps. Reducer/query/output limits
below still apply; a presentation limit does not relax ownership validation.

BUILT-IN SNAPSHOT SOURCES AND FILTERS
  cpu: per-core cumulative seconds. JSON: cpu[]. No selection filters.
  memory: RAM/swap byte gauges. JSON: memory. No selection filters.
  network: interface identity, MTU, addresses, flags, cumulative traffic.
    JSON: network[]. Filter: name = PATTERN.
  kernel: boot time, uptime, load averages, scheduling counters.
    JSON: kernel. No selection filters.
  sensors: temperatures/thresholds in Celsius. JSON: sensors[]. Filter: name.
  gpu: Nvidia identity, temperature, utilization, MiB memory, watts.
    JSON: gpus[]. Filter: name. Requires nvidia-smi and a driver.
  containers: Docker inventory, NOT Miren's containerd inventory.
    JSON: containers[]. Filter: name. Requires Linux/readable Docker socket.
  cgroups: visible Linux cgroup-v2 hierarchy, not v1. JSON: cgroups[].
    Filter: path = PATTERN (relative to /sys/fs/cgroup, beginning with /).
    Paths are relative to the runner's cgroup namespace; / is its visible root.
    io.devices[] is snapshot detail, not separate sampled aggregate records.
  process: current process table. JSON: processes[]. Filters: pid, name.
    Fields include pid, name, started, cpu_seconds, rss_bytes, vms_bytes, user,
    state, threads, command_line. command_line MAY CONTAIN SECRETS.
    action is rejected for snapshots. Visibility follows daemon OS privileges.
Unavailable metrics are omitted, not fabricated zeros. Missing dependencies or
unsupported platforms fail. Sensors can legitimately return an empty list.
All scalar sampling fields, units and counter/gauge semantics are listed in
the engine-derived appendix at the end of this reference.

Example query:
  cgroups where path = /system.slice/*

Example query:
  process where name = worker*

BUILT-IN EVENT SOURCES (REQUIRE AN AGGREGATE WINDOW)
  syscalls: Linux eBPF syscall attempts; default phase entry.
    Filters: pid, syscall = NUMBER/:NAME, syscall in (:fsync,:fdatasync,0),
    phase = entry/completion, paths = true/false, stacks = user/kernel/both.
    Names are case-sensitive native server ABI names, not client ABI numbers.
    Completion adds duration_ns and signed return_value (negative kernel errno).
    Only paired entry/exit calls contribute. Duration includes scheduling/waits,
    not pure device latency; concurrent summed durations can exceed wall time.
    paths captures first FD arguments of supported read/write/fsync/etc calls
    at entry, not open pathname arguments or file contents. It requires native
    64-bit ABI, kernel BTF and readable tracefs metadata. Fields: file.fd,
    file.path, file.error; file.dir is a parent-directory grouping projection.
    file.depth = 0..32 keeps that many parent components (0 = whole parent).
  disk: Linux block requests; default phase entry (block_rq_issue).
    Filters: device (numeric kernel device ID), operation (read/write/discard/
    flush), phase = entry/completion, device_name, rwbs, io.cgroup.path.
    Fields include sector, sectors (512-byte units), request_flags, rwbs,
    device_name, io.cgroup.id/path/error. Completion adds duration_ns and status
    (kernel blk_status_t, zero success, NOT negative errno).
    Completion pairs by request pointer, measures latest issue to full completion
    and requires native 64-bit ABI/BTF/supported kernel layouts. Reissues reset
    timing; this is not total retry latency. rwbs encodes preflush/operation/FUA/
    readahead/sync/metadata flags, not just operation.
  packets: Linux eBPF packet capture; no process ownership inferred.
    Filters: protocol = tcp/udp, direction = incoming/outgoing, src.ip, dst.ip,
    src.port, dst.port. Groupable fields include those plus length (frame bytes).
    Ethernet IPv4/IPv6 packets, not decrypted/reassembled streams; capture is
    best-effort, limited to 2048 bytes per packet, no IPv6 extension decoding.
  process: polled start/exit lifecycle; filters pid, name, action = start/exit.
    Without every, aggregates use events; existing tasks form the initial
    baseline. Short-lived tasks between one-second polls may be missed.
  tracepoint: named Linux scalar-integer kernel probe.
    Compact filters: event = CATEGORY:NAME and fields in (NAME,...), optional
    field.NAME = NUMBER or numeric comparisons. Select 1–16 payload fields.
    common_pid is the executing thread ID; common_type/common_flags/
    common_preempt_count, pointers, arrays, bitfields, dynamic fields are rejected.
    Available field names depend on the running kernel's tracefs format.
Syscall/disk/tracepoint task metadata: pid (process/thread-group ID), tid, name
(kernel comm, up to 15 bytes), process_name (best-effort executable basename),
name_group (verified kernel workers consolidated as kworker), cgroup.path.
These string fields accept exact/edge-glob where filters. Enrichment is receipt-
time procfs/sysfs data, may be missing or briefly stale, and does not reduce
kernel capture traffic. Issuing-task cgroup.path is NOT charged I/O ownership:
disk io.cgroup.path/id comes from the first bio at issue. Journal/shared writes
can have root ownership or no bio; missing ownership is never guessed.
Do not sum parent and descendant cgroup counters: they already overlap.
Linux event sources require kernel/eBPF privileges; packets also CAP_NET_RAW.
Source failures, unmatched boundaries, ring-buffer loss and metadata races can
affect capture; collection diagnostics are subscription-wide, not per-group.

Example query:
  syscalls where phase = completion and syscall in (:fsync,:fdatasync) count, sum(duration_ns), percentile(duration_ns,95) over 10s by process_name

Example query:
  packets where protocol = tcp and dst.port = 80 sum(length) over 5s by dst.ip

Example query:
  tracepoint where event = sched:sched_wakeup and fields in (pid,target_cpu) and field.target_cpu = 2 count over 5s by field.pid

AGGREGATE AND SAMPLING SEMANTICS
count counts observations/records, NOT unique entities or average population.
sum/avg/min/max/percentile/hist accept numeric fields; count_distinct accepts
scalar fields. Strings can be grouped/counted distinctly, not summed/averaged.
percentile(FIELD,P): P in 0..100, nearest rank (no interpolation); 0=min,
100=max. hist uses exact lower-inclusive/upper-exclusive buckets with four
sub-buckets per power of two and a separate zero count; occupied buckets only.
Reductions preserve full-width integers and exact rational arithmetic, rounding
fractional results to at most 18 decimal places. Use precision-preserving JSON
decoders. Empty ungrouped count/sum/distinct returns zero; avg/min/max/percentile
returns null. Empty grouped windows have no rows. Missing optional metrics are
skipped, not zero; missing group fields form a null bucket.

Snapshot-only built-ins sample immediately then on every's interval grid,
default every 1s, minimum 100ms, no longer than the window. Slow reads skip ticks.
Process needs explicit every for snapshot sampling; without it, lifecycle events
are aggregated. Event-only sources reject every in compact queries.
Sampled counter fields reject sum/avg/min/max/percentile/hist: use rate(COUNTER)
or FIELD_per_second instead. rate divides valid counter deltas by the union of
observed intervals, skips baselines/resets/missing identities, no extrapolation;
no valid interval gives null. avg(FIELD_per_second) weights observations equally.
CPU utilization_percent accounts for idle/iowait; process/cgroup cpu_percent
uses 100% for one busy core and may exceed 100%, not normalized to cgroup quota.
Gauges describe current values; sum(gauge) sums samples, not time-integrated use.

Example query:
  memory avg(used) over 5s every 1s

Example query:
  process where name = worker* avg(cpu_percent), max(rss_bytes) over 5s every 1s by pid,name,started

Example query:
  cgroups rate(io.write_ios), rate(io.write_bytes), max(memory_bytes) over 5s every 1s by path

SELECTOR/ACTION BLOCK GRAMMAR
  SELECTOR [where ...] {
    [let ALIAS = FIELD_OR_PROJECTION ...]
    @TABLE[GROUP[, GROUP ...]] = FUNCTION_OR_NAMED_METRIC_MAP
  }
  ... up to eight selectors ...
  after DURATION { emit @TABLE [OUTPUT_CONTROLS]; ... }
SELECTOR is SOURCE, syscalls:entry/completion, disk:entry/completion,
process:start/exit, or tracepoint:CATEGORY:NAME. Block syntax is case-sensitive,
uses double-quoted strings, allows in (...) or in [...], and has no comments.
Quote values containing punctuation (e.g. container IDs with hyphens).
Tracepoint blocks infer field.NAME selections from predicates/actions; explicit
fields in (...) is optional and combines with inferred fields (maximum 16).
One uniquely named table per selector. Locals are scoped to their selector.
GROUP is FIELD, local alias, or ALIAS: FIELD_OR_PROJECTION; [] is ungrouped.
Use count() in blocks; other functions use the same argument forms as compact
metrics. A lone function creates column value. Named maps use {NAME: FUNCTION,
...}; names are identifiers and need not match field names.
let only aliases fields or supported projections, not arbitrary expressions:
  path.prefix(file.path, N) — parent directory, first N components
  stack.user(...) / stack.kernel(...) — shaped stack grouping
Projection use automatically requests path/stack capture. Inline group aliases
also accept projections. No arbitrary scripts, event-level correlations, or
shared-table accumulation across selectors.

Example query:
  sandbox_events where action = exit {
    @exits[app] = {calls: count(), status_total: sum(exit_status)}
  } after 5s { emit @exits order by status_total desc limit 10 }

Example query:
  syscalls:completion where syscall in (:fsync,:fdatasync) {
    let caller = stack.user(from: ["os.(*File).Sync", glob("*Fdatasync")], offsets: false)
    @syncs[proc: process_name, dir: path.prefix(file.path, 3), caller] = {
      calls: count(), elapsed: sum(duration_ns), p95: percentile(duration_ns,95)
    }
  } after 10s { emit @syncs order by elapsed desc limit 25 }

OUTPUT CONTROLS AND PERIODIC REPORTS
Block emit supports order by METRIC [asc|desc] and limit N; these apply after
collection, keep nulls last, and resolve ties deterministically. No order means
the engine's default descending behavior. A limit does not relax capture limits.
Compact where controls: result.format = rows, result.nonzero = true/false,
result.limit = 0..4096 (0 unlimited), result.sort_metric = INDEX (zero-based).
These imply row format; nonzero removes only rows with all numeric-zero metrics,
not null rows. Metric index defaults to the first. total_groups is pre-limit.

For finite non-overlapping event buckets, use:
  every INTERVAL { emit @TABLE [CONTROLS]; clear @TABLE; ... }
  after TOTAL_DURATION { stop }
Each input must participate in an emit/join; clear each exactly once per report.
Reports arrive TOGETHER at completion, not streamed. Every is at least 100ms;
at most 64 buckets, final bucket may be shorter. Snapshot block aggregates are
one-shot with normal default sampling, not periodic. sandboxes cannot aggregate.
Multiple selectors run concurrently with shared boundaries but non-atomic kernel
attachment; failure cancels all. Repeated sources use independent collectors.

Example query:
  sandbox_events { @events[app,action] = count() }
  every 1s { emit @events; clear @events }
  after 5s { stop }

JOINS, ROLLUPS AND COMPUTED COLUMNS
  emit @LEFT [rollup by GROUP[, GROUP ...]]
    [inner|left|full join @RIGHT [rollup by GROUP[, ...]] on KEY[, KEY ...]]
    [select NAME = EXPRESSION[, NAME = EXPRESSION ...]]
    [order by METRIC [asc|desc]] [limit N]
Joins match completed tables one-to-one, NOT individual events or causal links.
Keys are 1–4 output grouping names present on both tables. Use aliases to align
different fields. Numeric keys compare exactly; strings/numbers differ; null
keys never match. Duplicate non-null keys on either input fail even if a limit
would hide them. inner retains matches; left also unmatched left; full both.
Non-key group names are qualified TABLE.GROUP. Joined metrics are nested by
table; qualify ambiguous names in sorting/arithmetic (TABLE.METRIC).
Joins can reuse inputs but cannot chain; periodic joins match corresponding
buckets only, then clear both inputs.
rollup by keeps 1–4 unique existing output group names and recomputes from the
same observations: weighted means, retained percentile samples/distinct sets,
merged histograms and unioned rate intervals, not summary-number arithmetic.
select appends up to eight numeric columns, supporting decimal literals, + - * /,
unary minus and parentheses; use existing metrics, TABLE.METRIC, earlier computed
columns, or window.seconds (actual bucket length). Unknown/ambiguous metrics,
histogram arithmetic and conflicting names fail. Null operands/division by zero
yield null, not zero. Expressions: max 64 nodes, depth 16, 128-byte decimals,
4096-bit results; rational arithmetic, at most 18 decimal places on output.

Example query:
  sandbox_events where action = start { @starts[app] = {starts: count()} }
  sandbox_events where action = exit { @exits[app] = {exits: count()} }
  after 5s {
    emit @starts full join @exits on app select exits_per_s = exits.exits / window.seconds order by exits_per_s desc limit 10
  }

Example query:
  sandbox_events { @events[app,action] = {calls: count()} }
  after 5s { emit @events rollup by app select per_s = calls / window.seconds }

SYMBOLS AND STACKS
symbols is snapshot-only on Linux. Filters:
  target = kernel/binary/process; name = EXACT_OR_EDGE_GLOB OR addresses in (...);
  path = /absolute/binary (binary target); pid = NUMBER (process target);
  limit = 1..4096 (name search default 256); at most 256 explicit hex addresses.
Kernel/process addresses are runtime; binary addresses are link-time virtual.
JSON: symbols with exact hex addresses, names/modules/offsets and unresolved
diagnostics. ELF and Go function metadata supported; no DWARF, demangling, JIT,
BTF inspection or arbitrary kprobe/uprobe/XDP programs.
Syscalls/tracepoints accept stacks = user/kernel/both and stack.depth = 0..64
(0 defaults to 32). Group fields user.stack/kernel.stack are full call paths,
preserving unresolved addresses/errors. Capture/symbolization is best-effort;
missing frame pointers, map collisions, process exit and kernel restrictions
can affect results. Block-event kernel stacks are issuer paths, not application
writeback stacks.
For aggregate grouping, user.stack/kernel.stack support:
  offsets = false/true; drop_bottom = N; drop_top = N; from = PATTERN;
  from in (PATTERN,...); until = PATTERN; top = N (all counts 0..64).
Order: drop_bottom, drop_top, from, until, top, then offset removal.
from starts at the first leaf-side matching frame and keeps its callers; no
match retains the trimmed stack. until keeps leaf frames through first match.
Pattern matching uses exact full function names or one edge wildcard; interior
* is literal (quote Go pointer methods). from lists contain 1–16 alternatives.
Block stack.user/stack.kernel options: offsets, from, until, top, drop_top,
drop_bottom. Use double-quoted literal names or glob("prefix*")/glob("*suffix");
from may be a list. Raw event frames are unchanged by grouping shapes.

Example query:
  symbols where target = process and pid = 1234 and name = handle* and limit = 100

RESULT JSON AND LIMITS
Snapshots: source, time and source-specific field (or custom data array).
Compact count: aggregation.counts[] = {group, count}; other single metrics:
aggregation.values[] = {group, value}, with function/field/percentile metadata.
Multi-metric compact: aggregation.metrics[] in request order.
Compact rows: aggregation.columns and rows[] = {group, values: [VALUE,...]}.
Named blocks: aggregation.table/columns/rows; row values is {METRIC: VALUE},
lone function is values.value. Periodic blocks: windows[]. Multiple selectors:
tables[] entries with source and aggregation/windows. Join/rollup/select reports
use tables[] in emit order even for one selector. Joined row values is nested:
{LEFT: {METRIC: VALUE}, RIGHT: {METRIC: VALUE}, COMPUTED: VALUE}.
Missing join measurements remain null. collection counters are cumulative per
subscription (don't sum them); joins keep separate collections.TABLE. Stack
coverage measures matching received events before shaping/output limits and
separates capture from symbolization; it cannot prove lossless capture.

Maximum query 4096 bytes; at most eight metrics/selectors/output reports,
four grouping fields, 4096 groups and 65536 retained values (percentile samples,
distinct pairs and occupied histogram buckets share the budget). Sampled
snapshots accept at most 4096 records. Script output capped at 7 MiB. Exceeding
limits fails explicitly, not with silently truncated/approximate results.
Engine windows allow up to 1h, but MIREN'S ONE-MINUTE DEADLINE STILL APPLIES.
Windows are half-open [start,end), assigned by server ingestion time, not event
timestamps/history. Setup time is included. A cancellation/source failure fails
the query rather than returning a misleading complete result.

ENGINE-DERIVED FIELD APPENDIX
The lists below are query/group field names, NOT necessarily output JSON paths
or equality-filter keys. Use the source-specific filters above. Numeric event
lists support numeric reductions/comparisons. Sample identities support distinct
counts/grouping; gauges/rates/utilization support numeric reductions; counters
use rate. Optional fields can be missing. Tracepoint field.NAME depends on the
selected kernel fields; Miren custom fields are listed above.
`

func RunnerQuery(ctx *Context, opts struct {
	ConfigCentric

	Node       string `position:"0" usage:"Runner to query (name, ID, or short ID)"`
	Expression string `position:"1" usage:"Portal monitoring query expression (not SQL); quote expressions containing spaces"`
	Reference  bool   `long:"reference" description:"Print the offline query syntax and source reference"`
}) error {
	if opts.Reference {
		var reference strings.Builder
		reference.WriteString(runnerQueryReference)
		for _, selection := range []string{
			"cpu", "memory", "network", "kernel", "sensors", "gpu", "containers", "cgroups", "process",
			"syscalls where phase = completion and paths = true and stacks = both",
			"disk where phase = completion", "packets",
		} {
			request, err := query.ParseMonitorQuery(selection)
			if err != nil {
				return err
			}
			metadata, err := query.Metadata(request)
			if err != nil {
				return err
			}
			fmt.Fprintf(&reference, "\n%s\n  Group/aggregate fields: %s\n  Numeric fields: %s\n",
				selection, strings.Join(metadata.GroupByFields, ", "), strings.Join(metadata.NumericFields, ", "))
			if metadata.Sampled {
				fmt.Fprintf(&reference, "  Sample group fields: %s\n  Sample numeric fields: %s\n",
					strings.Join(metadata.SampleGroupByFields, ", "), strings.Join(metadata.SampleNumericFields, ", "))
				for _, field := range metadata.SampleFields {
					fmt.Fprintf(&reference, "    %s: %s, %s", field.Path, field.Type, field.Semantics)
					if field.Unit != "" {
						fmt.Fprintf(&reference, ", %s", field.Unit)
					}
					if field.Optional {
						reference.WriteString(", optional")
					}
					reference.WriteByte('\n')
				}
			}
		}
		_, err := io.WriteString(ctx.Stdout, reference.String())
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
		return fmt.Errorf("%s", res.Error())
	}

	// Keep Portal's full-width integers intact rather than decoding through float64.
	return PrintJSONTo(ctx.Stdout, json.RawMessage(res.Data()))
}
