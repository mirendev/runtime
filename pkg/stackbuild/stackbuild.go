package stackbuild

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"github.com/moby/buildkit/util/system"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pelletier/go-toml/v2"
	"miren.dev/runtime/pkg/imagerefs"
)

// BuildOptions contains configuration for stack builds
type BuildOptions struct {
	Log *slog.Logger

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
	GenerateLLB(dir string, opts BuildOptions) (*llb.State, error)

	Image() ocispecs.Image

	Entrypoint() string

	// WebCommand returns the default command for the web service in a Procfile
	WebCommand() string

	// Events returns detection events collected during Detect() and Init()
	Events() []DetectionEvent
}

// DetectStack identifies the programming stack in the given directory
func DetectStack(dir string, opts BuildOptions) (Stack, error) {
	ms := MetaStack{dir: dir}
	ms.setupResult()

	stacks := []Stack{
		&RubyStack{MetaStack: ms},
		&PythonStack{MetaStack: ms},
		&NodeStack{MetaStack: ms},
		&BunStack{MetaStack: ms},
		&GoStack{MetaStack: ms},
		&RustStack{MetaStack: ms},
	}
	for _, stack := range stacks {
		if stack.Detect() {
			stack.Init(opts)
			return stack, nil
		}
	}

	return nil, fmt.Errorf("no supported stack detected in %s", dir)
}

type MetaStack struct {
	dir    string
	result ocispecs.Image
	events []DetectionEvent
}

func (s *MetaStack) Init(opts BuildOptions) {
	// Base implementation does nothing; stacks can override for specific initialization
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
	for _, sh := range opts.OnBuild {
		cur = cur.Dir("/app").Run(
			llb.Args([]string{"/bin/sh", "-c", sh}),
			llb.WithCustomName("[phase] Application onbuild: "+sh),
		).Root()
	}

	return cur
}

// RubyStack implements Stack for Ruby on Rails
type RubyStack struct {
	MetaStack
	gemfile     []byte
	gemfileLock []byte

	// Detection state set in Init()
	hasRails      bool
	hasPuma       bool
	hasUnicorn    bool
	hasBootsnap   bool
	hasConfigRu   bool
	hasPumaConfig bool
	hasRakefile   bool
}

func (s *RubyStack) Name() string {
	return "ruby"
}

func (s *RubyStack) Detect() bool {
	if !s.hasFile("Gemfile") {
		return false
	}
	s.Event("file", "Gemfile", "Found Gemfile")
	return true
}

func (s *RubyStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Detect framework and libraries, store state for later use
	s.hasRails = s.detectGem("rails")
	if s.hasRails {
		s.Event("framework", "rails", "Rails framework detected")
	}

	s.hasPuma = s.detectGem("puma")
	if s.hasPuma {
		s.Event("package", "puma", "Puma web server detected")
	}

	s.hasUnicorn = s.detectGem("unicorn")
	if s.hasUnicorn {
		s.Event("package", "unicorn", "Unicorn web server detected")
	}

	s.hasBootsnap = s.detectGem("bootsnap")
	if s.hasBootsnap {
		s.Event("package", "bootsnap", "Bootsnap detected (will precompile)")
	}

	s.hasConfigRu = s.hasFile("config.ru")
	if s.hasConfigRu {
		s.Event("file", "config.ru", "Rack config file detected")
	}

	s.hasPumaConfig = s.hasFile("config/puma.rb")
	if s.hasPumaConfig {
		s.Event("config", "puma.rb", "Puma configuration file detected")
	}

	s.hasRakefile = s.hasFile("Rakefile")
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
		llb.WithCustomName("[phase] Installing OS packages"),
	).State
}

func (h *highlevelBuilder) apkAdd(cur llb.State, pkgs ...string) llb.State {
	return cur.Run(
		llb.Shlexf("apk add --no-cache %s", strings.Join(pkgs, " ")),
		h.CacheMount("/var/cache/apk"),
		llb.WithCustomName("[phase] Installing OS packages"),
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
		llb.WithCustomName("[phase] Installing Ruby Gem dependencies"),
	).State
}

