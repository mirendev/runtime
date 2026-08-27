package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	exptypes "github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/stackbuild"
	"miren.dev/runtime/pkg/workloadidentity"

	"github.com/tonistiigi/fsutil"
)

type Buildkit struct {
	Client *client.Client

	Log            *slog.Logger
	WorkloadIssuer *workloadidentity.Issuer
}

type tarOutput struct {
	rc    io.ReadCloser
	mu    sync.Mutex
	bgErr error
}

func (t *tarOutput) Read(p []byte) (int, error) {
	t.mu.Lock()

	if t.bgErr != nil {
		t.mu.Unlock()
		return 0, t.bgErr
	}

	t.mu.Unlock()

	n, err := t.rc.Read(p)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.bgErr != nil {
		return n, t.bgErr
	}

	return n, err
}

func (t *tarOutput) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pe := t.rc.Close()

	if t.bgErr != nil {
		return t.bgErr
	}

	return pe
}

type transformOpt struct {
	statusUpdates func(ss *client.SolveStatus, sj []byte)
	phaseUpdates  func(phase string)
	cacheDir      string
	frontendAttrs map[string]string
	// buildSecrets maps a BuildKit secret id to its resolved plaintext. When
	// non-empty, a secrets session provider is attached to the solve so a
	// `RUN --mount=type=secret,id=<id>` step can read the value without it
	// landing in an image layer. The bytes live only for the duration of the
	// build and are never logged or exported.
	buildSecrets map[string][]byte
}

type TransformOptions func(*transformOpt)

func WithStatusUpdates(fn func(ss *client.SolveStatus, sj []byte)) TransformOptions {
	return func(o *transformOpt) {
		o.statusUpdates = fn
	}
}

func WithPhaseUpdates(fn func(phase string)) TransformOptions {
	return func(o *transformOpt) {
		o.phaseUpdates = fn
	}
}

func WithBuildArg(key, val string) TransformOptions {
	return func(o *transformOpt) {
		o.frontendAttrs["build-arg:"+key] = val
	}
}

func WithCacheDir(dir string) TransformOptions {
	return func(o *transformOpt) {
		o.cacheDir = dir
	}
}

func WithBuildArgs(args map[string]string) TransformOptions {
	return func(o *transformOpt) {
		for k, v := range args {
			o.frontendAttrs["build-arg:"+k] = v
		}
	}
}

// WithBuildSecrets attaches resolved secrets to the build, keyed by the mount id
// a Dockerfile references with `--mount=type=secret,id=<id>`. Unlike build args,
// these never enter the image or the frontend attributes.
func WithBuildSecrets(secrets map[string][]byte) TransformOptions {
	return func(o *transformOpt) {
		o.buildSecrets = secrets
	}
}

// attachBuildSecrets adds a secrets session provider to solveOpt when any build
// secret was supplied. Mirrors addRegistryAuth: the value reaches BuildKit over
// the session, never through frontend attributes or a layer.
func attachBuildSecrets(solveOpt *client.SolveOpt, secrets map[string][]byte) {
	if len(secrets) == 0 {
		return
	}
	solveOpt.Session = append(solveOpt.Session, secretsprovider.FromMap(secrets))
}

