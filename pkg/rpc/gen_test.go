package rpc_test

import (
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc/example"
)

func TestGeneratedCode(t *testing.T) {
	t.Run("can deal with a union type", func(t *testing.T) {
		r := require.New(t)

		var v example.Value

		iv := v.V()

		iv.SetS("hello")
		r.Equal("hello", iv.S())

		iv.SetI(42)

		r.Equal("", iv.S())
		r.Equal(int64(42), iv.I())

		data, err := cbor.Marshal(v)
		r.NoError(err)

		var v2 example.Value

		err = cbor.Unmarshal(data, &v2)
		r.NoError(err)

		iv2 := v2.V()

		r.Equal("", iv2.S())
		r.Equal(int64(42), iv2.I())
	})
}
