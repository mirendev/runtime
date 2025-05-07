package entity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSON(t *testing.T) {
	t.Run("restores the types", func(t *testing.T) {
		r := require.New(t)

		a := Named("test")

		b, err := json.Marshal(a)
		r.NoError(err)

		var m map[string]any
		err = json.Unmarshal(b, &m)
		r.NoError(err)

		r.Equal(m["id"], string(Ident))
		r.Equal(m["v"], map[string]any{
			"k": float64(KindKeyword),
			"v": "test",
		})

		var a2 Attr

		err = json.Unmarshal(b, &a2)
		r.NoError(err)

		r.Equal(a, a2)
	})
}
