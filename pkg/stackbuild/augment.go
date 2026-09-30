package stackbuild

import (
	"os"
	"path/filepath"

	"github.com/moby/buildkit/client/llb"
	"miren.dev/runtime/pkg/imagerefs"
)

// Augmentation represents a secondary toolchain layered onto the primary stack's
// base image. Augmentations are detected from lockfiles in the app directory
// (e.g. package-lock.json -> npm) and let apps written in one language pull in
// auxiliary build tooling — most commonly JS package managers used to build
// frontend assets for a Rails/Django/Go web app.
type Augmentation string

const (
	AugNpm  Augmentation = "npm"
	AugYarn Augmentation = "yarn"
	AugBun  Augmentation = "bun"
)

// DetectAugmentations returns the set of augmentations needed for the app
// located at dir, given the primary stack that was already selected.
//
// It returns nil (no augmentations) when the primary stack is "node" or "bun",
// since those stacks already ship npm or bun in their base image. The returned
// events describe what was detected so they can be surfaced alongside primary
// stack detection events in the build output.
//
// npm is triggered by the presence of package.json (not just package-lock.json),
// to preserve compatibility with apps — most notably Rails — that ship a
// package.json for asset pipelines without committing a lockfile.
//
// skipInstall is true when the app already ships a node_modules directory — in
// that case the JS tooling (npm/bun) is still installed so onBuild commands
// can use it, but the package install step is skipped because the user's
// vendored node_modules will be brought in by copyApp.
func DetectAugmentations(dir, primaryStack string) (augs []Augmentation, skipInstall bool, events []DetectionEvent) {
	if primaryStack == "node" || primaryStack == "bun" {
		return nil, false, nil
	}

	hasBunLock := hasRegularFile(dir, "bun.lock") || hasRegularFile(dir, "bun.lockb")

	if hasRegularFile(dir, "package.json") {
		switch {
		case hasBunLock:
			// Bun owns the install path below; don't also run npm install on
			// a bun-managed manifest.
		case hasRegularFile(dir, "yarn.lock"):
			augs = append(augs, AugYarn)
			events = append(events, DetectionEvent{
				Kind:    "augmentation",
				Name:    "yarn",
				Message: "Found yarn.lock — installing yarn via corepack",
			})
		case hasRegularFile(dir, "package-lock.json"):
			augs = append(augs, AugNpm)
			events = append(events, DetectionEvent{
				Kind:    "augmentation",
				Name:    "npm",
				Message: "Found package-lock.json — installing npm",
			})
		default:
			augs = append(augs, AugNpm)
			events = append(events, DetectionEvent{
				Kind:    "augmentation",
				Name:    "npm",
				Message: "Found package.json — installing npm",
			})
		}
	}

	if hasBunLock {
		augs = append(augs, AugBun)
		events = append(events, DetectionEvent{
			Kind:    "augmentation",
			Name:    "bun",
			Message: "Found bun lockfile — installing bun",
		})
	}

	if len(augs) > 0 && hasDir(dir, "node_modules") {
		skipInstall = true
		events = append(events, DetectionEvent{
			Kind:    "augmentation",
			Name:    "node_modules",
			Message: "Vendored node_modules detected — skipping JS install",
		})
	}

	return augs, skipInstall, events
}

func hasRegularFile(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && st.Mode().IsRegular()
}

func hasDir(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && st.IsDir()
}

// installNpm layers nodejs+npm onto cur using the package manager appropriate
// for the given base distro.
func (h *highlevelBuilder) installNpm(cur llb.State, distro string) llb.State {
	switch distro {
	case "alpine":
		return cur.Run(
			llb.Shlex("apk add --no-cache nodejs npm"),
			h.CacheMount("/var/cache/apk"),
			llb.WithCustomName("[phase] Installing npm augmentation"),
		).State
	default:
		return cur.Run(
			llb.Shlex("sh -c 'apt-get update && apt-get install -y nodejs npm'"),
			h.lockedCacheMount("/var/lib/apt/lists"),
			h.lockedCacheMount("/var/cache/apt/archives"),
			llb.WithCustomName("[phase] Installing npm augmentation"),
		).State
	}
}

// installBun copies the bun binary from the official bun image. Distro-agnostic
// because bun ships as a single static binary.
func (h *highlevelBuilder) installBun(cur llb.State) llb.State {
	bunImg := llb.Image(imagerefs.GetBunImage("1"))
	return cur.File(
		llb.Copy(bunImg, "/usr/local/bin/bun", "/usr/local/bin/bun", &llb.CopyInfo{
			FollowSymlinks: true,
		}),
		llb.WithCustomName("[phase] Installing bun augmentation"),
	)
}

