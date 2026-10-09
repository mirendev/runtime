//go:build blackbox

package blackbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func skipIfNotDistributed(t *testing.T, c *harness.Cluster) {
	t.Helper()
	if !c.IsPeers() {
		t.Skip("skipping: requires distributed environment (BLACKBOX_MODE=peers)")
	}
}

func TestDistributedRunnerList(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	harness.Poll(t, "at least 2 ready runners", 30*time.Second, 3*time.Second,
		func() (bool, string) {
			r := m.Run("runner", "list", "--format", "json")
			if !r.Success() {
				return false, "runner list failed"
			}

			var runners []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(r.Stdout), &runners); err != nil {
				return false, "failed to parse runner list JSON"
			}

			readyCount := 0
			for _, runner := range runners {
				if runner.Status == "ready" || runner.Status == "status.ready" {
					readyCount++
				}
			}
			if readyCount < 2 {
				return false, fmt.Sprintf("only %d ready runners", readyCount)
			}
			return true, ""
		},
	)
}

func TestDistributedRunnerMetrics(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Wait for metrics to be collected (the monitor runs every ~10s)
	harness.Poll(t, "metrics in VictoriaMetrics", 60*time.Second, 5*time.Second,
		func() (bool, string) {
			r := m.PeerExec("coordinator", "curl", "-sf",
				"http://localhost:8428/api/v1/label/__name__/values")
			if !r.Success() {
				return false, "VictoriaMetrics query failed"
			}

			var resp struct {
				Data []string `json:"data"`
			}
			if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
				return false, "failed to parse response"
			}

			hasCPU := false
			hasMem := false
			for _, name := range resp.Data {
				if name == "cpu_usage_seconds_total" {
					hasCPU = true
				}
				if name == "memory_usage_bytes" {
					hasMem = true
				}
			}

			if !hasCPU || !hasMem {
				return false, "waiting for cpu_usage_seconds_total and memory_usage_bytes"
			}
			return true, ""
		},
	)
}

