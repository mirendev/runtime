package sandbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	apitypes "github.com/containerd/containerd/api/types"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

// newSandboxController creates a SandboxController from TestDeps for testing.
func newSandboxController(d *testutils.TestDeps) (*SandboxController, error) {
	sbMetrics := NewMetrics()
	sbMetrics.Log = d.Log
	sbMetrics.CPUUsage = d.CPU
	sbMetrics.MemUsage = d.Mem
	cfg := SandboxControllerDeps{
		Log:            d.Log,
		CC:             d.CC,
		EAC:            d.EAC,
		Namespace:      d.Namespace,
		NodeId:         compute.NewNodeId("test-node"),
		NetServ:        d.NetServ,
		Bridge:         d.Bridge,
		Subnet:         d.Subnet,
		DataPath:       d.DataPath,
		Tempdir:        d.TempDir,
		LogsMaintainer: d.LogsMaintainer,
		LogWriter:      d.LogWriter,
		StatusMon:      d.StatusMon,
		Resolver:       d.Resolver,
		Metrics:        sbMetrics,
	}
	return NewSandboxController(cfg)
}

func TestSandbox(t *testing.T) {
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC
	ii := testDeps.NewImageImporter()
	ns := ii.Namespace

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer setupCancel()
	setupCtx = namespaces.WithNamespace(setupCtx, ns)

	// Build and import nginx replacement (Go HTTP file server)
	{
		o := buildGoOCIImage(t, "./testdata/testhttp", "/testhttp", []testImageFile{
			{Path: "usr/share/nginx/html", IsDir: true},
		})
		defer o.Close()
		err := ii.ImportImage(setupCtx, o, "mn-nginx:latest")
		require.NoError(t, err)
	}

	// Build and import sort image (CPU burner)
	{
		o := buildGoOCIImage(t, "./testdata/sort", "/bin/tp", nil)
		defer o.Close()
		err := ii.ImportImage(setupCtx, o, "mn-sort:latest")
		require.NoError(t, err)
	}

	// Pull busybox once
	{
		_, err := cc.Pull(setupCtx, "docker.io/library/busybox:latest", containerd.WithPullUnpack)
		require.NoError(t, err)
	}

	// Create a node entity so sandbox ScheduleKeys can reference it.
	// The test sandbox controller uses NodeId "test-node".
	// Only set the kind — Status is a session attribute and can't be set via Put.
	{
		nodeId := compute.NewNodeId("test-node").Id()
		node := &compute.Node{}
		var nodeE entityserver_v1alpha.Entity
		nodeE.SetId(nodeId.String())
		nodeE.SetAttrs(entity.New(entity.DBId, nodeId, node.Encode).Attrs())
		_, err := testDeps.EAC.Put(setupCtx, &nodeE)
		require.NoError(t, err)
	}

	sbName := func() string {
		return idgen.GenNS("sb")
	}

	t.Run("can run a container", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()

		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		cont := entity.New(
			entity.DBId, id,
			sb.Encode(),
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		pt, err := c.Task(ctx, nil)
		r.NoError(err)

		_ = pt

		lbls, err := c.Labels(ctx)
		r.NoError(err)

		r.Equal("mn-nginx", lbls["runtime.computer/app"])

		img, err := co.CC.Pull(ctx, "docker.io/library/busybox:latest", containerd.WithPullUnpack)
		r.NoError(err)

		bc, err := co.CC.NewContainer(ctx,
			"busybox",
			containerd.WithNewSnapshot("busybox-snapshot", img),
			containerd.WithRuntime("io.containerd.runc.v2", nil),
			containerd.WithNewSpec(
				oci.WithDefaultSpec(),
				oci.WithImageConfig(img),
				oci.WithProcessArgs("/bin/sh", "-c", "sleep 100"),
				oci.WithLinuxNamespace(specs.LinuxNamespace{
					Type: specs.NetworkNamespace,
					Path: fmt.Sprintf("/proc/%d/ns/net", pt.Pid()),
				}),
				oci.WithAnnotations(map[string]string{
					"io.kubernetes.cri.container-type": "container",
					"io.kubernetes.cri.sandbox-id":     c.ID(),
				}),
			),
		)
		r.NoError(err)

		ioc := cio.NewCreator(cio.WithStreams(os.Stdin, os.Stdout, os.Stderr))

		task, err := bc.NewTask(ctx, ioc)
		r.NoError(err)

		t.Logf("starting busybox task pid: %d, parent: %d", task.Pid(), pt.Pid())

		err = task.Start(ctx)
		r.NoError(err)

		t.Logf("started busybox task pid: %d", task.Pid())

		pr, pw, err := os.Pipe()
		r.NoError(err)

		defer pr.Close()
		defer pw.Close()

		ioc = cio.NewCreator(cio.WithStreams(os.Stdin, pw, os.Stderr))

		proc, err := task.Exec(ctx, "test", &specs.Process{
			Args: []string{"/bin/sh", "-c", "ip addr show dev eth0 | grep 'inet '"},
			Env:  []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			Cwd:  "/",
		}, ioc)
		r.NoError(err)

		err = proc.Start(ctx)
		r.NoError(err)

		ch, err := proc.Wait(ctx)
		r.NoError(err)

		var exitStatus containerd.ExitStatus
		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case exitStatus = <-ch:
			pw.Close()
		}

		r.NoError(exitStatus.Error(), "exec process returned an error")
		r.Equal(uint32(0), exitStatus.ExitCode(), "exec process exited with non-zero status")

		data, err := io.ReadAll(pr)
		r.NoError(err)

		output := strings.TrimSpace(string(data))
		r.NotEmpty(output, "expected ip addr output but got empty string")

		fields := strings.Fields(output)
		r.GreaterOrEqual(len(fields), 2, "expected at least 2 fields in ip addr output, got: %q", output)

		addr := fields[1]
		r.Equal(ca.Addr().String()+"/24", addr, "address doesn't match")

		t.Run("create on existing sandbox is no-op", func(t *testing.T) {
			searchRes, err := co.CheckSandbox(ctx, &sb, meta)
			r.NoError(err)

			r.Equal(same, searchRes)
		})

	})

	t.Run("calculates cpu usage correctly", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Spec.Container = append(sb.Spec.Container, compute.SandboxSpecContainer{
			Name:  "sort",
			Image: "mn-sort:latest",
		})

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		spec, err := c.Spec(ctx)
		r.NoError(err)

		path := filepath.Join("/sys/fs/cgroup", spec.Linux.CgroupsPath, "cpu.stat")
		fi, err := os.Stat(path)
		r.NoError(err)

		r.True(fi.Mode().IsRegular())

		defer testutils.ClearContainer(ctx, c)

		// Poll: collect metrics and query until VictoriaMetrics has indexed data
		var cpuSeconds float64
		r.Eventually(func() bool {
			_ = co.Metrics.writeStatsToStorage(ctx)
			co.Metrics.CPUUsage.Writer.Flush()

			queryResult, qerr := co.Metrics.CPUUsage.Reader.InstantQuery(ctx,
				fmt.Sprintf(`cpu_usage_seconds_total{entity="%s"}`, id.String()),
				time.Time{})
			if qerr != nil || len(queryResult.Data.Result) == 0 {
				return false
			}

			cpuSecondsStr, ok := queryResult.Data.Result[0].Value[1].(string)
			if !ok {
				return false
			}
			val, perr := strconv.ParseFloat(cpuSecondsStr, 64)
			if perr != nil {
				return false
			}
			cpuSeconds = val
			return cpuSeconds > 0.5
		}, 15*time.Second, 500*time.Millisecond, "should record at least 0.5 CPU seconds")

		t.Logf("total CPU seconds recorded: %f", cpuSeconds)
	})

	// A sandbox that outlives the process which booted it has to get its cgroup
	// metrics re-registered during boot reconciliation. Logs reattach either
	// way, so a sandbox that misses this looks perfectly healthy while its CPU
	// and memory series stop dead at the restart (MIR-1013).
	t.Run("re-registers metrics for sandboxes that survive a restart", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-sort")

		sb.Spec.Container = append(sb.Spec.Container, compute.SandboxSpecContainer{
			Name:  "sort",
			Image: "mn-sort:latest",
		})

		// Boot reconciliation only considers sandboxes scheduled to this node,
		// so the entity needs the schedule key the scheduler would have set.
		schedule := compute.Schedule{
			Key: compute.Key{
				Kind: compute.KindSandbox,
				Node: compute.NewNodeId("test-node").Id(),
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode,
			schedule.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(entity.New(
			entity.DBId, id,
			sb.Encode))

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		defer func() {
			r.NoError(co.StopSandbox(context.WithoutCancel(ctx), id, nil))
		}()

		// Create only updates its in-memory copy of the entity; by the time a
		// real restart happens the store holds the sandbox as RUNNING.
		tco.Status = compute.RUNNING

		var runningE entityserver_v1alpha.Entity
		runningE.SetId(id.String())
		runningE.SetAttrs(entity.New(
			entity.DBId, id,
			tco.Encode,
			schedule.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &runningE)
		r.NoError(err)

		le, _ := sandboxMetricsIdentity(&tco)

		// The process that booted the sandbox is gone, and its cgroup
		// monitoring went with it. The containers keep running.
		r.NoError(co.Metrics.Remove(le))

		co2, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co2.Close()

		r.False(co2.Metrics.Has(le), "a fresh controller starts with nothing registered")

		// Init is what runs boot reconciliation in production.
		r.NoError(co2.Init(ctx))

		r.True(co2.Metrics.Has(le), "boot reconciliation should re-register metrics")

		// Gather reads the cgroups we registered, so it fails loudly if the
		// recovered paths are wrong rather than just silently reporting zeros.
		snapshots, err := co2.Metrics.Gather(le)
		r.NoError(err)
		r.Len(snapshots, 2, "expected the pause container and the sort container")

		usage := map[string]int64{}
		for _, cs := range snapshots {
			usage[cs.Name()] = cs.Metrics().MemoryUsage()
		}

		r.Contains(usage, "", "pause container cgroup should be monitored")
		r.Contains(usage, "sort", "sort container cgroup should be monitored")

		for name, mem := range usage {
			r.Positive(mem, "container %q should report memory usage", name)
		}
	})

	t.Run("configures networking", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Spec.Container = append(sb.Spec.Container, compute.SandboxSpecContainer{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Port: []compute.SandboxSpecContainerPort{
				{
					Name:     "http",
					NodePort: 31001,
					Port:     80,
					Protocol: compute.SandboxSpecContainerPortTCP,
					Type:     "http",
				},
			},
		})

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		lbls, err := c.Labels(ctx)
		r.NoError(err)

		r.Equal("mn-nginx", lbls["runtime.computer/app"])

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		r.Eventually(func() bool {
			resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.Addr().String()))
			if err != nil {
				return false
			}
			resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}, 10*time.Second, 200*time.Millisecond, "HTTP server should become reachable")
	})

	t.Run("sets up host paths as volumes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		spath, err := filepath.Abs("testdata/static-site")
		r.NoError(err)

		sb.Spec.Volume = append(sb.Spec.Volume, compute.SandboxSpecVolume{
			Name:     "static-site",
			Provider: "host",
			Labels:   types.LabelSet("path", spath),
		})

		sb.Spec.Container = append(sb.Spec.Container, compute.SandboxSpecContainer{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Mount: []compute.SandboxSpecContainerMount{
				{
					Destination: "/usr/share/nginx/html",
					Source:      "static-site",
				},
			},
			Port: []compute.SandboxSpecContainerPort{
				{
					Name:     "http",
					Port:     80,
					Protocol: compute.SandboxSpecContainerPortTCP,
					Type:     "http",
				},
			},
		})

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		var body string
		r.Eventually(func() bool {
			resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.Addr().String()))
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			body = string(data)
			return resp.StatusCode == http.StatusOK
		}, 10*time.Second, 200*time.Millisecond, "HTTP server should become reachable")

		r.Contains(body, "this is from testdata/static-site")
	})

	t.Run("sets up named host volumes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Spec.Volume = append(sb.Spec.Volume, compute.SandboxSpecVolume{
			Name:     "static-site",
			Provider: "host",
			Labels:   types.LabelSet("name", "site-data"),
		})

		sb.Spec.Container = append(sb.Spec.Container, compute.SandboxSpecContainer{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Mount: []compute.SandboxSpecContainerMount{
				{
					Destination: "/usr/share/nginx/html",
					Source:      "static-site",
				},
			},
			Port: []compute.SandboxSpecContainerPort{
				{
					Name:     "http",
					Port:     80,
					Protocol: compute.SandboxSpecContainerPortTCP,
					Type:     "http",
				},
			},
		})

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		rawPath := filepath.Join(co.DataPath, "host-volumes", "site-data", "index.html")

		err = os.WriteFile(rawPath, []byte("this is from testdata/static-site"), 0644)
		r.NoError(err)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		var body string
		r.Eventually(func() bool {
			resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.Addr().String()))
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			body = string(data)
			return resp.StatusCode == http.StatusOK
		}, 10*time.Second, 200*time.Millisecond, "HTTP server should become reachable")

		r.Contains(body, "this is from testdata/static-site")
	})

	checkClosed := func(t *testing.T, c io.Closer) {
		t.Helper()
		err := c.Close()
		if err != nil {
			t.Errorf("failed to close: %v", err)
		}
	}

	t.Run("cleans up dead sandboxes older than 1 hour", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		sbc, err := newSandboxController(testDeps)
		r.NoError(err)

		defer checkClosed(t, sbc)

		err = sbc.Init(ctx)
		r.NoError(err)

		// Schedule key so sandboxes match the node-scoped index used by Periodic.
		schedule := compute.Schedule{
			Key: compute.Key{
				Kind: compute.KindSandbox,
				Node: compute.NewNodeId("test-node").Id(),
			},
		}

		// Create a few sandboxes
		sbID1 := entity.Id(sbName())
		sb1 := &compute.Sandbox{
			ID:     sbID1,
			Status: compute.RUNNING,
		}

		// Store sandbox in entity store with ident
		var rpcE1 entityserver_v1alpha.Entity
		rpcE1.SetId(sbID1.String())
		rpcE1.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, sbID1.String()),
			sb1.Encode,
			schedule.Encode,
		).Attrs())
		_, err = sbc.EAC.Put(ctx, &rpcE1)
		r.NoError(err)

		// Now retrieve it to get the entity with proper metadata
		result1, err := sbc.EAC.Get(ctx, sbID1.String())
		r.NoError(err)

		meta1 := &entity.Meta{
			Entity:   result1.Entity().Entity(),
			Revision: result1.Entity().Revision(),
		}

		err = sbc.Create(ctx, sb1, meta1)
		r.NoError(err)

		// Create a second sandbox
		sbID2 := entity.Id(sbName())
		sb2 := &compute.Sandbox{
			ID:     sbID2,
			Status: compute.RUNNING,
		}

		// Store sandbox in entity store with ident
		var rpcE2 entityserver_v1alpha.Entity
		rpcE2.SetId(sbID2.String())
		rpcE2.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, sbID2.String()),
			sb2.Encode,
			schedule.Encode,
		).Attrs())
		_, err = sbc.EAC.Put(ctx, &rpcE2)
		r.NoError(err)

		// Now retrieve it to get the entity with proper metadata
		result2, err := sbc.EAC.Get(ctx, sbID2.String())
		r.NoError(err)

		meta2 := &entity.Meta{
			Entity:   result2.Entity().Entity(),
			Revision: result2.Entity().Revision(),
		}

		err = sbc.Create(ctx, sb2, meta2)
		r.NoError(err)

		// Stop the first sandbox (this should set status to DEAD)
		err = sbc.Delete(ctx, sbID1, nil)
		r.NoError(err)

		// Manually update the UpdatedAt timestamp to be older than our test time horizon
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sbID1.String())

		// Set UpdatedAt to 3 seconds ago by updating the entity
		rpcE.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, sbID1.String()),
			(&compute.Sandbox{
				Status: compute.DEAD,
			}).Encode,
			schedule.Encode,
		).Attrs())

		_, err = sbc.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Use a negative time horizon so the cutoff is slightly in the future,
		// avoiding a race where updatedAt ≈ now causes Before(cutoff) to fail.
		err = sbc.Periodic(ctx, -time.Millisecond)
		r.NoError(err)

		// Check that the old dead sandbox was deleted
		resp, err := sbc.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
		r.NoError(err)

		// Verify sbID1 was cleaned up and sbID2 remains
		var found1 bool
		var remainingSb compute.Sandbox
		for _, v := range resp.Values() {
			var sb compute.Sandbox
			sb.Decode(v.Entity())
			if sb.ID == sbID1 {
				found1 = true
			}
			if sb.ID == sbID2 {
				remainingSb = sb
			}
		}
		r.False(found1, "dead sandbox sbID1 should have been cleaned up")
		r.Equal(sbID2, remainingSb.ID)
		r.Equal(compute.RUNNING, remainingSb.Status)

		// Clean up the remaining sandbox
		err = sbc.Delete(ctx, sbID2, nil)
		r.NoError(err)
	})

	t.Run("port detection works correctly", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer checkClosed(t, co)

		r.NoError(co.Init(ctx))

		ctx = namespaces.WithNamespace(ctx, ns)

		// Create a sandbox with a container that binds to port 8080
		id := entity.Id(sbName())

		var sb compute.Sandbox
		sb.ID = id
		sb.Labels = append(sb.Labels, "runtime.computer/app=test-port")

		// Add a container that runs a simple HTTP server on port 8080
		sb.Spec = compute.SandboxSpec{
			Container: []compute.SandboxSpecContainer{
				{
					Name:  "http-server",
					Image: "docker.io/library/busybox:latest",
					// Run a simple HTTP server using nc (netcat) on port 8080
					Command: "while true; do echo -e 'HTTP/1.1 200 OK\n\nHello' | nc -l -p 8080; done",
					Port: []compute.SandboxSpecContainerPort{
						{Port: 8080, Protocol: compute.SandboxSpecContainerPortTCP},
					},
				},
			},
		}

		cont := entity.New(
			entity.Ref(entity.DBId, id),
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		// Create the sandbox
		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		// Verify network was allocated
		r.Len(tco.Network, 1)
		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)
		ipAddr := ca.Addr().String()

		// Get the container ID for port checking
		containerID := fmt.Sprintf("%s-%s", containerPrefix(id), "http-server")

		// Wait for the port to be detected as bound (with timeout)
		portBound := false
		deadline := time.Now().Add(30 * time.Second)

		for time.Now().Before(deadline) {
			co.portMu.Lock()
			if ports, ok := co.portMap[containerID]; ok {
				for _, p := range ports.Ports {
					if p.Port == 8080 {
						portBound = true
						co.Log.Info("Port detected as bound", "port", p.Port, "addr", p.Addr)
						break
					}
				}
			}
			co.portMu.Unlock()

			if portBound {
				break
			}

			time.Sleep(500 * time.Millisecond)
		}

		r.True(portBound, "Port 8080 should have been detected as bound")

		// Try to connect to the port to verify it's actually listening
		resp, err := http.Get(fmt.Sprintf("http://%s:8080", ipAddr))
		if err == nil {
			defer checkClosed(t, resp.Body)
			r.Equal(200, resp.StatusCode, "HTTP server should respond with 200 OK")
		}

		// Clean up
		err = co.Delete(ctx, id, nil)
		r.NoError(err)

		// NOTE we only track port binding now, not unbounding.
	})

	t.Run("multiple ports detection", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer checkClosed(t, co)

		r.NoError(co.Init(ctx))

		ctx = namespaces.WithNamespace(ctx, ns)

		// Create a sandbox with a container that binds to multiple ports
		id := entity.Id(sbName())

		var sb compute.Sandbox
		sb.ID = id

		// Add a container that listens on multiple ports
		sb.Spec = compute.SandboxSpec{
			Container: []compute.SandboxSpecContainer{
				{
					Name:  "multi-port",
					Image: "docker.io/library/busybox:latest",
					// Script that listens on multiple ports
					Command: `sh -c '
						nc -l -p 8080 &
						nc -l -p 8081 &
						nc -l -p 8082 &
						wait
					'`,
					Port: []compute.SandboxSpecContainerPort{
						{Port: 8080, Protocol: compute.SandboxSpecContainerPortTCP},
						{Port: 8081, Protocol: compute.SandboxSpecContainerPortTCP},
						{Port: 8082, Protocol: compute.SandboxSpecContainerPortTCP},
					},
				},
			},
		}

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		// Create the sandbox
		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		// Get the container ID
		containerID := fmt.Sprintf("%s-%s", containerPrefix(id), "multi-port")

		// Wait for all ports to be detected as bound
		expectedPorts := map[int]bool{8080: false, 8081: false, 8082: false}
		deadline := time.Now().Add(30 * time.Second)

		for time.Now().Before(deadline) {
			co.portMu.Lock()
			if ports, ok := co.portMap[containerID]; ok {
				for _, p := range ports.Ports {
					if _, expected := expectedPorts[p.Port]; expected {
						expectedPorts[p.Port] = true
						co.Log.Info("Port detected as bound", "port", p.Port)
					}
				}
			}
			co.portMu.Unlock()

			// Check if all ports are detected
			allDetected := true
			for _, detected := range expectedPorts {
				if !detected {
					allDetected = false
					break
				}
			}

			if allDetected {
				break
			}

			time.Sleep(500 * time.Millisecond)
		}

		// Verify all ports were detected
		for port, detected := range expectedPorts {
			r.True(detected, fmt.Sprintf("Port %d should have been detected as bound", port))
		}

		// Clean up
		err = co.Delete(ctx, id, nil)
		r.NoError(err)
	})

	t.Run("waitForPort respects timeout", func(t *testing.T) {
		r := require.New(t)

		c := &SandboxController{
			Log:     slog.Default(),
			portMap: make(map[string]*containerPorts),
		}
		c.portMu = sync.Mutex{}
		c.portCond = sync.NewCond(&c.portMu)

		ctx := context.Background()

		// Test immediate return when port is already bound
		c.portMap["test-id"] = &containerPorts{
			Ports: []observability.BoundPort{{Port: 8080}},
		}

		err := c.WaitForPort(ctx, "test-id", 8080, 5*time.Second)
		r.NoError(err, "should return immediately when port is already bound")

		// Test timeout when port never binds
		start := time.Now()
		err = c.WaitForPort(ctx, "test-id", 9999, 100*time.Millisecond)
		elapsed := time.Since(start)

		r.Error(err, "should timeout when port never binds")
		r.Contains(err.Error(), "timeout waiting for port 9999")
		r.True(elapsed >= 100*time.Millisecond && elapsed < 200*time.Millisecond,
			"should timeout within expected window, got %v", elapsed)

		// Test port binding during wait
		go func() {
			time.Sleep(50 * time.Millisecond)
			c.SetPortStatus("test-id", observability.BoundPort{Port: 7777}, observability.PortStatusBound)
		}()

		start = time.Now()
		err = c.WaitForPort(ctx, "test-id", 7777, 5*time.Second)
		elapsed = time.Since(start)

		r.NoError(err, "should return when port is bound during wait")
		r.True(elapsed >= 50*time.Millisecond && elapsed < 150*time.Millisecond,
			"should detect port binding quickly, got %v", elapsed)

		// Test context cancellation
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start = time.Now()
		err = c.WaitForPort(ctx, "test-id", 6666, 5*time.Second)
		elapsed = time.Since(start)

		r.Error(err, "should error on context cancellation")
		r.Contains(err.Error(), "context cancelled")
		r.True(elapsed >= 50*time.Millisecond && elapsed < 150*time.Millisecond,
			"should detect context cancellation quickly, got %v", elapsed)
	})

	t.Run("reattaches logs after controller restart", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		lr := testDeps.Logs

		// Create first controller instance
		co1, err := newSandboxController(testDeps)
		r.NoError(err)

		r.NoError(co1.Init(ctx))
		defer co1.Close()

		ctx = namespaces.WithNamespace(ctx, ns)

		id := entity.Id(sbName())

		// Create a sandbox with a container that logs continuously
		var sb compute.Sandbox
		sb.ID = id
		sb.Labels = append(sb.Labels, "runtime.computer/app=heavy-logger")
		sb.Spec = compute.SandboxSpec{
			Container: []compute.SandboxSpecContainer{
				{
					Name:  "logger",
					Image: "docker.io/library/busybox:latest",
					// Continuously write to stdout - will fill the buffer if not drained
					// Each line is ~80 bytes, so this generates plenty of data
					Command: `sh -c 'i=0; while true; do echo "Log line $i: test message to fill stdout buffer"; i=$((i+1)); sleep 0.05; done'`,
				},
			},
		}

		cont := entity.New(
			entity.DBId, id,
			sb.Encode,
		)

		// Store sandbox in entity store
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co1.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co1.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		// Create the sandbox - this attaches logs initially
		err = co1.Create(ctx, &tco, meta)
		r.NoError(err)

		// Wait for the container task to be running
		containerID := fmt.Sprintf("%s-%s", containerPrefix(id), "logger")
		var subC containerd.Container
		r.Eventually(func() bool {
			var lerr error
			subC, lerr = cc.LoadContainer(ctx, containerID)
			if lerr != nil {
				return false
			}
			task, lerr := subC.Task(ctx, nil)
			if lerr != nil {
				return false
			}
			st, lerr := task.Status(ctx)
			return lerr == nil && st.Status == containerd.Running
		}, 10*time.Second, 200*time.Millisecond, "task should be running")

		// Now test the reattach function directly by calling it
		// This simulates what happens during controller restart
		err = co1.reattachLogs(ctx, &tco, containerID, "logger", "")
		r.NoError(err, "should be able to reattach logs to running container")

		// Verify task stays running after reattach and logs are collected.
		// If reattachment didn't work, the stdout buffer would fill and process would block.
		r.Eventually(func() bool {
			task, terr := subC.Task(ctx, nil)
			if terr != nil {
				return false
			}
			st, terr := task.Status(ctx)
			if terr != nil || st.Status != containerd.Running {
				return false
			}
			logs, lerr := lr.Read(ctx, sb.ID.String(), observability.WithLimit(100))
			if lerr != nil || len(logs) <= 10 {
				return false
			}
			for _, logEntry := range logs {
				if strings.Contains(logEntry.Body, "Log line") {
					return true
				}
			}
			return false
		}, 15*time.Second, 500*time.Millisecond, "task should stay running and logs should be collected")

		// Clean up
		err = co1.Delete(ctx, id, nil)
		r.NoError(err)
	})

	t.Run("checkNetworkHealth returns true for sandboxes without ports", func(t *testing.T) {
		r := require.New(t)

		c := &SandboxController{
			Log: slog.Default(),
		}

		ctx := context.Background()

		// Sandbox with network but no exposed ports (background worker)
		sb := &compute.Sandbox{
			ID: entity.Id("test-sb"),
			Network: []compute.Network{
				{Address: "10.8.0.1/32"},
			},
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name: "worker",
						Port: []compute.SandboxSpecContainerPort{}, // No ports
					},
				},
			},
		}

		healthy := c.checkNetworkHealth(ctx, sb)
		r.True(healthy, "sandbox without ports should be considered healthy")
	})

	t.Run("checkNetworkHealth returns true for sandboxes without network", func(t *testing.T) {
		r := require.New(t)

		c := &SandboxController{
			Log: slog.Default(),
		}

		ctx := context.Background()

		// Sandbox with no network allocated
		sb := &compute.Sandbox{
			ID:      entity.Id("test-sb"),
			Network: []compute.Network{},
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name: "app",
						Port: []compute.SandboxSpecContainerPort{
							{Port: 8080},
						},
					},
				},
			},
		}

		healthy := c.checkNetworkHealth(ctx, sb)
		r.True(healthy, "sandbox without network should be considered healthy")
	})

	// The "unreachable ports" check that used to live here verified the old
	// TCP-dial behavior, which checkNetworkHealth no longer does (MIR-1108).
	// The current implementation reads /proc/<pause-pid>/net/tcp via
	// checkPort; that path is exercised end-to-end by portmonitor_test.go.

	t.Run("PENDING sandbox with running containers should transition to RUNNING", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		ctx = namespaces.WithNamespace(ctx, ns)

		co, err := newSandboxController(testDeps)
		r.NoError(err)

		defer co.Close()

		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox
		sb.ID = id
		sb.Status = compute.PENDING
		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		cont := entity.New(
			entity.DBId, id,
			sb.Encode(),
		)

		// Store sandbox in entity store with PENDING status
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(id.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, id,
			sb.Encode).Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Retrieve it to get the entity with proper metadata
		result, err := co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta := &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		// First call to Create - this will start the sandbox
		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		// Verify sandbox was created and is running
		c, err := cc.LoadContainer(ctx, pauseContainerId(id))
		r.NoError(err)
		r.NotNil(c)
		defer testutils.ClearContainer(ctx, c)

		// Verify task is running
		pt, err := c.Task(ctx, nil)
		r.NoError(err)
		status, err := pt.Status(ctx)
		r.NoError(err)
		r.Equal(containerd.Running, status.Status)

		// Simulate the bug scenario: entity status update failed due to conflict
		// So entity is still PENDING even though container is running
		result, err = co.EAC.Get(ctx, id.String())
		r.NoError(err)

		var currentSb compute.Sandbox
		currentSb.Decode(result.Entity().Entity())

		// Manually set it back to PENDING to simulate the conflict scenario
		// Also set CreatedAt to be stale (> 2 minutes old) so it passes the staleness check
		// CreatedAt is honored by the entity store (unlike UpdatedAt which is always set to now)
		currentSb.Status = compute.PENDING
		staleTime := time.Now().Add(-3 * time.Minute)
		staleEntity := entity.New(currentSb.Encode())
		staleEntity.SetCreatedAt(staleTime)
		rpcE.SetId(id.String())
		rpcE.SetAttrs(staleEntity.Attrs())
		_, err = co.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Now fetch it again for the second reconciliation
		result, err = co.EAC.Get(ctx, id.String())
		r.NoError(err)

		meta = &entity.Meta{
			Entity:   result.Entity().Entity(),
			Revision: result.Entity().Revision(),
		}

		var pendingSb compute.Sandbox
		pendingSb.Decode(result.Entity().Entity())
		r.Equal(compute.PENDING, pendingSb.Status, "sandbox should be PENDING before second reconciliation")

		// Second call to Create - this should update status to RUNNING
		// because the sandbox containers are already running
		err = co.Create(ctx, &pendingSb, meta)
		r.NoError(err)

		// Verify status was updated to RUNNING in the entity
		result, err = co.EAC.Get(ctx, id.String())
		r.NoError(err)

		var finalSb compute.Sandbox
		finalSb.Decode(result.Entity().Entity())
		r.Equal(compute.RUNNING, finalSb.Status, "sandbox should transition from PENDING to RUNNING when containers are healthy")
	})

}

