package stackbuild

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"github.com/moby/buildkit/util/system"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// BuildOptions contains configuration for stack builds
type BuildOptions struct {
	Log *slog.Logger

	// Version specifies the language/runtime version to use
	// If empty, defaults to latest stable version
	Version string

	// CacheNS specifies the namespace for persistent cache mounts
	CacheNS string

	// The alpine image to use for the base image.
	AlpineImage string

	OnBuild []string
}

// Stack represents a programming language/framework stack
type Stack interface {
	Name() string
	// Detect returns true if the given directory contains code for this stack
	Detect() bool
	// GenerateLLB creates the BuildKit LLB for building this stack
	GenerateLLB(dir string, opts BuildOptions) (*llb.State, error)

	Image() ocispecs.Image
}

// DetectStack identifies the programming stack in the given directory
func DetectStack(dir string) (Stack, error) {
	ms := MetaStack{dir: dir}
	ms.setupResult()

	stacks := []Stack{
		&RubyStack{MetaStack: ms},
		&PythonStack{MetaStack: ms},
		&NodeStack{MetaStack: ms},
		&BunStack{MetaStack: ms},
		&GoStack{MetaStack: ms},
	}
	for _, stack := range stacks {
		if stack.Detect() {
			return stack, nil
		}
	}

	return nil, fmt.Errorf("no supported stack detected in %s", dir)
}

type MetaStack struct {
	dir    string
	result ocispecs.Image
}

func (s *MetaStack) setupResult() {
	pl := platforms.Normalize(platforms.DefaultSpec())
	s.result.Architecture = pl.Architecture
	s.result.OS = pl.OS
	s.result.OSVersion = pl.OSVersion
	s.result.OSFeatures = pl.OSFeatures
	s.result.Variant = pl.Variant
	s.result.RootFS.Type = "layers"
	s.result.Config.WorkingDir = "/"
	s.result.Config.Env = []string{"PATH=" + system.DefaultPathEnv(pl.OS)}
}

func (s *MetaStack) Image() ocispecs.Image {
	return s.result
}

func (s *MetaStack) AddEnv(key, value string) {
	s.result.Config.Env = append(s.result.Config.Env, fmt.Sprintf("%s=%s", key, value))
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

func (s *MetaStack) readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, path))
}

func (s *MetaStack) applyOnBuild(cur llb.State, opts BuildOptions) llb.State {
	for _, sh := range opts.OnBuild {
		cur = cur.Dir("/app").Run(llb.Shlex(sh)).Root()
	}

	return cur
}

// RubyStack implements Stack for Ruby on Rails
type RubyStack struct {
	MetaStack
	gemfile     []byte
	gemfileLock []byte
}

func (s *RubyStack) Name() string {
	return "ruby"
}

func (s *RubyStack) Detect() bool {
	return s.hasFile("Gemfile") && s.detectGem("rails")
}

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
	).State
}

func (h *highlevelBuilder) apkAdd(cur llb.State, pkgs ...string) llb.State {
	return cur.Run(
		llb.Shlexf("apk add --no-cache %s", strings.Join(pkgs, " ")),
		h.CacheMount("/var/cache/apk"),
	).State
}

func (h *highlevelBuilder) bundleInstall(cur, mnt llb.State) llb.State {
	// Because bundle install likes to modify the lock file, we copy the Gemfile and Gemfile.lock
	// in rather than using h.Access to mount them in read only.

	origin := time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC)
	cur = cur.File(
		llb.Copy(mnt, "Gemfile*", "/app/", &llb.CopyInfo{
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
		//h.Access(mnt, "Gemfile", "/app/Gemfile"),
		//h.Access(mnt, "Gemfile.lock", "/app/Gemfile.lock"),
		//llb.AddMount("/app/Gemfile", mnt, llb.SourcePath("/app/Gemfile"), llb.Readonly),
		//llb.AddMount("/app/Gemfile.lock", mnt, llb.SourcePath("/app/Gemfile.lock"), llb.Readonly),
	).State
}

func (h *highlevelBuilder) bootsnap(cur llb.State, args ...string) llb.State {
	return cur.Dir("/app").Run(
		llb.Shlexf("bundle exec bootsnap precompile %s", strings.Join(args, " ")),
	).State
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
		}))
}

