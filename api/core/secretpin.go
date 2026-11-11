package compute

import (
	"context"
	"fmt"
	"strings"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/secret"
)

// SecretReferences collects every backend-sourced variable in a config, both
// the shared ones and the per-service ones.
func SecretReferences(spec *core_v1alpha.ConfigSpec) []secret.Reference {
	var refs []secret.Reference

	for _, v := range spec.Variables {
		if v.Backend == "" {
			continue
		}
		refs = append(refs, secret.Reference{
			Backend: v.Backend,
			Ref:     v.Value,
			Key:     v.Key,
		})
	}

	for _, svc := range spec.Services {
		for _, e := range svc.Env {
			if e.Backend == "" {
				continue
			}
			refs = append(refs, secret.Reference{
				Backend: e.Backend,
				Ref:     e.Value,
				Key:     e.Key,
				Service: svc.Name,
			})
		}
	}

	return refs
}

// PinSecrets resolves every backend-sourced variable in a config and rewrites
// each one's value to the fully-qualified reference it resolved to.
//
// Call it immediately before a ConfigVersion is created. An authored reference
// usually floats ("payments/stripe-key"); what the ConfigVersion records is
// what it resolved to at that moment ("payments/stripe-key@x1A"). That is what
// makes "which secret did this version actually ship with?" answerable, and
// what makes a rollback come back on the value the old version ran with rather
// than today's.
//
// It is idempotent, because resolving an already-pinned reference returns the
// same reference. A redeploy therefore re-pins a floating app.toml reference to
// whatever is current — picking up a rotation — while leaving a hand-set
// reference at the version it was set to.
//
// A config with no backend-sourced variables never touches the resolver, so a
// cluster with no secret backends registered (resolver nil) keeps working
// exactly as before. One that does reference a backend fails the write rather
// than minting a version whose credential silently never arrives.
//
// app is the app the config belongs to; a reference to a secret another app
// owns fails the pin (see secret.Pin).
//
// Every path that mints a ConfigVersion calls this, including for a config with
// no references, because the literal check below has to run on those too.
func PinSecrets(ctx context.Context, resolver secret.Resolver, spec *core_v1alpha.ConfigSpec, app entity.Id) error {
	if err := rejectKeysWithEquals(spec); err != nil {
		return err
	}
	if err := rejectLiteralsPosingAsReferences(spec); err != nil {
		return err
	}

	refs := SecretReferences(spec)
	if len(refs) == 0 {
		return nil
	}
	if resolver == nil {
		return fmt.Errorf("config references secret %s but this cluster has no secret backends configured", refs[0])
	}

	pinned, err := secret.Pin(ctx, resolver, refs, string(app))
	if err != nil {
		return err
	}

	// Walk in the same order SecretReferences produced, so each resolved
	// reference lands back on the variable it came from.
	next := 0
	for i := range spec.Variables {
		if spec.Variables[i].Backend == "" {
			continue
		}
		spec.Variables[i].Value = pinned[next]
		next++
	}

	for s := range spec.Services {
		for e := range spec.Services[s].Env {
			if spec.Services[s].Env[e].Backend == "" {
				continue
			}
			spec.Services[s].Env[e].Value = pinned[next]
			next++
		}
	}

	return nil
}

// rejectKeysWithEquals refuses a variable name containing "=". A sandbox's env
// is serialized as KEY=VALUE and split again at the first "=", so such a key
// would move part of itself into the value. That is how a key could smuggle in
// a secret reference the literal check below never sees as a value.
func rejectKeysWithEquals(spec *core_v1alpha.ConfigSpec) error {
	bad := func(key string) bool { return strings.Contains(key, "=") }

	for _, v := range spec.Variables {
		if bad(v.Key) {
			return fmt.Errorf("variable name %q contains \"=\", which an environment variable name cannot", v.Key)
		}
	}
	for _, svc := range spec.Services {
		for _, e := range svc.Env {
			if bad(e.Key) {
				return fmt.Errorf("variable name %q of service %s contains \"=\", which an environment variable name cannot", e.Key, svc.Name)
			}
		}
	}
	for _, task := range spec.Tasks {
		for _, e := range task.Env {
			if bad(e.Key) {
				return fmt.Errorf("variable name %q of task %s contains \"=\", which an environment variable name cannot", e.Key, task.Name)
			}
		}
	}
	return nil
}

// rejectLiteralsPosingAsReferences refuses an inline value that looks like the
// placeholder a backend-sourced variable contributes to a sandbox spec.
//
// Backend identity travels in-band, alongside the reference in a single env
// string, because the spec's env is a flat []string with nowhere structured to
// put it. That makes the mapping breakable from the other side: a literal whose
// value happens to start with the scheme would be materialized as though it
// named a secret. Since a secret can belong to an app, that would be a way
// around the ownership check in secret.Pin: the runner resolves the literal at
// container start without asking whose secret it names. So it is refused at the
// one point every config passes through.
func rejectLiteralsPosingAsReferences(spec *core_v1alpha.ConfigSpec) error {
	for _, v := range spec.Variables {
		if v.Backend == "" && strings.HasPrefix(v.Value, secret.SentinelScheme) {
			return fmt.Errorf("variable %s has no backend but its value starts with %s, which is reserved for secret references",
				v.Key, secret.SentinelScheme)
		}
	}

	for _, svc := range spec.Services {
		for _, e := range svc.Env {
			if e.Backend == "" && strings.HasPrefix(e.Value, secret.SentinelScheme) {
				return fmt.Errorf("variable %s of service %s has no backend but its value starts with %s, which is reserved for secret references",
					e.Key, svc.Name, secret.SentinelScheme)
			}
		}
	}

	// Task env has no backend field at all, so a value with the scheme there
	// can only be a literal posing as a reference.
	for _, task := range spec.Tasks {
		for _, e := range task.Env {
			if strings.HasPrefix(e.Value, secret.SentinelScheme) {
				return fmt.Errorf("variable %s of task %s starts with %s, which is reserved for secret references",
					e.Key, task.Name, secret.SentinelScheme)
			}
		}
	}

	return nil
}
