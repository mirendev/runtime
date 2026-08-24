package stackbuild

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"miren.dev/runtime/pkg/imagerefs"
)

// goModuleEnvVars maps Go module paths to the environment variables they typically require
var goModuleEnvVars = map[string][]packageEnvVarDef{
	// Database drivers
	"github.com/lib/pq":                   {{name: "DATABASE_URL", confidence: "recommended"}},
	"github.com/jackc/pgx":                {{name: "DATABASE_URL", confidence: "recommended"}},
	"github.com/go-sql-driver/mysql":      {{name: "DATABASE_URL", confidence: "recommended"}},
	"go.mongodb.org/mongo-driver":         {{name: "MONGODB_URI", confidence: "recommended"}},
	"github.com/go-redis/redis":           {{name: "REDIS_URL", confidence: "recommended"}},
	"github.com/redis/go-redis":           {{name: "REDIS_URL", confidence: "recommended"}},
	"github.com/elastic/go-elasticsearch": {{name: "ELASTICSEARCH_URL", confidence: "recommended"}},
	"github.com/olivere/elastic":          {{name: "ELASTICSEARCH_URL", confidence: "recommended"}},

	// Cloud services
	"github.com/aws/aws-sdk-go":    {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},
	"github.com/aws/aws-sdk-go-v2": {{name: "AWS_ACCESS_KEY_ID", confidence: "recommended"}, {name: "AWS_SECRET_ACCESS_KEY", confidence: "recommended"}},

	// Third-party services
	"github.com/getsentry/sentry-go":   {{name: "SENTRY_DSN", confidence: "recommended"}},
	"github.com/stripe/stripe-go":      {{name: "STRIPE_API_KEY", confidence: "recommended"}},
	"github.com/newrelic/go-agent":     {{name: "NEW_RELIC_LICENSE_KEY", confidence: "recommended"}},
	"github.com/sendgrid/sendgrid-go":  {{name: "SENDGRID_API_KEY", confidence: "recommended"}},
	"github.com/twilio/twilio-go":      {{name: "TWILIO_ACCOUNT_SID", confidence: "recommended"}, {name: "TWILIO_AUTH_TOKEN", confidence: "recommended"}},
	"github.com/pusher/pusher-http-go": {{name: "PUSHER_APP_ID", confidence: "recommended"}, {name: "PUSHER_KEY", confidence: "recommended"}, {name: "PUSHER_SECRET", confidence: "recommended"}},
}

