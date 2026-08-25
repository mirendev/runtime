package stackbuild

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"miren.dev/runtime/pkg/imagerefs"
)

// rubyGemEnvVars maps gem names to the environment variables they typically
// pair with. Gem presence is only a heuristic — the app might use a YAML
// config, hard-coded values, or a different env name — so entries default to
// "recommended". Source-code scanning elevates a guess to "required" when
// the named variable is read directly from ENV.
var rubyGemEnvVars = map[string][]rubyEnvVarDef{
	"pg":            {{name: "DATABASE_URL", confidence: "recommended"}},
	"mysql2":        {{name: "DATABASE_URL", confidence: "recommended"}},
	"redis":         {{name: "REDIS_URL", confidence: "recommended"}},
	"sidekiq":       {{name: "REDIS_URL", confidence: "recommended"}},
	"aws-sdk-s3":    {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}, {name: "AWS_REGION", confidence: "recommended"}},
	"aws-sdk-core":  {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}, {name: "AWS_REGION", confidence: "recommended"}},
	"stripe":        {{name: "STRIPE_API_KEY", confidence: "recommended"}},
	"sentry-ruby":   {{name: "SENTRY_DSN", confidence: "recommended"}},
	"sentry-rails":  {{name: "SENTRY_DSN", confidence: "recommended"}},
	"honeybadger":   {{name: "HONEYBADGER_API_KEY", confidence: "recommended"}},
	"rollbar":       {{name: "ROLLBAR_ACCESS_TOKEN", confidence: "recommended"}},
	"bugsnag":       {{name: "BUGSNAG_API_KEY", confidence: "recommended"}},
	"newrelic_rpm":  {{name: "NEW_RELIC_LICENSE_KEY", confidence: "recommended"}},
	"scout_apm":     {{name: "SCOUT_KEY", confidence: "recommended"}},
	"sendgrid":      {{name: "SENDGRID_API_KEY", confidence: "recommended"}},
	"mailgun-ruby":  {{name: "MAILGUN_API_KEY", confidence: "recommended"}},
	"postmark":      {{name: "POSTMARK_API_TOKEN", confidence: "recommended"}},
	"twilio-ruby":   {{name: "TWILIO_ACCOUNT_SID", confidence: "recommended"}, {name: "TWILIO_AUTH_TOKEN", confidence: "recommended"}},
	"pusher":        {{name: "PUSHER_APP_ID", confidence: "recommended"}, {name: "PUSHER_KEY", confidence: "recommended"}, {name: "PUSHER_SECRET", confidence: "recommended"}},
	"elasticsearch": {{name: "ELASTICSEARCH_URL", confidence: "recommended"}},
	"searchkick":    {{name: "ELASTICSEARCH_URL", confidence: "recommended"}},
	"cloudinary":    {{name: "CLOUDINARY_URL", confidence: "recommended"}},
}

type rubyEnvVarDef struct {
	name       string
	confidence string
}

// rubyEnvPatterns are regex patterns to find ENV usage in Ruby source code
var rubyEnvPatterns = []*regexp.Regexp{
	// ENV['VAR'] or ENV["VAR"]
	regexp.MustCompile(`ENV\[['"]([A-Z][A-Z0-9_]+)['"]\]`),
	// ENV.fetch('VAR') or ENV.fetch('VAR', default) or ENV.fetch('VAR') { block }
	regexp.MustCompile(`ENV\.fetch\(['"]([A-Z][A-Z0-9_]+)['"]`),
}

// Patterns to detect optional ENV usage (has a default/fallback)
var optionalEnvPatterns = []*regexp.Regexp{
	// ENV.fetch('VAR') { default } - fetch with block
	regexp.MustCompile(`ENV\.fetch\(['"]([A-Z][A-Z0-9_]+)['"]\)\s*\{`),
	// ENV.fetch('VAR', default) - fetch with second argument
	regexp.MustCompile(`ENV\.fetch\(['"]([A-Z][A-Z0-9_]+)['"],`),
	// ENV['VAR'] || default - bracket access with fallback
	regexp.MustCompile(`ENV\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*\|\|`),
}

// configEnvPatterns are patterns for finding ENV usage in config files (YAML with ERB, etc.)
var configEnvPatterns = []*regexp.Regexp{
	// ERB: <%= ENV['VAR'] %> or <%= ENV["VAR"] %>
	regexp.MustCompile(`<%=\s*ENV\[['"]([A-Z][A-Z0-9_]+)['"]\]\s*%>`),
	// ERB: <%= ENV.fetch('VAR') %> or with default
	regexp.MustCompile(`<%=\s*ENV\.fetch\(['"]([A-Z][A-Z0-9_]+)['"]`),
	// Plain Ruby patterns (also valid in ERB)
	regexp.MustCompile(`ENV\[['"]([A-Z][A-Z0-9_]+)['"]\]`),
	regexp.MustCompile(`ENV\.fetch\(['"]([A-Z][A-Z0-9_]+)['"]`),
}

// configEnvVar holds an env var found in a config file along with its source file
type configEnvVar struct {
	name     string
	file     string
	optional bool
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

	// Detected Ruby version (from .ruby-version or the Gemfile ruby directive)
	rubyVersion string

	// Detected environment variable requirements
	requiredEnvVars []EnvVarRequirement
}

