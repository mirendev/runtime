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
		name    string
		args    []string
		env     string
		runtime string
		want    string
		wantErr bool
	}{
		{name: "config", want: "local-app"},
		{name: "legacy positional", args: []string{"legacy"}, want: "legacy"},
		{name: "flag", args: []string{"--app", "explicit"}, want: "explicit"},
		{name: "conflicting flag", args: []string{"legacy", "-a", "explicit"}, wantErr: true},
		{name: "environment", env: "from-env", want: "from-env"},
		{name: "conflicting environment", args: []string{"legacy"}, env: "from-env", wantErr: true},
		{name: "matching environment", args: []string{"legacy"}, env: "legacy", want: "legacy"},
		{name: "sandbox environment", args: []string{"legacy"}, env: "legacy", runtime: "legacy", want: "legacy"},
		{name: "flag over environment", args: []string{"-a", "explicit"}, env: "from-env", want: "explicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MIREN_APP", tc.env)
			t.Setenv("MIREN_RUNTIME_APP", tc.runtime)
			cmd := Infer("app delete", "test", AppDelete)
			require.NoError(t, cmd.fs.Parse(tc.args))
			opts := cmd.opts.Interface().(*AppDeleteOptions)
			if tc.wantErr {
				require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "disagrees with --app or MIREN_APP")
				return
			}
			require.NoError(t, opts.Validate(&GlobalFlags{}))
			require.Equal(t, tc.want, opts.App)
		})
	}
}
