package asm

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestASM(t *testing.T) {
	t.Run("can populate a struct from available components", func(t *testing.T) {
		r := require.New(t)
		var reg Registry

		reg.Register("port", int(3000))

		var s struct {
			Port int `asm:"port"`
		}

		err := reg.Populate(&s)
		r.NoError(err)

		r.Equal(3000, s.Port)
	})

	t.Run("errors out if the wrong type is available", func(t *testing.T) {
		r := require.New(t)
		var reg Registry

		reg.Register("port", "foo")

		var s struct {
			Port int `asm:"port"`
		}

		err := reg.Populate(&s)
		r.Error(err)
	})

	t.Run("can populate based only on type", func(t *testing.T) {
		type thing struct {
			name string
		}

		r := require.New(t)

		var reg Registry
		reg.Register("blah", &thing{name: "blah"})

		var s struct {
			Thing *thing
		}

		err := reg.Populate(&s)
		r.NoError(err)

		r.NotNil(s.Thing)
		r.Equal(s.Thing.name, "blah")
	})

	t.Run("will not populate based only on type and wrong name", func(t *testing.T) {
		type thing struct {
			name string
		}

		r := require.New(t)

		var reg Registry
		reg.Register("blah", &thing{name: "blah"})

		var s struct {
			Thing *thing `asm:"thing"`
		}

		err := reg.Populate(&s)
		r.Error(err)
	})

	t.Run("can resolve a pointer directly", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.Provide(func() *thing { return &thing{} })
		reg.Register("name", "blah")

		var x *thing

		err := reg.Resolve(&x)
		r.NoError(err)

		r.NotNil(x)
		r.Equal(x.Name, "blah")
	})

	t.Run("considers the name when running a provider", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.ProvideName("name", func() string { return "miren" })
		reg.ProvideName("blah", func() string { return "blah" })

		var x thing

		err := reg.Populate(&x)
		r.NoError(err)

		r.Equal(x.Name, "miren")
	})

	t.Run("providers can satisfy interfaces", func(t *testing.T) {
		type thing struct {
			Writer io.Writer `asm:"stdout"`
		}

		r := require.New(t)

		var reg Registry

		reg.ProvideName("stdout", func() *os.File { return os.Stdout })

		var x thing

		err := reg.Populate(&x)
		r.NoError(err)

		r.Same(x.Writer, os.Stdout)
	})

	t.Run("named providers can be used without a name", func(t *testing.T) {

		type bar struct {
			Name string
		}

		type thing struct {
			Name *bar
		}

		r := require.New(t)

		var reg Registry
		reg.ProvideName("better", func() *bar { return &bar{Name: "miren"} })

		var x thing

		err := reg.Populate(&x)
		r.NoError(err)

		r.Equal(x.Name.Name, "miren")
	})

	t.Run("can't use a non-struct type as a named providers without a name", func(t *testing.T) {
		type thing struct {
			Name string
		}

		r := require.New(t)

		var reg Registry
		reg.ProvideName("name", func() string { return "miren" })

		var x thing

		err := reg.Populate(&x)
		r.Error(err)
	})

	t.Run("returns the same value when using a provider", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.Provide(func() *thing { return &thing{} })
		reg.Register("name", "blah")

		var s struct {
			Thing *thing
		}

		err := reg.Populate(&s)
		r.NoError(err)

		r.NotNil(s.Thing)
		r.Equal(s.Thing.Name, "blah")

		var s2 struct {
			Thing *thing
		}

		err = reg.Populate(&s2)
		r.NoError(err)

		r.Same(s.Thing, s2.Thing)
	})

	t.Run("can build and populate all at once", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.Provide(func() *thing { return &thing{} })
		reg.Register("name", "blah")

		var s struct {
			Thing *thing
		}

		err := reg.Populate(&s)
		r.NoError(err)

		r.NotNil(s.Thing)
		r.Equal(s.Thing.Name, "blah")
	})

	t.Run("builders can return errors", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.Provide(func() (*thing, error) { return &thing{}, nil })
		reg.Register("name", "blah")

		var s struct {
			Thing *thing
		}

		err := reg.Populate(&s)
		r.NoError(err)

		r.NotNil(s.Thing)
		r.Equal(s.Thing.Name, "blah")
	})

	t.Run("builders errors stop building", func(t *testing.T) {
		type thing struct {
			Name string `asm:"name"`
		}

		r := require.New(t)

		var reg Registry
		reg.Provide(func() (*thing, error) { return &thing{}, fmt.Errorf("errrer") })
		reg.Register("name", "blah")

		var s struct {
			Thing *thing
		}

		err := reg.Populate(&s)
		r.Error(err)

		r.Nil(s.Thing)
	})

	t.Run("can populate based on an interface type", func(t *testing.T) {
		type thing struct {
			Writer io.Writer
		}

		r := require.New(t)

		var reg Registry

		reg.Register("writer", os.Stdout)

		var th thing

		err := reg.Populate(&th)
		r.NoError(err)

		r.NotNil(th.Writer)
		r.Equal(th.Writer, os.Stdout)
	})

	t.Run("doesn't confuse provider types", func(t *testing.T) {
		type thing struct {
			Writer *os.File
		}

		r := require.New(t)

		var reg Registry

		reg.ProvideName("writer", func() io.Writer { return nil })

		var th thing

		err := reg.Populate(&th)
		r.Error(err)
	})

	t.Run("runs a populated hook after populating", func(t *testing.T) {
		r := require.New(t)
		var reg Registry

		reg.Register("port", int(3000))

		var s Server

		err := reg.Populate(&s)
		r.NoError(err)

		r.Equal(3000, s.Port)
		r.Equal(3001, s.otherPort)
	})
}

type Server struct {
	Port int `asm:"port"`

	otherPort int
}

func (s *Server) Populated() error {
	s.otherPort = s.Port + 1
	return nil
}
