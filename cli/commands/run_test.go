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
	t.Run("Help method includes Miren team message", func(t *testing.T) {
		r := require.New(t)

		cmd := Infer("test command", "A command being tested", Deploy)
		helpText := cmd.Help()

		r.Contains(helpText, "From your friends at Miren")
	})
}