// applyAugmentations layers all requested augmentations onto cur in order:
// install the tool, and (unless skipInstall is true) run its package install
// against the lockfiles from localCtx so JS deps are available during
// subsequent build phases (asset precompile, onBuild, etc).
//
// When skipInstall is true the user already ships a node_modules directory;
// we install the tool (so onBuild can shell out to npm/bun) but leave the
// vendored node_modules to flow in via copyApp.
func (h *highlevelBuilder) applyAugmentations(cur, localCtx llb.State, distro string, augs []Augmentation, skipInstall bool) llb.State {
	for _, aug := range augs {
		switch aug {
		case AugNpm:
			cur = h.installNpm(cur, distro)
			if !skipInstall {
				cur = h.runNpmInstall(cur, localCtx)
			}
		case AugYarn:
			cur = h.installYarn(cur, distro)
			if !skipInstall {
				cur = h.runYarnInstall(cur, localCtx)
			}
		case AugBun:
			cur = h.installBun(cur)
			if !skipInstall {
				cur = h.runBunInstall(cur, localCtx)
			}
		}
	}
	return cur
}

// installYarn layers nodejs+npm and enables corepack so the yarn shim is on
// PATH. Yarn itself is provisioned lazily by corepack from package.json's
// "packageManager" field (or falls back to classic yarn 1).
func (h *highlevelBuilder) installYarn(cur llb.State, distro string) llb.State {
	cur = h.installNpm(cur, distro)
	return cur.Run(
		llb.Shlex("corepack enable"),
		llb.WithCustomName("[phase] Enabling corepack for yarn"),
	).State
}

// ensureAppDir creates /app owned by the app user so the install can run
// unprivileged.
func (h *highlevelBuilder) ensureAppDir(cur llb.State) llb.State {
	return cur.File(llb.Mkdir("/app", 0755,
		llb.WithParents(true),
		llb.WithUIDGID(2010, 2011),
	))
}

// runNpmInstall copies JS package files into /app and runs npm install as the
// app user.
//
// No persistent cache mount: BuildKit's cache mount root is root-owned
// (it comes from the scratch state's root, not a subdir we create), so an
// unprivileged npm install can't write to it. The pragmatic fix is to skip
// the cache — the per-layer cache still avoids re-running this step when
// package files don't change.
func (h *highlevelBuilder) runNpmInstall(cur, mnt llb.State) llb.State {
	cur = h.ensureAppDir(cur)

	cur = cur.File(llb.Copy(mnt, "/", "/app", &llb.CopyInfo{
		IncludePatterns:    []string{"package.json", "package-lock.json"},
		CreateDestPath:     true,
		AllowWildcard:      true,
		AllowEmptyWildcard: true,
		ChownOpt:           &appChown,
	}), llb.WithCustomName("copy npm package files"))

	return cur.Dir("/app").Run(
		llb.Shlex("npm install"),
		llb.AddEnv("HOME", "/home/app"),
		llb.User("app"),
		llb.WithCustomName("[phase] Installing JS deps with npm augmentation"),
	).State
}

// runYarnInstall copies yarn package files into /app and runs yarn install as
// the app user. COREPACK_ENABLE_DOWNLOAD_PROMPT silences the interactive
// download prompt newer corepack versions emit when fetching a yarn release.
func (h *highlevelBuilder) runYarnInstall(cur, mnt llb.State) llb.State {
	cur = h.ensureAppDir(cur)

	// Yarn Berry projects pin a yarn release via `yarnPath` in .yarnrc.yml,
	// and yarnPath takes precedence over corepack. Include .yarn/releases,
	// plugins, and patches so pinned binaries / plugins / patches are
	// present during install.
	cur = cur.File(llb.Copy(mnt, "/", "/app", &llb.CopyInfo{
		IncludePatterns: []string{
			"package.json", "yarn.lock",
			".yarnrc", ".yarnrc.yml",
			".yarn/releases/**", ".yarn/plugins/**", ".yarn/patches/**",
		},
		CreateDestPath:     true,
		AllowWildcard:      true,
		AllowEmptyWildcard: true,
		ChownOpt:           &appChown,
	}), llb.WithCustomName("copy yarn package files"))

	return cur.Dir("/app").Run(
		llb.Shlex("yarn install"),
		llb.AddEnv("HOME", "/home/app"),
		llb.AddEnv("COREPACK_ENABLE_DOWNLOAD_PROMPT", "0"),
		llb.User("app"),
		llb.WithCustomName("[phase] Installing JS deps with yarn augmentation"),
	).State
}

// runBunInstall copies bun package files into /app and runs bun install as the
// app user. See runNpmInstall for the cache-mount rationale.
func (h *highlevelBuilder) runBunInstall(cur, mnt llb.State) llb.State {
	cur = h.ensureAppDir(cur)

	cur = cur.File(llb.Copy(mnt, "/", "/app", &llb.CopyInfo{
		IncludePatterns:    []string{"package.json", "bun.lock", "bun.lockb"},
		CreateDestPath:     true,
		AllowWildcard:      true,
		AllowEmptyWildcard: true,
		ChownOpt:           &appChown,
	}), llb.WithCustomName("copy bun package files"))

	return cur.Dir("/app").Run(
		llb.Shlex("bun install"),
		llb.AddEnv("HOME", "/home/app"),
		llb.User("app"),
		llb.WithCustomName("[phase] Installing JS deps with bun augmentation"),
	).State
}