// gemfileRubyDirective matches an inline ruby version in the Gemfile, e.g.
//
//	ruby "3.3.0"
//	ruby '3.3'
//
// The `ruby file: ".ruby-version"` form is intentionally not matched here; it
// points back at .ruby-version, which parseRubyVersion reads first.
var gemfileRubyDirective = regexp.MustCompile(`(?m)^\s*ruby\s+['"]([0-9]+\.[0-9]+(?:\.[0-9]+)?)['"]`)

func (s *RubyStack) BaseDistro() string {
	return "debian"
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

	s.rubyVersion = s.parseRubyVersion()
	if s.rubyVersion != "" {
		s.Event("config", "ruby-version", "Ruby version "+s.rubyVersion+" detected")
	}

	// Detect required environment variables
	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
	}
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
		if os.IsNotExist(err) {
			// Gemfile.lock is optional - proceed without it
			return gemfileContent, nil, nil
		}
		return gemfileContent, nil, err
	}

	s.gemfileLock = gemfileLockContent

	return gemfileContent, gemfileLockContent, nil
}

// parseRubyVersion detects the desired Ruby version, preferring an explicit
// .ruby-version file over an inline `ruby "x.y"` directive in the Gemfile. The
// Gemfile's `ruby file: ".ruby-version"` form resolves through .ruby-version,
// so reading that file first covers both. Returns "" when nothing is found.
func (s *RubyStack) parseRubyVersion() string {
	if content, err := s.readFile(".ruby-version"); err == nil {
		if v := strings.TrimSpace(string(content)); v != "" {
			// .ruby-version may carry a "ruby-" prefix (e.g. "ruby-3.3.0").
			return strings.TrimPrefix(v, "ruby-")
		}
	}

	gemfile, _, err := s.Gemfile()
	if err != nil {
		return ""
	}
	if m := gemfileRubyDirective.FindSubmatch(gemfile); m != nil {
		return string(m[1])
	}

	return ""
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

func (s *RubyStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	version := "3.4"
	if opts.Version != "" {
		version = opts.Version
	} else if s.rubyVersion != "" {
		version = s.rubyVersion
	}
	// Carry the upstream ruby image's environment (PATH, GEM_HOME, LANG,
	// BUNDLE_*, ...) into the final image; our own AddEnv calls below still win.
	base := s.baseImage(ctx, imagerefs.GetRubyImage(version), opts)

	base = s.addAppUser(base)

	h := &highlevelBuilder{opts}

	// nodejs is load-bearing here even with the npm augmentation: some rubygems
	// shell out to `node` directly during install/runtime without a package.json,
	// so it must be present on every Ruby build regardless of augmentation state.
	base = h.aptInstall(base, "build-essential", "libpq-dev", "nodejs", "libyaml-dev", "postgresql-client", "git", "curl", "ssh")

	base = h.applyAugmentations(base, localCtx, s.BaseDistro(), s.Augmentations(), s.SkipJSInstall())

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
			AddEnv("RACK_ENV", "production")

		// Inject user env vars so they're available during asset precompilation
		for k, v := range opts.EnvVars {
			base = base.AddEnv(k, v)
		}

		base = base.Run(
			llb.Shlex(`sh -c 'bundle exec rake -T | grep -q "rake assets:precompile" && bundle exec rake assets:precompile || echo "no assets:precompile"'`),
			llb.AddEnv("SECRET_KEY_BASE_DUMMY", "1"),
			llb.WithCustomName("[phase] Precompiling assets"),
		).State
	}

	base = s.applyOnBuild(base, opts)

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

// RequiredEnvVars returns the detected environment variable requirements
func (s *RubyStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

// detectEnvVars analyzes the app to find required environment variables
func (s *RubyStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	// 1. Rails core vars - SECRET_KEY_BASE is required for all Rails apps in production
	if s.hasRails {
		// RAILS_ENV should be set to production by default
		results = append(results, EnvVarRequirement{
			Name:         "RAILS_ENV",
			Source:       "rails_core",
			Confidence:   "required",
			Reason:       "Rails environment mode",
			DefaultValue: "production",
		})

		results = append(results, EnvVarRequirement{
			Name:        "SECRET_KEY_BASE",
			Source:      "rails_core",
			Confidence:  "required",
			Reason:      "Required by Rails in production",
			CanGenerate: true,
		})

		// RAILS_MASTER_KEY decrypts the Rails encrypted credentials. Each
		// credentials file has its own key file: config/credentials.yml.enc
		// pairs with config/master.key, while environment-scoped files like
		// config/credentials/production.yml.enc pair with the matching
		// config/credentials/production.key. Prefer the env-scoped one when
		// it's present.
		var keyFile string
		switch {
		case s.hasFile("config/credentials/production.yml.enc"):
			keyFile = "config/credentials/production.key"
		case s.hasFile("config/credentials.yml.enc"):
			keyFile = "config/master.key"
		}
		if keyFile != "" {
			results = append(results, EnvVarRequirement{
				Name:         "RAILS_MASTER_KEY",
				Source:       "rails_core",
				Confidence:   "required",
				Reason:       "Required to decrypt Rails encrypted credentials",
				ReadFromFile: keyFile,
			})
		}
	}

	// 2. Gem-based inference
	gemfile, gemfileLock, _ := s.Gemfile()
	gemVars := s.detectGemEnvVars(gemfile, gemfileLock)
	results = append(results, gemVars...)

	// 3. Source code scan. Direct, non-default code references are hard
	// requirements; default to "required" rather than the weaker
	// "recommended" used for gem-inferred guesses.
	codeVars := s.scanRubySourceForEnvVars()
	for _, v := range codeVars {
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

	// 4. Config file parsing (.env.sample, .env.example)
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

	// 5. Scan config/ directory for ENV references in YAML and other config files
	configVars := s.scanConfigDirectory()
	for _, v := range configVars {
		if !hasEnvVar(results, v.name) {
			confidence := "recommended"
			reason := "Referenced in " + v.file
			if v.optional {
				confidence = "optional"
				reason = "Referenced in " + v.file + " (has default)"
			}
			results = append(results, EnvVarRequirement{
				Name:       v.name,
				Source:     "config",
				Confidence: confidence,
				Reason:     reason,
			})
		}
	}

	return results
}

// detectGemEnvVars analyzes Gemfile and Gemfile.lock to infer required env vars from gems
func (s *RubyStack) detectGemEnvVars(gemfile, gemfileLock []byte) []EnvVarRequirement {
	var results []EnvVarRequirement
	seen := make(map[string]bool)

	// Combine gemfile and lock for searching
	content := string(gemfile) + "\n" + string(gemfileLock)

	for gem, vars := range rubyGemEnvVars {
		// Check if gem is present in Gemfile or Gemfile.lock
		if strings.Contains(content, gem) {
			for _, v := range vars {
				if !seen[v.name] {
					seen[v.name] = true
					results = append(results, EnvVarRequirement{
						Name:       v.name,
						Source:     "gem",
						Confidence: v.confidence,
						Reason:     gem + " gem detected in Gemfile",
					})
				}
			}
		}
	}

	return results
}

// scanRubySourceForEnvVars walks .rb files in the directory and extracts ENV usage
func (s *RubyStack) scanRubySourceForEnvVars() []detectedEnvVar {
	var found []detectedEnvVar
	seen := make(map[string]bool)

	_ = filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip common non-source directories
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == "node_modules" || base == ".git" || base == "tmp" || base == "log" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only scan Ruby files
		if !strings.HasSuffix(path, ".rb") {
			return nil
		}

		vars := scanRubyFileForEnvVars(path)
		for _, v := range vars {
			if !seen[v.name] && !ignoredEnvVars[v.name] {
				seen[v.name] = true
				found = append(found, v)
			}
		}

		return nil
	})

	return found
}

// scanRubyFileForEnvVars extracts ENV variable names from a single Ruby file
func scanRubyFileForEnvVars(path string) []detectedEnvVar {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var found []detectedEnvVar
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pattern := range rubyEnvPatterns {
			matches := pattern.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) > 1 {
					varName := match[1]
					if !seen[varName] {
						seen[varName] = true
						optional := isOptionalEnvUsage(line, varName)
						found = append(found, detectedEnvVar{
							name:     varName,
							optional: optional,
						})
					}
				}
			}
		}
	}

	return found
}