// mockTask implements containerd.Task for testing monitorTaskExit
type mockTask struct {
	waitCalls int
	waitErr   error
	waitCh    chan containerd.ExitStatus
}

func (m *mockTask) ID() string                  { return "mock-task" }
func (m *mockTask) Pid() uint32                 { return 1234 }
func (m *mockTask) Start(context.Context) error { return nil }
func (m *mockTask) Delete(context.Context, ...containerd.ProcessDeleteOpts) (*containerd.ExitStatus, error) {
	return nil, nil
}
func (m *mockTask) Kill(context.Context, syscall.Signal, ...containerd.KillOpts) error { return nil }
func (m *mockTask) Wait(context.Context) (<-chan containerd.ExitStatus, error) {
	m.waitCalls++
	if m.waitCalls > 1 {
		// After the first call, return an error to simulate failed re-establishment
		return nil, m.waitErr
	}
	return m.waitCh, nil
}
func (m *mockTask) CloseIO(context.Context, ...containerd.IOCloserOpts) error { return nil }
func (m *mockTask) Resize(ctx context.Context, w, h uint32) error             { return nil }
func (m *mockTask) IO() cio.IO                                                { return nil }
func (m *mockTask) Status(context.Context) (containerd.Status, error) {
	return containerd.Status{}, nil
}
func (m *mockTask) Pause(context.Context) error  { return nil }
func (m *mockTask) Resume(context.Context) error { return nil }
func (m *mockTask) Exec(context.Context, string, *specs.Process, cio.Creator) (containerd.Process, error) {
	return nil, nil
}
func (m *mockTask) Pids(context.Context) ([]containerd.ProcessInfo, error) { return nil, nil }
func (m *mockTask) Checkpoint(context.Context, ...containerd.CheckpointTaskOpts) (containerd.Image, error) {
	return nil, nil
}
func (m *mockTask) Update(context.Context, ...containerd.UpdateTaskOpts) error { return nil }
func (m *mockTask) LoadProcess(context.Context, string, cio.Attach) (containerd.Process, error) {
	return nil, nil
}
func (m *mockTask) Metrics(context.Context) (*apitypes.Metric, error) { return nil, nil }
func (m *mockTask) Spec(context.Context) (*oci.Spec, error)           { return nil, nil }

