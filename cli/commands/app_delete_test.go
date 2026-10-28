package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppDeleteSelection(t *testing.T) {
	t.Setenv("MIREN_RUNTIME_APP", "")
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "local-app"`)
	chdir(t, dir)
	for _, tc := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{name: "config", want: "local-app"},
		{name: "legacy positional", args: []string{"legacy"}, want: "legacy"},
		{name: "flag", args: []string{"--app", "explicit"}, want: "explicit"},
		{name: "flag over positional", args: []string{"legacy", "-a", "explicit"}, want: "explicit"},
		{name: "environment", env: "from-env", want: "from-env"},
		{name: "environment over positional", args: []string{"legacy"}, env: "from-env", want: "from-env"},
		{name: "flag over environment", args: []string{"-a", "explicit"}, env: "from-env", want: "explicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MIREN_APP", tc.env)
			cmd := Infer("app delete", "test", AppDelete)
			require.NoError(t, cmd.fs.Parse(tc.args))
			opts := cmd.opts.Interface().(*AppDeleteOptions)
			require.NoError(t, opts.Validate(&GlobalFlags{}))
			require.Equal(t, tc.want, opts.App)
		})
	}
}