// isOptionalEnvUsage checks if the line indicates the ENV var has a default/fallback
func isOptionalEnvUsage(line, varName string) bool {
	for _, pattern := range optionalEnvPatterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 1 && match[1] == varName {
			return true
		}
	}
	return false
}

// scanConfigDirectory scans all files in the config/ directory for ENV references
func (s *RubyStack) scanConfigDirectory() []configEnvVar {
	var found []configEnvVar
	seen := make(map[string]bool)

	configDir := filepath.Join(s.dir, "config")

	// Check if config directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return nil
	}

	_ = filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Scan files that might contain ENV references
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".yml", ".yaml", ".erb", ".rb":
			// These file types might contain ENV references
		default:
			return nil
		}

		vars := scanConfigFileForEnvVars(path)
		relPath, _ := filepath.Rel(s.dir, path)
		if relPath == "" {
			relPath = filepath.Base(path)
		}

		for _, v := range vars {
			if !seen[v.name] && !ignoredEnvVars[v.name] {
				seen[v.name] = true
				found = append(found, configEnvVar{
					name:     v.name,
					file:     relPath,
					optional: v.optional,
				})
			}
		}

		return nil
	})

	return found
}

// scanConfigFileForEnvVars extracts ENV variable names from a config file
func scanConfigFileForEnvVars(path string) []detectedEnvVar {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var found []detectedEnvVar
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pattern := range configEnvPatterns {
			matches := pattern.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) > 1 {
					varName := match[1]
					if !seen[varName] {
						seen[varName] = true
						optional := isOptionalEnvUsage(line, varName)
						found = append(found, detectedEnvVar{
							name:     varName,
							optional: optional,
						})
					}
				}
			}
		}
	}

	return found
}
