package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/stackbuild"

	"github.com/tonistiigi/fsutil"
)

type Buildkit struct {
	Client *client.Client

	Log *slog.Logger

	// RegistryURLOverride is used for fetching image configs when the push URL
	// is not accessible from the current host (e.g., in tests where push goes to
	// a docker network address but fetch needs localhost:port)
	RegistryURLOverride string
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

type BuildResult struct {
	Entrypoint     string // The entrypoint (from stack or image ENTRYPOINT)
	Command        string // The command (from image CMD)
	ManifestDigest string
	WorkingDir     string
}

// fetchImageConfigFromRegistry fetches the image config JSON from a registry using the config digest.
// imageURL is like "registry:5000/repo:tag" and configDigest is like "sha256:abc123..."
// If insecure is true, falls back to HTTP if HTTPS fails (for local/test registries).
func fetchImageConfigFromRegistry(ctx context.Context, imageURL, configDigest string, insecure bool) ([]byte, error) {
	// Parse the image URL to extract registry and repository
	// Format: [registry/]repository[:tag]
	parts := strings.SplitN(imageURL, "/", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid image URL format: %s", imageURL)
	}

	registry := parts[0]
	repoWithTag := parts[1]

	// Remove tag from repository
	repo := strings.Split(repoWithTag, ":")[0]

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Try HTTPS first
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, configDigest)
	body, err := doRegistryFetch(ctx, httpClient, url)
	if err == nil {
		return body, nil
	}

	// Fall back to HTTP only if insecure flag is explicitly set
	if insecure {
		url = fmt.Sprintf("http://%s/v2/%s/blobs/%s", registry, repo, configDigest)
		body, err = doRegistryFetch(ctx, httpClient, url)
		if err == nil {
			return body, nil
		}
	}

	return nil, fmt.Errorf("failed to fetch config from registry: %w", err)
}

// doRegistryFetch performs an HTTP GET request with the given context and client.
func doRegistryFetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
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
			Name:        app,
			OnBuild:     bs.OnBuild,
			Version:     bs.Version,
			AlpineImage: bs.AlpineImage,
		}

		stack, err := stackbuild.DetectStack(bs.CodeDir, buildOpts)
		if err != nil {
			return nil, err
		}

		state, err := stack.GenerateLLB(bs.CodeDir, buildOpts)
		if err != nil {
			return nil, err
		}

		res.Entrypoint = stack.Entrypoint()

		if wc := stack.WebCommand(); wc != "" {
			res.Command = wc
		}

		res.WorkingDir = stack.Image().Config.WorkingDir

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

		return res, nil
	}, ssProgress)

	if err == nil && buildResp != nil && buildResp.ExporterResponse != nil {
		if digest, ok := buildResp.ExporterResponse["containerimage.digest"]; ok {
			res.ManifestDigest = digest
		}

		// Extract working directory from image config for Dockerfile builds
		var configJSON string
		if cfg, ok := buildResp.ExporterResponse["containerimage.config"]; ok {
			// Config is directly available in response
			configJSON = cfg
		} else if configDigest, ok := buildResp.ExporterResponse["containerimage.config.digest"]; ok {
			// Config needs to be fetched from registry
			fetchURL := imageURL
			if b.RegistryURLOverride != "" {
				// Use override URL for fetching (e.g., localhost:port in tests)
				// Extract the repository path from imageURL and combine with override registry
				parts := strings.SplitN(imageURL, "/", 2)
				if len(parts) == 2 {
					fetchURL = b.RegistryURLOverride + "/" + parts[1]
				}
			}
			b.Log.Debug("fetching config from registry", "digest", configDigest, "fetchURL", fetchURL)
			// Use insecure (HTTP fallback) when RegistryURLOverride is set (test/local registries)
			insecure := b.RegistryURLOverride != ""
			configBytes, fetchErr := fetchImageConfigFromRegistry(ctx, fetchURL, configDigest, insecure)
			if fetchErr != nil {
				b.Log.Warn("failed to fetch config from registry", "error", fetchErr)
			} else {
				configJSON = string(configBytes)
			}
		}

		if configJSON != "" {
			var imgConfig struct {
				Config struct {
					WorkingDir string   `json:"WorkingDir"`
					Entrypoint []string `json:"Entrypoint"`
					Cmd        []string `json:"Cmd"`
				} `json:"config"`
			}
			if err := json.Unmarshal([]byte(configJSON), &imgConfig); err == nil {
				res.WorkingDir = imgConfig.Config.WorkingDir
				if res.Entrypoint == "" {
					res.Entrypoint = buildImageCommand(imgConfig.Config.Entrypoint, nil)
				}

				if res.Command == "" {
					res.Command = buildImageCommand(imgConfig.Config.Cmd, nil)
				}
			} else {
				b.Log.Warn("failed to parse image config", "error", err)
			}
		}
	}

	return &res, err
}
