package stackbuild

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/pelletier/go-toml/v2"
	"miren.dev/runtime/pkg/imagerefs"
)

// rustCrateEnvVars maps Rust crate names to the environment variables they typically require
var rustCrateEnvVars = map[string][]packageEnvVarDef{
	// Database drivers - sqlx is special because it requires DATABASE_URL at compile time
	"sqlx":           {{name: "DATABASE_URL", confidence: "required"}}, // compile-time requirement
	"diesel":         {{name: "DATABASE_URL", confidence: "recommended"}},
	"tokio-postgres": {{name: "DATABASE_URL", confidence: "recommended"}},
	"postgres":       {{name: "DATABASE_URL", confidence: "recommended"}},
	"mongodb":        {{name: "MONGODB_URI", confidence: "recommended"}},
	"redis":          {{name: "REDIS_URL", confidence: "recommended"}},

	// Cloud services - rusoto is the older SDK, aws-sdk-* is the newer one
	"rusoto_core":      {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"rusoto_s3":        {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"aws-sdk-s3":       {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"aws-sdk-dynamodb": {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"aws-config":       {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},

	// Third-party services
	"sentry": {{name: "SENTRY_DSN", confidence: "recommended"}},
}

// rustEnvPatterns are regex patterns to find env var usage in Rust source code
var rustEnvPatterns = []*regexp.Regexp{
	// std::env::var("VAR")
	regexp.MustCompile(`std::env::var\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// env::var("VAR") - with use std::env
	regexp.MustCompile(`env::var\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// std::env::var_os("VAR")
	regexp.MustCompile(`std::env::var_os\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// env::var_os("VAR")
	regexp.MustCompile(`env::var_os\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// env!("VAR") - compile-time macro
	regexp.MustCompile(`env!\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// option_env!("VAR") - optional compile-time macro
	regexp.MustCompile(`option_env!\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
}

// rustOptionalEnvPatterns detect patterns where env var has a fallback
var rustOptionalEnvPatterns = []*regexp.Regexp{
	// env::var("VAR").unwrap_or(...)
	regexp.MustCompile(`env::var\(['"]([A-Z][A-Z0-9_]+)['"]\)\.unwrap_or`),
	// env::var("VAR").unwrap_or_else(...)
	regexp.MustCompile(`env::var\(['"]([A-Z][A-Z0-9_]+)['"]\)\.unwrap_or_else`),
	// env::var("VAR").ok()
	regexp.MustCompile(`env::var\(['"]([A-Z][A-Z0-9_]+)['"]\)\.ok\(`),
	// option_env!("VAR") is always optional
	regexp.MustCompile(`option_env!\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
}

// RustStack implements Stack for Rust
type RustStack struct {
	MetaStack

	// Detection state set in Init()
	packageName string
	edition     string

	// Cached Cargo.toml for dependency detection
	cargoTomlContent []byte

	// Detected environment variable requirements
	requiredEnvVars []EnvVarRequirement
}

func (s *RustStack) BaseDistro() string {
	return "debian"
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

	// Cache Cargo.toml content
	s.cargoTomlContent, _ = s.readFile("Cargo.toml")

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

	// Detect required environment variables
	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
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

func (s *RustStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
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

	// baseImage resolves the image config with a meta resolver, which is what
	// makes buildkit add the base image's own env (PATH, CARGO_HOME, ...) to the
	// build steps, and it also folds that env into the final image config.
	base := s.baseImage(ctx, imagerefs.GetRustImage(version), opts)

	base = s.addAppUser(base)

	h := &highlevelBuilder{opts}

	base = h.applyAugmentations(base, localCtx, s.BaseDistro(), s.Augmentations(), s.SkipJSInstall())

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

	// Cargo converts hyphens to underscores in binary names (e.g. my-app -> my_app)
	normalizedName := strings.ReplaceAll(binaryName, "-", "_")

	// Build the application and copy it out of the cache dir.
	// Try the normalized name first (with underscores), then fall back to the original name.
	var cpCmd string
	if normalizedName != binaryName {
		cpCmd = fmt.Sprintf("cp target/release/%s /bin/app 2>/dev/null || cp target/release/%s /bin/app", normalizedName, binaryName)
	} else {
		cpCmd = fmt.Sprintf("cp target/release/%s /bin/app", binaryName)
	}

	state = state.Dir("/app").Run(
		llb.Args([]string{"/bin/sh", "-c",
			fmt.Sprintf("%s && %s", s.buildCommand(), cpCmd)}),
		h.CacheMount("/usr/local/cargo/registry"),
		h.CacheMount("/app/target"),
		llb.WithCustomName("[phase] Building Rust application"),
	).Root()

	state = state.AddEnv("APP", "/bin/app")

	state = s.applyOnBuild(state, opts)

	return &state, nil
}

// buildCommand force-rebuilds the workspace crate to dodge the buildkit-mtime / cargo-fingerprint staleness bug (MIR-1027).
func (s *RustStack) buildCommand() string {
	if s.packageName != "" {
		return fmt.Sprintf("cargo clean --release -p %s && cargo build --release", s.packageName)
	}
	return "cargo build --release"
}

func (s *RustStack) WebCommand() string {
	return "/bin/app"
}

// RequiredEnvVars returns the detected environment variable requirements
func (s *RustStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

// detectEnvVars analyzes the app to find required environment variables
func (s *RustStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	// 1. Scan source code first to know what env vars are actually used
	sourceVars := scanSourceFilesForEnvVars(s.dir, []string{".rs"}, rustEnvPatterns, rustOptionalEnvPatterns)

	// 2. Framework defaults - RUST_LOG is commonly used for logging.
	// Elevate to required if the source code reads it directly without a
	// fallback, since the seed-then-skip pass below would otherwise pin
	// it at "recommended" even when the app explicitly depends on it.
	rustLogConfidence := "recommended"
	rustLogReason := "Rust logging level (common convention)"
	if elevateToRequired("RUST_LOG", sourceVars) {
		rustLogConfidence = "required"
		rustLogReason = "Referenced in application code"
	}
	results = append(results, EnvVarRequirement{
		Name:         "RUST_LOG",
		Source:       "rust_core",
		Confidence:   rustLogConfidence,
		Reason:       rustLogReason,
		DefaultValue: "info",
	})

	// 3. Crate-based inference with elevation logic
	crateVars := s.detectCrateEnvVars()
	for _, cv := range crateVars {
		confidence := cv.Confidence
		// Elevate to required if source code references this var
		// Note: sqlx is already marked as required since it needs DATABASE_URL at compile time
		if confidence == "recommended" && elevateToRequired(cv.Name, sourceVars) {
			confidence = "required"
		}
		if !hasEnvVar(results, cv.Name) {
			results = append(results, EnvVarRequirement{
				Name:       cv.Name,
				Source:     cv.Source,
				Confidence: confidence,
				Reason:     cv.Reason,
			})
		}
	}

	// 4. Add remaining source-detected vars not covered by crates.
	// Direct, non-default code references are hard requirements; default
	// to "required" rather than the weaker "recommended" used for
	// crate-inferred guesses.
	for _, v := range sourceVars {
		if !hasEnvVar(results, v.name) {
			confidence := "required"
			reason := "Referenced in application code"
			if v.optional {
				confidence = "optional"
				reason = "Referenced in application code (has fallback)"
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

// detectCrateEnvVars analyzes Cargo.toml to infer required env vars from dependencies
func (s *RustStack) detectCrateEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement
	seen := make(map[string]bool)

	if s.cargoTomlContent == nil {
		return results
	}

	content := string(s.cargoTomlContent)

	// Also check Cargo.lock for more accurate dependency detection
	cargoLock, _ := s.readFile("Cargo.lock")
	if cargoLock != nil {
		content += "\n" + string(cargoLock)
	}

	for crate, vars := range rustCrateEnvVars {
		// Check if the crate appears in Cargo.toml or Cargo.lock
		// Look for patterns like: crate = "version" or name = "crate"
		if strings.Contains(content, crate) {
			for _, v := range vars {
				if !seen[v.name] {
					seen[v.name] = true
					results = append(results, EnvVarRequirement{
						Name:       v.name,
						Source:     "crate",
						Confidence: v.confidence,
						Reason:     crate + " crate detected",
					})
				}
			}
		}
	}

	return results
}
