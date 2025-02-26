package urlx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAsMysqlDSN(t *testing.T) {
	url := "mysql://user:password@localhost:3306/database"
	dsn := "user:password@tcp(localhost:3306)/database"

	got, err := AsMysqlDSN(url)
	require.NoError(t, err)

	require.Equal(t, dsn, got)

}
