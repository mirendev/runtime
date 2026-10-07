package stackbuild

import (
	"encoding/json"
	"regexp"

	"github.com/moby/buildkit/client/llb"
)

// The Node, Bun, and Python images are slim and ship without git, so a
// dependency fetched from a git repository fails to install no matter what
// credentials the build has. Those stacks look for one in the app's manifests
// and lockfiles and install git before the dependency step when they find it.
// The other stacks' images already carry git.

// npmGitSpec matches a package.json dependency fetched with git: an explicit
// git URL, a forge shorthand, or the bare "owner/repo" GitHub shorthand.
var npmGitSpec = regexp.MustCompile(`^(git\+|git://|github:|gitlab:|bitbucket:|[\w-][\w.-]*/[\w.-]+(#.*)?$)`)

// jsLockGitSource matches a git-resolved entry in package-lock.json, yarn.lock,
// or bun.lock. Lockfiles carry no repository metadata, so unlike package.json
// a text match can't be fooled by a "repository" field.
var jsLockGitSource = regexp.MustCompile(`git\+(https?|ssh|git|file)://|"git://`)

// pythonGitSource matches a git dependency in requirements.txt, a Pipfile,
// pyproject.toml, poetry.lock, or uv.lock: a PEP 508 "git+https://" direct
// reference, or a TOML table's git key (Pipfile, Poetry, uv sources).
var pythonGitSource = regexp.MustCompile(`git\+(https?|ssh|git|file)://|(?m)(^|[\s{,])git\s*=\s*"`)

// detectGitDeps records whether any JS or Python dependency is fetched with
// git. A Python app can carry JS through an augmentation, so both are checked
// whatever the primary stack.
func (s *MetaStack) detectGitDeps() {
	s.gitDeps = s.npmManifestHasGitDep() ||
		s.anyFileMatches(jsLockGitSource, "package-lock.json", "yarn.lock", "bun.lock") ||
		s.anyFileMatches(pythonGitSource, "requirements.txt", "Pipfile", "Pipfile.lock", "pyproject.toml", "poetry.lock", "uv.lock")
	if s.gitDeps {
		s.Event("package", "git", "Installing git for dependencies fetched from git repositories")
	}
}

func (s *MetaStack) npmManifestHasGitDep() bool {
	data, err := s.readFile("package.json")
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies, pkg.OptionalDependencies} {
		for _, spec := range deps {
			if npmGitSpec.MatchString(spec) {
				return true
			}
		}
	}
	return false
}

func (s *MetaStack) anyFileMatches(re *regexp.Regexp, paths ...string) bool {
	for _, p := range paths {
		if data, err := s.readFile(p); err == nil && re.Match(data) {
			return true
		}
	}
	return false
}

// withGitIfNeeded installs git on cur when detectGitDeps found a dependency
// that needs it, and leaves cur untouched otherwise.
func (s *MetaStack) withGitIfNeeded(h *highlevelBuilder, cur llb.State) llb.State {
	if !s.gitDeps {
		return cur
	}
	return h.aptInstall(cur, "git")
}