func (h *highlevelBuilder) withConfig(state llb.State, img ocispecs.Image) (llb.State, error) {
	configbytes, err := json.Marshal(img)
	if err != nil {
		return llb.State{}, err
	}

	return state.WithImageConfig(configbytes)
}

func (s *RubyStack) Gemfile() ([]byte, []byte, error) {
	if s.gemfile != nil {
		return s.gemfile, s.gemfileLock, nil
	}

	gemfilePath := "Gemfile"
	gemfileContent, err := os.ReadFile(filepath.Join(s.dir, gemfilePath))
	if err != nil {
		return nil, nil, err
	}

	s.gemfile = gemfileContent

	gemfileLockPath := "Gemfile.lock"
	gemfileLockContent, err := os.ReadFile(filepath.Join(s.dir, gemfileLockPath))
	if err != nil {
		return gemfileContent, nil, err
	}

	s.gemfileLock = gemfileLockContent

	return gemfileContent, gemfileLockContent, nil
}

func (s *RubyStack) detectGem(gem string) bool {
	data, lock, err := s.Gemfile()
	if err != nil {
		return false
	}

	if strings.Contains(string(lock), gem) {
		return true
	}

	return strings.Contains(string(data), gem)
}

func (s *RubyStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	s.dir = dir

	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	mr := imagemetaresolver.Default()

	version := "3.2"
	if opts.Version != "" {
		version = opts.Version
	}
	base := llb.Image(fmt.Sprintf("ruby:%s-slim", version), llb.WithMetaResolver(mr))

	h := &highlevelBuilder{opts}

	// My kingdom for a pipe operator.
	base = h.aptInstall(base, "build-essential", "libpq-dev", "nodejs", "libyaml-dev")

	base = base.
		AddEnv("SECRET_KEY_BASE_DUMMY", "1").
		AddEnv("BUNDLE_PATH", "/usr/local/bundle").
		AddEnv("BUNDLE_WITHOUT", "development")

	base = h.bundleInstall(base, localCtx)
	base = h.copyApp(base, localCtx)

	if s.detectGem("bootsnap") {
		base = h.bootsnap(base, "--gemfile")
		base = h.bootsnap(base, "app/", "lib/")
	}

	/*
			// Install system dependencies
			deps := base.Run(
				llb.Shlex("sh -c 'apt-get update && apt-get install -y build-essential libpq-dev nodejs libyaml-dev'"),
				llb.AddMount("/var/lib/apt/lists", llb.Scratch(), llb.AsPersistentCacheDir("apt", llb.CacheMountShared)),
				llb.AddMount("/var/cache/apt/archives", llb.Scratch(), llb.AsPersistentCacheDir("apt-archives", llb.CacheMountShared)),
			)

			gemInstall := deps.Dir("/app").Run(
				llb.Shlex("sh -c 'bundle install && bundle exec bootsnap precompile --gemfile'"),
				llb.AddEnv("BUNDLE_PATH", "/usr/local/bundle"),
				llb.AddEnv("BUNDLE_DEPLOYMENT", "1"),
				llb.AddEnv("BUNDLE_WITHOUT", "development"),
				llb.AddMount("/app/Gemfile", localCtx, llb.SourcePath("/app/Gemfile"), llb.Readonly),
				llb.AddMount("/app/Gemfile.lock", localCtx, llb.SourcePath("/app/Gemfile.lock"), llb.Readonly),
			)

			origin := time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC)

			// Copy the rest of the application code
			appState := gemInstall.File(
				llb.Copy(llb.Local("context"), "*", "/app/", &llb.CopyInfo{
					CopyDirContentsOnly: true,
					AllowWildcard:       true,
					CreatedTime:         &origin,
				}))

		prep := base.Dir("/app").Run(
			llb.Shlex("bundle exec bootsnap precompile app/ lib/"),
			llb.AddEnv("BUNDLE_PATH", "/usr/local/bundle"),
			llb.AddEnv("BUNDLE_WITHOUT", "development"),
		)
	*/

	if s.hasFile("Rakefile") {
		base = base.Dir("/app").Run(
			llb.Shlex(`sh -c 'bundle exec rake -T | grep -q "rake assets:precompile" && bundle exec rake assets:precompile || echo "no assets:precompile"'`),
			llb.AddEnv("SECRET_KEY_BASE_DUMMY", "1"),
		).State
	}

	base = s.applyOnBuild(base, opts)

	var ep string

	switch {
	case s.detectGem("rails"):
		ep = "bundle exec rails server -b 0.0.0.0 -p $PORT"
	case s.detectGem("puma"):
		if s.hasFile("config/puma.rb") {
			ep = "bundle exec puma -C config/puma.rb"
		} else {
			// Not great startup options but the user needs to use a puma.rb to have
			// better control anyway.
			ep = "bundle exec puma -b tcp://0.0.0.0 -p $PORT"
		}
	case s.hasFile("config.ru"):
		ep = "bundle exec rackup -p $PORT"
	}

	s.AddEnv("BUNDLE_PATH", "/usr/local/bundle")
	s.AddEnv("BUNDLE_WITHOUT", "development")
	s.AddEnv("RACK_ENV", "production")

	if s.detectGem("rails") {
		s.AddEnv("RAILS_ENV", "production")
	}

	s.SetCwd("/app")

	if ep != "" {
		s.SetEntrypoint([]string{"/bin/sh", "-c", "exec " + ep})
	}

	return &base, nil
}

