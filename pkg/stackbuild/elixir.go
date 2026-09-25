package stackbuild

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"miren.dev/runtime/pkg/imagerefs"
)

// elixirDepEnvVars maps Mix dependency names to the environment variables they
// typically require.
var elixirDepEnvVars = map[string][]packageEnvVarDef{
	"postgrex": {{name: "DATABASE_URL", confidence: "recommended"}},
	"myxql":    {{name: "DATABASE_URL", confidence: "recommended"}},
	"redix":    {{name: "REDIS_URL", confidence: "recommended"}},
	"sentry":   {{name: "SENTRY_DSN", confidence: "recommended"}},
	"ex_aws":   {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
}

// elixirCode anchors a pattern to code that isn't commented out: everything
// before the match is either not a '#' or a '#{' interpolation. phx.new's
// runtime.exs is full of commented-out System.get_env examples. The anchor
// means only the first read on a line is found, which is fine for config.
const elixirCode = `^(?:[^#]|#\{)*?`

// elixirEnvPatterns find env var reads in Elixir source and config.
var elixirEnvPatterns = []*regexp.Regexp{
	regexp.MustCompile(elixirCode + `System\.(?:get_env|fetch_env!?)\(\s*"([A-Z][A-Z0-9_]+)"`),
}

// elixirOptionalEnvPatterns mark reads that tolerate a missing value.
// System.get_env/1 returns nil, so it's optional unless the nil is turned into
// a crash: `System.get_env("X") || raise ...`, including the phx.new style that
// breaks the line after `||` and raises on the next one. RE2 has no lookahead,
// so "anything after || except raise or end of line" is spelled out as a
// character class; a fallback that happens to start with "r" reads as
// required, which errs on the side of asking.
var elixirOptionalEnvPatterns = []*regexp.Regexp{
	regexp.MustCompile(elixirCode + `System\.get_env\(\s*"([A-Z][A-Z0-9_]+)"\s*(?:,|\)\s*(?:\|\|\s*[^\sr]|[^|\s]|$))`),
	regexp.MustCompile(elixirCode + `System\.fetch_env\(\s*"([A-Z][A-Z0-9_]+)"`),
}

var (
	mixAppRe      = regexp.MustCompile(`\bapp:\s*:([a-z_][a-zA-Z0-9_]*)`)
	mixReleasesRe = regexp.MustCompile(`\breleases:\s*\[\s*([a-z_][a-zA-Z0-9_]*):`)
	mixElixirRe   = regexp.MustCompile(`\belixir:\s*"([^"]+)"`)
)

// ElixirStack implements Stack for Mix projects. It builds a Mix release on a
// hexpm/elixir image and ships only the release, which bundles ERTS, on
// debian-slim.
type ElixirStack struct {
	MetaStack

	mixExs      []byte
	releaseName string
	// namedRelease is set when mix.exs has a releases: block; only then does
	// mix release accept the name as an argument.
	namedRelease bool
	umbrella     bool
	hasPhoenix   bool
	hasAssets    bool
	// assetsNpm is set when assets/package.json exists: Phoenix keeps npm
	// dependencies there, out of reach of the root-level npm augmentation.
	assetsNpm bool
	// pin is an Elixir version the app pins for its version manager, if any.
	pin *elixirPin

	requiredEnvVars []EnvVarRequirement
}

func (s *ElixirStack) BaseDistro() string {
	return "debian"
}

func (s *ElixirStack) Name() string {
	return "elixir"
}

func (s *ElixirStack) Detect() bool {
	if !s.hasFile("mix.exs") {
		return false
	}
	s.Event("file", "mix.exs", "Found mix.exs")
	return true
}

func (s *ElixirStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	s.mixExs, _ = s.readFile("mix.exs")

	if m := mixElixirRe.FindSubmatch(s.mixExs); m != nil {
		s.Event("config", "elixir-requirement", "mix.exs requires Elixir "+string(m[1]))
	}

	s.umbrella = bytes.Contains(s.mixExs, []byte("apps_path:"))
	if s.umbrella {
		s.Event("config", "umbrella", "Umbrella project")
	}

	// A releases: block names the release explicitly, and is the only option
	// for an umbrella; otherwise mix release uses the OTP app name.
	if m := mixReleasesRe.FindSubmatch(s.mixExs); m != nil {
		s.releaseName = string(m[1])
		s.namedRelease = true
	} else if m := mixAppRe.FindSubmatch(s.mixExs); m != nil && !s.umbrella {
		s.releaseName = string(m[1])
	}
	if s.releaseName != "" {
		s.Event("config", "release", "Mix release: "+s.releaseName)
	}

	// Check the lock too: an umbrella declares phoenix in its web app's
	// mix.exs, not the root one, but the root mix.lock covers every app.
	lock, _ := s.readFile("mix.lock")
	if s.hasLockedDep("phoenix", lock) {
		s.hasPhoenix = true
		s.Event("framework", "phoenix", "Detected Phoenix")
		if s.umbrella {
			s.Event("config", "umbrella-assets", "Umbrella Phoenix app: assets under apps/ aren't built automatically; add their mix assets.deploy to [build] onbuild")
		}
	}

	if bytes.Contains(s.mixExs, []byte(`"assets.deploy"`)) {
		s.hasAssets = true
		s.Event("config", "assets.deploy", "Will build assets with mix assets.deploy")
	}

	if s.hasFile("assets/package.json") {
		s.assetsNpm = true
		s.Event("augmentation", "npm", "Found assets/package.json, installing npm")
	}

	if s.hasFile("mix.lock") {
		s.Event("file", "mix.lock", "Found mix.lock")
	}

	if s.pin = s.readElixirPin(); s.pin != nil {
		s.Event("config", "elixir-version", "Elixir "+s.pin.elixir+" pinned in "+s.pin.source)
	}

	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
	}
}

