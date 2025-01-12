package rpc

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerator(t *testing.T) {
	t.Run("can generate code for a message", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/fixed.yml")
		r.NoError(err)

		output, err := g.Generate("fixed")
		r.NoError(err)

		data, err := os.ReadFile("testdata/fixed.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})

	t.Run("can generate code for a message with variable fields", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/variable.yml")
		r.NoError(err)

		output, err := g.Generate("variable")
		r.NoError(err)

		data, err := os.ReadFile("testdata/variable.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})

	t.Run("can generate code for a message with embed messages", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/embed.yml")
		r.NoError(err)

		output, err := g.Generate("embed")
		r.NoError(err)

		data, err := os.ReadFile("testdata/embed.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})

	t.Run("can generate code for a message with union fields", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/union.yml")
		r.NoError(err)

		output, err := g.Generate("union")
		r.NoError(err)

		data, err := os.ReadFile("testdata/union.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})

	t.Run("can generate code for an interface", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/rpc.yml")
		r.NoError(err)

		output, err := g.Generate("rpc")
		r.NoError(err)

		data, err := os.ReadFile("testdata/rpc.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})
}
