package enttest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/entity"
)

// EqualAttr checks if the entity has an attribute with the given ID and if its value matches expected.
// Returns true if the assertion passes, false otherwise.
func EqualAttr(t *testing.T, ent *entity.Entity, id entity.Id, expected any, msgAndArgs ...any) bool {
	t.Helper()

	attrs := ent.GetAll(id)
	if len(attrs) == 0 {
		return assert.Fail(t, fmt.Sprintf("Entity does not have attribute %s", id), msgAndArgs...)
	}

	if len(attrs) == 1 {
		return assert.Equal(t, expected, attrs[0].Value.Any(), msgAndArgs...)
	}

	for _, attr := range ent.GetAll(id) {
		actual := attr.Value.Any()
		if expected == nil && actual == nil {
			return true
		}
		if expected == actual {
			return true
		}
	}

	return assert.Fail(t, fmt.Sprintf("No attribute %s with expected value %v found", id, expected), msgAndArgs...)
}
