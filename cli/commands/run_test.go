package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("Run the application", func(t *testing.T) {
		r := require.New(t)

		out, err := RunCommand(Run, "-h")
		r.Error(err)

		r.Contains(out.Stderr.String(), "Command Options")
	})
}
