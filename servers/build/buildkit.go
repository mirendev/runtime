package build

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/stackbuild"

	"github.com/tonistiigi/fsutil"
)

type Buildkit struct {
	Client *client.Client

	Log *slog.Logger
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

type ociWriter struct {
	log *slog.Logger
	io.WriteCloser
}

func (o *ociWriter) Close() error {
	o.log.Info("closing oci writer")
	return o.WriteCloser.Close()
}

type transformOpt struct {
	statusUpdates func(ss *client.SolveStatus, sj []byte)
	phaseUpdates  func(phase string)
	cacheDir      string
	frontendAttrs map[string]string
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

	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}

	output := client.ExportEntry{
		Type: "image",
		Attrs: map[string]string{
			"push": "true",
			"name": "registry.cluster:5000/",
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
}

type ImageConfig struct {
	Services map[string]string
}

func (b *Buildkit) loadAppConfig(dfs fsutil.FS) (*appconfig.AppConfig, error) {
	dr, err := dfs.Open("app.json")
	if err != nil {
		return nil, nil
	}

	defer dr.Close()

	data, err := io.ReadAll(dr)
	if err != nil {
		return nil, err
	}

	ac, err := appconfig.Parse(data)
	if err != nil {
		return nil, err
	}

	return ac, nil
}

type BuildResult struct {
	Entrypoint string
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
		stack, err := stackbuild.DetectStack(bs.CodeDir)
		if err != nil {
			return nil, err
		}

		state, err := stack.GenerateLLB(bs.CodeDir, stackbuild.BuildOptions{
			Name:        app,
			OnBuild:     bs.OnBuild,
			Version:     bs.Version,
			AlpineImage: bs.AlpineImage,
		})
		if err != nil {
			return nil, err
		}

		res.Entrypoint = stack.Entrypoint()

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
						for _, s := range ss.Vertexes { // codespell:ignore
							if s.Started == nil || s.Completed != nil {
								continue
							}

							if strings.HasPrefix(s.Name, "[phase] ") {
								phase := strings.TrimPrefix(s.Name, "[phase] ")
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
		_, err := b.Client.Solve(ctx, def, solveOpt, ssProgress)
		if opts.phaseUpdates != nil {
			opts.phaseUpdates("solved")
		}
		return &res, err
	}

	_, err := b.Client.Build(ctx, solveOpt, "runtime", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
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

	return &res, err
}

func initializeBuildKitCache(cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	// Create a minimal index.json, which prevents a warning log on buildkit's first boot
	indexPath := filepath.Join(cacheDir, "index.json")
	indexContent := []byte(`{"manifests":[]}`)

	// Only create if it doesn't already exist
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, indexContent, 0644); err != nil {
			return err
		}
	}

	return nil
}
