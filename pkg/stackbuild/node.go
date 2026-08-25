package stackbuild

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"miren.dev/runtime/pkg/imagerefs"
)

// nodePackageEnvVars maps npm package names to the environment variables they typically require
var nodePackageEnvVars = map[string][]packageEnvVarDef{
	// Database drivers
	"pg":                     {{name: "DATABASE_URL", confidence: "recommended"}},
	"mysql2":                 {{name: "DATABASE_URL", confidence: "recommended"}},
	"mysql":                  {{name: "DATABASE_URL", confidence: "recommended"}},
	"mongodb":                {{name: "MONGODB_URI", confidence: "recommended"}},
	"mongoose":               {{name: "MONGODB_URI", confidence: "recommended"}},
	"redis":                  {{name: "REDIS_URL", confidence: "recommended"}},
	"ioredis":                {{name: "REDIS_URL", confidence: "recommended"}},
	"@prisma/client":         {{name: "DATABASE_URL", confidence: "recommended"}},
	"@elastic/elasticsearch": {{name: "ELASTICSEARCH_URL", confidence: "recommended"}},

	// Cloud services
	"aws-sdk":                  {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"@aws-sdk/client-s3":       {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"@aws-sdk/client-dynamodb": {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},

	// Third-party services
	"@sentry/node":   {{name: "SENTRY_DSN", confidence: "recommended"}},
	"stripe":         {{name: "STRIPE_SECRET_KEY", confidence: "recommended"}},
	"@sendgrid/mail": {{name: "SENDGRID_API_KEY", confidence: "recommended"}},
	"newrelic":       {{name: "NEW_RELIC_LICENSE_KEY", confidence: "recommended"}},
	"jsonwebtoken":   {{name: "JWT_SECRET", confidence: "recommended"}},
	"twilio":         {{name: "TWILIO_ACCOUNT_SID", confidence: "recommended"}, {name: "TWILIO_AUTH_TOKEN", confidence: "recommended"}},
	"mailgun-js":     {{name: "MAILGUN_API_KEY", confidence: "recommended"}},
	"pusher":         {{name: "PUSHER_APP_ID", confidence: "recommended"}, {name: "PUSHER_KEY", confidence: "recommended"}, {name: "PUSHER_SECRET", confidence: "recommended"}},
	"cloudinary":     {{name: "CLOUDINARY_URL", confidence: "recommended"}},
}

// nodeEnvPatterns are regex patterns to find env var usage in JavaScript/TypeScript source code
var nodeEnvPatterns = []*regexp.Regexp{
	// process.env.VAR
	regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]+)`),
	// process.env['VAR'] or process.env["VAR"]
	regexp.MustCompile(`process\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]`),
}

// nodeOptionalEnvPatterns detect patterns where env var has a default value
var nodeOptionalEnvPatterns = []*regexp.Regexp{
	// process.env.VAR || 'default'
	regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]+)\s*\|\|`),
	// process.env['VAR'] || 'default'
	regexp.MustCompile(`process\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*\|\|`),
	// process.env.VAR ?? 'default' (nullish coalescing)
	regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]+)\s*\?\?`),
	// process.env['VAR'] ?? 'default' (bracket notation, nullish coalescing)
	regexp.MustCompile(`process\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*\?\?`),
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
	hasNext        bool

	// Parsed dependencies from package.json
	dependencies    map[string]string
	devDependencies map[string]string

	// Detected environment variable requirements
	requiredEnvVars []EnvVarRequirement
}

func (s *NodeStack) BaseDistro() string {
	return "debian"
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

	// Parse package.json once for scripts and dependencies
	s.parsePackageJSON()

	if s.scripts != nil {
		if _, ok := s.scripts["start"]; ok {
			s.Event("script", "start", "npm start script detected")
		}
		if _, ok := s.scripts["build"]; ok {
			s.Event("script", "build", "npm build script detected")
		}
	}

	// Detect Next.js so we can run its production build and serve it with
	// `next start`, mirroring how the Ruby stack special-cases Rails.
	if s.hasDependency("next") {
		s.hasNext = true
		s.Event("framework", "nextjs", "Next.js framework detected")
	}

	// Check for common entry points and store the first one found
	for _, entry := range []string{"index.ts", "index.js", "server.ts", "server.js", "app.ts", "app.js", "main.ts", "main.js"} {
		if s.hasFile(entry) {
			s.entryPoint = entry
			s.Event("file", entry, "Entry point file detected")
			break
		}
	}

	// Detect required environment variables
	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
	}
}

func (s *NodeStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
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
	// Carry the upstream node image's environment (PATH, NODE_VERSION, ...)
	// into the final image.
	base := s.baseImage(ctx, imagerefs.GetNodeImage(version), opts)

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
	case nodePkgNpm:
		// npm is also the default when no package manager is detected.
		fallthrough
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

	// Run the framework build (e.g. `next build`) after the app is copied.
	// Inject user env vars so build-time values (e.g. NEXT_PUBLIC_* that Next
	// inlines at build time) are available, mirroring how the Ruby stack
	// injects env vars for asset precompilation.
	if buildCmd := s.frameworkBuildCommand(); buildCmd != "" {
		build := state.Dir("/app")
		for k, v := range opts.EnvVars {
			build = build.AddEnv(k, v)
		}
		state = build.Run(
			llb.Shlex(buildCmd),
			llb.WithCustomName("[phase] Building Next.js application"),
		).Root()

		// The build runs as root, so the generated tree would be root-owned while
		// the image runs as the app user. Next writes its ISR, image, and prerender
		// caches beneath .next at runtime, so hand the tree over before export or
		// those writes fail with EACCES.
		//
		// Guard on the directory existing: an app that sets `distDir` in
		// next.config.js builds somewhere else entirely, and a bare chown would
		// fail the build over a path that was never going to be there. Such an app
		// keeps the ownership it has today rather than regressing to a hard error.
		state = state.Dir("/app").Run(
			llb.Args([]string{"/bin/sh", "-c", "if [ -d /app/.next ]; then chown -R app:app /app/.next; fi"}),
			llb.WithCustomName("[phase] Fixing Next.js build permissions"),
		).Root()
	}

	state = s.applyOnBuild(state, opts)

	return &state, nil
}

// hasDependency reports whether the package.json lists the given package in
// either dependencies or devDependencies.
func (s *NodeStack) hasDependency(name string) bool {
	if _, ok := s.dependencies[name]; ok {
		return true
	}
	_, ok := s.devDependencies[name]
	return ok
}

// frameworkBuildCommand returns the in-image build command for a detected
// framework, or "" when no build step is needed. Next.js must be compiled with
// `next build` before it can be served; a plain Node app is copied as-is.
func (s *NodeStack) frameworkBuildCommand() string {
	if !s.hasNext {
		return ""
	}
	if s.packageManager == nodePkgYarn {
		return "yarn build"
	}
	return "npm run build"
}

func (s *NodeStack) parsePackageJSON() {
	data, err := s.readFile("package.json")
	if err != nil {
		return
	}

	var pkg struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	s.scripts = pkg.Scripts
	s.dependencies = pkg.Dependencies
	s.devDependencies = pkg.DevDependencies
}

func (s *NodeStack) WebCommand() string {
	// Determine the runner based on detected package manager
	var runner string
	if s.packageManager == nodePkgYarn {
		runner = "yarn"
	} else {
		runner = "npm run"
	}

	// An explicit web server script always wins, including for Next.js: it may
	// launch a custom server, do setup work, or pass extra flags that a
	// synthesized `next start` would silently drop. Next reads PORT from the
	// environment, so going through the script still binds the platform port.
	if s.scripts != nil {
		for _, script := range []string{"start", "serve", "server"} {
			if _, ok := s.scripts[script]; ok {
				return runner + " " + script
			}
		}
	}

	// No web server script: a Next.js app is served with `next start`, bound to
	// the platform-provided port. Resolve the local `next` binary through the
	// detected package manager so a yarn project doesn't shell out to npm's npx
	// (matches frameworkBuildCommand).
	if s.hasNext {
		if s.packageManager == nodePkgYarn {
			return "yarn next start -p $PORT"
		}
		return "npx next start -p $PORT"
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

// RequiredEnvVars returns the detected environment variable requirements
func (s *NodeStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

// detectEnvVars analyzes the app to find required environment variables
func (s *NodeStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	// 1. Scan source code first to know what env vars are actually used
	sourceVars := scanSourceFilesForEnvVars(s.dir, []string{".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs"}, nodeEnvPatterns, nodeOptionalEnvPatterns)

	// 2. Framework defaults - NODE_ENV should always be set in production
	results = append(results, EnvVarRequirement{
		Name:         "NODE_ENV",
		Source:       "node_core",
		Confidence:   "required",
		Reason:       "Node.js environment mode",
		DefaultValue: "production",
	})

	// 3. Package-based inference with elevation logic
	packageVars := s.detectPackageEnvVars()
	for _, pv := range packageVars {
		confidence := pv.Confidence
		// Elevate to required if source code references this var
		if confidence == "recommended" && elevateToRequired(pv.Name, sourceVars) {
			confidence = "required"
		}
		if !hasEnvVar(results, pv.Name) {
			results = append(results, EnvVarRequirement{
				Name:       pv.Name,
				Source:     pv.Source,
				Confidence: confidence,
				Reason:     pv.Reason,
			})
		}
	}

	// 4. Add remaining source-detected vars not covered by packages.
	// A direct, non-default code reference is a hard requirement — the app
	// will not boot without it — so default to "required" rather than the
	// weaker "recommended" used for package-inferred guesses.
	for _, v := range sourceVars {
		if !hasEnvVar(results, v.name) {
			confidence := "required"
			reason := "Referenced in application code"
			if v.optional {
				confidence = "optional"
				reason = "Referenced in application code (has default)"
			}
			results = append(results, EnvVarRequirement{
				Name:       v.name,
				Source:     "code",
				Confidence: confidence,
				Reason:     reason,
			})
		}
	}

	// 5. Config file parsing (.env.sample, .env.example)
	for _, filename := range []string{".env.sample", ".env.example"} {
		sampleVars := parseEnvSampleFile(s.dir, filename)
		for _, v := range sampleVars {
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

// detectPackageEnvVars analyzes package.json to infer required env vars from dependencies
func (s *NodeStack) detectPackageEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement
	seen := make(map[string]bool)

	// Check both dependencies and devDependencies
	allDeps := make(map[string]bool)
	for dep := range s.dependencies {
		allDeps[dep] = true
	}
	for dep := range s.devDependencies {
		allDeps[dep] = true
	}

	for pkg, vars := range nodePackageEnvVars {
		// Check for exact match or prefix match (for scoped packages like @aws-sdk/*)
		matched := false
		if allDeps[pkg] {
			matched = true
		} else if strings.Contains(pkg, "*") {
			// Handle wildcard patterns like @aws-sdk/*
			prefix := strings.TrimSuffix(pkg, "*")
			for dep := range allDeps {
				if strings.HasPrefix(dep, prefix) {
					matched = true
					break
				}
			}
		}

		if matched {
			for _, v := range vars {
				if !seen[v.name] {
					seen[v.name] = true
					results = append(results, EnvVarRequirement{
						Name:       v.name,
						Source:     "package",
						Confidence: v.confidence,
						Reason:     pkg + " package detected",
					})
				}
			}
		}
	}

	return results
}
