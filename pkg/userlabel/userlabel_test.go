package userlabel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLabel(t *testing.T) {
	cases := []struct {
		name                string
		who, email, subject string
		want                string
	}{
		{"name and email", "Ada Lovelace", "ada@example.com", "usr-ada", "Ada Lovelace (ada@example.com)"},
		{"email only", "", "ada@example.com", "usr-ada", "ada@example.com"},
		{"name only", "Ada Lovelace", "", "usr-ada", "Ada Lovelace"},
		{"subject only", "", "", "usr-ada", "usr-ada"},
		{"nothing", "", "", "", ""},
		{"blank name falls through to email", "  ", "ada@example.com", "", "ada@example.com"},
		{"surrounding space trimmed", " Ada Lovelace ", " ada@example.com ", "", "Ada Lovelace (ada@example.com)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Label(tc.who, tc.email, tc.subject))
		})
	}
}
