package containerd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuncVersionReadsFirstLine(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\necho 'runc version 1.2.2'\necho 'commit: v1.2.2-0-g7cb36325'\necho 'spec: 1.2.0'\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "runc"), []byte(script), 0755))

	require.Equal(t, "1.2.2", runcVersion(context.Background(), binDir))
}

func TestRuncVersionIsEmptyWithoutRunc(t *testing.T) {
	require.Equal(t, "", runcVersion(context.Background(), t.TempDir()))
}