// hasDep reports whether mix.exs declares the named dependency.
func (s *ElixirStack) hasDep(name string) bool {
	return bytes.Contains(s.mixExs, []byte("{:"+name+","))
}

// hasLockedDep reports whether the named package is a direct dependency or
// appears in mix.lock (so transitive deps count too).
func (s *ElixirStack) hasLockedDep(name string, lock []byte) bool {
	return s.hasDep(name) || bytes.Contains(lock, []byte(`"`+name+`": {`))
}

func (s *ElixirStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
	if s.releaseName == "" {
		return nil, fmt.Errorf("could not determine the Mix release name: add `app: :name` to project/0 in mix.exs, or a releases: block for an umbrella project")
	}

	build, err := s.builderImage(opts)
	if err != nil {
		return nil, err
	}
	tag := build.tag
	// The release runs on bookworm-slim and links against the builder's glibc
	// and OpenSSL, so a builder on another distro would build fine and then
	// crash at boot. Only a full tag from build.version can get here that way.
	if !strings.Contains(tag, "-debian-bookworm-") {
		return nil, fmt.Errorf("build version %q is not a hexpm/elixir Debian bookworm tag: use a version like %s, or a full bookworm tag (see https://miren.md/guides/elixir#versions)", tag, imagerefs.ElixirDefaultVersion)
	}
	s.Event("config", "elixir-image", "Building on hexpm/elixir:"+tag+": "+build.detail)

	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		// Local build output would clobber the build's own deps and _build, and
		// assets/node_modules is always installed fresh below, since a copy
		// from a laptop can carry the wrong platform's native modules. A root
		// node_modules is left alone: the JS augmentations treat it as vendored.
		llb.ExcludePatterns(contextExcludes("_build", "deps", ".elixir_ls", "assets/node_modules")),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	// The builder never becomes the final image, so resolve its config for the
	// build steps without folding its env into the result (see GoStack).
	mr := imagemetaresolver.New()
	builder := llb.Image(imagerefs.GetElixirImage(tag), llb.WithMetaResolver(mr))

	h := &highlevelBuilder{opts}

	// The slim images carry no C toolchain or git, which NIF deps and git deps
	// need respectively.
	builder = h.aptInstall(builder, "build-essential", "git", "ca-certificates")
	augs := s.Augmentations()
	if s.assetsNpm && !slices.Contains(augs, AugNpm) && !slices.Contains(augs, AugYarn) {
		builder = h.installNpm(builder, s.BaseDistro())
	}
	// The JS augmentations install packages as the app user.
	builder = s.addAppUser(builder)
	builder = h.applyAugmentations(builder, localCtx, s.BaseDistro(), augs, s.SkipJSInstall())

	builder = builder.
		AddEnv("MIX_ENV", "prod").
		AddEnv("LANG", "C.UTF-8").
		Dir("/app")

	// User env vars have to be in place before any mix step: config/*.exs is
	// evaluated by compile and again by release, and an
	// Application.compile_env value that differs between the two makes the
	// release refuse to boot. Sorted, because env order is part of every later
	// step's cache key and map order would change it on each build.
	for _, k := range slices.Sorted(maps.Keys(opts.EnvVars)) {
		builder = builder.AddEnv(k, opts.EnvVars[k])
	}

	builder = builder.Run(
		llb.Shlex("sh -c 'mix local.hex --force && mix local.rebar --force'"),
		llb.WithCustomName("[phase] Installing Hex and Rebar"),
	).Root()

	// Fetch and compile deps from mix.exs, mix.lock and config/ alone, so the
	// layer survives changes to application code. An umbrella's deps are
	// declared across apps/*/mix.exs, so it skips straight to the full copy.
	if !s.umbrella {
		builder = s.copyDepInputs(builder, localCtx)
		builder = builder.Run(
			llb.Shlex("sh -c 'mix deps.get --only prod && mix deps.compile'"),
			h.CacheMount("/root/.hex/packages"),
			llb.WithCustomName("[phase] Fetching and compiling Mix dependencies"),
		).Root()
	}

	builder = h.copyApp(builder, localCtx)

	// Compile before assets.deploy: Phoenix 1.8 generates colocated LiveView
	// hooks during compilation, and esbuild needs them.
	builder = builder.Run(
		llb.Shlex("sh -c 'mix deps.get --only prod && mix compile'"),
		h.CacheMount("/root/.hex/packages"),
		llb.WithCustomName("[phase] Compiling Elixir application"),
	).Root()

	if s.assetsNpm {
		npmCmd := "npm install"
		if s.hasFile("assets/package-lock.json") {
			npmCmd = "npm ci"
		}
		builder = builder.Dir("/app/assets").Run(
			llb.Shlex(npmCmd),
			h.CacheMount("/root/.npm"),
			llb.WithCustomName("[phase] Installing asset JS deps with npm"),
		).Root().Dir("/app")
	}

	if s.hasAssets {
		builder = builder.Run(
			llb.Shlex("mix assets.deploy"),
			llb.WithCustomName("[phase] Building assets"),
		).Root()
	}

	builder = s.applyOnBuild(builder, opts)

	releaseCmd := "mix release --overwrite"
	if s.namedRelease {
		releaseCmd += " " + s.releaseName
	}
	builder = builder.Run(
		llb.Shlex(releaseCmd),
		llb.WithCustomName("[phase] Assembling Mix release"),
	).Root()

	rt := s.assembleRuntime(ctx, h, builder, opts)
	return &rt, nil
}