func (h *highlevelBuilder) bootsnap(cur llb.State, args ...string) llb.State {
	return cur.Dir("/app").Run(
		llb.Shlexf("bundle exec bootsnap precompile %s", strings.Join(args, " ")),
		llb.WithCustomName("[phase] Precompiling Bootsnap cache"),
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
		}),
		llb.WithCustomName("[phase] Copying application code"),
	)
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
	base := llb.Image(imagerefs.GetRubyImage(version), llb.WithMetaResolver(mr))

	base = s.addAppUser(base)

	h := &highlevelBuilder{opts}

	// My kingdom for a pipe operator.
	base = h.aptInstall(base, "build-essential", "libpq-dev", "nodejs", "libyaml-dev", "postgresql-client", "git", "curl", "ssh")

	base = base.
		AddEnv("SECRET_KEY_BASE_DUMMY", "1").
		AddEnv("BUNDLE_PATH", "/usr/local/bundle").
		AddEnv("BUNDLE_WITHOUT", "development")

	base = h.bundleInstall(base, localCtx)
	base = h.copyApp(base, localCtx)

	if s.hasBootsnap {
		base = h.bootsnap(base, "--gemfile")
		base = h.bootsnap(base, "app/", "lib/")
	}

	if s.hasRakefile {
		base = base.Dir("/app").
			AddEnv("RAILS_ENV", "production").
			AddEnv("RACK_ENV", "production").
			Run(
				llb.Shlex(`sh -c 'bundle exec rake -T | grep -q "rake assets:precompile" && bundle exec rake assets:precompile || echo "no assets:precompile"'`),
				llb.AddEnv("SECRET_KEY_BASE_DUMMY", "1"),
				llb.WithCustomName("[phase] Precompiling assets"),
			).State
	}

	base = s.applyOnBuild(base, opts)

	base = s.chownApp(base)

	s.AddEnv("BUNDLE_PATH", "/usr/local/bundle")
	s.AddEnv("BUNDLE_WITHOUT", "development")
	s.AddEnv("RACK_ENV", "production")

	if s.hasRails {
		s.AddEnv("RAILS_ENV", "production")
	}

	return &base, nil
}

func (s *RubyStack) Entrypoint() string {
	return "bundle exec"
}

func (s *RubyStack) WebCommand() string {
	switch {
	case s.hasRails:
		return "rails server -b 0.0.0.0 -p $PORT"
	case s.hasPuma:
		if s.hasPumaConfig {
			return "puma -C config/puma.rb"
		}
		return "puma -b tcp://0.0.0.0 -p $PORT"
	case s.hasUnicorn:
		return "unicorn -p $PORT"
	case s.hasConfigRu:
		// Covers Sinatra and other Rack apps
		return "rackup -p $PORT"
	}
	return ""
}

// pythonPackageManager represents the detected package manager
type pythonPackageManager string

const (
	pythonPkgPip    pythonPackageManager = "pip"
	pythonPkgPipenv pythonPackageManager = "pipenv"
	pythonPkgPoetry pythonPackageManager = "poetry"
	pythonPkgUv     pythonPackageManager = "uv"
)

// PythonStack implements Stack for Python
type PythonStack struct {
	MetaStack

	// Detection state set in Init()
	packageManager    pythonPackageManager
	hasDjango         bool
	hasFlask          bool
	hasFastAPI        bool
	hasGunicorn       bool
	hasUvicorn        bool
	hasManagePy       bool
	wsgiModule        string
	asgiModule        string
	fastapiEntrypoint string // from [tool.fastapi] entrypoint in pyproject.toml

	// Cached uv.lock packages for accurate detection
	uvPackages map[string]bool
}

func (s *PythonStack) Name() string {
	return "python"
}