func (b *Buildkit) Transform(ctx context.Context, dfs fsutil.FS, tos ...TransformOptions) (io.ReadCloser, chan struct{}, error) {
	var opts transformOpt

	opts.frontendAttrs = map[string]string{
		//"attest:sbom":       "mode:max",
		//"attest:provenance": "mode:max",
		"build-arg:BUILDKIT_INLINE_CACHE": "1",
	}

	for _, o := range tos {
		o(&opts)
	}

	mounts := map[string]fsutil.FS{
		"dockerfile": dfs,
		"context":    dfs,
	}

	output := client.ExportEntry{
		Type: "image",
		Attrs: map[string]string{
			"push": "true",
			"name": ocireg.Host + "/",
		},
	}

	ref := idgen.Gen("r")

	solveOpt := client.SolveOpt{
		Exports:       []client.ExportEntry{output},
		LocalMounts:   mounts,
		Frontend:      "dockerfile.v0",
		FrontendAttrs: opts.frontendAttrs,
		Ref:           ref,
	}
	if err := b.addRegistryAuth(&solveOpt, ocireg.Host); err != nil {
		return nil, nil, err
	}
	attachBuildSecrets(&solveOpt, opts.buildSecrets)

	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}

	if opts.cacheDir != "" {
		solveOpt.CacheImports = []client.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"src": opts.cacheDir,
				},
			},
		}
		solveOpt.CacheExports = []client.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"dest": opts.cacheDir,
				},
			},
		}
	}

	sreq := gateway.SolveRequest{
		Frontend:    solveOpt.Frontend,
		FrontendOpt: solveOpt.FrontendAttrs,
	}

	sreq.CacheImports = make([]frontend.CacheOptionsEntry, len(solveOpt.CacheImports))
	for i, e := range solveOpt.CacheImports {
		sreq.CacheImports[i] = frontend.CacheOptionsEntry{
			Type:  e.Type,
			Attrs: e.Attrs,
		}
	}

	// not using shared context to not disrupt display but let is finish reporting errors
	//pw, err := progresswriter.NewPrinter(ctx, os.Stderr, "rawjson")
	//if err != nil {
	//return nil, err
	//}

	//mw := progresswriter.NewMultiWriter(pw)

	//mw.Status()

	b.Log.Info("building from fs walker", "ref", ref)

	var to tarOutput
	to.rc = r

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer w.Close()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		ssProgress := make(chan *client.SolveStatus, 1)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ss, ok := <-ssProgress:
					if !ok {
						b.Log.Info("status channel closed", "ref", ref)
						return
					}
					if data, err := json.Marshal(ss); err == nil {
						if opts.statusUpdates != nil {
							opts.statusUpdates(ss, data)
						}
					}
				}
			}
		}()

		_, err = b.Client.Build(ctx, solveOpt, "runtime", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			if opts.phaseUpdates != nil {
				opts.phaseUpdates("solving")
			}

			b.Log.Info("solving", "ref", ref)
			res, err := c.Solve(ctx, sreq)
			if err != nil {
				b.Log.Error("failed to solve", "err", err)
				return nil, err
			}

			if opts.phaseUpdates != nil {
				opts.phaseUpdates("solved")
			}

			b.Log.Info("solved", "ref", ref)

			return res, nil
		}, ssProgress)

		to.mu.Lock()
		if err != nil {
			w.Close()
			r.Close()
		}
		to.bgErr = err
		to.mu.Unlock()
	}()

	b.Log.Debug("returning tar output", "ref", ref)
	return &to, done, nil
}

type BuildStack struct {
	Stack   string
	CodeDir string
	Input   string

	Version     string
	OnBuild     []string
	AlpineImage string

	// EnvVars are user-configured environment variables to inject into the build.
	// For auto-stack builds, these are set on intermediate LLB states before
	// onBuild commands and asset precompilation steps.
	EnvVars map[string]string
}

type ImageConfig struct {
	Services map[string]string
}

type BuildResult struct {
	Entrypoint      string // The entrypoint (from stack or image ENTRYPOINT)
	Command         string // The command (from image CMD)
	ManifestDigest  string
	WorkingDir      string
	ExposedPorts    []string // OCI port/protocol keys, such as "4000/tcp"
	DetectionEvents []stackbuild.DetectionEvent
}

func exposedPortNames(ports map[string]struct{}) []string {
	if len(ports) == 0 {
		return nil
	}

	names := make([]string, 0, len(ports))
	for port := range ports {
		names = append(names, port)
	}
	sort.Strings(names)
	return names
}