// TestMonitorTaskExitIgnoresErrorStatus verifies that monitorTaskExit does not
// mark a sandbox as STOPPED when the exit status contains an error.
// This is critical for server restart scenarios where containerd may return
// spurious exit events with UnknownExitStatus (255) and zero exit time.
func TestMonitorTaskExitIgnoresErrorStatus(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()

	r.NoError(co.Init(ctx))

	// Create a sandbox entity with RUNNING status
	id := entity.Id(idgen.GenNS("sb"))

	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING
	sb.Labels = append(sb.Labels, "runtime.computer/app=test-app")

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	// Create an exit channel and send an ExitStatus WITH an error
	// This simulates what happens during server restart when containerd
	// returns spurious exit events
	exitCh := make(chan containerd.ExitStatus, 1)
	testErr := fmt.Errorf("simulated containerd error during restart")
	exitStatus := containerd.NewExitStatus(containerd.UnknownExitStatus, time.Time{}, testErr)
	exitCh <- *exitStatus

	// Create a mock task that will fail to re-establish monitoring
	// Set waitCalls to 1 so the first call to Wait() from inside monitorTaskExit
	// (which happens when trying to re-establish after the error status)
	// will return an error immediately
	task := &mockTask{
		waitCalls: 1,
		waitErr:   fmt.Errorf("task not found"),
	}

	// Call monitorTaskExit - it should ignore the error status and fail to re-establish
	co.monitorTaskExit(&sb, "test-container", "app", task, exitCh)

	// Verify the sandbox status was NOT changed to STOPPED
	r.Eventually(func() bool {
		result, err := co.EAC.Get(ctx, id.String())
		if err != nil {
			return false
		}
		var checkSb compute.Sandbox
		checkSb.Decode(result.Entity().Entity())
		return checkSb.Status == compute.RUNNING
	}, 5*time.Second, 50*time.Millisecond, "sandbox should remain RUNNING when exit status has an error")
}

