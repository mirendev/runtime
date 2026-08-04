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

	t.Run("can generate code for generic types", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/generic.yml")
		r.NoError(err)

		output, err := g.Generate("generic")
		r.NoError(err)

		data, err := os.ReadFile("testdata/generic.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})

	t.Run("can generate code for a message with map fields", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/map.yml")
		r.NoError(err)

		output, err := g.Generate("maptype")
		r.NoError(err)

		data, err := os.ReadFile("testdata/map.go")
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

	t.Run("can generate code for an interface with http annotations", func(t *testing.T) {
		r := require.New(t)

		g, err := NewGenerator()
		r.NoError(err)

		err = g.Read("testdata/rest.yml")
		r.NoError(err)

		output, err := g.Generate("rest")
		r.NoError(err)

		data, err := os.ReadFile("testdata/rest.go")
		r.NoError(err)

		r.Equal(string(data), output)
	})
}

func TestGeneratorHTTPValidation(t *testing.T) {
	// validate runs the http: annotation checks over a single-method interface,
	// plus any extra interfaces the case needs declared (for capability types).
	validate := func(t *testing.T, m *DescMethods, extra ...*DescInterface) error {
		t.Helper()

		g, err := NewGenerator()
		require.NoError(t, err)

		g.Interfaces = append([]*DescInterface{{
			Name:   "Widgets",
			Method: []*DescMethods{m},
		}}, extra...)
		require.NoError(t, g.populateTypeInfo())

		return g.validateHTTP()
	}

	t.Run("accepts a well-formed annotation", func(t *testing.T) {
		r := require.New(t)

		r.NoError(validate(t, &DescMethods{
			Name:       "get",
			HTTP:       &DescHTTPMethod{Get: "/widgets/{id}"},
			Parameters: []*DescParamater{{Name: "id", Type: "string"}},
		}))
	})

	t.Run("rejects more than one verb", func(t *testing.T) {
		r := require.New(t)

		// verbPath picks the first non-empty field, so the post: would vanish.
		err := validate(t, &DescMethods{
			Name: "get",
			HTTP: &DescHTTPMethod{Get: "/widgets", Post: "/widgets"},
		})
		r.ErrorContains(err, "only one verb")
	})

	t.Run("rejects a body that is not \"*\"", func(t *testing.T) {
		r := require.New(t)

		err := validate(t, &DescMethods{
			Name: "create",
			HTTP: &DescHTTPMethod{Post: "/widgets", Body: "yes"},
		})
		r.ErrorContains(err, `body must be "" or "*"`)
	})

	t.Run("rejects a path wildcard with no matching parameter", func(t *testing.T) {
		r := require.New(t)

		// {widget} does not name a declared parameter, so the generated args
		// struct would have no field to receive it.
		err := validate(t, &DescMethods{
			Name:       "get",
			HTTP:       &DescHTTPMethod{Get: "/widgets/{widget}"},
			Parameters: []*DescParamater{{Name: "id", Type: "string"}},
		})
		r.ErrorContains(err, `path parameter "widget"`)
	})

	t.Run("rejects a capability result", func(t *testing.T) {
		r := require.New(t)

		err := validate(t, &DescMethods{
			Name:    "getSetter",
			HTTP:    &DescHTTPMethod{Get: "/widgets/setter"},
			Results: []*DescParamater{{Name: "setter", Type: "SetTemp"}},
		}, &DescInterface{Name: "SetTemp"})
		r.ErrorContains(err, "capability")
	})

	t.Run("rejects a capability parameter", func(t *testing.T) {
		r := require.New(t)

		err := validate(t, &DescMethods{
			Name:       "adjust",
			HTTP:       &DescHTTPMethod{Post: "/widgets/adjust"},
			Parameters: []*DescParamater{{Name: "setter", Type: "SetTemp"}},
		}, &DescInterface{Name: "SetTemp"})
		r.ErrorContains(err, "capability")
	})

	t.Run("ignores methods with no annotation", func(t *testing.T) {
		r := require.New(t)

		r.NoError(validate(t, &DescMethods{
			Name:    "internalOnly",
			Results: []*DescParamater{{Name: "setter", Type: "SetTemp"}},
		}, &DescInterface{Name: "SetTemp"}))
	})
}
