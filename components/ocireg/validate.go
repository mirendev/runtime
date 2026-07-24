package ocireg

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Grammars from the OCI distribution spec for the path components we accept.
// The registry is reachable from every sandbox on the bridge and runs as root,
// so a component that doesn't match one of these never reaches a filesystem
// operation (MIR-1474).
var (
	// repositoryNameRe is <name>: lowercase alphanumeric components joined by
	// single separators, optionally nested with "/". It has no way to express
	// a ".." segment, which is the point. Legitimate clients can't produce a
	// name outside this grammar either, since containerd and buildkit both
	// parse the reference before they ever open a connection.
	repositoryNameRe = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)

	// digestRe is <digest>: an algorithm followed by its encoded value.
	digestRe = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$`)

	// tagRe is <reference> in its tag form. References may also be digests,
	// which validateReference accepts separately.
	tagRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

	// uploadIDRe matches what idgen.Gen produces: a prefix letter followed by
	// base58, so alphanumerics and nothing else.
	uploadIDRe = regexp.MustCompile(`^[0-9A-Za-z]+$`)
)

const maxRepositoryNameLen = 255

func validateRepositoryName(name string) error {
	if len(name) > maxRepositoryNameLen {
		return fmt.Errorf("repository name exceeds %d characters", maxRepositoryNameLen)
	}
	if !repositoryNameRe.MatchString(name) {
		return fmt.Errorf("invalid repository name %q", name)
	}
	return nil
}

func validateDigest(digest string) error {
	if !digestRe.MatchString(digest) {
		return fmt.Errorf("invalid digest %q", digest)
	}
	return nil
}

// validateReference accepts either form a manifest reference can take: a tag
// or a digest.
func validateReference(reference string) error {
	if tagRe.MatchString(reference) || digestRe.MatchString(reference) {
		return nil
	}
	return fmt.Errorf("invalid reference %q", reference)
}

func validateUploadID(id string) error {
	if !uploadIDRe.MatchString(id) {
		return fmt.Errorf("invalid upload id %q", id)
	}
	return nil
}

// splitEscapedPath parses an OCI registry path into its components, decoding
// each one individually.
//
// Decoding per-segment rather than reading r.URL.Path is what makes the
// routing trustworthy. Go's ServeMux runs cleanPath against the *escaped*
// path, so a literal "/v2/x/blobs/../../etc" is redirected away before it
// reaches us, while "%2e%2e%2f" survives cleanPath untouched and then shows up
// in r.URL.Path fully decoded, with its slashes acting as real separators.
// Splitting first and decoding after means an encoded slash stays inside its
// segment, where the grammars above reject it.
//
// It reports false if the path isn't under /v2/ or any segment is malformed.
func splitEscapedPath(escapedPath string) ([]string, bool) {
	if !strings.HasPrefix(escapedPath, "/v2/") {
		return nil, false
	}

	trimmed := strings.TrimPrefix(escapedPath, "/v2/")
	if trimmed == "" {
		return nil, true
	}

	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, false
		}
		if strings.ContainsRune(decoded, 0) {
			return nil, false
		}
		parts[i] = decoded
	}

	return parts, true
}

// digestFile hashes the file at path using the algorithm named by claimed, and
// returns the result in the same "<algorithm>:<hex>" form so the caller can
// compare strings. Layers can be gigabytes, so it streams rather than reading
// the file in.
//
// An algorithm we can't compute is an error rather than a pass, since silently
// skipping verification would put us right back where we started.
func digestFile(path, claimed string) (string, error) {
	algorithm, _, found := strings.Cut(claimed, ":")
	if !found {
		return "", fmt.Errorf("digest %q has no algorithm", claimed)
	}

	var hasher hash.Hash
	switch algorithm {
	case "sha256":
		hasher = sha256.New()
	case "sha512":
		hasher = sha512.New()
	default:
		return "", fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return algorithm + ":" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// containedPath joins elems onto root and confirms the result stays underneath
// it. The validators above should already make an escape impossible; this is
// the backstop that makes containment a structural property of the storage
// helpers rather than something every call site has to get right on its own.
func containedPath(root string, elems ...string) (string, error) {
	cleanRoot := filepath.Clean(root)

	joined := filepath.Join(append([]string{cleanRoot}, elems...)...)
	if joined != cleanRoot && !strings.HasPrefix(joined, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes storage root %q", joined, cleanRoot)
	}

	return joined, nil
}