// TestMonitorTaskExitHandlesValidExit verifies that monitorTaskExit correctly
// marks a sandbox as STOPPED when receiving a valid exit status (no error).
func TestMonitorTaskExitHandlesValidExit(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()

	r.NoError(co.Init(ctx))

	// Create a sandbox entity with RUNNING status
	id := entity.Id(idgen.GenNS("sb"))

	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING
	sb.Labels = append(sb.Labels, "runtime.computer/app=test-app")

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	// Create an exit channel and send a valid ExitStatus (no error)
	exitCh := make(chan containerd.ExitStatus, 1)
	exitStatus := containerd.NewExitStatus(0, time.Now(), nil)
	exitCh <- *exitStatus

	// Create a mock task (not used for valid exits, but required by interface)
	task := &mockTask{waitCh: exitCh}

	// Call monitorTaskExit - it should process the valid exit and mark as STOPPED
	co.monitorTaskExit(&sb, "test-container", "app", task, exitCh)

	// Verify the sandbox status was changed to STOPPED
	r.Eventually(func() bool {
		result, err := co.EAC.Get(ctx, id.String())
		if err != nil {
			return false
		}
		var checkSb compute.Sandbox
		checkSb.Decode(result.Entity().Entity())
		return checkSb.Status == compute.STOPPED
	}, 5*time.Second, 50*time.Millisecond, "sandbox should be STOPPED after valid exit event")
}

