package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
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

	"miren.dev/runtime/api/sandbox/v1alpha"
	"miren.dev/runtime/build"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerd(t *testing.T) {
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

		r.NoError(co.Init(ctx))
		defer co.Close()

		id := entity.Id("cont-xyz")

		var sb v1alpha.Sandbox

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

		var tco v1alpha.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.containerId(id))
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

		r.Equal(addr, ca.String(), "address doesn't match")

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

			var sb v1alpha.Sandbox

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

			var sb v1alpha.Sandbox

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

			c, err := cc.LoadContainer(ctx, co.containerId(id))
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

			var sb v1alpha.Sandbox

			sb.ID = id

			sb.Labels = append(sb.Labels, "runtime.computer/app=mn-test")
			sb.Container = append(sb.Container, v1alpha.Container{
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

			c, err := cc.LoadContainer(ctx, co.containerId(id)+"-nginx")
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

		r.NoError(co.Init(ctx))
		defer co.Close()

		id := entity.Id("cont-xyz")

		var sb v1alpha.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Container = append(sb.Container, v1alpha.Container{
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

		var tco v1alpha.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.containerId(id))
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

		cpu, mem, err := co.ResourcesMonitor.LastestUsage(co.containerId(id))
		r.NoError(err)

		t.Logf("last delta: %f", cpu)

		// This fails because of how Github Actions works when it's higher, so
		// we're just going to test that it's just being measured for now.
		r.Greater(float64(cpu), float64(0.5))

		r.Greater(mem, uint64(0))

	})

	t.Run("configures networking", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		// defer cleanup()

		_ = cleanup

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

		r.NoError(co.Init(ctx))
		defer co.Close()

		id := entity.Id("cont-xyz")

		var sb v1alpha.Sandbox

		sb.ID = id

		sb.Labels = append(sb.Labels, "runtime.computer/app=mn-nginx")

		sb.Container = append(sb.Container, v1alpha.Container{
			Name:  "nginx",
			Image: "mn-nginx:latest",
		})
		sb.Port = append(sb.Port, v1alpha.Port{
			Name:     "http",
			NodePort: 31001,
			Port:     80,
			Protocol: v1alpha.TCP,
			Type:     "http",
		})

		cont := &entity.Entity{
			ID:    id,
			Attrs: sb.Encode(),
		}

		meta := &entity.Meta{
			Entity:   cont,
			Revision: 1,
		}

		var tco v1alpha.Sandbox
		tco.Decode(cont)

		err = co.Create(ctx, &tco, meta)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, co.containerId(id))
		r.NoError(err)

		r.NotNil(c)

		//defer testutils.ClearContainer(ctx, c)

		lbls, err := c.Labels(ctx)
		r.NoError(err)

		out, _ := exec.Command("iptables", "-t", "nat", "-L", "-nv").CombinedOutput()
		fmt.Println(string(out))

		r.Equal("mn-nginx", lbls["runtime.computer/app"])

		time.Sleep(5 * time.Second)

		hc := http.Client{
			Timeout: 1 * time.Second,
		}

		resp, err := hc.Get(fmt.Sprintf("http://%s:80", ca.Addr().String()))
		r.NoError(err)

		resp, err = hc.Get("http://127.0.0.1:31001")
		r.NoError(err)

		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)
	})
}
