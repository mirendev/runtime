package deploylifecycle

import "strings"

// DescribeDeployer renders who started a deployment for people to read,
// preferring a display name, then an email, then the authenticated subject.
// It returns "" when nothing identifies the deployer, leaving the caller to
// pick its own placeholder.
//
// Name and email come from the cloud token of whoever deployed, so they are
// absent for deploys made before cloud stamped them, and for identities with
// no cloud user behind them (cert auth, CI). Those fall through to the
// subject.
func DescribeDeployer(name, email, subject, authMethod string) string {
	switch {
	case name != "":
		return name
	case email != "":
		return email
	case authMethod == "oidc":
		return describeOIDCSubject(subject)
	default:
		return subject
	}
}

// describeOIDCSubject shortens a GitHub Actions subject such as
// "repo:acme/web:ref:refs/heads/main" to "github:acme/web@main". Subjects in
// any other shape are returned unchanged.
func describeOIDCSubject(subject string) string {
	rest, ok := strings.CutPrefix(subject, "repo:")
	if !ok {
		return subject
	}
	repo, qualifier, _ := strings.Cut(rest, ":")
	if repo == "" {
		return subject
	}
	out := "github:" + repo

	kind, value, _ := strings.Cut(qualifier, ":")
	switch kind {
	case "ref":
		if branch, ok := strings.CutPrefix(value, "refs/heads/"); ok {
			return out + "@" + branch
		}
		if tag, ok := strings.CutPrefix(value, "refs/tags/"); ok {
			return out + "@" + tag
		}
		return out + "@" + value
	case "environment":
		return out + " (environment " + value + ")"
	case "pull_request":
		return out + " (pull request)"
	default:
		return out
	}
}