// copyDepInputs stages just what deps.get and deps.compile read. config/ comes
// along because deps are compiled against the app's compile-time config, but
// only when it exists: mix new stopped generating it in Elixir 1.9, and a
// literal (non-wildcard) copy source fails the build when it's missing.
func (s *ElixirStack) copyDepInputs(cur, mnt llb.State) llb.State {
	origin := time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC)
	srcs := []string{"mix.exs", "mix.lock*"}
	if s.hasDir("config") {
		srcs = append(srcs, "config")
	}
	for _, src := range srcs {
		cur = cur.File(llb.Copy(mnt, src, "/app/", &llb.CopyInfo{
			CreateDestPath:     true,
			FollowSymlinks:     true,
			AllowWildcard:      true,
			AllowEmptyWildcard: true,
			CreatedTime:        &origin,
		}))
	}
	return cur
}

// assembleRuntime puts the release on debian-slim. The release bundles ERTS,
// so the runtime needs only the shared libraries ERTS links against. hexpm's
// bookworm builders match DebianSlim's glibc and OpenSSL.
func (s *ElixirStack) assembleRuntime(ctx context.Context, h *highlevelBuilder, builder llb.State, opts BuildOptions) llb.State {
	rt := s.baseImage(ctx, imagerefs.DebianSlim, opts)
	rt = h.aptInstall(rt, "libstdc++6", "openssl", "libncurses6", "ca-certificates")
	rt = s.addAppUser(rt)
	rt = rt.File(llb.Mkdir("/app", 0o755, llb.WithParents(true), llb.WithUIDGID(2010, 2011)))
	rt = rt.File(llb.Copy(builder, "/app/_build/prod/rel/"+s.releaseName, "/app", &llb.CopyInfo{
		CopyDirContentsOnly: true,
		CreateDestPath:      true,
		ChownOpt:            &appChown,
	}), llb.WithCustomName("[phase] Copying Mix release"))

	// The BEAM wants a UTF-8 locale; C.UTF-8 ships with Debian, so no
	// locales package or locale-gen is needed.
	s.AddEnv("LANG", "C.UTF-8")
	s.AddEnv("MIX_ENV", "prod")
	// The app user's home is /app, which it owns; the release writes its
	// tmp dir and the BEAM its cookie relative to it.
	s.AddEnv("HOME", "/app")
	if s.hasPhoenix {
		// phx.new's runtime.exs only starts the endpoint when this is set.
		s.AddEnv("PHX_SERVER", "true")
	}
	return rt
}

