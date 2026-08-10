package secret

import (
	"context"
	"fmt"
	"strings"
)

// MaterializeEnv replaces every secret placeholder in a KEY=VALUE environment
// slice with the resolved value, returning a new slice and leaving the input
// untouched.
//
// This is the point of use: the value exists in the returned slice, in memory,
// and goes straight into the container's process environment. Nothing here
// writes to disk, and the spec these values came from keeps carrying only
// references.
//
// It fails rather than degrading. An app started with a placeholder where its
// credential should be does not fail at boot in a way anyone can read — it
// fails later, deep in whatever the credential was for. A missing resolver, an
// unknown backend, a revoked version, and an unreadable reference are all
// errors here, so the failure lands where it can name the variable.
func MaterializeEnv(ctx context.Context, resolver Resolver, env []string) ([]string, error) {
	// Most sandboxes reference no secrets at all. Detect that first so the
	// common path allocates nothing and never needs a resolver.
	if !containsSentinel(env) {
		return env, nil
	}

	out := make([]string, len(env))
	copy(out, env)

	for i, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found || !strings.HasPrefix(value, SentinelScheme) {
			continue
		}

		backend, ref, ok := ParseSentinel(value)
		if !ok {
			return nil, MalformedSentinelError{Key: key}
		}

		if resolver == nil {
			return nil, fmt.Errorf("environment variable %s references secret %s:%s but no secret backends are available here", key, backend, ref)
		}

		val, err := resolver.ResolveRef(ctx, backend, ref)
		if err != nil {
			return nil, fmt.Errorf("cannot materialize environment variable %s: %w", key, err)
		}

		out[i] = key + "=" + string(val.Bytes)
	}

	return out, nil
}

func containsSentinel(env []string) bool {
	for _, entry := range env {
		if _, value, found := strings.Cut(entry, "="); found && strings.HasPrefix(value, SentinelScheme) {
			return true
		}
	}
	return false
}