// A runner's own process series reach the coordinator's embedded store under
// the runner entity, each naming its runner. This cannot tell the operational
// fanout from the unread forwarding path, since both write to the same store
// and the runner applies these labels before sending; the handler's branching
// is pinned by the unit tests in servers/runnertelemetry, and this harness
// never enables the managed-metrics sink the fanout ships through.
func TestDistributedRunnerOperationalMetrics(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	for _, name := range []string{"process_start_time_seconds", "miren_build_info", "go_goroutines"} {
		query := name + `{entity="miren/runner"}`
		harness.Poll(t, query, 90*time.Second, 5*time.Second,
			func() (bool, string) {
				r := m.PeerExec("coordinator", "curl", "-sf", "-G",
					"http://localhost:8428/api/v1/query", "--data-urlencode", "query="+query)
				if !r.Success() {
					return false, "VictoriaMetrics query failed"
				}

				var resp struct {
					Data struct {
						Result []struct {
							Metric map[string]string `json:"metric"`
						} `json:"result"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
					return false, "failed to parse response"
				}
				if len(resp.Data.Result) == 0 {
					return false, "no series yet"
				}
				for _, series := range resp.Data.Result {
					if series.Metric["miren_runner"] == "" {
						return false, fmt.Sprintf("series without miren_runner: %v", series.Metric)
					}
				}
				return true, ""
			},
		)
	}
}

func TestDistributedRunnerLogs(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// App logs should flow from the runner through VictoriaLogs and be
	// queryable via the miren logs command on the coordinator.
	harness.Poll(t, "app logs available", 60*time.Second, 3*time.Second,
		func() (bool, string) {
			r := m.Run("logs", "-a", name)
			if r.OutputContains("starting on port") || r.OutputContains("Server starting") {
				return true, ""
			}
			return false, "no app startup log yet"
		},
	)
}

// TestDistributedRunnerNodePort guards the MIR-1032 fix: NodePort DNAT rules
// must install on every node that runs the service controller (coordinator
// and all runners), not only on the node hosting the sandbox. The tcp-echo
// testdata app declares node_port = 7000, so after deploy every peer's nft
// service_nodeports map must contain an entry for tcp/7000.
// runnerListEntry mirrors the JSON emitted by `runner list --format json` for
// the fields the cordon/drain tests care about.
type runnerListEntry struct {
	RunnerID   string   `json:"runner_id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Scheduling string   `json:"scheduling"`
	Cordoned   bool     `json:"cordoned"`
	Labels     []string `json:"labels"`
}

func listRunners(t *testing.T, m *harness.Miren) []runnerListEntry {
	t.Helper()
	r := m.Run("runner", "list", "--format", "json")
	r.RequireSuccess(t)
	var runners []runnerListEntry
	if err := json.Unmarshal([]byte(r.Stdout), &runners); err != nil {
		t.Fatalf("failed to parse runner list JSON: %v", err)
	}
	return runners
}

func isCoordinator(r runnerListEntry) bool {
	for _, l := range r.Labels {
		if l == "role=coordinator" {
			return true
		}
	}
	return false
}

// findRunnerNode returns the stable runner_id of a non-coordinator (distributed
// runner) node, waiting until at least one is ready. runner_id is used rather
// than the mutable Name so lookups stay unambiguous even for unnamed or
// duplicate-named runners; the miren CLI accepts it wherever a runner is named.
func findRunnerNode(t *testing.T, m *harness.Miren) string {
	t.Helper()
	var runnerID string
	harness.Poll(t, "a ready distributed runner node", 30*time.Second, 3*time.Second,
		func() (bool, string) {
			for _, r := range listRunners(t, m) {
				if isCoordinator(r) {
					continue
				}
				if r.Status == "ready" || r.Status == "status.ready" {
					runnerID = r.RunnerID
					return true, ""
				}
			}
			return false, "no ready runner node yet"
		},
	)
	return runnerID
}

func runnerEntry(t *testing.T, m *harness.Miren, runnerID string) runnerListEntry {
	t.Helper()
	for _, r := range listRunners(t, m) {
		if r.RunnerID == runnerID {
			return r
		}
	}
	t.Fatalf("runner %q not found in runner list", runnerID)
	return runnerListEntry{}
}

func TestDistributedRunnerQuery(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)
	runnerID := findRunnerNode(t, m)
	runnerName := runnerEntry(t, m, runnerID).Name

	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	iface := "pq" + hex.EncodeToString(nonce[:])
	// Only the runner has this interface: executing on the coordinator would
	// return an empty result even though both peers share the same kernel.
	m.PeerExec("runner1", "ip", "link", "add", iface, "type", "dummy").RequireSuccess(t)
	t.Cleanup(func() {
		m.PeerExec("runner1", "ip", "link", "delete", iface).RequireSuccess(t)
	})
	m.PeerExec("runner1", "ip", "link", "set", iface, "mtu", "1379").RequireSuccess(t)
	if m.PeerExec("coordinator", "ip", "link", "show", iface).Success() {
		t.Fatal("query fixture interface unexpectedly exists on the coordinator")
	}

	query := func(target, expression string) []byte {
		t.Helper()
		r := m.Run("runner", "query", target, expression)
		r.RequireSuccess(t)
		return []byte(r.Stdout)
	}

	expression := "network where name = " + iface
	data := query(runnerID, expression)
	var snapshot struct {
		Source  string `json:"source"`
		Network []struct {
			Name string `json:"name"`
			MTU  int    `json:"mtu"`
		} `json:"network"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "network" || len(snapshot.Network) != 1 ||
		snapshot.Network[0].Name != iface || snapshot.Network[0].MTU != 1379 {
		t.Fatalf("unexpected runner snapshot: %s", data)
	}

	data = query(runnerName, expression+" avg(mtu) over 300ms every 100ms")
	var aggregate struct {
		Source      string `json:"source"`
		Aggregation struct {
			Values []struct {
				Value float64 `json:"value"`
			} `json:"values"`
		} `json:"aggregation"`
	}
	if err := json.Unmarshal(data, &aggregate); err != nil {
		t.Fatal(err)
	}
	if aggregate.Source != "network" || len(aggregate.Aggregation.Values) != 1 ||
		aggregate.Aggregation.Values[0].Value != 1379 {
		t.Fatalf("unexpected runner aggregate: %s", data)
	}

	r := m.Run("runner", "query", runnerID, "SELECT * FROM network")
	if r.Success() || strings.TrimSpace(r.Stdout) != "" {
		t.Fatalf("invalid query returned exit=%d stdout=%q stderr=%q", r.ExitCode, r.Stdout, r.Stderr)
	}
	r.RequireContains(t, "invalid query syntax")

	r = m.Run("runner", "query", "missing-"+iface, "memory")
	if r.Success() {
		t.Fatal("querying a missing runner succeeded")
	}
	r.RequireContains(t, "not found")
}

func TestDistributedRunnerSandboxQuery(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)
	runnerID := findRunnerNode(t, m)
	id := fmt.Sprintf("portal-query-%d", time.Now().UnixNano())
	sandboxID := "sandbox/" + id
	ctr := func(args ...string) *harness.Result {
		t.Helper()
		return m.PeerExec("runner1", append([]string{"ctr", "--address", "/var/lib/miren/runner/containerd/containerd.sock", "--namespace", "miren"}, args...)...)
	}
	image := "docker.io/library/busybox:1.37"
	ctr("images", "pull", image).RequireSuccess(t)
	// This disposable task exits with a distinctive status without deploying an app.
	ctr("containers", "create", "--read-only", "--net-host",
		"--label", "runtime.computer/entity-id="+sandboxID,
		"--env", "QUERY_TEST_SECRET=must-not-appear", image, id,
		"/bin/sh", "-c", "sleep 1; exit 19").RequireSuccess(t)
	t.Cleanup(func() {
		ctr("tasks", "delete", "--force", id)
		ctr("containers", "delete", id).RequireSuccess(t)
	})
	ctr("containers", "create", "--label", "runtime.computer/entity-id="+sandboxID,
		image, id+"-other", "/bin/true").RequireSuccess(t)
	t.Cleanup(func() {
		ctr("containers", "delete", id+"-other").RequireSuccess(t)
	})
	r := m.Run("runner", "query", runnerID, "sandboxes where container_id = "+id)
	r.RequireSuccess(t)
	var snapshot struct {
		Data []struct {
			SandboxID   string `json:"sandbox_id"`
			ContainerID string `json:"container_id"`
			State       string `json:"state"`
			PID         int    `json:"pid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Data) != 1 || snapshot.Data[0].SandboxID != sandboxID ||
		snapshot.Data[0].ContainerID != id || snapshot.Data[0].State != "no_task" || snapshot.Data[0].PID != 0 {
		t.Fatalf("unexpected inventory: %s", r.Stdout)
	}
	if strings.Contains(r.Stdout, "must-not-appear") || strings.Contains(r.Stdout, "QUERY_TEST_SECRET") {
		t.Fatal("inventory exposed container environment")
	}
	coordinatorID := ""
	for _, node := range listRunners(t, m) {
		if isCoordinator(node) {
			coordinatorID = node.RunnerID
			break
		}
	}
	if coordinatorID == "" {
		t.Fatal("coordinator node not found")
	}
	other := m.Run("runner", "query", coordinatorID, "sandboxes where container_id = "+id)
	other.RequireSuccess(t)
	if err := json.Unmarshal([]byte(other.Stdout), &snapshot); err != nil || len(snapshot.Data) != 0 {
		t.Fatalf("runner fixture appeared on coordinator: %s", other.Stdout)
	}

	result := make(chan *harness.Result, 1)
	go func() {
		result <- m.Run("runner", "query", runnerID,
			fmt.Sprintf("sandbox_events where container_id = %q and action = exit { @exits[] = sum(exit_status) } after 5s { emit @exits }", id))
	}()
	// Give the RPC time to subscribe before starting the already-inventoried task.
	time.Sleep(time.Second)
	ctr("tasks", "start", "--detach", id).RequireSuccess(t)
	r = <-result
	r.RequireSuccess(t)
	var aggregate struct {
		Aggregation struct {
			Rows []struct {
				Values map[string]int `json:"values"`
			} `json:"rows"`
		} `json:"aggregation"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &aggregate); err != nil {
		t.Fatal(err)
	}
	if len(aggregate.Aggregation.Rows) != 1 || aggregate.Aggregation.Rows[0].Values["value"] != 19 {
		t.Fatalf("unexpected task exit aggregate: %s", r.Stdout)
	}
}

func TestDistributedRunnerAppDiskIOQuery(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)
	runnerID := findRunnerNode(t, m)
	app := harness.DeployApp(t, m, harness.AppOptions{Testdata: "image-only"})

	// Independently check the workload's actual cgroup membership, so an OCI
	// path naming assumption or attribution to the pause container cannot pass.
	membership := m.MustRun("sandbox", "exec", "-a", app, "--", "cat", "/proc/self/cgroup")
	actualCgroup := strings.TrimSpace(membership.Stdout)
	if !strings.HasPrefix(actualCgroup, "0::/") {
		t.Fatalf("expected unified cgroup membership, got %q", actualCgroup)
	}
	actualCgroup = strings.TrimPrefix(actualCgroup, "0::")
	selection := fmt.Sprintf("cgroups using (sandboxes where app = %q and state = running) on path = cgroup", app)
	readIO := func() (bytes, ios uint64, identity string) {
		t.Helper()
		r := m.MustRun("runner", "query", runnerID, selection)
		var snapshot struct {
			Data []struct {
				Path        string  `json:"path"`
				ID          string  `json:"id"`
				App         string  `json:"inventory.app"`
				Version     string  `json:"inventory.version"`
				SandboxID   string  `json:"inventory.sandbox_id"`
				ContainerID string  `json:"inventory.container_id"`
				PID         uint32  `json:"inventory.pid"`
				WriteBytes  *uint64 `json:"io.write_bytes"`
				WriteIOS    *uint64 `json:"io.write_ios"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &snapshot); err != nil {
			t.Fatal(err)
		}
		for _, row := range snapshot.Data {
			if row.App != app || !strings.HasPrefix(row.Version, "app_version/") || row.SandboxID == "" || row.ContainerID == "" || row.PID == 0 {
				t.Fatalf("invalid correlated inventory row: %+v", row)
			}
			if row.Path == actualCgroup {
				if row.ID == "" || row.WriteBytes == nil || row.WriteIOS == nil {
					t.Fatalf("app cgroup has no disk I/O accounting: %s", r.Stdout)
				}
				return *row.WriteBytes, *row.WriteIOS, row.ID
			}
		}
		t.Fatalf("no correlated workload row matching %s in %s", actualCgroup, r.Stdout)
		return 0, 0, ""
	}
	beforeBytes, beforeIOS, identity := readIO()
	// This single request discovers the cgroups and computes app-wide rates.
	// There is no client-side cgroup lookup or dynamically constructed path filter.
	result := make(chan *harness.Result, 1)
	go func() {
		result <- m.Run("runner", "query", runnerID, selection+
			" where result.format = rows rate(io.write_bytes), rate(io.write_ios) over 6s every 250ms by inventory.app")
	}()
	time.Sleep(time.Second)
	const writtenBytes = 16 * 1024 * 1024
	m.MustRun("sandbox", "exec", "-a", app, "--", "sh", "-c",
		"for i in 1 2 3 4; do dd if=/dev/zero of=/tmp/portal-query-io-$i bs=1048576 count=4 && sync || exit 1; sleep 0.3; done")
	r := <-result
	r.RequireSuccess(t)
	var aggregate struct {
		Aggregation struct {
			Columns []struct {
				Function string `json:"function"`
				Field    string `json:"field"`
			} `json:"columns"`
			Rows []struct {
				Group  map[string]string `json:"group"`
				Values []json.RawMessage `json:"values"`
			} `json:"rows"`
		} `json:"aggregation"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &aggregate); err != nil {
		t.Fatal(err)
	}
	a := aggregate.Aggregation
	if len(a.Rows) != 1 || a.Rows[0].Group["inventory.app"] != app || len(a.Rows[0].Group) != 1 || len(a.Rows[0].Values) != 2 ||
		len(a.Columns) != 2 || a.Columns[0].Function != "rate" || a.Columns[0].Field != "io.write_bytes" ||
		a.Columns[1].Function != "rate" || a.Columns[1].Field != "io.write_ios" {
		t.Fatalf("unexpected app-rate result: %s", r.Stdout)
	}
	for _, value := range a.Rows[0].Values {
		rate, err := strconv.ParseFloat(string(value), 64)
		if err != nil || rate <= 0 {
			t.Fatalf("expected a positive measured rate, got %s", value)
		}
	}
	t.Logf("single-query app disk I/O: %s bytes/s, %s operations/s", a.Rows[0].Values[0], a.Rows[0].Values[1])
	harness.Poll(t, "app-attributed disk write counters", 15*time.Second, time.Second,
		func() (bool, string) {
			bytes, ios, currentID := readIO()
			if currentID != identity || bytes < beforeBytes || ios < beforeIOS {
				t.Fatal("app cgroup changed identity or its disk counters reset")
			}
			deltaBytes, deltaIOS := bytes-beforeBytes, ios-beforeIOS
			if deltaBytes >= writtenBytes && deltaIOS > 0 {
				t.Logf("app=%s runner=%s cgroup=%s disk writes: %d bytes, %d operations", app, runnerID, actualCgroup, deltaBytes, deltaIOS)
				return true, ""
			}
			return false, fmt.Sprintf("got %d bytes and %d operations, want at least %d bytes and positive operations", deltaBytes, deltaIOS, writtenBytes)
		},
	)
}

// TestDistributedRunnerCordon verifies cordon/uncordon toggle a distributed
// runner's schedulability from the coordinator without going through SIGUSR2.
func TestDistributedRunnerCordon(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	runner := findRunnerNode(t, m)

	m.Run("runner", "cordon", runner, "--reason", "blackbox cordon test").RequireSuccess(t)

	harness.Poll(t, "runner reports cordoned", 15*time.Second, 2*time.Second,
		func() (bool, string) {
			e := runnerEntry(t, m, runner)
			if !e.Cordoned || e.Scheduling != "cordoned" {
				return false, "runner not yet cordoned"
			}
			return true, ""
		},
	)

	m.Run("runner", "uncordon", runner).RequireSuccess(t)

	harness.Poll(t, "runner reports uncordoned", 15*time.Second, 2*time.Second,
		func() (bool, string) {
			if runnerEntry(t, m, runner).Cordoned {
				return false, "runner still cordoned"
			}
			return true, ""
		},
	)
}

// TestDistributedRunnerDrain deploys a stateless app (which prefers the runner
// node), drains that runner from the coordinator, and verifies the app recovers
// (rescheduled onto the coordinator) while the runner ends up cordoned. It then
// uncordons to restore the cluster.
func TestDistributedRunnerDrain(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	runner := findRunnerNode(t, m)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Drain the runner. This cordons it and evicts its sandboxes; the drain
	// command blocks until the node is empty (or times out).
	m.Run("runner", "drain", runner, "--reason", "blackbox drain test", "--timeout", "120").RequireSuccess(t)

	// The runner must end up cordoned and still present (drain does not remove
	// the node).
	harness.Poll(t, "runner cordoned after drain", 15*time.Second, 2*time.Second,
		func() (bool, string) {
			if !runnerEntry(t, m, runner).Cordoned {
				return false, "runner not cordoned after drain"
			}
			return true, ""
		},
	)

	// The app must recover on another node now that the runner is drained.
	harness.WaitForAppReady(t, m, name, 120*time.Second)

	// Restore the cluster for subsequent tests.
	m.Run("runner", "uncordon", runner).RequireSuccess(t)
}

func TestDistributedRunnerNodePort(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "tcp-echo",
	})

	for _, peer := range []string{"coordinator", "runner1"} {
		harness.Poll(t, fmt.Sprintf("nodeport rule on %s", peer), 60*time.Second, 2*time.Second,
			func() (bool, string) {
				r := m.PeerExec(peer, "nft", "list", "map", "inet", "miren", "service_nodeports")
				if !r.Success() {
					return false, fmt.Sprintf("nft list map failed (exit %d): %s", r.ExitCode, strings.TrimSpace(r.Stderr))
				}
				if !strings.Contains(r.Stdout, "tcp . 7000") {
					return false, fmt.Sprintf("service_nodeports has no entry for tcp/7000 on %s yet", peer)
				}
				return true, ""
			},
		)
	}
}

// peerIP returns a single IP from a peer, failing the test if the lookup
// produces nothing usable.
func peerIP(t *testing.T, m *harness.Miren, peer, cmd string) string {
	t.Helper()

	r := m.PeerExec(peer, "bash", "-c", cmd)
	if !r.Success() {
		t.Fatalf("failed to read IP on %s (exit %d): %s", peer, r.ExitCode, strings.TrimSpace(r.Stderr))
	}

	ip := strings.TrimSpace(r.Stdout)
	if ip == "" {
		t.Fatalf("got an empty IP from %s running %q", peer, cmd)
	}
	return ip
}

// startCapture runs tcpdump on an interface and blocks until it has attached,
// so a probe sent immediately afterwards cannot slip past an unarmed capture.
func startCapture(t *testing.T, m *harness.Miren, peer, iface, filter, outFile, pidFile string) {
	t.Helper()

	start := fmt.Sprintf(
		"nohup tcpdump -i %s -A -s0 %s >%s 2>&1 </dev/null & echo $! >%s; disown",
		iface, filter, outFile, pidFile)

	r := m.PeerExec(peer, "bash", "-c", start)
	if !r.Success() {
		t.Fatalf("failed to start tcpdump on %s/%s: %s", peer, iface, strings.TrimSpace(r.Stderr))
	}

	t.Cleanup(func() {
		m.PeerExec(peer, "bash", "-c",
			fmt.Sprintf("kill $(cat %s) 2>/dev/null; rm -f %s %s", pidFile, pidFile, outFile))
	})

	harness.Poll(t, fmt.Sprintf("tcpdump listening on %s/%s", peer, iface), 30*time.Second, time.Second,
		func() (bool, string) {
			r := m.PeerExec(peer, "grep", "-q", "listening on", outFile)
			if !r.Success() {
				return false, "tcpdump has not attached yet"
			}
			return true, ""
		},
	)
}

// stopCapture terminates tcpdump and waits for it to exit. tcpdump block-buffers
// its text output, so a capture file cannot be read meaningfully until the
// process has flushed on the way out. Stopping first also sidesteps the ordering
// between the two capture points: a packet reaches the overlay device before its
// encrypted form leaves the physical one, so reading a live capture could check
// the underlay before the frames we care about had been written.
func stopCapture(t *testing.T, m *harness.Miren, peer, pidFile string) {
	t.Helper()

	m.PeerExec(peer, "bash", "-c", fmt.Sprintf("kill $(cat %s) 2>/dev/null", pidFile))

	harness.Poll(t, fmt.Sprintf("tcpdump on %s exits", peer), 30*time.Second, time.Second,
		func() (bool, string) {
			r := m.PeerExec(peer, "bash", "-c",
				fmt.Sprintf("kill -0 $(cat %s) 2>/dev/null && echo running || echo stopped", pidFile))
			if strings.TrimSpace(r.Stdout) != "stopped" {
				return false, "tcpdump is still running"
			}
			return true, ""
		},
	)
}

// TestDistributedOverlayEncryption sends a known marker between two nodes over
// the sandbox overlay and asserts it cannot be read off the underlay.
//
// The marker is captured twice: on the overlay device, where it must appear,
// and on the physical interface, where it must not. Requiring the overlay
// capture to see it is what keeps this honest. Without that, a probe that
// never left the host, a mistyped filter, or a capture that failed to attach
// would all produce a clean underlay capture and a passing test.
//
// RFD-51 chose WireGuard precisely so that app-to-database traffic crossing
// between hosts is not visible to anyone on the network. This also catches a
// stale VXLAN route that survived an upgrade and kept carrying traffic.
func TestDistributedOverlayEncryption(t *testing.T) {
	c := harness.NewCluster(t)
	skipIfNotDistributed(t, c)
	m := harness.NewMiren(t, c)

	// Deliberately a failure rather than a skip. A cluster running the plaintext
	// backend is the condition this test exists to catch, and a security check
	// that goes quiet in exactly the case it was written for is worth less than
	// no check at all.
	if r := m.PeerExec("coordinator", "ip", "link", "show", "flannel-wg"); !r.Success() {
		t.Fatal("coordinator has no wireguard device: the overlay is unencrypted")
	}

	const probePort = "39999"

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("failed to generate probe marker: %v", err)
	}
	// Distinctive and unlikely to collide with anything else on the wire, so a
	// hit in either capture is unambiguously our probe.
	suffix := hex.EncodeToString(nonce[:])
	marker := "MIRENOVERLAYPROBE" + suffix

	// These live on the peers rather than the machine running the test, so
	// t.TempDir cannot isolate them. Give every run its own names instead:
	// two test processes may legitimately share a persistent dev cluster.
	probeInput := fmt.Sprintf("/tmp/overlay-probe-%s-in.txt", suffix)
	probePID := fmt.Sprintf("/tmp/overlay-probe-%s.pid", suffix)
	underlayCapture := fmt.Sprintf("/tmp/overlay-cap-underlay-%s.txt", suffix)
	underlayPID := fmt.Sprintf("/tmp/overlay-cap-underlay-%s.pid", suffix)
	overlayCapture := fmt.Sprintf("/tmp/overlay-cap-overlay-%s.txt", suffix)
	overlayPID := fmt.Sprintf("/tmp/overlay-cap-overlay-%s.pid", suffix)

	runnerUnderlay := peerIP(t, m, "runner1", "hostname -I | awk '{print $1}'")
	runnerOverlay := peerIP(t, m, "runner1", "ip -4 -o addr show flannel-wg | awk '{print $4}' | cut -d/ -f1")
	t.Logf("runner underlay=%s overlay=%s", runnerUnderlay, runnerOverlay)

	listener := fmt.Sprintf(
		"nohup nc -l -p %s >%s 2>&1 </dev/null & echo $! >%s; disown",
		probePort, probeInput, probePID)
	if r := m.PeerExec("runner1", "bash", "-c", listener); !r.Success() {
		t.Fatalf("failed to start probe listener on runner1: %s", strings.TrimSpace(r.Stderr))
	}
	t.Cleanup(func() {
		m.PeerExec("runner1", "bash", "-c",
			fmt.Sprintf("kill $(cat %s) 2>/dev/null; rm -f %s %s", probePID, probePID, probeInput))
	})

	// Scope the underlay capture to the runner's real address so unrelated
	// chatter on the peers network cannot muddy the result.
	startCapture(t, m, "coordinator", "eth0", "host "+runnerUnderlay,
		underlayCapture, underlayPID)
	startCapture(t, m, "coordinator", "flannel-wg", "",
		overlayCapture, overlayPID)

	send := fmt.Sprintf("echo %s | nc -w 5 %s %s", marker, runnerOverlay, probePort)
	if r := m.PeerExec("coordinator", "bash", "-c", send); !r.Success() {
		t.Fatalf("failed to send probe across the overlay: %s", strings.TrimSpace(r.Stderr))
	}

	harness.Poll(t, "probe arrives on runner1", 30*time.Second, time.Second,
		func() (bool, string) {
			r := m.PeerExec("runner1", "grep", "-q", marker, probeInput)
			if !r.Success() {
				return false, "runner has not received the probe yet"
			}
			return true, ""
		},
	)

	stopCapture(t, m, "coordinator", overlayPID)
	stopCapture(t, m, "coordinator", underlayPID)

	// The control: the same marker must be readable where it is supposed to be
	// readable. If this fails the probe never traversed the overlay, and the
	// underlay assertion below would pass for the wrong reason.
	if r := m.PeerExec("coordinator", "grep", "-q", marker, overlayCapture); !r.Success() {
		t.Fatalf("marker %q never appeared on the overlay device, so this test cannot tell whether the underlay is encrypted", marker)
	}

	// A capture that saw no packets would also report zero marker matches. Read
	// tcpdump's final count rather than the file size, since the listening
	// banner makes even an empty capture file nonempty.
	count := m.PeerExec("coordinator", "awk",
		`/^[0-9]+ packets? captured$/ { captured=$1 } END { if (captured != "") print captured }`,
		underlayCapture)
	captured, err := strconv.Atoi(strings.TrimSpace(count.Stdout))
	if !count.Success() || err != nil {
		t.Fatalf("could not read tcpdump packet count from underlay capture: stdout=%q stderr=%q",
			strings.TrimSpace(count.Stdout), strings.TrimSpace(count.Stderr))
	}
	if captured == 0 {
		t.Fatal("underlay capture saw no packets, so the absence of the marker proves nothing")
	}

	if r := m.PeerExec("coordinator", "grep", "-q", marker, underlayCapture); r.Success() {
		t.Fatalf("overlay traffic is readable on the underlay: found marker %q in the eth0 capture", marker)
	}
}
