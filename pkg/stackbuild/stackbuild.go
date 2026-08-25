package stackbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/util/system"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"miren.dev/runtime/pkg/imagerefs"
)

// BuildOptions contains configuration for stack builds
type BuildOptions struct {
	Log interface {
		Info(string, ...any)
		Warn(string, ...any)
	}

	// Name is the name of the application being built
	Name string

	// Version specifies the language/runtime version to use
	// If empty, defaults to latest stable version
	Version string

	// CacheNS specifies the namespace for persistent cache mounts
	CacheNS string

	// The alpine image to use for the base image.
	AlpineImage string

	OnBuild []string

	// EnvVars are user-configured environment variables to inject into build steps
	// (onBuild commands, asset precompilation). These are set on intermediate LLB
	// states only and do not persist to the final image config.
	EnvVars map[string]string
}

// DetectionEvent represents something detected during stack analysis
type DetectionEvent struct {
	Kind    string // e.g., "file", "package", "framework", "config"
	Name    string // e.g., "Gemfile", "rails", "puma"
	Message string // Human-readable description
}

// Stack represents a programming language/framework stack
type Stack interface {
	Name() string
	// Detect returns true if the given directory contains code for this stack
	Detect() bool
	// Init is called after detection to perform common initialization
	Init(opts BuildOptions)
	// GenerateLLB creates the BuildKit LLB for building this stack
	GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error)

	Image() ocispecs.Image

	Entrypoint() string

	// WebCommand returns the default command for the web service in a Procfile
	WebCommand() string

	// Events returns detection events collected during Detect() and Init()
	Events() []DetectionEvent

	// RequiredEnvVars returns environment variables detected as required/recommended
	RequiredEnvVars() []EnvVarRequirement

	// BaseDistro returns the package manager family of the stack's base image
	// ("debian" or "alpine"). Used to dispatch apt vs apk for augmentations.
	BaseDistro() string

	// metaStack returns the embedded *MetaStack. Unexported so only stacks in
	// this package can satisfy Stack — and since every concrete stack embeds
	// MetaStack, the method is auto-fulfilled, making it impossible to add a
	// new stack without also wiring up augmentation/events plumbing.
	metaStack() *MetaStack
}

// DetectStack identifies the programming stack in the given directory
func DetectStack(dir string, opts BuildOptions) (Stack, error) {
	ms := MetaStack{dir: dir}
	ms.setupResult()

	stacks := []Stack{
		&RubyStack{MetaStack: ms},
		&PythonStack{MetaStack: ms},
		&BunStack{MetaStack: ms},
		&NodeStack{MetaStack: ms},
		&GoStack{MetaStack: ms},
		&RustStack{MetaStack: ms},
	}
	for _, stack := range stacks {
		if stack.Detect() {
			stack.Init(opts)
			attachAugmentations(stack, dir)
			return stack, nil
		}
	}

	return nil, fmt.Errorf("no supported stack detected in %s", dir)
}

// attachAugmentations detects and records augmentations for the given primary
// stack so that Events() reports them and GenerateLLB() can apply them.
func attachAugmentations(stack Stack, dir string) {
	augs, skipInstall, events := DetectAugmentations(dir, stack.Name())
	ms := stack.metaStack()
	ms.augmentations = augs
	ms.skipJSInstall = skipInstall
	ms.events = append(ms.events, events...)
}

// MetaStack provides shared functionality for all stack implementations
type MetaStack struct {
	dir           string
	result        ocispecs.Image
	events        []DetectionEvent
	augmentations []Augmentation
	skipJSInstall bool
}

// metaStack returns s itself, fulfilling the unexported Stack interface
// requirement automatically for every concrete stack that embeds MetaStack.
func (s *MetaStack) metaStack() *MetaStack {
	return s
}

// Augmentations returns the secondary tooling layers (npm, bun, ...) attached
// to this stack by DetectStack.
func (s *MetaStack) Augmentations() []Augmentation {
	return s.augmentations
}

// SkipJSInstall reports whether the app already ships a node_modules directory,
// in which case the JS package install (npm install / bun install) should be
// skipped — the tool itself is still installed so onBuild commands can use it.
func (s *MetaStack) SkipJSInstall() bool {
	return s.skipJSInstall
}

func (s *MetaStack) Init(opts BuildOptions) {
	// Base implementation does nothing; stacks can override for specific initialization
}

// RequiredEnvVars returns nil by default; stacks can override to provide detected env vars
func (s *MetaStack) RequiredEnvVars() []EnvVarRequirement {
	return nil
}

func (s *MetaStack) Entrypoint() string {
	return ""
}

// Event adds a detection event
func (s *MetaStack) Event(kind, name, message string) {
	s.events = append(s.events, DetectionEvent{
		Kind:    kind,
		Name:    name,
		Message: message,
	})
}

// Events returns all detection events
func (s *MetaStack) Events() []DetectionEvent {
	return s.events
}

