package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// stringSliceField reads a []string option off a parsed command by field name.
// The option structs are anonymous, so the fields cannot be type-asserted.
func stringSliceField(t *testing.T, cmd *Cmd, name string) []string {
	t.Helper()
	f := cmd.opts.Elem().FieldByName(name)
	require.True(t, f.IsValid(), "field %s", name)
	return f.Interface().([]string)
}

func TestEnvSetKeepsCommasAndAcceptsBareArgs(t *testing.T) {
	cmd := Infer("env set", "test", EnvSet)
	require.NoError(t, cmd.fs.Parse([]string{
		"-e", "A=1,2", "B=3,4", "-s", "SECRET=x,y", "--env=C=5", "D=6", "-eE=7,8",
	}))

	require.Equal(t, []string{"A=1,2", "C=5", "E=7,8"}, stringSliceField(t, cmd, "Env"))
	require.Equal(t, []string{"SECRET=x,y"}, stringSliceField(t, cmd, "Sensitive"))
	require.Equal(t, []string{"B=3,4", "D=6"}, stringSliceField(t, cmd, "Args"))
}

func TestDeployKeepsCommasInEnvFlags(t *testing.T) {
	cmd := Infer("deploy", "test", Deploy)
	require.NoError(t, cmd.fs.Parse([]string{"-e", "A=1,2", "-s", "SECRET=x,y", "-e", "B=3"}))

	require.Equal(t, []string{"A=1,2", "B=3"}, stringSliceField(t, cmd, "Env"))
	require.Equal(t, []string{"SECRET=x,y"}, stringSliceField(t, cmd, "Sensitive"))
}

func TestAuthProviderGitHubOrgTeamsSurviveParsing(t *testing.T) {
	cmd := Infer("auth provider add github", "test", AuthProviderAddGitHub)
	require.NoError(t, cmd.fs.Parse([]string{
		"gh", "--client-id", "id", "--client-secret", "secret",
		"--org", "mirendev:platform,eng", "--org", "other",
	}))

	orgs := stringSliceField(t, cmd, "Orgs")
	require.Equal(t, []string{"mirendev:platform,eng", "other"}, orgs)
}
