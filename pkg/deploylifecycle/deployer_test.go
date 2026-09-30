package deploylifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDescribeDeployer(t *testing.T) {
	cases := []struct {
		name                        string
		who, email, subject, method string
		want                        string
	}{
		{name: "name wins", who: "Ada Lovelace", email: "ada@example.com", subject: "usr-ada", method: "jwt", want: "Ada Lovelace"},
		{name: "email when no name", email: "ada@example.com", subject: "usr-ada", method: "jwt", want: "ada@example.com"},
		{name: "subject when token predates profile claims", subject: "usr-ada", method: "jwt", want: "usr-ada"},
		{name: "cert subject as-is", subject: "miren-admin", method: "cert", want: "miren-admin"},
		{name: "nobody", want: ""},
		{name: "github branch", subject: "repo:acme/web:ref:refs/heads/main", method: "oidc", want: "github:acme/web@main"},
		{name: "github tag", subject: "repo:acme/web:ref:refs/tags/v1.2.0", method: "oidc", want: "github:acme/web@v1.2.0"},
		{name: "github environment", subject: "repo:acme/web:environment:production", method: "oidc", want: "github:acme/web (environment production)"},
		{name: "github pull request", subject: "repo:acme/web:pull_request", method: "oidc", want: "github:acme/web (pull request)"},
		{name: "github bare repo", subject: "repo:acme/web", method: "oidc", want: "github:acme/web"},
		{name: "other oidc issuer untouched", subject: "project_path:acme/web:ref_type:branch:ref:main", method: "oidc", want: "project_path:acme/web:ref_type:branch:ref:main"},
		{name: "repo prefix only rewritten for oidc", subject: "repo:acme/web:ref:refs/heads/main", method: "jwt", want: "repo:acme/web:ref:refs/heads/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DescribeDeployer(tc.who, tc.email, tc.subject, tc.method))
		})
	}
}