func applyImageConfig(res *BuildResult, data []byte) error {
	var imgConfig struct {
		Config struct {
			WorkingDir   string              `json:"WorkingDir"`
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		} `json:"config"`
	}
	if err := json.Unmarshal(data, &imgConfig); err != nil {
		return err
	}

	res.WorkingDir = imgConfig.Config.WorkingDir
	res.ExposedPorts = exposedPortNames(imgConfig.Config.ExposedPorts)
	return nil
}

func (b *Buildkit) BuildImage(
	ctx context.Context,
	dfs fsutil.FS,
	bs BuildStack,
	app, imageURL string,
	tos ...TransformOptions,
) (*BuildResult, error) {
	var opts transformOpt

	opts.frontendAttrs = map[string]string{
		//"attest:sbom":       "mode:max",
		//"attest:provenance": "mode:max",
		"build-arg:BUILDKIT_INLINE_CACHE": "1",
	}

	for _, o := range tos {
		o(&opts)
	}

	mounts := map[string]fsutil.FS{
		"context": dfs,
	}

	ref := idgen.Gen("r")

	solveOpt := client.SolveOpt{
		LocalMounts: mounts,
		Ref:         ref,
	}

	var def *llb.Definition

	exportAttr := map[string]string{
		"push": "true",
		"name": imageURL,
	}

	var res BuildResult

	if bs.Stack == "dockerfile" {
		mounts["dockerfile"] = dfs
		solveOpt.Frontend = "dockerfile.v0"
		solveOpt.FrontendAttrs = opts.frontendAttrs
		solveOpt.FrontendAttrs["filename"] = bs.Input
	} else {
		buildOpts := stackbuild.BuildOptions{
			Log:         b.Log,
			Name:        app,
			OnBuild:     bs.OnBuild,
			Version:     bs.Version,
			AlpineImage: bs.AlpineImage,
			EnvVars:     bs.EnvVars,
		}

		stack, err := stackbuild.DetectStack(bs.CodeDir, buildOpts)
		if err != nil {
			return nil, err
		}

		state, err := stack.GenerateLLB(ctx, bs.CodeDir, buildOpts)
		if err != nil {
			return nil, err
		}

		res.Entrypoint = stack.Entrypoint()
		res.DetectionEvents = stack.Events()

		if wc := stack.WebCommand(); wc != "" {
			res.Command = wc
		}

		res.WorkingDir = stack.Image().Config.WorkingDir
		res.ExposedPorts = exposedPortNames(stack.Image().Config.ExposedPorts)

		def, err = state.Marshal(ctx)
		if err != nil {
			return nil, err
		}

		data, err := json.Marshal(stack.Image())
		if err != nil {
			return nil, err
		}

		exportAttr["containerimage.config"] = string(data)

		b.Log.Info("using stack", "stack", stack.Name())
	}

	output := client.ExportEntry{
		Type:  "image",
		Attrs: exportAttr,
	}

	solveOpt.Exports = []client.ExportEntry{output}
	registryHost, _, _ := strings.Cut(imageURL, "/")
	if err := b.addRegistryAuth(&solveOpt, registryHost); err != nil {
		return nil, err
	}
	attachBuildSecrets(&solveOpt, opts.buildSecrets)

	if opts.cacheDir != "" {
		solveOpt.CacheImports = []client.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"src": opts.cacheDir,
				},
			},
		}
		solveOpt.CacheExports = []client.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"dest": opts.cacheDir,
				},
			},
		}
	}

	sreq := gateway.SolveRequest{
		Frontend:    solveOpt.Frontend,
		FrontendOpt: solveOpt.FrontendAttrs,
	}

	sreq.CacheImports = make([]frontend.CacheOptionsEntry, len(solveOpt.CacheImports))
	for i, e := range solveOpt.CacheImports {
		sreq.CacheImports[i] = frontend.CacheOptionsEntry{
			Type:  e.Type,
			Attrs: e.Attrs,
		}
	}

	b.Log.Info("building from fs walker", "ref", ref)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ssProgress := make(chan *client.SolveStatus, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ss, ok := <-ssProgress:
				if !ok {
					b.Log.Info("status channel closed", "ref", ref)
					return
				}
				if data, err := json.Marshal(ss); err == nil {
					if opts.phaseUpdates != nil {
						for _, s := range ss.Vertexes {
							if s.Started == nil || s.Completed != nil {
								continue
							}

							if after, ok0 := strings.CutPrefix(s.Name, "[phase] "); ok0 {
								phase := after
								b.Log.Debug("phase update", "phase", phase)
								opts.phaseUpdates(phase)
							}
						}
					}

					if opts.statusUpdates != nil {
						opts.statusUpdates(ss, data)
					}
				}
			}
		}
	}()

	if def != nil {
		if opts.phaseUpdates != nil {
			opts.phaseUpdates("solving")
		}
		solveResp, err := b.Client.Solve(ctx, def, solveOpt, ssProgress)
		if opts.phaseUpdates != nil {
			opts.phaseUpdates("solved")
		}
		if err == nil && solveResp != nil && solveResp.ExporterResponse != nil {
			if digest, ok := solveResp.ExporterResponse["containerimage.digest"]; ok {
				res.ManifestDigest = digest
			}
		}
		return &res, err
	}

	// inlineImageConfig captures the built image's config JSON straight from the
	// gateway result metadata, where the frontend deposits it under
	// exptypes.ExporterImageConfigKey. Reading it here keeps config extraction in
	// the coordinator's own context, so we never round-trip to the registry by its
	// in-cluster name (cluster.local), which the coordinator can't resolve.
	var inlineImageConfig []byte

	buildResp, err := b.Client.Build(ctx, solveOpt, "runtime", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		if opts.phaseUpdates != nil {
			opts.phaseUpdates("solving")
		}

		b.Log.Info("solving", "ref", ref)
		res, err := c.Solve(ctx, sreq)
		if err != nil {
			b.Log.Error("failed to solve", "err", err)
			return nil, err
		}

		if opts.phaseUpdates != nil {
			opts.phaseUpdates("solved")
		}

		b.Log.Info("solved", "ref", ref)

		// Single-platform builds emit the config under the plain key; ParseKey
		// with a nil platform reads exactly that.
		inlineImageConfig = exptypes.ParseKey(res.Metadata, exptypes.ExporterImageConfigKey, nil)

		return res, nil
	}, ssProgress)

	if err == nil && buildResp != nil && buildResp.ExporterResponse != nil {
		if digest, ok := buildResp.ExporterResponse["containerimage.digest"]; ok {
			res.ManifestDigest = digest
		}

		// Extract working directory and exposed ports from the image config for
		// Dockerfile builds. The config comes from the gateway result metadata
		// captured during the solve above.
		if len(inlineImageConfig) > 0 {
			if err := applyImageConfig(&res, inlineImageConfig); err != nil {
				b.Log.Warn("failed to parse image config", "error", err)
			}
			// We intentionally do not flatten the image's ENTRYPOINT/CMD into a
			// command string here. When a service has no command, the image's
			// own ENTRYPOINT+CMD run in exec form via oci.WithImageConfig at
			// launch (MIR-1444), preserving argv boundaries, PID 1, and signals.
		}
	}

	return &res, err
}

func (b *Buildkit) addRegistryAuth(solveOpt *client.SolveOpt, registryHost string) error {
	if b.WorkloadIssuer == nil || registryHost != ocireg.Host {
		return nil
	}

	token, err := b.WorkloadIssuer.IssueSystemWorkloadToken(
		workloadidentity.SystemWorkloadBuildKit,
		workloadidentity.TokenOptions{Audience: []string{ocireg.Audience}},
	)
	if err != nil {
		return fmt.Errorf("issuing registry token: %w", err)
	}

	cfg := &configfile.ConfigFile{
		AuthConfigs: map[string]types.AuthConfig{
			registryHost: {RegistryToken: token},
		},
	}
	solveOpt.Session = append(solveOpt.Session, authprovider.NewDockerAuthProvider(cfg, nil))
	return nil
}