// A legitimate exit code of 0 has to survive encoding. It is the single most
// likely value for a successful task run, and the one a naive schema drops:
// entity.Empty() skips zero-valued attributes, so the exit code lives inside a
// component whose inner code field is marked required.
func TestMonitorTaskExitRecordsZeroExitCode(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()
	r.NoError(co.Init(ctx))

	id := entity.Id(idgen.GenNS("sb"))
	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	exitedAt := time.Now().Truncate(time.Second)
	exitCh := make(chan containerd.ExitStatus, 1)
	exitCh <- *containerd.NewExitStatus(0, exitedAt, nil)

	co.monitorTaskExit(&sb, "test-container", "app", &mockTask{waitCh: exitCh}, exitCh)

	var got compute.Sandbox
	r.Eventually(func() bool {
		result, err := co.EAC.Get(ctx, id.String())
		if err != nil {
			return false
		}
		got = compute.Sandbox{}
		got.Decode(result.Entity().Entity())
		return got.Status == compute.STOPPED
	}, 5*time.Second, 50*time.Millisecond, "sandbox should be STOPPED after exit")

	// The exit must land in the same write as the stop, so anything woken by
	// the stop already has the code.
	r.False(got.Exit.Empty(), "exit must be recorded, including a zero code")
	r.Equal(int64(0), got.Exit.Code)
	r.Equal("app", got.Exit.Container)
	r.False(got.Exit.At.IsZero())
}

