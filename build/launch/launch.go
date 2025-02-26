package launch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	buildkit "github.com/moby/buildkit/client"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/run"
)

type LaunchBuildkit struct {
	CR        *run.ContainerRunner
	Subnet    *netdb.Subnet
	Namespace string `asm:"namespace"`
	Bridge    string `asm:"bridge-iface"`
}

type RunningBuildkit struct {
	*LaunchBuildkit

	task client.Task
	id   string
}

type launchOptions struct {
	cacheDir string
}

type LaunchOption func(*launchOptions)

func WithCacheDir(dir string) LaunchOption {
	return func(o *launchOptions) {
		o.cacheDir = dir
	}
}

func (l *LaunchBuildkit) Launch(ctx context.Context, lo ...LaunchOption) (*RunningBuildkit, error) {
	var opts launchOptions
	for _, o := range lo {
		o(&opts)
	}

	ctx = namespaces.WithNamespace(ctx, l.Namespace)

	img, err := l.CR.CC.GetImage(ctx, "ghcr.io/mirendev/buildkit:latest")
	if img == nil || err != nil {
		_, err := l.CR.CC.Pull(ctx, "ghcr.io/mirendev/buildkit:latest", client.WithPullUnpack)
		if err != nil {
			return nil, err
		}
	}

	ec, err := network.AllocateOnBridge(l.Bridge, l.Subnet)
	if err != nil {
		return nil, err
	}

	id, err := l.CR.RunContainer(ctx, &run.ContainerConfig{
		Image:      "ghcr.io/mirendev/buildkit:latest",
		LogEntity:  "buildkit",
		Endpoint:   ec,
		Privileged: true,
	})
	if err != nil {
		return nil, err
	}

	return &RunningBuildkit{
		LaunchBuildkit: l,
		id:             id,
	}, nil
}

type pipeNetConn struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *pipeNetConn) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func (p *pipeNetConn) Write(b []byte) (int, error) {
	return p.w.Write(b)
}

func (p *pipeNetConn) Close() error {
	p.r.Close()
	p.w.Close()
	return nil
}

type pipeAddr struct{}

func (p pipeAddr) Network() string {
	return "pipe"
}

func (p pipeAddr) String() string {
	return "pipe"
}

func (p *pipeNetConn) LocalAddr() net.Addr {
	return pipeAddr{}
}

func (p *pipeNetConn) RemoteAddr() net.Addr {
	return pipeAddr{}
}

func (p *pipeNetConn) SetDeadline(t time.Time) error {
	return nil
}

func (p *pipeNetConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (p *pipeNetConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (l *RunningBuildkit) checkReady(ctx context.Context, cont client.Container) error {
	task, err := cont.Task(ctx, nil)
	if err != nil {
		return err
	}

	var out bytes.Buffer

	proc, err := task.Exec(ctx,
		idgen.Gen("t"),
		&specs.Process{
			Args: []string{"buildctl", "debug", "info"},
			Env:  []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			Cwd:  "/",
		},
		cio.NewCreator(cio.WithStreams(nil, &out, &out)),
	)
	if err != nil {
		return err
	}

	err = proc.Start(ctx)
	if err != nil {
		return err
	}

	es, err := proc.Wait(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		proc.Kill(context.Background(), 9)
		return ctx.Err()
	case status := <-es:
		proc.IO().Wait()

		err = status.Error()
		if err != nil {
			return nil
		}
	}

	return nil
}

func (l *RunningBuildkit) Client(ctx context.Context) (*buildkit.Client, error) {
	ctx = namespaces.WithNamespace(ctx, l.Namespace)

	cont, err := l.CR.CC.LoadContainer(ctx, l.id)
	if err != nil {
		return nil, err
	}

	for {
		err := l.checkReady(ctx, cont)
		if err == nil {
			break
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		l.CR.Log.Debug("waiting for buildkit to be ready", "error", err)

		time.Sleep(100 * time.Millisecond)
	}

	or, ow, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	ir, iw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	task, err := cont.Task(ctx, nil)
	if err != nil {
		return nil, err
	}

	proc, err := task.Exec(ctx,
		"dialstdio",
		&specs.Process{
			Args: []string{"buildctl", "dial-stdio"},
			Env:  []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			Cwd:  "/",
		},
		cio.NewCreator(cio.WithStreams(ir, ow, os.Stderr)),
	)
	if err != nil {
		return nil, err
	}

	err = proc.Start(ctx)
	if err != nil {
		return nil, err
	}

	bk, err := buildkit.New(ctx, "",
		buildkit.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return &pipeNetConn{r: or, w: iw}, nil
		}))

	return bk, err
}

func (l *RunningBuildkit) Close(ctx context.Context) error {
	ctx = namespaces.WithNamespace(ctx, l.Namespace)

	cont, err := l.CR.CC.LoadContainer(ctx, l.id)
	if err != nil {
		return err
	}

	task, _ := cont.Task(ctx, nil)
	if task != nil {
		task.Delete(ctx, client.WithProcessKill)
	}

	return cont.Delete(ctx, client.WithSnapshotCleanup)
}
