package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBranchOf(t *testing.T) {
	cases := map[string]string{
		"v0.15.0":          "v0.15.0",
		"v0.16.0-rc.1":     "v0.16.0-rc.1",
		"main:abc1234":     "main",
		"HEAD:abc1234":     "main",
		"feature:abc1234":  "feature",
		"release/x:abc123": "release/x",
		"dev":              "dev",
		"unknown":          "",
		"":                 "",
	}

	for in, want := range cases {
		require.Equal(t, want, BranchOf(in), "BranchOf(%q)", in)
	}
}

func TestBranch(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "HEAD:abc1234"
	require.Equal(t, "main", Branch())

	Version = "v0.15.0"
	require.Equal(t, "v0.15.0", Branch())
}