func TestMonitorTaskExitRecordsNonZeroExitCode(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()
	r.NoError(co.Init(ctx))

	id := entity.Id(idgen.GenNS("sb"))
	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	exitCh := make(chan containerd.ExitStatus, 1)
	exitCh <- *containerd.NewExitStatus(137, time.Now(), nil)

	co.monitorTaskExit(&sb, "test-container", "app", &mockTask{waitCh: exitCh}, exitCh)

	var got compute.Sandbox
	r.Eventually(func() bool {
		result, err := co.EAC.Get(ctx, id.String())
		if err != nil {
			return false
		}
		got = compute.Sandbox{}
		got.Decode(result.Entity().Entity())
		return got.Status == compute.STOPPED
	}, 5*time.Second, 50*time.Millisecond)

	r.Equal(int64(137), got.Exit.Code)
}

// containerd can report a clean exit with a zero ExitTime. An Exit carrying a
// zero code and a zero time is Empty() and would encode to nothing, taking the
// code with it, so the write site substitutes now.
func TestMonitorTaskExitSubstitutesMissingExitTime(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()
	r.NoError(co.Init(ctx))

	id := entity.Id(idgen.GenNS("sb"))
	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	// Zero exit code AND zero exit time: the combination that vanishes.
	exitCh := make(chan containerd.ExitStatus, 1)
	exitCh <- *containerd.NewExitStatus(0, time.Time{}, nil)

	co.monitorTaskExit(&sb, "test-container", "app", &mockTask{waitCh: exitCh}, exitCh)

	var got compute.Sandbox
	r.Eventually(func() bool {
		result, err := co.EAC.Get(ctx, id.String())
		if err != nil {
			return false
		}
		got = compute.Sandbox{}
		got.Decode(result.Entity().Entity())
		return got.Status == compute.STOPPED
	}, 5*time.Second, 50*time.Millisecond)

	r.False(got.Exit.Empty(), "a zero code with no reported time must still be recorded")
	r.False(got.Exit.At.IsZero(), "exit time falls back to now rather than encoding to nothing")
}