// goEnvPatterns are regex patterns to find env var usage in Go source code
var goEnvPatterns = []*regexp.Regexp{
	// os.Getenv("VAR")
	regexp.MustCompile(`os\.Getenv\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
	// os.LookupEnv("VAR")
	regexp.MustCompile(`os\.LookupEnv\(['"]([A-Z][A-Z0-9_]+)['"]\)`),
}

// goOptionalEnvPatterns detect patterns where env var has a fallback.
//
// LookupEnv on its own is *not* optional — apps frequently use it to
// distinguish "unset" from "empty" for a hard requirement, so blanket-
// matching every LookupEnv call would silently downgrade those to
// optional. The patterns below only match when a default value or
// fallback expression is visible on the same line.
var goOptionalEnvPatterns = []*regexp.Regexp{
	// cmp.Or(os.Getenv("VAR"), default) - single-line default expression
	regexp.MustCompile(`cmp\.Or\(\s*os\.Getenv\(['"]([A-Z][A-Z0-9_]+)['"]\)\s*,`),
	// cmp.Or(os.LookupEnv("VAR"), default) — though typically wrapped
	regexp.MustCompile(`cmp\.Or\(\s*os\.LookupEnv\(['"]([A-Z][A-Z0-9_]+)['"]\)\s*,`),
}

// GoStack implements Stack for Go
type GoStack struct {
	MetaStack

	// Detection state set in Init()
	hasVendor    bool
	hasCmdDir    bool
	cmdDir       string
	goModVersion string

	// Cached go.mod content for dependency detection
	goModContent []byte

	// cgoEnabled is the resolved cgo decision: true builds with CGO_ENABLED=1
	// against glibc and ships on a debian-slim runtime; false builds a static
	// binary (CGO_ENABLED=0) bound for the distroless runtime. Set in Init().
	cgoEnabled bool

	// Detected environment variable requirements
	requiredEnvVars []EnvVarRequirement
}

func (s *GoStack) BaseDistro() string {
	// The Go stack builds on golang:<v>-bookworm (glibc/debian), so the
	// apt-based augmentation machinery applies, same as every other stack.
	return "debian"
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

	// Cache go.mod content for later use
	s.goModContent, _ = s.readFile("go.mod")

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

	// cgo is opt-in via the standard CGO_ENABLED build env var (set under [env]
	// in app.toml). Off (the default) builds a static binary bound for the
	// distroless runtime; on links against glibc and ships on debian-slim.
	// Honoring the canonical Go knob keeps cgo off our app.toml schema, and it
	// is a clean seam for the planned auto-detection follow-up, which will ask
	// the toolchain directly and make the env var unnecessary.
	s.cgoEnabled = opts.EnvVars["CGO_ENABLED"] == "1"
	if s.cgoEnabled {
		s.Event("config", "cgo", "Building with cgo enabled (CGO_ENABLED=1)")
	}

	// Detect required environment variables
	s.requiredEnvVars = s.detectEnvVars()
	for _, ev := range s.requiredEnvVars {
		s.Event("env_var", ev.Name, ev.Reason)
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

	if len(entries) == 1 && entries[0].IsDir() {
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

func (s *GoStack) GenerateLLB(ctx context.Context, dir string, opts BuildOptions) (*llb.State, error) {
	// Set up local context with the directory
	localCtx := llb.Local("context",
		llb.SharedKeyHint(dir),
		llb.ExcludePatterns([]string{".git"}),
		llb.FollowPaths([]string{"."}),
		llb.WithCustomName("application code"),
	)

	// New(), not the process-wide Default() singleton, so a long-lived build
	// server resolves a coherent current config for this build's builder image
	// rather than a stale cached revision of the mutable tag (see baseImage).
	mr := imagemetaresolver.New()
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

	// golang:bookworm already ships git and ca-certificates, and provides the C
	// toolchain that makes cgo work, so no extra package install is needed on
	// the builder before fetching private deps or compiling.
	builder := h.applyAugmentations(base, localCtx, s.BaseDistro(), s.Augmentations(), s.SkipJSInstall())

	// Copy the application code (owned by the app user, uid 2010)
	builder = h.copyApp(builder, localCtx)

	// Use the pre-computed cmdDir from Init()
	buildDir := s.cmdDir

	// Build command - skip go mod download if vendor directory exists
	var buildCmd string
	if s.hasVendor {
		buildCmd = fmt.Sprintf("go build -mod=vendor -o /bin/app ./%s", buildDir)
	} else {
		buildCmd = fmt.Sprintf("sh -c 'go mod download -json && go build -o /bin/app ./%s'", buildDir)
	}

	// Set CGO_ENABLED explicitly: bookworm ships gcc, so Go would otherwise
	// default cgo on. Off (the default) keeps the binary static and bound for
	// the distroless runtime; on links against glibc and ships on debian-slim.
	cgoEnabled := "0"
	if s.cgoEnabled {
		cgoEnabled = "1"
	}

	// Build with cache
	builder = builder.Dir("/app").Run(
		llb.Shlex(buildCmd),
		llb.AddEnv("CGO_ENABLED", cgoEnabled),

		// This basically is just a scratch mount until we add the ability to
		// properly export and import the cache dirs.
		h.CacheMount("/root/.cache/go-build"),
		llb.WithCustomName("[phase] Building Go application"),
	).Root()

	// Make the built binary path available to onBuild commands, which run on
	// the builder where the toolchain and full /app tree still exist.
	builder = builder.AddEnv("APP", "/bin/app")
	builder = s.applyOnBuild(builder, opts)

	runtime := s.assembleRuntime(ctx, h, builder, opts)

	return &runtime, nil
}

// assembleRuntime copies the build output onto a minimal runtime base, leaving
// the heavyweight Go toolchain and module cache behind. The base adapts to the
// build:
//
//   - cgo or JS-augmented apps land on debian-slim: cgo needs glibc at runtime,
//     and augmented apps carry a built /app tree (compiled frontend assets,
//     templates) that the binary serves. The full working tree is copied so
//     that behavior is preserved.
//   - everything else lands on distroless/static — the canonical tiny Go
//     container. The compiled binary is joined by the app's non-Go files
//     (READMEs, templates, data dirs) so it can read them at runtime relative
//     to /app; Go source and the module/vendor build inputs are left behind
//     (see goRuntimeExcludePatterns). A JS augmentation or an explicit
//     build.cgo = true routes to the slim base instead.
//
// Both paths run as uid 2010 (the app user) for consistency with every other
// stack: debian-slim creates it with adduser, distroless gets a written
// /etc/passwd since it has no shell to run adduser.
func (s *GoStack) assembleRuntime(ctx context.Context, h *highlevelBuilder, builder llb.State, opts BuildOptions) llb.State {
	if s.cgoEnabled || len(s.Augmentations()) > 0 {
		rt := s.baseImage(ctx, imagerefs.DebianSlim, opts)
		rt = h.aptInstall(rt, "ca-certificates")
		rt = s.addAppUser(rt) // sets result.Config.User = "2010"
		rt = rt.File(llb.Mkdir("/app", 0o755,
			llb.WithParents(true), llb.WithUIDGID(2010, 2011)))
		rt = rt.File(llb.Copy(builder, "/bin/app", "/bin/app", &llb.CopyInfo{}))
		// Carry the built working tree (assets, templates, data files).
		rt = rt.File(llb.Copy(builder, "/app", "/app", &llb.CopyInfo{
			CopyDirContentsOnly: true,
			CreateDestPath:      true,
			FollowSymlinks:      true,
			AllowWildcard:       true,
			AllowEmptyWildcard:  true,
			ChownOpt:            &appChown,
		}))
		return rt
	}

	rt := s.baseImage(ctx, imagerefs.GoRuntimeStatic, opts)

	// distroless ships no shell or coreutils, which breaks two things: the
	// runner launches the app as `/bin/sh -c <command>` (controllers/sandbox),
	// and `miren sandbox exec` runs arbitrary commands like `echo`/`ls` in the
	// container. Drop in a static (musl) busybox and symlink its whole applet
	// set into /bin, giving /bin/sh plus the common coreutils. busybox is
	// static so it runs regardless of the base's libc, and the lot adds about a
	// megabyte.
	busybox := llb.Image(imagerefs.BusyboxDefault)
	rt = rt.File(llb.Copy(busybox, "/bin/busybox", "/bin/busybox", &llb.CopyInfo{}))
	rt = rt.Run(
		llb.Args([]string{"/bin/busybox", "sh", "-c",
			"for a in $(/bin/busybox --list); do /bin/busybox ln -s /bin/busybox /bin/$a; done"}),
		llb.WithCustomName("[phase] Installing busybox shell and coreutils"),
	).Root()

	rt = rt.File(llb.Mkfile("/etc/passwd", 0o644, goRuntimePasswd))
	rt = rt.File(llb.Mkfile("/etc/group", 0o644, goRuntimeGroup))
	rt = rt.File(llb.Mkdir("/app", 0o755,
		llb.WithParents(true), llb.WithUIDGID(2010, 2011)))
	rt = rt.File(llb.Copy(builder, "/bin/app", "/bin/app", &llb.CopyInfo{}))

	// Carry the app's non-Go files (READMEs, templates, data dirs) from the
	// built working tree so a pure-Go app can still read them at runtime. The
	// distroless base would otherwise ship only the binary, silently dropping
	// everything else — a footgun for apps that open files relative to /app.
	// Go source and the module/vendor build inputs are excluded: the compiled
	// binary needs none of them, and copying them would only bloat the image.
	rt = rt.File(llb.Copy(builder, "/app", "/app", &llb.CopyInfo{
		CopyDirContentsOnly: true,
		CreateDestPath:      true,
		FollowSymlinks:      true,
		AllowWildcard:       true,
		AllowEmptyWildcard:  true,
		ChownOpt:            &appChown,
		ExcludePatterns:     goRuntimeExcludePatterns,
	}))

	s.result.Config.User = "2010"
	return rt
}

// goRuntimePasswd/goRuntimeGroup define the app user (uid 2010) on the
// distroless static runtime, which has no shell to run adduser. A passwd entry
// also lets pure-Go os/user lookups resolve the running uid.
var (
	goRuntimePasswd = []byte("root:x:0:0:root:/root:/sbin/nologin\napp:x:2010:2011:app:/app:/sbin/nologin\n")
	goRuntimeGroup  = []byte("root:x:0:\napp:x:2011:\n")
)

// goRuntimeExcludePatterns lists build-only inputs stripped when the pure-Go
// distroless runtime carries the app's data files. The compiled binary needs
// none of these at runtime, so they are left behind on the builder. Patterns
// use .dockerignore semantics, so **/*.go matches Go source at any depth.
var goRuntimeExcludePatterns = []string{"**/*.go", "go.mod", "go.sum", "vendor"}

func (s *GoStack) WebCommand() string {
	return "/bin/app"
}

// RequiredEnvVars returns the detected environment variable requirements
func (s *GoStack) RequiredEnvVars() []EnvVarRequirement {
	return s.requiredEnvVars
}

// detectEnvVars analyzes the app to find required environment variables
func (s *GoStack) detectEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement

	// 1. Scan source code first to know what env vars are actually used
	sourceVars := scanSourceFilesForEnvVars(s.dir, []string{".go"}, goEnvPatterns, goOptionalEnvPatterns)

	// 2. Framework defaults - GO_ENV is a Buffalo/framework-specific convention,
	// not a general Go convention. Surface it as optional so we don't suggest it
	// as a best practice for arbitrary Go apps. Elevate to required if the
	// source code reads it directly without a fallback.
	goEnvConfidence := "optional"
	goEnvReason := "Go environment mode (Buffalo/framework convention)"
	if elevateToRequired("GO_ENV", sourceVars) {
		goEnvConfidence = "required"
		goEnvReason = "Referenced in application code"
	}
	results = append(results, EnvVarRequirement{
		Name:         "GO_ENV",
		Source:       "go_core",
		Confidence:   goEnvConfidence,
		Reason:       goEnvReason,
		DefaultValue: "production",
	})

	// 3. Module-based inference with elevation logic
	moduleVars := s.detectModuleEnvVars()
	for _, mv := range moduleVars {
		confidence := mv.Confidence
		// Elevate to required if source code references this var
		if confidence == "recommended" && elevateToRequired(mv.Name, sourceVars) {
			confidence = "required"
		}
		if !hasEnvVar(results, mv.Name) {
			results = append(results, EnvVarRequirement{
				Name:       mv.Name,
				Source:     mv.Source,
				Confidence: confidence,
				Reason:     mv.Reason,
			})
		}
	}

	// 4. Add remaining source-detected vars not covered by modules.
	// Direct, non-default code references are hard requirements; default
	// to "required" rather than the weaker "recommended" used for
	// module-inferred guesses.
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

// detectModuleEnvVars analyzes go.mod to infer required env vars from dependencies
func (s *GoStack) detectModuleEnvVars() []EnvVarRequirement {
	var results []EnvVarRequirement
	seen := make(map[string]bool)

	if s.goModContent == nil {
		return results
	}

	content := string(s.goModContent)

	// Also check go.sum for more accurate dependency detection
	goSum, _ := s.readFile("go.sum")
	if goSum != nil {
		content += "\n" + string(goSum)
	}

	for modulePath, vars := range goModuleEnvVars {
		// Check if the module path appears in go.mod or go.sum
		if strings.Contains(content, modulePath) {
			for _, v := range vars {
				if !seen[v.name] {
					seen[v.name] = true
					results = append(results, EnvVarRequirement{
						Name:       v.name,
						Source:     "module",
						Confidence: v.confidence,
						Reason:     modulePath + " module detected",
					})
				}
			}
		}
	}

	return results
}
