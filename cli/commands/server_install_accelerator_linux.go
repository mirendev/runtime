package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	buildkitclient "github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/components/buildkit"
	containerdcomp "miren.dev/runtime/components/containerd"
	containerimage "miren.dev/runtime/image"
	buildkitbuild "miren.dev/runtime/pkg/buildkit"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/lbdmod/ctrbuild"
)

// installServerDiskAccelerator prepares lbd before server install starts systemd.
func installServerDiskAccelerator(ctx *Context) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing a kernel module requires root privileges (use sudo)")
	}
	dataPath := mirenDataDir
	options := lbdmod.HostOptions(dataPath)
	unlock, err := lockLocalDiskAccelerator(dataPath)
	if err != nil {
		return err
	}
	defer unlock()
	status, err := lbdmod.Probe(options)
	if err != nil {
		return err
	}
	if status.Marker != nil && status.Available() && !status.Stale() {
		ctx.Completed("Accelerator mode is already ready on this host, kernel %s", status.Host.KernelRelease)
		return nil
	}
	installer := &lbdmod.Installer{Log: ctx.Log, Options: options}
	if err := installer.CheckHost(status); err != nil {
		return err
	}
	socket := filepath.Join(dataPath, "containerd", "containerd.sock")

	listening, err := unixSocketListening(socket)
	if err != nil {
		return fmt.Errorf("checking containerd socket %s: %w", socket, err)
	}
	startedContainerd := !listening
	if !listening {
		binary := filepath.Join(releaseDir, "containerd")
		if _, err := os.Stat(binary); err != nil {
			return fmt.Errorf("server release containerd is unavailable: %w", err)
		}
		ctx.Begin("Starting temporary Miren containerd")
		runtime := containerdcomp.NewContainerdComponent(ctx.Log, dataPath)
		config := containerdcomp.EmbeddedBootConfig(ctx.Log, dataPath, binary, releaseDir, socket)
		if err := runtime.Start(ctx, config.Embedded); err != nil {
			return fmt.Errorf("starting containerd: %w", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
			defer cancel()
			if err := runtime.Stop(stopCtx); err != nil {
				ctx.Log.Warn("could not stop temporary containerd", "error", err)
			}
		}()
	}
	if err := checkSocketAccess(socket); err != nil {
		return err
	}
	cc, err := containerd.New(socket)
	if err != nil {
		return fmt.Errorf("connecting to containerd at %s: %w", socket, err)
	}
	defer cc.Close()

	image := "localhost/miren-system/lbd-builder:" + lbdmod.BuilderVersion()
	if _, err := cc.GetImage(namespaces.WithNamespace(ctx, ctrbuild.DefaultNamespace), image); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("checking the local lbd toolchain image: %w", err)
		}
		buildkitSocket := filepath.Join(dataPath, "buildkit", "socket", "buildkitd.sock")
		listening, err := unixSocketListening(buildkitSocket)
		if err != nil {
			return fmt.Errorf("checking BuildKit socket %s: %w", buildkitSocket, err)
		}
		if !listening {
			if !startedContainerd {
				return fmt.Errorf("BuildKit is not listening at %s while server containerd is running; check the server's BuildKit socket configuration or stop the server before installing the disk accelerator", buildkitSocket)
			}
			ctx.Begin("Starting temporary Miren BuildKit")
			builder := buildkit.NewComponent(ctx.Log, cc, ctrbuild.DefaultNamespace, dataPath)
			if err := builder.Start(ctx, buildkit.Config{SocketDir: filepath.Dir(buildkitSocket)}); err != nil {
				return fmt.Errorf("starting BuildKit: %w", err)
			}
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
				defer cancel()
				if err := builder.Stop(stopCtx); err != nil {
					ctx.Log.Warn("could not stop temporary BuildKit", "error", err)
				}
			}()
		}
		if err := buildLocalLbdToolchain(ctx, cc, buildkitSocket, image); err != nil {
			return err
		}
	}

	ctx.Begin("Installing the lbd kernel module on this host")
	installer.Builder = ctrbuild.New(cc, ctx.Log)
	installer.Image = image
	status, err = installer.Install(ctx, false)
	if err != nil {
		return err
	}
	ctx.Completed("Accelerator mode is ready on this host, kernel %s", status.Host.KernelRelease)
	return nil
}

func lockLocalDiskAccelerator(dataPath string) (func(), error) {
	dir := filepath.Join(dataPath, "lbd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "local-install.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("another local accelerator installation is in progress: %w", err)
		}
		return nil, err
	}
	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}

// A stopped daemon may leave its socket inode behind; existence alone does not
// mean another process owns the data directory.
func unixSocketListening(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("%s exists but is not a Unix socket", path)
	}
	conn, err := net.DialTimeout("unix", path, 3*time.Second)
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conn.Close()
	return true, nil
}

func buildLocalLbdToolchain(ctx *Context, cc *containerd.Client, buildkitSocket, image string) error {
	dir, err := os.MkdirTemp("", "miren-lbd-builder-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := lbdmod.MaterializeBuilder(dir); err != nil {
		return err
	}
	bkc, err := buildkitclient.New(ctx, "unix://"+buildkitSocket)
	if err != nil {
		return fmt.Errorf("connecting to BuildKit: %w", err)
	}
	defer bkc.Close()

	ctx.Begin("Building the local lbd toolchain image")
	return solveAndImportLbdToolchain(ctx, ctx.Log, bkc, cc, dir, image)
}

func solveAndImportLbdToolchain(ctx context.Context, log *slog.Logger, bkc *buildkitclient.Client, cc *containerd.Client, dir, image string) error {
	dfs, err := fsutil.NewFS(dir)
	if err != nil {
		return fmt.Errorf("opening the lbd builder context: %w", err)
	}
	r, w := io.Pipe()
	importDone := make(chan error, 1)
	go func() {
		err := containerimage.NewImageImporter(cc, ctrbuild.DefaultNamespace).ImportImage(ctx, r, image)
		r.CloseWithError(err)
		importDone <- err
	}()
	_, buildErr := (&buildkitbuild.Buildkit{Client: bkc, Log: log}).BuildImage(ctx, dfs,
		buildkitbuild.BuildStack{Stack: "dockerfile", Input: lbdmod.BuilderDockerfile},
		func() (io.WriteCloser, error) { return w, nil })
	w.CloseWithError(buildErr)
	importErr := <-importDone
	if buildErr != nil {
		return fmt.Errorf("building the lbd toolchain image: %w", buildErr)
	}
	if importErr != nil {
		return fmt.Errorf("importing the lbd toolchain image: %w", importErr)
	}
	return nil
}
