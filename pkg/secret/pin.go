package secret

import (
	"context"
	"fmt"
)

// Reference is one backend-sourced value inside a config, identified well
// enough to name it in an error without quoting the value.
type Reference struct {
	// Backend is the registered instance name the value comes from.
	Backend string

	// Ref is the backend-relative reference, floating or already pinned.
	Ref string

	// Key is the variable this reference feeds, for error messages.
	Key string

	// Service scopes the variable to one service, or is empty for a variable
	// shared by all of them.
	Service string
}

// String renders a reference the way it is written and displayed.
func (r Reference) String() string {
	return r.Backend + ":" + r.Ref
}

// describe names where a reference lives, so a failure says which variable
// could not be resolved rather than only which secret.
func (r Reference) describe() string {
	if r.Service == "" {
		return fmt.Sprintf("variable %s (%s)", r.Key, r)
	}
	return fmt.Sprintf("variable %s of service %s (%s)", r.Key, r.Service, r)
}

// Pin resolves a set of references and returns the fully-qualified reference
// each one resolves to, keyed by the same index as the input.
//
// It is what freezes a config: an authored reference usually floats, and
// recording what it resolved to at mint time is how a ConfigVersion becomes
// answerable for the most sensitive input it carries. Because Resolve returns a
// concrete version even for an already-pinned input, pinning is idempotent —
// which is what lets a hand-set reference stay at the version it was set to
// while an app.toml reference re-pins on every deploy, with no second piece of
// state to keep in sync.
//
// The resolved bytes are deliberately discarded. Pinning happens in the control
// plane at config-mint time; materializing the value belongs at the point of
// use, so nothing here can leak into a persisted config.
//
// A failure names the variable and the backend, and never the value or the
// backend's own error text, which can quote surrounding context.
func Pin(ctx context.Context, resolver Resolver, refs []Reference) ([]string, error) {
	pinned := make([]string, len(refs))

	for i, ref := range refs {
		if ref.Backend == "" {
			return nil, fmt.Errorf("%s has no backend to resolve against", ref.describe())
		}
		if ref.Ref == "" {
			return nil, fmt.Errorf("%s names backend %q but no secret", ref.describe(), ref.Backend)
		}

		val, err := resolver.ResolveRef(ctx, ref.Backend, ref.Ref)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", ref.describe(), err)
		}
		pinned[i] = val.Ref
	}

	return pinned, nil
}