// The DEAD transition that follows cleanup patches Sandbox by struct literal
// with an empty Exit. The generated encoder must skip it, leaving the recorded
// exit intact -- otherwise the code is destroyed moments after being written.
func TestDeadPatchPreservesRecordedExit(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()
	r.NoError(co.Init(ctx))

	id := entity.Id(idgen.GenNS("sb"))
	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.STOPPED
	sb.Exit = compute.Exit{Code: 0, At: time.Now(), Container: "app"}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	// Exactly the patch StopSandbox issues once cleanup finishes.
	_, err = co.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{Status: compute.DEAD}).Encode,
	).Attrs(), 0)
	r.NoError(err)

	result, err := co.EAC.Get(ctx, id.String())
	r.NoError(err)
	var got compute.Sandbox
	got.Decode(result.Entity().Entity())

	r.Equal(compute.DEAD, got.Status)
	r.False(got.Exit.Empty(), "the DEAD patch must not clobber the recorded exit")
	r.Equal("app", got.Exit.Container)
}

// A sandbox whose command must execute at most once is finished when its
// containers vanish. Rebooting it would re-run the command -- for a migration,
// not a recoverable mistake.
func TestRestartPolicyNeverRetiresInsteadOfRebooting(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	co, err := newSandboxController(testDeps)
	r.NoError(err)
	defer co.Close()
	r.NoError(co.Init(ctx))

	id := entity.Id(idgen.GenNS("sb"))
	var sb compute.Sandbox
	sb.ID = id
	sb.Status = compute.RUNNING
	sb.Spec.RestartPolicy = compute.SandboxSpecNEVER

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(id.String())
	rpcE.SetAttrs(entity.New(entity.DBId, id, sb.Encode).Attrs())
	_, err = co.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	meta := &entity.Meta{}

	// No containers exist for this sandbox, so CheckSandbox reports it gone --
	// the state a runner restart mid-run leaves behind.
	r.NoError(co.Create(ctx, &sb, meta))

	result, err := co.EAC.Get(ctx, id.String())
	r.NoError(err)
	var got compute.Sandbox
	got.Decode(result.Entity().Entity())

	r.Equal(compute.DEAD, got.Status, "a no-restart sandbox retires rather than rebooting")
	r.True(got.Exit.Empty(), "no exit was observed, so none should be invented")
}

// The RUNNING check in the retire guard is load-bearing. Without it every
// no-restart sandbox is retired on its first reconcile -- its containers are
// legitimately absent because none have been created yet -- so every task run
// would die before executing anything.
func TestShouldRetireInsteadOfRestart(t *testing.T) {
	never := func(status compute.SandboxStatus) *compute.Sandbox {
		sb := &compute.Sandbox{Status: status}
		sb.Spec.RestartPolicy = compute.SandboxSpecNEVER
		return sb
	}

	r := require.New(t)

	r.True(shouldRetireInsteadOfRestart(never(compute.RUNNING)),
		"a running sandbox that lost its containers already executed its command")

	r.False(shouldRetireInsteadOfRestart(never(compute.PENDING)),
		"a pending sandbox has never started; creating it is the first execution")
	r.False(shouldRetireInsteadOfRestart(never("")),
		"a sandbox with no status yet has never started either")

	// The default policy never retires; services depend on being rebooted.
	r.False(shouldRetireInsteadOfRestart(&compute.Sandbox{Status: compute.RUNNING}))
}
