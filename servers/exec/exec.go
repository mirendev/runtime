package exec

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Server struct {
	Log *slog.Logger
	CC  *containerd.Client

	EAC *entityserver_v1alpha.EntityAccessClient

	Namespace string
}

// NewServer creates a new exec Server.
func NewServer(log *slog.Logger, cc *containerd.Client, eac *entityserver_v1alpha.EntityAccessClient, namespace string) *Server {
	return &Server{
		Log:       log,
		CC:        cc,
		EAC:       eac,
		Namespace: namespace,
	}
}

var _ exec_v1alpha.SandboxExec = (*Server)(nil)

func (s *Server) Exec(ctx context.Context, req *exec_v1alpha.SandboxExecExec) error {
	// The container lives in this runner's containerd namespace, so scope the
	// operations below to it directly rather than depending on the caller's
	// context or the containerd client's default namespace. The CLI path only
	// worked by accident of that default; programmatic callers (e.g. addon
	// credential rotation) reach us with a bare context.
	ctx = namespaces.WithNamespace(ctx, s.Namespace)

	args := req.Args()

	if args.Category() != "id" {
		return fmt.Errorf("invalid category %s", args.Category())
	}

	id := args.Value()

	// Resolve short IDs to full entity IDs for the containerd label lookup
	if resolved, err := s.EAC.Get(ctx, id); err == nil {
		id = resolved.Entity().Id()
	}

	containers, err := s.CC.Containers(ctx, `labels."runtime.computer/entity-id"==`+id)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		return fmt.Errorf("no container found for %s", id)
	}

	s.Log.Debug("found containers", "count", len(containers))

	var (
		firstContainer containerd.Container
		verId          string
	)

	// Find the first non-sandbox container
	for _, container := range containers {
		lbls, err := container.Labels(ctx)
		if err != nil {
			continue
		}

		if lbls["runtime.computer/container-kind"] != "sandbox" {
			verId = lbls["runtime.computer/version-entity"]
			firstContainer = container
			break
		}
	}

	if firstContainer == nil {
		return fmt.Errorf("no non-sandbox container found for %s", id)
	}

	// Confine an app-scoped caller to its own app. On the normal path this is a
	// no-op: the exec proxy already enforces it and reaches this server over the
	// coordinator cert (an unscoped identity). The guard matters only if this
	// server is ever exposed to clients directly — an unresolvable app denies an
	// app-scoped caller (rpc.AllowApp fails closed against ""), so the fail-open
	// gap where verId is empty is covered.
	if app := s.resolveVersionApp(ctx, verId); !rpc.AllowApp(ctx, app) {
		return rpc.AppAccessError(ctx, app)
	}

	s.Log.Debug("found container", "id", firstContainer.ID())

	// TODO support specifying which container to exec into

	task, err := firstContainer.Task(ctx, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	input := stream.ToReader(ctx, args.Input())
	output := stream.ToWriter(ctx, args.Output())

	defer input.Close()
	defer output.Close()

	spec, err := firstContainer.Spec(ctx)
	if err != nil {
		return err
	}

	var cfgSpec *core_v1alpha.ConfigSpec

	if verId != "" {
		res, err := s.EAC.Get(ctx, verId)
		if err != nil {
			return err
		}

		var v core_v1alpha.AppVersion
		v.Decode(res.Entity().Entity())

		// Resolve config from ConfigVersion if available
		resolvedCfg, err := coreutil.ResolveConfig(ctx, s.EAC, &v)
		if err == nil {
			s.Log.Debug("found version", "id", verId)

			// Only use the app version config (entrypoint, console command) if the container
			// is using the app's built image. Service containers with custom images
			// (like postgres) should not have the app's entrypoint applied.
			containerImage, err := firstContainer.Image(ctx)
			if err == nil {
				containerImageName := containerImage.Name()
				if imageMatchesAppVersion(containerImageName, v.ImageUrl) {
					cfgSpec = resolvedCfg
				} else {
					s.Log.Debug("container image differs from app version, skipping entrypoint",
						"container_image", containerImageName,
						"app_image", v.ImageUrl)
				}
			} else {
				s.Log.Debug("failed to get container image, skipping entrypoint", "error", err)
			}
		}
	}

	pspec, err := s.spec(args.Options(), spec, cfgSpec)
	if err != nil {
		return err
	}

	copts := []cio.Opt{cio.WithStreams(input, output, output)}

	if pspec.Terminal {
		copts = append(copts, cio.WithTerminal)
	}

	cstreams := cio.NewCreator(copts...)

	proc, err := task.Exec(ctx,
		idgen.Gen("t"),
		pspec,
		cstreams,
	)
	if err != nil {
		return err
	}

	err = proc.Start(ctx)
	if err != nil {
		return err
	}

	// Handle window resize events
	if args.HasWindowUpdates() {
		winCh := make(chan *exec_v1alpha.WindowSize)
		stream.ChanWriter(ctx, args.WindowUpdates(), winCh)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ws, ok := <-winCh:
					if !ok {
						return
					}
					if err := proc.Resize(ctx, uint32(ws.Width()), uint32(ws.Height())); err != nil {
						s.Log.Debug("failed to resize terminal", "error", err)
					}
				}
			}
		}()
	}

	es, err := proc.Wait(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		req.Results().SetCode(int32(130))
		proc.Kill(context.Background(), 9)
		return ctx.Err()
	case status := <-es:
		proc.IO().Wait()

		err = status.Error()
		if err != nil {
			return nil
		}

		req.Results().SetCode(int32(status.ExitCode()))
	}

	return nil
}

