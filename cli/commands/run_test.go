package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("Run the application", func(t *testing.T) {
		r := require.New(t)

		out, err := RunCommand(Deploy, "-h")
		r.Error(err)

		r.Contains(out.Stderr.String(), "Command Options")
	})
}

func TestHelpOutput(t *testing.T) {
	t.Run("Print current help output", func(t *testing.T) {
		out, err := RunCommand(Deploy, "-h")
		if err != nil {
			t.Logf("Error (expected): %v", err)
		}
		t.Logf("Current help output:\n%s", out.Stderr.String())
	})
}
