package cluster

import (
	"fmt"
	"regexp"
	"strings"
)

// pathPattern constrains cluster secret paths to slash-separated segments of
// alphanumerics, underscores and hyphens.
//
// Dots are excluded on purpose: entity names cannot hold a slash, so a path is
// stored under its slash-to-dot translation, and allowing both characters would
// let "a/b" and "a.b" land on the same entity.
var pathPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*(/[a-zA-Z0-9][a-zA-Z0-9_-]*)*$`)

// maxPathLen bounds a path so it cannot grow into an unwieldy entity name.
const maxPathLen = 253

// ValidatePath reports whether a path is addressable in the cluster backend.
// External backends address secrets in shapes Miren does not control, so this
// constrains only paths the cluster itself stores.
func ValidatePath(path string) error {
	switch {
	case path == "":
		return fmt.Errorf("secret path is empty")
	case len(path) > maxPathLen:
		return fmt.Errorf("secret path is %d characters, limit is %d", len(path), maxPathLen)
	case !pathPattern.MatchString(path):
		return fmt.Errorf("secret path %q must be slash-separated segments of letters, digits, underscores and hyphens", path)
	}
	return nil
}

// entityName translates a path into the name its secret entity is stored under.
// Validation guarantees the path holds no dots, so this stays reversible and
// two different paths can never collide on one entity.
func entityName(path string) string {
	return strings.ReplaceAll(path, "/", ".")
}
