package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/stretchr/testify/require"

	"github.com/opencontainers/runtime-spec/specs-go"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	build "miren.dev/runtime/pkg/buildkit"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/mountinfo"
	"miren.dev/runtime/pkg/tarx"
	"miren.dev/runtime/pkg/testutils"
)

func TestSandbox(t *testing.T) {

	sbName := func() string {
		return idgen.GenNS("sb")
	}

	t.Run("can run a container", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := tarx.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := tarx.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()

		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParseAddr(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
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
			containerd.WithRuntime("io.containerd.runsc.v1", nil),
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

		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case <-ch:
			pw.Close()
			// ok
		}

		data, err := io.ReadAll(pr)
		r.NoError(err)

		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		r.Len(lines, 1)

		sort.Strings(lines)

		addr := strings.Fields(strings.TrimSpace(lines[0]))[1]

		r.Equal(addr, ca.String()+"/24", "address doesn't match")

		t.Run("create on existing sandbox is no-op", func(t *testing.T) {
			searchRes, err := co.checkSandbox(ctx, &sb, meta)
			r.NoError(err)

			r.Equal(same, searchRes)
		})

		t.Run("detects changes", func(t *testing.T) {
			r := require.New(t)
			meta := &entity.Meta{
				Entity:   cont,
				Revision: 2,
			}

			searchRes, err := co.checkSandbox(ctx, &sb, meta)
			r.NoError(err)

			r.Equal(differentVersion, searchRes)
		})

		t.Run("can update in place with just label changes", func(t *testing.T) {
			r := require.New(t)

			task.Delete(ctx, containerd.WithProcessKill)
			bc.Delete(ctx, containerd.WithSnapshotCleanup)

			var sb compute.Sandbox

			sb.ID = id

			sb.Labels = append(sb.Labels, "runtime.computer/app=mn-test")

			cont := &entity.Entity{
				ID:    id,
				Attrs: sb.Encode(),
			}

			meta := &entity.Meta{
				Entity:   cont,
				Revision: 2,
			}

			canUpdate, _, err := co.canUpdateInPlace(ctx, &sb, meta)
			r.NoError(err)
			r.True(canUpdate)
		})

		t.Run("updates container in place when labels change", func(t *testing.T) {
			r := require.New(t)

			var sb compute.Sandbox

			sb.ID = id

			sb.Labels = append(sb.Labels, "runtime.computer/app=mn-test")

			cont := &entity.Entity{
				ID:    id,
				Attrs: sb.Encode(),
			}

			meta := &entity.Meta{
				Entity:   cont,
				Revision: 2,
			}

			err := co.Create(ctx, &sb, meta)
			r.NoError(err)

			c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
			r.NoError(err)

			labels, err := c.Labels(ctx)
			r.NoError(err)

			r.Equal("mn-test", labels["runtime.computer/app"], "container label not updated")

			diskMeta, err := co.readEntity(ctx, id)
			r.NoError(err)

			r.Equal(int64(2), diskMeta.Revision)
		})

		t.Run("rebuilds sandbox when necessary", func(t *testing.T) {
			r := require.New(t)

			task.Delete(ctx, containerd.WithProcessKill)
			bc.Delete(ctx, containerd.WithSnapshotCleanup)

			var sb compute.Sandbox

			sb.ID = id

			sb.Labels = append(sb.Labels, "runtime.computer/app=mn-test")
			sb.Container = append(sb.Container, compute.Container{
				Name:  "nginx",
				Image: "mn-nginx:latest",
			})

			cont := &entity.Entity{
				ID:    id,
				Attrs: sb.Encode(),
			}

			meta := &entity.Meta{
				Entity:   cont,
				Revision: 3,
			}

			canUpdate, _, err := co.canUpdateInPlace(ctx, &sb, meta)
			r.NoError(err)
			r.False(canUpdate)

			err = co.Create(ctx, &sb, meta)
			r.NoError(err)

			c, err := cc.LoadContainer(ctx, co.containerPrefix(id)+"-nginx")
			r.NoError(err)

			task, err := c.Task(ctx, nil)
			r.NoError(err)

			status, err := task.Status(ctx)
			r.NoError(err)

			r.Equal(containerd.Running, status.Status, "container task not running")

			diskMeta, err := co.readEntity(ctx, id)
			r.NoError(err)

			r.Equal(int64(3), diskMeta.Revision)
		})
	})

	t.Run("calculates cpu usage correctly", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/sort")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-sort:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-sort:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Container = append(sb.Container, compute.Container{
			Name:  "sort",
			Image: "mn-sort:latest",
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity: cont,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		spec, err := c.Spec(ctx)
		r.NoError(err)

		path := filepath.Join("/sys/fs/cgroup", spec.Linux.CgroupsPath, "cpu.stat")
		fi, err := os.Stat(path)
		r.NoError(err)

		r.True(fi.Mode().IsRegular())

		defer testutils.ClearContainer(ctx, c)

		// Let sort ... sort.
		time.Sleep(3 * time.Second)

		cpu, err := co.Metrics.CPUUsage.CurrentCPUUsage(id.String())
		r.NoError(err)

		t.Logf("last delta: %f", cpu)

		// This fails because of how Github Actions works when it's higher, so
		// we're just going to test that it's just being measured for now.
		r.Greater(float64(cpu), float64(0.5))
	})

	t.Run("configures networking", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Container = append(sb.Container, compute.Container{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Port: []compute.Port{
				{
					Name:     "http",
					NodePort: 31001,
					Port:     80,
					Protocol: compute.TCP,
					Type:     "http",
				},
			},
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParseAddr(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		lbls, err := c.Labels(ctx)
		r.NoError(err)

		r.Equal("mn-nginx", lbls["runtime.computer/app"])

		time.Sleep(5 * time.Second)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.String()))
		r.NoError(err)

		defer resp.Body.Close()

		resp, err = hc.Get("http://127.0.0.1:31001")
		r.NoError(err)

		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)
	})

	t.Run("sets up host paths as volumes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		spath, err := filepath.Abs("testdata/static-site")
		r.NoError(err)

		sb.Volume = append(sb.Volume, compute.Volume{
			Name:     "static-site",
			Provider: "host",
			Labels:   types.LabelSet("path", spath),
		})

		sb.Container = append(sb.Container, compute.Container{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Mount: []compute.Mount{
				{
					Destination: "/usr/share/nginx/html",
					Source:      "static-site",
				},
			},
			Port: []compute.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: compute.TCP,
					Type:     "http",
				},
			},
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParseAddr(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		time.Sleep(5 * time.Second)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.String()))
		r.NoError(err)

		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		r.NoError(err)

		r.Contains(string(data), "this is from testdata/static-site")

		r.Equal(http.StatusOK, resp.StatusCode)
	})

	t.Run("sets up named host volumes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id(sbName())

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Volume = append(sb.Volume, compute.Volume{
			Name:     "static-site",
			Provider: "host",
			Labels:   types.LabelSet("name", "site-data"),
		})

		sb.Container = append(sb.Container, compute.Container{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Mount: []compute.Mount{
				{
					Destination: "/usr/share/nginx/html",
					Source:      "static-site",
				},
			},
			Port: []compute.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: compute.TCP,
					Type:     "http",
				},
			},
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParseAddr(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		time.Sleep(5 * time.Second)

		rawPath := filepath.Join(co.DataPath, "host-volumes", "site-data", "index.html")

		err = os.WriteFile(rawPath, []byte("this is from testdata/static-site"), 0644)
		r.NoError(err)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.String()))
		r.NoError(err)

		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		r.NoError(err)

		r.Contains(string(data), "this is from testdata/static-site")

		r.Equal(http.StatusOK, resp.StatusCode)
	})

	t.Run("sets up miren volumes", func(t *testing.T) {
		if !testutils.IsModuleLoaded("nbd") {
			t.Skip("miren volumes require nbd kernel module and it doesn't look loaded")
		}

		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var co SandboxController

		err = reg.Populate(&co)
		r.NoError(err)

		defer co.Close()
		r.NoError(co.Init(ctx))

		id := entity.Id("sb-xyz")

		var sb compute.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Volume = append(sb.Volume, compute.Volume{
			Name:     "static-site",
			Provider: "miren",
			Labels:   types.LabelSet("name", "testing", "size", "50MB"),
		})

		sb.Container = append(sb.Container, compute.Container{
			Name:  "nginx",
			Image: "mn-nginx:latest",
			Mount: []compute.Mount{
				{
					Destination: "/usr/share/nginx/html",
					Source:      "static-site",
				},
			},
			Port: []compute.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: compute.TCP,
					Type:     "http",
				},
			},
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco compute.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParseAddr(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.pauseContainerId(id))
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		time.Sleep(5 * time.Second)

		rawPath := co.sandboxPath(&sb, "volumes", "static-site", "index.html")

		mp, err := mountinfo.MountPoint(rawPath)
		r.NoError(err)
		r.NotNil(mp)

		r.Equal(filepath.Dir(rawPath), mp.Mountpoint)

		err = os.WriteFile(rawPath, []byte("this is from testdata/static-site"), 0644)
		r.NoError(err)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.String()))
		r.NoError(err)

		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		r.NoError(err)

		r.Contains(string(data), "this is from testdata/static-site")

		r.Equal(http.StatusOK, resp.StatusCode)
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

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var sbc SandboxController

		err := reg.Populate(&sbc)
		r.NoError(err)

		defer checkClosed(t, &sbc)

		err = sbc.Init(ctx)
		r.NoError(err)

		// Create a few sandboxes
		sbID1 := entity.Id(sbName())
		sb1 := &compute.Sandbox{
			ID:     sbID1,
			Status: compute.RUNNING,
		}

		// Store sandbox in entity store with ident
		var rpcE1 entityserver_v1alpha.Entity
		rpcE1.SetId(sbID1.String())
		rpcE1.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, sbID1.String()),
			sb1.Encode))
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
		rpcE2.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, sbID2.String()),
			sb2.Encode))
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

		// Wait a bit for containers to be created
		time.Sleep(2 * time.Second)

		// Stop the first sandbox (this should set status to DEAD)
		err = sbc.Delete(ctx, sbID1)
		r.NoError(err)

		// Manually update the UpdatedAt timestamp to be more than 1 hour ago
		// We need to get the entity and update it
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sbID1.String())

		// Set UpdatedAt to 2 hours ago by updating the entity
		twoHoursAgo := time.Now().Add(-2 * time.Hour).UnixMilli()
		rpcE.SetAttrs(entity.Attrs(
			entity.Keyword(entity.Ident, sbID1.String()),
			(&compute.Sandbox{
				Status: compute.DEAD,
			}).Encode,
			entity.Int64(entity.UpdatedAt, twoHoursAgo)))

		_, err = sbc.EAC.Put(ctx, &rpcE)
		r.NoError(err)

		// Run the periodic cleanup
		err = sbc.Periodic(ctx)
		r.NoError(err)

		// Check that the old dead sandbox was deleted
		resp, err := sbc.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
		r.NoError(err)

		// Should only have one sandbox left (sbID2)
		r.Equal(1, len(resp.Values()))

		var remainingSb compute.Sandbox
		remainingSb.Decode(resp.Values()[0].Entity())
		r.Equal(sbID2, remainingSb.ID)
		r.Equal(compute.RUNNING, remainingSb.Status)

		// Clean up the remaining sandbox
		err = sbc.Delete(ctx, sbID2)
		r.NoError(err)
	})
}
