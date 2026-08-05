package entity

import (
	"crypto/rand"
	"strings"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// minBase58Len is the minimum length of a base58 segment to be considered
// high-entropy (i.e., derived from a UUID rather than a human-chosen name).
const minBase58Len = 10

// defaultShortIdLen is the initial length of a short-id candidate.
const defaultShortIdLen = 3

// maxShortIdLen caps how long a short-id can grow before we give up.
// At length 8, there are 58^8 ≈ 128 trillion possibilities.
const maxShortIdLen = 8

// knownPrefixes are 1-2 character prefixes that precede the base58 portion
// in entity IDs of Category 2 (name + base58 suffix).
var knownPrefixes = []string{"ob", "v", "a", "s", "t"}

// isBase58 reports whether every character in s belongs to the Base58 alphabet.
func isBase58(s string) bool {
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(base58Alphabet, rune(s[i])) {
			return false
		}
	}
	return len(s) > 0
}

// ExtractBase58Suffix extracts the high-entropy base58 portion from an entity ID.
// Returns the base58 portion and true if found, or empty string and false otherwise.
//
// Handles all entity ID categories:
//   - Category 1 (named): "app/blog" → ("", false)
//   - Category 2 (name+prefix+base58): "blog-vCZ1eUgSgNd28ed6vt2DgY" → ("CZ1eUgSgNd28ed6vt2DgY", true)
//   - Category 3 (namespace/prefix-base58): "sandbox/blog-web-CZAtBvhsMNbG38MceikkB" → ("CZAtBvhsMNbG38MceikkB", true)
//   - Category 4 (kind-base58): "deployment-CZ1eUgSgNd28ed6vt2DgY" → ("CZ1eUgSgNd28ed6vt2DgY", true)
func ExtractBase58Suffix(entityId string) (string, bool) {
	id := entityId

	// Strip namespace prefix (everything before and including the last "/")
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}

	// Split on "-" and take the last segment
	if idx := strings.LastIndex(id, "-"); idx >= 0 {
		id = id[idx+1:]
	} else {
		// No "-" at all — this is a purely named entity (Category 1)
		return "", false
	}

	// Try stripping known prefixes
	base58Part := id
	for _, prefix := range knownPrefixes {
		if strings.HasPrefix(id, prefix) {
			candidate := id[len(prefix):]
			if isBase58(candidate) && len(candidate) >= minBase58Len {
				return candidate, true
			}
		}
	}

	// Check if the whole segment is base58
	if isBase58(base58Part) && len(base58Part) >= minBase58Len {
		return base58Part, true
	}

	return "", false
}

// ExistsCheck is a function that reports whether a given short-id candidate
// is already in use.
type ExistsCheck func(candidate string) (bool, error)

// AllocateShortId generates a short, globally-unique identifier for an entity.
// It first tries to derive the short-id from the entity's base58 suffix,
// falling back to random generation if no suffix is available or all
// suffix-derived candidates collide.
func AllocateShortId(entityId string, exists ExistsCheck) (string, error) {
	suffix, ok := ExtractBase58Suffix(entityId)
	if ok && len(suffix) >= defaultShortIdLen {
		// Try progressively longer suffixes starting from the end
		for length := defaultShortIdLen; length <= min(len(suffix), maxShortIdLen); length++ {
			candidate := suffix[len(suffix)-length:]
			taken, err := exists(candidate)
			if err != nil {
				return "", err
			}
			if !taken {
				return candidate, nil
			}
		}
	}

	// Fall back to random base58 generation
	return allocateRandomShortId(exists)
}

func allocateRandomShortId(exists ExistsCheck) (string, error) {
	for length := defaultShortIdLen; length <= maxShortIdLen; length++ {
		for range 5 {
			candidate, err := randomBase58(length)
			if err != nil {
				return "", err
			}
			taken, err := exists(candidate)
			if err != nil {
				return "", err
			}
			if !taken {
				return candidate, nil
			}
		}
	}
	return "", ErrShortIdExhausted
}

// randomBase58 generates a random string of the given length using base58
// characters. Note: this is NOT a base58 encoding of the random bytes — the
// result cannot be decoded back to the original bytes. We're just using the
// base58 alphabet as a convenient source of human-friendly characters.
func randomBase58(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = base58Alphabet[int(b[i])%len(base58Alphabet)]
	}
	return string(b), nil
}

// ShortId returns the entity's short-id, or empty string if not set.
func (e *Entity) ShortId() string {
	if attr, ok := e.Get(DBShortId); ok {
		return attr.Value.String()
	}
	return ""
}
