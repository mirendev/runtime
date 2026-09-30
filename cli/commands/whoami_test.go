package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDescribeUser(t *testing.T) {
	assert.Equal(t, "Ada Lovelace <ada@example.com>", describeUser("Ada Lovelace", "ada@example.com"))
	assert.Equal(t, "Ada Lovelace", describeUser("Ada Lovelace", ""))
	assert.Equal(t, "ada@example.com", describeUser("", "ada@example.com"))
	assert.Equal(t, "", describeUser("", ""))
}
