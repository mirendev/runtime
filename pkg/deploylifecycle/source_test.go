package deploylifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/api/core/core_v1alpha"
)

func TestSourceFromGitInfoSanitizesRepository(t *testing.T) {
	tests := map[string]string{
		"https://user:secret@example.com/acme/web.git?token=nope#fragment": "https://example.com/acme/web.git",
		"git@example.com:acme/web.git":                                     "example.com:acme/web.git",
		"git@example.com:acme/web.git@v1":                                  "example.com:acme/web.git@v1",
		"example.com:acme/pkg@2":                                           "example.com:acme/pkg@2",
		"file:///home/user/private/repo":                                   "",
	}
	for repository, want := range tests {
		source := SourceFromGitInfo(core_v1alpha.GitInfo{
			Sha: "abc", Branch: "main", Repository: repository,
		})
		assert.Equal(t, "abc", source.GitSha)
		assert.Equal(t, "main", source.GitBranch)
		assert.Equal(t, want, source.Repository)
	}
}