func (s *PythonStack) Detect() bool {
	if s.hasFile("Pipfile") {
		s.packageManager = pythonPkgPipenv
		s.Event("file", "Pipfile", "Found Pipfile (pipenv)")
		return true
	}

	// Check for uv.lock before pyproject.toml since uv also uses pyproject.toml
	if s.hasFile("uv.lock") {
		s.packageManager = pythonPkgUv
		s.Event("file", "uv.lock", "Found uv.lock (uv)")
		return true
	}

	if s.hasFile("pyproject.toml") {
		s.packageManager = pythonPkgPoetry
		s.Event("file", "pyproject.toml", "Found pyproject.toml (poetry)")
		return true
	}

	if s.hasFile("requirements.txt") {
		s.packageManager = pythonPkgPip
		s.Event("file", "requirements.txt", "Found requirements.txt (pip)")
		return true
	}

	return false
}

func (s *PythonStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Detect frameworks and libraries, store state for later use
	s.hasDjango = s.detectPackage("django")
	if s.hasDjango {
		s.Event("framework", "django", "Django framework detected")
	}

	s.hasFlask = s.detectPackage("flask")
	if s.hasFlask {
		s.Event("framework", "flask", "Flask framework detected")
	}

	s.hasFastAPI = s.detectPackage("fastapi")
	if s.hasFastAPI {
		s.Event("framework", "fastapi", "FastAPI framework detected")
	}

	s.hasGunicorn = s.detectPackage("gunicorn")
	if s.hasGunicorn {
		s.Event("package", "gunicorn", "Gunicorn WSGI server detected")
	}

	s.hasUvicorn = s.detectPackage("uvicorn")
	if s.hasUvicorn {
		s.Event("package", "uvicorn", "Uvicorn ASGI server detected")
	}

	s.hasManagePy = s.hasFile("manage.py")
	if s.hasManagePy {
		s.Event("file", "manage.py", "Django manage.py detected")
	}

	// Pre-compute WSGI/ASGI modules
	s.wsgiModule = s.findWSGIModule()
	s.asgiModule = s.findASGIModule()

	// Check for FastAPI entrypoint in pyproject.toml [tool.fastapi]
	s.fastapiEntrypoint = s.findFastAPIEntrypoint()
	if s.fastapiEntrypoint != "" {
		s.Event("config", "fastapi", "FastAPI entrypoint: "+s.fastapiEntrypoint)
	}
}

