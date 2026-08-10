package secret

import (
	"fmt"
	"strings"
)

// versionSep separates a secret's path from its version handle in a reference.
const versionSep = "@"

// ParseRef splits a backend-relative reference into its path and version.
// A version-less reference ("payments/stripe-key") returns an empty version,
// meaning "whatever is current"; a pinned one ("payments/stripe-key@x1A")
// returns the handle.
//
// The separator is matched from the right so a path may itself contain "@"
// — external backends address secrets in shapes Miren does not control.
func ParseRef(ref string) (path, version string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("secret reference is empty")
	}

	idx := strings.LastIndex(ref, versionSep)
	if idx < 0 {
		return ref, "", nil
	}

	path, version = ref[:idx], ref[idx+len(versionSep):]
	if path == "" {
		return "", "", fmt.Errorf("secret reference %q has no path", ref)
	}
	if version == "" {
		return "", "", fmt.Errorf("secret reference %q has a trailing %q but no version", ref, versionSep)
	}
	return path, version, nil
}

// FormatRef builds the fully-qualified reference for a path at a version.
// Every Resolve returns one of these regardless of what it was handed, so that
// what a ConfigVersion records is always concrete.
func FormatRef(path, version string) string {
	if version == "" {
		return path
	}
	return path + versionSep + version
}

// IsPinned reports whether a reference already names a concrete version.
func IsPinned(ref string) bool {
	_, version, err := ParseRef(ref)
	return err == nil && version != ""
}