func (s *ElixirStack) WebCommand() string {
	if s.releaseName == "" {
		return ""
	}
	return "/app/bin/" + s.releaseName + " start"
}

// RequiredEnvVars returns the detected environment variable requirements
func (s *ElixirStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

func (s *ElixirStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	sourceVars := scanSourceFilesForEnvVars(s.dir, []string{".ex", ".exs"}, elixirEnvPatterns, elixirOptionalEnvPatterns)
	sourceVars = withoutDevOnlyConfig(s.dir, sourceVars)

	// 1. Phoenix core. phx.new's runtime.exs raises without SECRET_KEY_BASE,
	// and PHX_HOST drives both URL generation and the websocket origin
	// check, so LiveView can't connect until it matches the app's route.
	if s.hasPhoenix {
		results = append(results, EnvVarRequirement{
			Name:        "SECRET_KEY_BASE",
			Source:      "phoenix_core",
			Confidence:  "required",
			Reason:      "Required by Phoenix in production",
			CanGenerate: true,
		}, EnvVarRequirement{
			Name:       "PHX_HOST",
			Source:     "phoenix_core",
			Confidence: "recommended",
			Reason:     "Public hostname for URLs and the LiveView websocket origin check",
		})
	}

	// 2. Dependency-based inference, elevated when the code reads the var
	// without a fallback.
	lock, _ := s.readFile("mix.lock")
	deps := make([]string, 0, len(elixirDepEnvVars))
	for dep := range elixirDepEnvVars {
		deps = append(deps, dep)
	}
	slices.Sort(deps)
	for _, dep := range deps {
		if !s.hasLockedDep(dep, lock) {
			continue
		}
		for _, v := range elixirDepEnvVars[dep] {
			if hasEnvVar(results, v.name) {
				continue
			}
			confidence := v.confidence
			if confidence == "recommended" && elevateToRequired(v.name, sourceVars) {
				confidence = "required"
			}
			results = append(results, EnvVarRequirement{
				Name:       v.name,
				Source:     "dep",
				Confidence: confidence,
				Reason:     dep + " dependency detected",
			})
		}
	}

	// 3. Remaining source references.
	for _, v := range sourceVars {
		if hasEnvVar(results, v.name) {
			continue
		}
		confidence, reason := "required", "Referenced in application code"
		if v.optional {
			confidence, reason = "optional", "Referenced in application code (has fallback)"
		}
		results = append(results, EnvVarRequirement{
			Name:       v.name,
			Source:     "code",
			Confidence: confidence,
			Reason:     reason,
		})
	}

	// 4. Sample env files.
	for _, filename := range []string{".env.sample", ".env.example"} {
		for _, v := range parseEnvSampleFile(s.dir, filename) {
			if !hasEnvVar(results, v) {
				results = append(results, EnvVarRequirement{
					Name:       v,
					Source:     "config",
					Confidence: "required",
					Reason:     "Declared in " + filename,
				})
			}
		}
	}

	return results
}

// withoutDevOnlyConfig drops vars that are only read from config/dev.exs or
// config/test.exs, which never load in a prod release.
func withoutDevOnlyConfig(dir string, vars []detectedEnvVar) []detectedEnvVar {
	devOnly := map[string]bool{}
	for _, f := range []string{"config/dev.exs", "config/test.exs"} {
		for _, v := range scanFileForEnvVars(filepath.Join(dir, f), elixirEnvPatterns, elixirOptionalEnvPatterns) {
			devOnly[v.name] = true
		}
	}
	if len(devOnly) == 0 {
		return vars
	}

	// A var read from dev/test config may also be read elsewhere; keep it if so.
	elsewhere := map[string]bool{}
	for _, v := range scanSourceFilesForEnvVars(dir, []string{".ex"}, elixirEnvPatterns, nil) {
		elsewhere[v.name] = true
	}
	for _, f := range []string{"config/config.exs", "config/prod.exs", "config/runtime.exs"} {
		for _, v := range scanFileForEnvVars(filepath.Join(dir, f), elixirEnvPatterns, nil) {
			elsewhere[v.name] = true
		}
	}

	var kept []detectedEnvVar
	for _, v := range vars {
		if devOnly[v.name] && !elsewhere[v.name] {
			continue
		}
		kept = append(kept, v)
	}
	return kept
}