// resolveVersionApp returns the app owning an AppVersion, or "" if verId is
// empty or resolution fails. "" denies an app-scoped caller (rpc.AllowApp fails
// closed against an empty app) while leaving unscoped callers unaffected.
func (s *Server) resolveVersionApp(ctx context.Context, verId string) string {
	if verId == "" {
		return ""
	}

	verResp, err := s.EAC.Get(ctx, verId)
	if err != nil {
		return ""
	}

	var ver core_v1alpha.AppVersion
	ver.Decode(verResp.Entity().Entity())
	if ver.App == "" {
		return ""
	}

	appResp, err := s.EAC.Get(ctx, ver.App.String())
	if err != nil {
		return ""
	}

	var meta core_v1alpha.Metadata
	meta.Decode(appResp.Entity().Entity())
	return meta.Name
}

func (e *Server) command(cfgSpec *core_v1alpha.ConfigSpec, service string) string {
	for _, svc := range cfgSpec.Services {
		if svc.Name == service && svc.Command != "" {
			if cfgSpec.Entrypoint != "" {
				return cfgSpec.Entrypoint + " " + svc.Command
			}
			return svc.Command
		}
	}

	return ""
}

func (e *Server) spec(opts *exec_v1alpha.ShellOptions, spec *oci.Spec, cfgSpec *core_v1alpha.ConfigSpec) (*specs.Process, error) {
	proc := &specs.Process{
		Cwd:  spec.Process.Cwd,
		Env:  spec.Process.Env,
		User: spec.Process.User,
	}

	var ep string
	if cfgSpec != nil {
		ep = cfgSpec.Entrypoint
	}

	args := opts.Command()

	if len(args) == 0 {
		if cfgSpec != nil {
			if con := e.command(cfgSpec, "console"); con != "" {
				// CommandFor already prepends the entrypoint
				args = []string{"/bin/sh", "-c", "exec " + con}
			} else if ep != "" {
				args = []string{"/bin/sh", "-c", "exec " + ep + " /bin/sh"}
			} else {
				args = []string{"/bin/sh"}
			}
		} else {
			args = []string{"/bin/sh"}
		}
	} else if ep != "" {
		args = []string{"/bin/sh", "-c", "exec " + ep + " " + strings.Join(args, " ")}
	}

	proc.Args = args

	winsize := opts.WinSize()
	if winsize != nil {
		proc.Terminal = true
		proc.ConsoleSize = &specs.Box{
			Height: uint(winsize.Height()),
			Width:  uint(winsize.Width()),
		}
	}

	return proc, nil
}

// imageMatchesAppVersion checks if a container image matches the app version's image.
// This is used to determine whether to apply the app's entrypoint when exec'ing
// into a container. Service containers with custom images (like postgres) should
// not have the app's entrypoint applied.
func imageMatchesAppVersion(containerImage, appVersionImage string) bool {
	// Exact match
	if containerImage == appVersionImage {
		return true
	}
	// Handle case where container image has registry prefix (e.g., docker.io/library/)
	// but the app version doesn't
	if strings.HasSuffix(containerImage, "/"+appVersionImage) {
		return true
	}
	return false
}