func (s *MetaStack) setupResult() {
	pl := platforms.Normalize(platforms.DefaultSpec())
	s.result.Architecture = pl.Architecture
	s.result.OS = pl.OS
	s.result.OSVersion = pl.OSVersion
	s.result.OSFeatures = pl.OSFeatures
	s.result.Variant = pl.Variant
	s.result.RootFS.Type = "layers"
	s.result.Config.WorkingDir = "/app"
	s.setResultEnv("PATH", system.DefaultPathEnv(pl.OS))
}

// setResultEnv sets key=value in the final image config, replacing an existing
// entry for key if present and appending otherwise.
func (s *MetaStack) setResultEnv(key, value string) {
	entry := key + "=" + value
	prefix := key + "="
	for i, e := range s.result.Config.Env {
		if strings.HasPrefix(e, prefix) {
			s.result.Config.Env[i] = entry
			return
		}
	}
	s.result.Config.Env = append(s.result.Config.Env, entry)
}

// hasResultEnv reports whether the final image config already sets key.
func (s *MetaStack) hasResultEnv(key string) bool {
	prefix := key + "="
	for _, e := range s.result.Config.Env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// pathFromEnv returns the value of the PATH entry in a docker/OCI-style env
// slice ("KEY=VALUE"), or "" if none is present.
func pathFromEnv(env []string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			return v
		}
	}
	return ""
}

// baseImage builds the LLB state for a base image ref and inherits the image's
// environment into the final image config. Every stack should build the base(s)
// that become its final image through this helper so env inheritance is
// automatic and uniform — a new stack gets it for free.
func (s *MetaStack) baseImage(ctx context.Context, ref string, opts BuildOptions) llb.State {
	// New(), not Default(): Default() is a process-wide singleton whose cache
	// lives for the process lifetime, keyed by ref+platform. The build server is
	// long-lived and our refs are mutable tags, so a later build could inherit
	// Env from a stale cached revision while BuildKit solves against the current
	// one — the very PATH/config mismatch this helper exists to prevent. A fresh
	// resolver per build resolves a coherent current config; it is still shared
	// within the build between inheritBaseEnv and the LLB marshal below.
	mr := imagemetaresolver.New()
	s.inheritBaseEnv(ctx, mr, ref, opts)
	return llb.Image(ref, llb.WithMetaResolver(mr))
}

// inheritBaseEnv resolves ref's image config and folds the environment the
// upstream Dockerfile set into the final image config, so the exported image
// behaves like `docker run <base>` would:
//
//   - PATH is always taken from the base. Upstream images (e.g. Bun's, which
//     relocates node to a non-standard bin dir) prepend their own directories
//     to the standard set, so the base PATH is a superset and supersedes our
//     seeded default.
//   - Every other var (LANG, GEM_HOME, NODE_VERSION, ...) is filled in only
//     when the stack has not already set it deliberately, so Miren's own
//     AddEnv choices always win regardless of call order.
//
// Best-effort: the base image must be pullable for the build to proceed at all,
// so a resolve failure here is unlikely; when it happens we keep what we have
// rather than failing the build.
func (s *MetaStack) inheritBaseEnv(ctx context.Context, mr llb.ImageMetaResolver, ref string, opts BuildOptions) {
	pl := platforms.Normalize(platforms.DefaultSpec())
	_, _, cfg, err := mr.ResolveImageConfig(ctx, ref, sourceresolver.Opt{Platform: &pl})
	if err != nil {
		if opts.Log != nil {
			opts.Log.Warn("could not resolve base image config for env inheritance; keeping defaults", "ref", ref, "error", err.Error())
		}
		return
	}

	var img ocispecs.Image
	if err := json.Unmarshal(cfg, &img); err != nil {
		if opts.Log != nil {
			opts.Log.Warn("could not parse base image config for env inheritance; keeping defaults", "ref", ref, "error", err.Error())
		}
		return
	}

	for _, e := range img.Config.Env {
		key, value, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if key == "PATH" || !s.hasResultEnv(key) {
			s.setResultEnv(key, value)
		}
	}
}

func (s *MetaStack) Image() ocispecs.Image {
	return s.result
}

// AddEnv sets a deliberate env var on the final image config. It replaces any
// existing entry for key — including one inherited from the base image — so the
// stack's explicit choice wins.
func (s *MetaStack) AddEnv(key, value string) {
	s.setResultEnv(key, value)
}

func (s *MetaStack) SetEntrypoint(ep []string) {
	s.result.Config.Entrypoint = ep
}

func (s *MetaStack) SetCwd(cwd string) {
	s.result.Config.WorkingDir = cwd
}

func (s *MetaStack) SetCmd(cmd []string) {
	s.result.Config.Cmd = cmd
}

func (s *MetaStack) hasFile(path string) bool {
	st, err := os.Stat(filepath.Join(s.dir, path))
	return err == nil && st.Mode().IsRegular()
}

func (s *MetaStack) hasDir(path string) bool {
	st, err := os.Stat(filepath.Join(s.dir, path))
	return err == nil && st.Mode().IsDir()
}

func (s *MetaStack) readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, path))
}

func (s *MetaStack) detectInFile(path, re string) bool {
	content, err := s.readFile(path)
	if err != nil {
		return false
	}

	r, err := regexp.Compile(re)
	if err != nil {
		return false
	}

	return r.Match(content)
}

