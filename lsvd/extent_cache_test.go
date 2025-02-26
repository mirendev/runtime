package lsvd

import (
	"os"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

func TestExtentCache(t *testing.T) {
	t.Run("makeRoom", func(t *testing.T) {
		r := require.New(t)

		tmp, err := os.CreateTemp("", "")
		r.NoError(err)

		defer tmp.Close()
		defer os.Remove(tmp.Name())

		ec, err := NewExtentCache(hclog.L(), tmp.Name())
		r.NoError(err)

		ec.blocks = maxBlocks

		ec.inUse.Add("blah", lruEntry{ext: Extent{0, 100}})

		ec.db.Update(func(tx *bbolt.Tx) error {
			buk := tx.Bucket(extentsBucket)

			if err := ec.makeRoom(buk, 1); err != nil {
				return err
			}

			return nil
		})

		r.Equal((maxBlocks - 100), ec.blocks)
		r.Equal(0, ec.inUse.Len())
	})
}
