package deploylifecycle

import (
	"net/url"
	"strings"

	"miren.dev/runtime/api/core/core_v1alpha"
)

// SourceFromGitInfo converts the verbose, legacy build metadata into the small
// provenance record owned by AppVersion. Repository credentials and request
// decorations are deliberately discarded before the value becomes durable.
func SourceFromGitInfo(info core_v1alpha.GitInfo) core_v1alpha.Source {
	return core_v1alpha.Source{
		GitSha:     info.Sha,
		GitBranch:  info.Branch,
		Repository: sanitizeRepository(info.Repository),
	}
}

func sanitizeRepository(repository string) string {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return ""
	}

	// Git's scp-like syntax is not a URL, but it can still carry a user name.
	// Preserve the familiar host:path form while dropping that user component.
	if !strings.Contains(repository, "://") {
		host := repository
		if cut := strings.IndexAny(repository, ":/"); cut >= 0 {
			host = repository[:cut]
		}
		if at := strings.LastIndex(host, "@"); at >= 0 {
			repository = repository[at+1:]
		}
		if cut := strings.IndexAny(repository, "?#"); cut >= 0 {
			repository = repository[:cut]
		}
		return repository
	}

	u, err := url.Parse(repository)
	if err != nil || u.Scheme == "" {
		return ""
	}
	// A local checkout path is not useful cluster provenance and can disclose
	// host-specific filesystem details. Reject file URLs explicitly rather than
	// relying on their normally empty host to do so incidentally.
	if strings.EqualFold(u.Scheme, "file") || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}