func (s *MetaStack) applyOnBuild(cur llb.State, opts BuildOptions) llb.State {
	// Inject user env vars so they're available to onBuild commands
	for k, v := range opts.EnvVars {
		cur = cur.AddEnv(k, v)
	}

	for _, sh := range opts.OnBuild {
		cur = cur.Dir("/app").Run(
			llb.Args([]string{"/bin/sh", "-c", sh}),
			llb.WithCustomName("[phase] Application onbuild: "+sh),
		).Root()
	}

	return cur
}

func (m *MetaStack) addAppUser(cur llb.State) llb.State {
	m.result.Config.User = "2010"

	bb := llb.Image(imagerefs.BusyboxDefault)

	return cur.Run(
		llb.Args([]string{"/bin/sh", "-c",
			"/bin/busybox addgroup -g 2011 app && /bin/busybox adduser -u 2010 -G app -D app",
		}),
		llb.WithCustomName("[phase] Adding app user"),
		llb.AddMount("/bin/busybox", bb, llb.SourcePath("/bin/busybox"), llb.Readonly),
	).State
}

func (m *MetaStack) chownApp(cur llb.State) llb.State {
	return cur.Run(
		llb.Shlex("chown -R app:app /app"),
		llb.WithCustomName("[phase] Fixing application code permissions"),
	).Root()
}

// highlevelBuilder provides high-level build helpers
type highlevelBuilder struct {
	BuildOptions
}

func (h *highlevelBuilder) CacheMount(path string) llb.RunOption {
	return h.CacheMountFrom(path, llb.Scratch())
}

func (h *highlevelBuilder) CacheMountFrom(path string, from llb.State) llb.RunOption {
	return llb.AddMount(path, from,
		llb.AsPersistentCacheDir(h.CacheNS+"-"+path, llb.CacheMountShared),
	)
}

func (h *highlevelBuilder) Access(cur llb.State, path, into string) llb.RunOption {
	return llb.AddMount(into, cur, llb.SourcePath(path), llb.Readonly)
}

func (h *highlevelBuilder) aptInstall(cur llb.State, pkgs ...string) llb.State {
	return cur.Run(
		llb.Shlexf("sh -c 'apt-get update && apt-get install -y %s'", strings.Join(pkgs, " ")),
		h.CacheMount("/var/lib/apt/lists"),
		h.CacheMount("/var/cache/apt/archives"),
		llb.WithCustomName("[phase] Installing OS packages"),
	).State
}

func (h *highlevelBuilder) bundleInstall(cur, mnt llb.State) llb.State {
	// Because bundle install likes to modify the lock file, we copy the Gemfile and Gemfile.lock
	// in rather than using h.Access to mount them in read only.

	origin := time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC)
	// Stage Gemfile* and .ruby-version (matched via wildcard so a missing file
	// is not an error). A Gemfile that does `ruby file: ".ruby-version"` reads
	// the file during bundle install, before copyApp brings in the full context.
	cur = cur.File(
		llb.Copy(mnt, "Gemfile*", "/app/", &llb.CopyInfo{
			CopyDirContentsOnly: true,
			CreateDestPath:      true,
			FollowSymlinks:      true,
			AllowWildcard:       true,
			AllowEmptyWildcard:  true,
			CreatedTime:         &origin,
		})).File(
		llb.Copy(mnt, ".ruby-version*", "/app/", &llb.CopyInfo{
			CopyDirContentsOnly: true,
			CreateDestPath:      true,
			FollowSymlinks:      true,
			AllowWildcard:       true,
			AllowEmptyWildcard:  true,
			CreatedTime:         &origin,
		}))

	return cur.Dir("/app").Run(
		llb.Shlex("bundle install"),
		llb.AddEnv("BUNDLE_SILENCE_ROOT_WARNING", "true"),
		llb.WithCustomName("[phase] Installing Ruby Gem dependencies"),
	).State
}

func (h *highlevelBuilder) bootsnap(cur llb.State, args ...string) llb.State {
	return cur.Dir("/app").Run(
		llb.Shlexf("bundle exec bootsnap precompile %s", strings.Join(args, " ")),
		llb.WithCustomName("[phase] Precompiling Bootsnap cache"),
	).State
}

// appChown specifies ownership for app files (UID 2010, GID 2011)
var appChown = llb.ChownOpt{
	User:  &llb.UserOpt{UID: 2010},
	Group: &llb.UserOpt{UID: 2011},
}

func (h *highlevelBuilder) copyApp(cur, mnt llb.State) llb.State {
	origin := time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC)
	return cur.File(
		llb.Copy(mnt, "/", "/app/", &llb.CopyInfo{
			CopyDirContentsOnly: true,
			CreateDestPath:      true,
			FollowSymlinks:      true,
			AllowWildcard:       true,
			AllowEmptyWildcard:  true,
			CreatedTime:         &origin,
			ChownOpt:            &appChown,
		}),
		llb.WithCustomName("[phase] Copying application code"),
	)
}
