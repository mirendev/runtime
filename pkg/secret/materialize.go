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
//
// It is also where ownership is finally enforced. A secret that belongs to an
// app is handed only to that app's sandboxes, whatever path the reference took
// to get here. Pinning refuses such a reference much earlier, with a better
// error, but this check does not depend on every write path having pinned.
// app reports the id of the app the sandbox belongs to (empty for none, which
// receives only unowned secrets). It is called at most once, and only when the
// env holds a reference, so a sandbox with no secrets costs no lookup. A
// resolver that is an AppResolver is also told the app, so a remote
// coordinator can refuse an owned secret before the plaintext ever leaves it.
func MaterializeEnv(ctx context.Context, resolver Resolver, env []string, app func(context.Context) (string, error)) ([]string, error) {
	// Most sandboxes reference no secrets at all. Detect that first so the
	// common path allocates nothing and never needs a resolver.
	if !containsSentinel(env) {
		return env, nil
	}

	out := make([]string, len(env))
	copy(out, env)

	var (
		owner       string
		ownerLoaded bool
	)
	loadOwner := func(key string) error {
		if ownerLoaded {
			return nil
		}
		ownerLoaded = true
		if app == nil {
			return nil
		}
		var err error
		if owner, err = app(ctx); err != nil {
			return fmt.Errorf("cannot materialize environment variable %s: finding the sandbox's app: %w", key, err)
		}
		return nil
	}
	forApp, tellsApp := resolver.(AppResolver)

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

		var val SecretValue
		var err error
		if tellsApp {
			if err := loadOwner(key); err != nil {
				return nil, err
			}
			val, err = forApp.ResolveRefForApp(ctx, backend, ref, owner)
		} else {
			val, err = resolver.ResolveRef(ctx, backend, ref)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot materialize environment variable %s: %w", key, err)
		}

		if val.Owner != "" {
			if err := loadOwner(key); err != nil {
				return nil, err
			}
			if err := CheckOwner(val, owner); err != nil {
				return nil, fmt.Errorf("cannot materialize environment variable %s: %w", key, err)
			}
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