func (s *PythonStack) detectPackage(pkg string) bool {
	// Normalize package name for comparison
	pkgLower := strings.ToLower(pkg)

	// Check uv.lock first using parsed TOML for accurate detection
	if uvPkgs := s.parseUvLock(); uvPkgs != nil {
		if uvPkgs[pkgLower] {
			return true
		}
	}

	// Check requirements.txt
	if data, err := s.readFile("requirements.txt"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), pkgLower) {
			return true
		}
	}

	// Check Pipfile and Pipfile.lock
	if data, err := s.readFile("Pipfile"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), pkgLower) {
			return true
		}
	}
	if data, err := s.readFile("Pipfile.lock"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), pkgLower) {
			return true
		}
	}

	// Check pyproject.toml
	if data, err := s.readFile("pyproject.toml"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), pkgLower) {
			return true
		}
	}

	return false
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

	base := llb.Image(imagerefs.GetPythonImage(version))

	base = s.addAppUser(base)

	// Create pip cache mount
	pipCache := llb.Scratch().File(
		llb.Mkdir("/pip-cache", 0777, llb.WithParents(true)),
	)
	userPipCache := llb.Scratch().File(
		llb.Mkdir("/pip-cache", 0777, llb.WithParents(true)),
	)

	var state llb.State
	state = base

	// Handle different dependency management systems
	switch s.packageManager {
	case pythonPkgPip:
		// Copy only requirements.txt first
		pipState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"requirements.txt"},
		}), llb.WithCustomName("copy requirements.txt"))

		// Install dependencies with cache
		state = pipState.Dir("/app").Run(
			llb.Shlex("pip install -r requirements.txt"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing Python dependencies with pip"),
		).Root()

	case pythonPkgPipenv:
		// Copy only Pipfile and Pipfile.lock first
		pipState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"Pipfile", "Pipfile.lock"},
		}), llb.WithCustomName("copy Pipfile"))

		state = pipState.Dir("/app").Run(
			llb.Shlex("pip install pipenv"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing Python pipenv"),
		).Root()

		state = state.File(llb.Mkdir("/home/app/.cache", 0777, llb.WithParents(true)))

		// Install pipenv and dependencies with cache
		state = state.Dir("/app").Run(
			llb.Shlex("pipenv install --deploy"),
			llb.AddMount("/home/app/.cache/pip", userPipCache, llb.AsPersistentCacheDir("user-pip", llb.CacheMountShared)),
			llb.User("app"),
			llb.WithCustomName("[phase] Installing Python dependencies with pipenv"),
		).Root()

	case pythonPkgPoetry:
		// Copy only pyproject.toml and poetry.lock first
		poetryState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"pyproject.toml", "poetry.lock", "README.md"},
		}), llb.WithCustomName("copy pyproject.toml"))

		state = poetryState.Run(
			llb.Shlex("pip install poetry"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing Python poetry"),
		).Root()

		state = state.File(llb.Mkdir("/home/app/.cache", 0777, llb.WithParents(true)))

		// Install poetry and dependencies with cache
		state = state.Dir("/app").Run(
			llb.Shlex("poetry install --no-root"),
			llb.AddMount("/home/app/.cache/pip", userPipCache, llb.AsPersistentCacheDir("user-pip", llb.CacheMountShared)),
			llb.User("app"),
			llb.WithCustomName("[phase] Installing Python dependencies with poetry"),
		).Root()

	case pythonPkgUv:
		// Copy pyproject.toml and uv.lock first
		uvState := state.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
			IncludePatterns: []string{"pyproject.toml", "uv.lock", "README.md"},
		}), llb.WithCustomName("copy pyproject.toml and uv.lock"))

		// Install uv
		state = uvState.Run(
			llb.Shlex("pip install uv"),
			llb.AddMount("/root/.cache/pip", pipCache, llb.AsPersistentCacheDir("pip", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing uv"),
		).Root()

		// Install dependencies with uv sync
		state = s.chownApp(state).Dir("/app").Run(
			llb.Shlex("uv sync --no-dev"),
			llb.AddMount("/home/app/.cache", llb.Scratch().File(
				llb.Mkdir("/uv", 0777, llb.WithParents(true)),
			), llb.AsPersistentCacheDir("user-uv", llb.CacheMountShared)),
			llb.User("app"),
			llb.WithCustomName("[phase] Installing Python dependencies with uv"),
		).Root()
	}

	h := &highlevelBuilder{opts}

	// Copy the rest of the application code
	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	state = s.chownApp(state)

	return &state, nil
}

func (s *PythonStack) Entrypoint() string {
	switch s.packageManager {
	case pythonPkgPoetry:
		return "poetry run"
	case pythonPkgPipenv:
		return "pipenv run"
	case pythonPkgUv:
		return "uv run"
	default:
		return ""
	}
}

func (s *PythonStack) findWSGIModule() string {
	// Look for wsgi.py in subdirectories (Django convention)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			wsgiPath := filepath.Join(entry.Name(), "wsgi.py")
			if s.hasFile(wsgiPath) {
				return entry.Name() + ".wsgi:application"
			}
		}
	}
	// Check for wsgi.py in root
	if s.hasFile("wsgi.py") {
		return "wsgi:app"
	}
	return ""
}

func (s *PythonStack) findASGIModule() string {
	// Look for asgi.py in subdirectories (Django ASGI convention)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			asgiPath := filepath.Join(entry.Name(), "asgi.py")
			if s.hasFile(asgiPath) {
				return entry.Name() + ".asgi:application"
			}
		}
	}
	// Check for asgi.py in root
	if s.hasFile("asgi.py") {
		return "asgi:app"
	}
	return ""
}

// pyprojectToml represents the structure of a pyproject.toml file for FastAPI config
type pyprojectToml struct {
	Tool struct {
		FastAPI struct {
			Entrypoint string `toml:"entrypoint"`
		} `toml:"fastapi"`
	} `toml:"tool"`
}

