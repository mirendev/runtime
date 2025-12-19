package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("Run the application", func(t *testing.T) {
		r := require.New(t)

		_, err := RunCommand(Deploy, "-h")
		r.Error(err)

		// mflags returns "help requested" error when -h is passed
		r.Contains(err.Error(), "help requested")
	})
}
