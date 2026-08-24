package stackbuild

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"miren.dev/runtime/pkg/imagerefs"
)

// bunEnvPatterns extend nodeEnvPatterns with Bun's runtime-specific
// `Bun.env` accessor (in addition to the standard process.env).
var bunEnvPatterns = []*regexp.Regexp{
	// Bun.env.VAR
	regexp.MustCompile(`Bun\.env\.([A-Z][A-Z0-9_]+)`),
	// Bun.env['VAR'] or Bun.env["VAR"]
	regexp.MustCompile(`Bun\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]`),
}

// bunOptionalEnvPatterns mirror nodeOptionalEnvPatterns for Bun.env.
var bunOptionalEnvPatterns = []*regexp.Regexp{
	// Bun.env.VAR || 'default'
	regexp.MustCompile(`Bun\.env\.([A-Z][A-Z0-9_]+)\s*\|\|`),
	// Bun.env['VAR'] || 'default'
	regexp.MustCompile(`Bun\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*\|\|`),
	// Bun.env.VAR ?? 'default' (nullish coalescing)
	regexp.MustCompile(`Bun\.env\.([A-Z][A-Z0-9_]+)\s*\?\?`),
	// Bun.env['VAR'] ?? 'default' (bracket notation, nullish coalescing)
	regexp.MustCompile(`Bun\.env\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*\?\?`),
}

// BunStack implements Stack for Bun
type BunStack struct {
	MetaStack

	// Detection state set in Init()
	scripts    map[string]string
	entryPoint string

	// Parsed dependencies from package.json
	dependencies    map[string]string
	devDependencies map[string]string

	// Detected environment variable requirements
	requiredEnvVars []EnvVarRequirement
}

func (s *BunStack) BaseDistro() string {
	return "debian"
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
	if s.hasFile("bun.lockb") {
		s.Event("file", "bun.lockb", "Found bun.lockb (Bun runtime, legacy)")
		return true
	}
	if s.hasFile("bunfig.toml") {
		s.Event("file", "bunfig.toml", "Found bunfig.toml (Bun runtime)")
		return true
	}
	if s.detectPackageManagerBun() {
		s.Event("config", "packageManager", "package.json packageManager field specifies bun")
		return true
	}
	if s.detectBunInScripts() {
		s.Event("config", "scripts", "package.json scripts reference bun")
		return true
	}
	if s.detectInFile("Procfile", `web:\s+bun`) {
		s.Event("file", "Procfile", "Procfile references bun")
		return true
	}
	return false
}

func (s *BunStack) detectPackageManagerBun() bool {
	data, err := s.readFile("package.json")
	if err != nil {
		return false
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	return strings.HasPrefix(pkg.PackageManager, "bun@")
}

var bunCommandRe = regexp.MustCompile(`(?:^|\s)bunx?(?:\s|$)`)

func (s *BunStack) detectBunInScripts() bool {
	scripts := s.readPackageScripts()
	for _, cmd := range scripts {
		if bunCommandRe.MatchString(cmd) {
			return true
		}
	}
	return false
}

// readPackageScripts reads only the scripts section of package.json.
// Used during Detect() before Init() runs parsePackageJSON.
func (s *BunStack) readPackageScripts() map[string]string {
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

func (s *BunStack) Init(opts BuildOptions) {
	s.SetCwd("/app")

	// Parse package.json for scripts and dependencies
	s.parsePackageJSON()

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

	// Detect required environment variables
	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
	}
}

func (s *BunStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
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
	// Bun's image relocates node onto a PATH its Dockerfile sets, plus other
	// runtime env; baseImage carries that environment into the final image.
	base := s.baseImage(ctx, imagerefs.GetBunImage(version), opts)

	base = s.addAppUser(base)

	// Copy package files first for better caching
	pkgFiles := []string{"package.json", "bun.lock", "bun.lockb"}
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

	return &state, nil
}

func (s *BunStack) parsePackageJSON() {
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

// RequiredEnvVars returns the detected environment variable requirements
func (s *BunStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

// detectEnvVars analyzes the app to find required environment variables
// Bun uses the same patterns as Node.js since it's compatible with the Node ecosystem
func (s *BunStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	// 1. Scan source code first to know what env vars are actually used.
	// Bun apps may use either process.env (Node-compatible) or Bun.env
	// (Bun-specific), so combine both pattern sets.
	envPatterns := append(append([]*regexp.Regexp{}, nodeEnvPatterns...), bunEnvPatterns...)
	optionalPatterns := append(append([]*regexp.Regexp{}, nodeOptionalEnvPatterns...), bunOptionalEnvPatterns...)
	sourceVars := scanSourceFilesForEnvVars(s.dir, []string{".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs"}, envPatterns, optionalPatterns)

	// 2. Framework defaults - NODE_ENV is recognized by Bun
	results = append(results, EnvVarRequirement{
		Name:         "NODE_ENV",
		Source:       "bun_core",
		Confidence:   "required",
		Reason:       "Bun/Node.js environment mode",
		DefaultValue: "production",
	})

	// 3. Package-based inference with elevation logic
	// Reuse the same package map as Node.js since Bun is npm-compatible
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
	// Direct, non-default code references are hard requirements; default
	// to "required" rather than the weaker "recommended" used for
	// package-inferred guesses.
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
func (s *BunStack) detectPackageEnvVars() []EnvVarRequirement {
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

	// Reuse the same package map as Node.js
	for pkg, vars := range nodePackageEnvVars {
		if allDeps[pkg] {
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