// PythonStack implements Stack for Python
type PythonStack struct {
	MetaStack
}

func (s *PythonStack) Name() string {
	return "python"
}

func (s *PythonStack) Detect() bool {
	return s.hasFile("requirements.txt") || s.hasFile("Pipfile") || s.hasFile("pyproject.toml")
}

func (s *PythonStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	version := "3.11"
	if opts.Version != "" {
		version = opts.Version
	}
	base := llb.Image(fmt.Sprintf("python:%s-slim", version))

	// Create pip cache mount
	pipCache := llb.Scratch().File(
		llb.Mkdir("/pip-cache", 0755, llb.WithParents(true)),
	)

	var state llb.State
	state = base

	// Handle different dependency management systems
	if s.hasFile("requirements.txt") {
		// Copy only requirements.txt first
		pipState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"requirements.txt"},
		}))

		// Install dependencies with cache
		state = pipState.Dir("/app").Run(
			llb.Shlex("pip install -r requirements.txt"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
		).Root()
	} else if s.hasFile("Pipfile") {
		// Copy only Pipfile and Pipfile.lock first
		pipState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"Pipfile", "Pipfile.lock"},
		}))

		// Install pipenv and dependencies with cache
		state = pipState.Dir("/app").Run(
			llb.Shlex("pip install pipenv && pipenv install --system"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
		).Root()
	} else if s.hasFile("pyproject.toml") {
		// Copy only pyproject.toml and poetry.lock first
		poetryState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"pyproject.toml", "poetry.lock", "README.md"},
		}))

		// Poetry cache mount
		poetryCache := llb.Scratch().File(
			llb.Mkdir("/poetry", 0755, llb.WithParents(true)),
		)

		// Install poetry and dependencies with cache
		state = poetryState.Dir("/app").Run(
			llb.Shlex("sh -c 'pip install poetry && poetry install --no-root'"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
			llb.AddMount("/root/.cache/pypoetry", poetryCache, llb.AsPersistentCacheDir("poetry", llb.CacheMountShared)),
		).Root()
	}

	h := &highlevelBuilder{opts}

	// Copy the rest of the application code
	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	return &state, nil
}

// NodeStack implements Stack for Node.js
type NodeStack struct {
	MetaStack
}

func (s *NodeStack) Name() string {
	return "node"
}

func (s *NodeStack) Detect() bool {
	return s.hasFile("package.json") && !s.hasFile("bun.lockb")
}

