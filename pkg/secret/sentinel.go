package secret

import (
	"fmt"
	"strings"
)

// SentinelScheme prefixes an environment value that names a secret rather than
// holding one.
const SentinelScheme = "miren+secret://"

// FormatSentinel renders a reference as the placeholder that stands in for a
// value inside a sandbox spec.
//
// A sandbox spec is a persisted entity, so a resolved value written into it
// would sit in etcd in plaintext — exactly what referencing a secret is meant
// to avoid. The spec therefore carries this placeholder, and the value is
// substituted in memory at the moment the container is created.
//
// It doubles as the unit of change detection for pool reuse: the reference
// carries a concrete version, so it differs precisely when the bytes behind it
// would differ.
func FormatSentinel(backend, ref string) string {
	return SentinelScheme + backend + "/" + ref
}

// ParseSentinel splits a placeholder back into its backend and reference.
// The second return is false for an ordinary value, which is the common case.
func ParseSentinel(value string) (backend, ref string, ok bool) {
	rest, found := strings.CutPrefix(value, SentinelScheme)
	if !found {
		return "", "", false
	}

	backend, ref, found = strings.Cut(rest, "/")
	if !found || backend == "" || ref == "" {
		return "", "", false
	}
	return backend, ref, true
}

// IsSentinel reports whether a value names a secret rather than holding one.
func IsSentinel(value string) bool {
	_, _, ok := ParseSentinel(value)
	return ok
}

// MalformedSentinelError reports a placeholder that carries the scheme but does
// not parse. It is deliberately distinct from "not a placeholder": a value that
// looks like a reference and cannot be read is a bug or a tampered spec, and
// starting the container with it verbatim would hand the app a URL where its
// credential should be.
type MalformedSentinelError struct {
	Key string
}

func (e MalformedSentinelError) Error() string {
	return fmt.Sprintf("environment variable %s names a secret but the reference could not be read", e.Key)
}