func (s *PythonStack) findFastAPIEntrypoint() string {
	content, err := s.readFile("pyproject.toml")
	if err != nil {
		return ""
	}

	var pyproject pyprojectToml
	if err := toml.Unmarshal(content, &pyproject); err != nil {
		return ""
	}

	return pyproject.Tool.FastAPI.Entrypoint
}

// uvLock represents the structure of a uv.lock file
type uvLock struct {
	Package []struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"package"`
}

func (s *PythonStack) parseUvLock() map[string]bool {
	if s.uvPackages != nil {
		return s.uvPackages
	}

	content, err := s.readFile("uv.lock")
	if err != nil {
		return nil
	}

	var lock uvLock
	if err := toml.Unmarshal(content, &lock); err != nil {
		return nil
	}

	s.uvPackages = make(map[string]bool)
	for _, pkg := range lock.Package {
		// Normalize package name (replace - with _ for consistent matching)
		name := strings.ToLower(pkg.Name)
		s.uvPackages[name] = true
		// Also store with underscores replaced by hyphens and vice versa
		s.uvPackages[strings.ReplaceAll(name, "-", "_")] = true
		s.uvPackages[strings.ReplaceAll(name, "_", "-")] = true
	}

	return s.uvPackages
}

func (s *PythonStack) WebCommand() string {
	// Check for gunicorn with Django WSGI
	if s.hasGunicorn && !s.hasFastAPI {
		if s.wsgiModule != "" {
			return "gunicorn " + s.wsgiModule + " -b 0.0.0.0:$PORT"
		}
		// Fallback: check common entry points
		if s.hasFile("app.py") {
			return "gunicorn app:app -b 0.0.0.0:$PORT"
		}
		return "gunicorn app:app -b 0.0.0.0:$PORT"
	}

	// FastAPI - use fastapi run command (FastAPI CLI)
	// This takes precedence over uvicorn since fastapi run is the recommended way
	if s.hasFastAPI {
		// Use configured entrypoint from [tool.fastapi] if available
		if s.fastapiEntrypoint != "" {
			return "fastapi run " + s.fastapiEntrypoint + " --host 0.0.0.0 --port $PORT"
		}
		// Fallback: check common entry points
		if s.hasFile("main.py") {
			return "fastapi run main.py --host 0.0.0.0 --port $PORT"
		}
		if s.hasFile("app.py") {
			return "fastapi run app.py --host 0.0.0.0 --port $PORT"
		}
		return "fastapi run main.py --host 0.0.0.0 --port $PORT"
	}

	// Check for uvicorn (ASGI - Starlette, other ASGI apps)
	if s.hasUvicorn {
		if s.asgiModule != "" {
			return "uvicorn " + s.asgiModule + " --host 0.0.0.0 --port $PORT"
		}
		// Fallback: check common entry points
		if s.hasFile("main.py") {
			return "uvicorn main:app --host 0.0.0.0 --port $PORT"
		}
		if s.hasFile("app.py") {
			return "uvicorn app:app --host 0.0.0.0 --port $PORT"
		}
		return "uvicorn main:app --host 0.0.0.0 --port $PORT"
	}

	// Flask without gunicorn (dev server)
	if s.hasFlask {
		return "flask run --host=0.0.0.0 --port=$PORT"
	}

	// Django without gunicorn (dev server - not recommended for production)
	if s.hasDjango && s.hasManagePy {
		return "python manage.py runserver 0.0.0.0:$PORT"
	}

	return ""
}

// nodePackageManager represents the detected package manager
type nodePackageManager string

const (
	nodePkgNpm  nodePackageManager = "npm"
	nodePkgYarn nodePackageManager = "yarn"
)

// NodeStack implements Stack for Node.js
type NodeStack struct {
	MetaStack

	// Detection state set in Init()
	packageManager nodePackageManager
	scripts        map[string]string
	entryPoint     string
}

func (s *NodeStack) Name() string {
	return "node"
}

func (s *NodeStack) Detect() bool {
	if !s.hasFile("package.json") {
		return false
	}
	s.Event("file", "package.json", "Found package.json")

	if s.hasFile("yarn.lock") {
		s.packageManager = nodePkgYarn
		s.Event("file", "yarn.lock", "Found yarn.lock (yarn)")
		return true
	}
	if s.hasFile("package-lock.json") {
		s.packageManager = nodePkgNpm
		s.Event("file", "package-lock.json", "Found package-lock.json (npm)")
		return true
	}
	if s.detectInFile("Procfile", `web:\s+(node|npm|yarn)`) {
		s.packageManager = nodePkgNpm // default to npm
		s.Event("file", "Procfile", "Procfile references node/npm/yarn")
		return true
	}
	return false
}

func (s *NodeStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Store scripts for later use
	s.scripts = s.getPackageScripts()
	if s.scripts != nil {
		if _, ok := s.scripts["start"]; ok {
			s.Event("script", "start", "npm start script detected")
		}
		if _, ok := s.scripts["build"]; ok {
			s.Event("script", "build", "npm build script detected")
		}
	}

	// Check for common entry points and store the first one found
	for _, entry := range []string{"index.ts", "index.js", "server.ts", "server.js", "app.ts", "app.js", "main.ts", "main.js"} {
		if s.hasFile(entry) {
			s.entryPoint = entry
			s.Event("file", entry, "Entry point file detected")
			break
		}
	}
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
	base := llb.Image(imagerefs.GetNodeImage(version))

	base = s.addAppUser(base)

	h := &highlevelBuilder{opts}

	// Copy package files first for better caching
	pkgFiles := []string{"package.json", "package-lock.json", "yarn.lock"}
	depState := base.File(llb.Copy(localCtx, "/", "/app", &llb.CopyInfo{
		IncludePatterns: pkgFiles,
	}), llb.WithCustomName("copy package files"))

	// Use the detected package manager
	var state llb.State
	switch s.packageManager {
	case nodePkgYarn:
		yarnCache := llb.Scratch().File(
			llb.Mkdir("/yarn-cache", 0755, llb.WithParents(true)),
		)

		state = depState.Dir("/app").Run(
			llb.Shlex("yarn install"),
			llb.AddMount("/usr/local/share/.cache/yarn", yarnCache, llb.AsPersistentCacheDir("yarn", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing Node.js dependencies with yarn"),
		).Root()
	default:
		// Create cache mounts
		npmCache := llb.Scratch().File(
			llb.Mkdir("/npm-cache", 0755, llb.WithParents(true)),
		)

		state = depState.Dir("/app").Run(
			llb.Shlex("npm install"),
			llb.AddMount("/root/.npm", npmCache, llb.AsPersistentCacheDir("npm", llb.CacheMountShared)),
			llb.WithCustomName("[phase] Installing Node.js dependencies with npm"),
		).Root()
	}

	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	state = s.chownApp(state)

	return &state, nil
}

func (s *NodeStack) getPackageScripts() map[string]string {
	data, err := s.readFile("package.json")
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

func (s *NodeStack) WebCommand() string {
	// Determine the runner based on detected package manager
	var runner string
	if s.packageManager == nodePkgYarn {
		runner = "yarn"
	} else {
		runner = "npm run"
	}

	// Check for common web server scripts in order of preference
	if s.scripts != nil {
		for _, script := range []string{"start", "serve", "server"} {
			if _, ok := s.scripts[script]; ok {
				return runner + " " + script
			}
		}
	}

	// Fallback: use detected entry point
	if s.entryPoint != "" {
		if strings.HasSuffix(s.entryPoint, ".ts") {
			return "npx tsx " + s.entryPoint
		}
		return "node " + s.entryPoint
	}

	return ""
}

// BunStack implements Stack for Bun
type BunStack struct {
	MetaStack

	// Detection state set in Init()
	scripts    map[string]string
	entryPoint string
}

func (s *BunStack) Name() string {
	return "bun"
}

func (s *BunStack) Detect() bool {
	if !s.hasFile("package.json") {
		return false
	}
	s.Event("file", "package.json", "Found package.json")

	if s.hasFile("bun.lock") {
		s.Event("file", "bun.lock", "Found bun.lock (Bun runtime)")
		return true
	}
	if s.detectInFile("Procfile", `web:\s+bun`) {
		s.Event("file", "Procfile", "Procfile references bun")
		return true
	}
	return false
}

func (s *BunStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Store scripts for later use
	s.scripts = s.getPackageScripts()
	if s.scripts != nil {
		if _, ok := s.scripts["start"]; ok {
			s.Event("script", "start", "bun start script detected")
		}
	}

	// Check for common entry points and store the first one found
	for _, entry := range []string{"index.ts", "index.js", "server.ts", "server.js", "app.ts", "app.js", "main.ts", "main.js"} {
		if s.hasFile(entry) {
			s.entryPoint = entry
			s.Event("file", entry, "Entry point file detected")
			break
		}
	}
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
	base := llb.Image(imagerefs.GetBunImage(version))

	base = s.addAppUser(base)

	// Copy package files first for better caching
	pkgFiles := []string{"package.json", "bun.lock"}
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
		llb.WithCustomName("[phase] Installing Bun dependencies"),
	).Root()

	h := &highlevelBuilder{opts}

	// Copy the rest of the application code
	state = h.copyApp(state, localCtx)

	state = s.applyOnBuild(state, opts)

	state = s.chownApp(state)

	return &state, nil
}

func (s *BunStack) getPackageScripts() map[string]string {
	data, err := s.readFile("package.json")
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

func (s *BunStack) WebCommand() string {
	// Check for common web server scripts in order of preference
	if s.scripts != nil {
		for _, script := range []string{"start", "serve", "server"} {
			if _, ok := s.scripts[script]; ok {
				return "bun run " + script
			}
		}
	}

	// Fallback: use detected entry point
	if s.entryPoint != "" {
		return "bun " + s.entryPoint
	}

	return ""
}

// GoStack implements Stack for Go
type GoStack struct {
	MetaStack

	// Detection state set in Init()
	hasVendor    bool
	hasCmdDir    bool
	cmdDir       string
	goModVersion string
}

func (s *GoStack) Name() string {
	return "go"
}

func (s *GoStack) Detect() bool {
	if !s.hasFile("go.mod") {
		return false
	}
	s.Event("file", "go.mod", "Found go.mod")
	return true
}

func (s *GoStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Store detection state for later use
	s.hasVendor = s.hasDir("vendor")
	if s.hasVendor {
		s.Event("dir", "vendor", "Vendor directory detected (will use -mod=vendor)")
	}

	s.hasCmdDir = s.hasDir("cmd")
	if s.hasCmdDir {
		s.Event("dir", "cmd", "cmd directory detected")
	}

	// Pre-compute the command directory
	s.cmdDir = s.commandDir(opts)
	if s.cmdDir != "" {
		s.Event("dir", s.cmdDir, "Build target directory detected")
	} else {
		s.Event("dir", ".", "No specific command directory detected, using root")
	}

	s.goModVersion = s.parseGoModVersion()
	if s.goModVersion != "" {
		s.Event("config", "go-version", "Go version "+s.goModVersion+" specified in go.mod")
	}
}

func (s *GoStack) commandDir(opts BuildOptions) string {
	if !s.hasCmdDir {
		return ""
	}

	entries, err := os.ReadDir(filepath.Join(s.dir, "cmd"))
	if err != nil {
		return ""
	}

	if len(entries) == 1 {
		return filepath.Join("cmd", entries[0].Name())
	}

	for _, entry := range entries {
		if entry.IsDir() && entry.Name() == opts.Name {
			return filepath.Join("cmd", entry.Name())
		}
	}

	return ""
}

func (s *GoStack) parseGoModVersion() string {
	content, err := s.readFile("go.mod")
	if err != nil {
		return ""
	}

	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

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
	} else if s.goModVersion != "" {
		version = s.goModVersion
	}
	base := llb.Image(imagerefs.GetGolangImage(version), llb.WithMetaResolver(mr))

	// At some later time, we should convert this to use persistent cache mounts
	// but ONLY when we can actually make them persistent. For now, cache
	// within the layers.

	h := &highlevelBuilder{opts}

	// Install git for private dependencies
	state := h.apkAdd(base, "git", "ca-certificates")

	// Copy the rest of the application code
	appState := h.copyApp(state, localCtx)

	// Use the pre-computed cmdDir from Init()
	buildDir := s.cmdDir

	// Build command - skip go mod download if vendor directory exists
	var buildCmd string
	if s.hasVendor {
		buildCmd = fmt.Sprintf("go build -mod=vendor -o /bin/app ./%s", buildDir)
	} else {
		buildCmd = fmt.Sprintf("sh -c 'go mod download -json && go build -o /bin/app ./%s'", buildDir)
	}

	// Build with cache
	state = appState.Dir("/app").Run(
		llb.Shlex(buildCmd),

		// This basically is just a scratch mount until we add the ability to
		// properly export and import the cache dirs.
		h.CacheMount("/root/.cache/go-build"),
		llb.WithCustomName("[phase] Building Go application"),
	).Root()

	if opts.AlpineImage == "" {
		opts.AlpineImage = imagerefs.AlpineDefault
	}

	state = state.AddEnv("APP", "/bin/app")

	state = s.addAppUser(state)
	state = s.applyOnBuild(state, opts)
	state = s.chownApp(state)

	return &state, nil
}

func (s *GoStack) WebCommand() string {
	return "/bin/app"
}

// RustStack implements Stack for Rust
type RustStack struct {
	MetaStack

	// Detection state set in Init()
	packageName string
	edition     string
}

func (s *RustStack) Name() string {
	return "rust"
}

func (s *RustStack) Detect() bool {
	if !s.hasFile("Cargo.toml") {
		return false
	}
	s.Event("file", "Cargo.toml", "Found Cargo.toml")
	return true
}

func (s *RustStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Parse Cargo.toml once and extract all info
	cargo := s.parseCargoToml()
	if cargo != nil {
		s.packageName = cargo.Package.Name
		if s.packageName != "" {
			s.Event("config", "package", "Package name: "+s.packageName)
		}

		s.edition = cargo.Package.Edition
		if s.edition != "" {
			s.Event("config", "edition", "Rust edition "+s.edition)
		}
	}

	// Check for Cargo.lock
	if s.hasFile("Cargo.lock") {
		s.Event("file", "Cargo.lock", "Found Cargo.lock")
	}
}

// cargoToml represents the structure of a Cargo.toml file
type cargoToml struct {
	Package struct {
		Name    string `toml:"name"`
		Edition string `toml:"edition"`
	} `toml:"package"`
}

func (s *RustStack) parseCargoToml() *cargoToml {
	content, err := s.readFile("Cargo.toml")
	if err != nil {
		return nil
	}

	var cargo cargoToml
	if err := toml.Unmarshal(content, &cargo); err != nil {
		return nil
	}
	return &cargo
}

func (s *RustStack) GenerateLLB(dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git", "target"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	version := "1"
	if opts.Version != "" {
		version = opts.Version
	}

	// NOTE: If we don't pass this in with WithMetaResolver, then
	// buildkit doesn't add the info from the image, info like
	// the PATH env var.
	mr := imagemetaresolver.Default()

	base := llb.Image(imagerefs.GetRustImage(version), llb.WithMetaResolver(mr))

	base = s.addAppUser(base)

	h := &highlevelBuilder{opts}

	// Copy the application code
	state := h.copyApp(base, localCtx)

	// Determine the binary name
	binaryName := s.packageName
	if binaryName == "" {
		binaryName = opts.Name
	}
	if binaryName == "" {
		binaryName = "app"
	}

	// Build the application and copy it out of the cache dir.
	state = state.Dir("/app").Run(
		llb.Args([]string{"/bin/sh", "-c",
			fmt.Sprintf("cargo build --release && cp target/release/%s /bin/app", binaryName)}),
		h.CacheMount("/usr/local/cargo/registry"),
		h.CacheMount("/app/target"),
		llb.WithCustomName("[phase] Building Rust application"),
	).Root()

	state = state.AddEnv("APP", "/bin/app")

	state = s.applyOnBuild(state, opts)
	state = s.chownApp(state)

	return &state, nil
}

func (s *RustStack) WebCommand() string {
	return "/bin/app"
}