func (s *NodeStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	version := "20"
	if opts.Version != "" {
		version = opts.Version
	}
	base := llb.Image(fmt.Sprintf("node:%s-slim", version))

	h := &highlevelBuilder{opts}

	// Copy package files first for better caching
	pkgFiles := []string{"package.json", "package-lock.json", "yarn.lock"}
	depState := base.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
		IncludePatterns: pkgFiles,
	}))

	// Use yarn if yarn.lock exists, otherwise npm
	var state llb.State
	if s.hasFile("yarn.lock") {
		yarnCache := llb.Scratch().File(
			llb.Mkdir("/yarn-cache", 0755, llb.WithParents(true)),
		)

		state = depState.Dir("/app").Run(
			llb.Shlex("yarn install"),
			llb.AddMount("/usr/local/share/.cache/yarn", yarnCache, llb.AsPersistentCacheDir("yarn", llb.CacheMountShared)),
		).Root()
	} else {
		// Create cache mounts
		npmCache := llb.Scratch().File(
			llb.Mkdir("/npm-cache", 0755, llb.WithParents(true)),
		)

		state = depState.Dir("/app").Run(
			llb.Shlex("npm install"),
			llb.AddMount("/root/.npm", npmCache, llb.AsPersistentCacheDir("npm", llb.CacheMountShared)),
		).Root()
	}

	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	return &state, nil
}

// BunStack implements Stack for Bun
type BunStack struct {
	MetaStack
}

func (s *BunStack) Name() string {
	return "bun"
}

func (s *BunStack) Detect() bool {
	return s.hasFile("package.json") && s.hasFile("bun.lockb")
}

func (s *BunStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	version := "1"
	if opts.Version != "" {
		version = opts.Version
	}
	base := llb.Image(fmt.Sprintf("oven/bun:%s", version))

	// Copy package files first for better caching
	pkgFiles := []string{"package.json", "bun.lockb"}
	depState := base.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
		IncludePatterns: pkgFiles,
	}))

	// Create bun cache mount
	bunCache := llb.Scratch().File(
		llb.Mkdir("/bun-cache", 0755, llb.WithParents(true)),
	)

	// Install dependencies with cache
	state := depState.Dir("/app").Run(
		llb.Shlex("bun install"),
		llb.AddMount("/root/.bun", bunCache, llb.AsPersistentCacheDir("bun", llb.CacheMountShared)),
	).Root()

	h := &highlevelBuilder{opts}

	// Copy the rest of the application code
	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	return &state, nil
}

// GoStack implements Stack for Go
type GoStack struct {
	MetaStack
}

func (s *GoStack) Name() string {
	return "go"
}

func (s *GoStack) Detect() bool {
	return s.hasFile("go.mod")
}

const (
	GoDefault     = "1.23"
	AlpineDefault = "alpine:3.21"
)

func (s *GoStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	mr := imagemetaresolver.Default()
	version := "1.23"
	if opts.Version != "" {
		version = opts.Version
	}
	base := llb.Image(fmt.Sprintf("golang:%s-alpine", version), llb.WithMetaResolver(mr))

	// At some later time, we should convert this to use persistent cache mounts
	// but ONLY when we can actually make them persistent. For now, cache
	// within the layers.

	h := &highlevelBuilder{opts}

	// Install git for private dependencies
	state := h.apkAdd(base, "git", "ca-certificates")

	// Copy the rest of the application code
	appState := h.copyApp(state, localCtx)

	// Build with cache
	state = appState.Dir("/app").Run(
		llb.Shlex("sh -c 'go mod download -json && go build -o /bin/app'"),

		// This basically is just a scratch mount until we add the ability to
		// properly export and import the cache dirs.
		h.CacheMount("/root/.cache/go-build"),
	).Root()

	if opts.AlpineImage == "" {
		opts.AlpineImage = AlpineDefault
	}

	clean := llb.Image(opts.AlpineImage, llb.WithMetaResolver(mr))
	c1 := clean.File(llb.Copy(state, "/bin/app", "/bin/app"))

	c1 = s.applyOnBuild(c1, opts)

	return &c1, nil
}
