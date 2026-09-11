package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/mflags"
	"miren.dev/runtime/appconfig"
)

func TestTokenizeCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "simple words",
			input: "app logs -f",
			want:  []string{"app", "logs", "-f"},
		},
		{
			name:  "double quoted string",
			input: `app exec -i --reuse "bin/rails console"`,
			want:  []string{"app", "exec", "-i", "--reuse", "bin/rails console"},
		},
		{
			name:  "single quoted string",
			input: `app exec -i 'bin/rails console'`,
			want:  []string{"app", "exec", "-i", "bin/rails console"},
		},
		{
			name:  "escaped quote in double quotes",
			input: `echo "hello \"world\""`,
			want:  []string{"echo", `hello "world"`},
		},
		{
			name:  "multiple spaces between words",
			input: "app   logs   -f",
			want:  []string{"app", "logs", "-f"},
		},
		{
			name:    "unterminated double quote",
			input:   `app exec "unclosed`,
			wantErr: true,
		},
		{
			name:    "unterminated single quote",
			input:   `app exec 'unclosed`,
			wantErr: true,
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "only spaces",
			input: "   ",
			want:  nil,
		},
		{
			name:  "single word",
			input: "version",
			want:  []string{"version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokenizeCommand(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestExpandAlias(t *testing.T) {
	newDispatcher := func() *mflags.Dispatcher {
		d := mflags.NewDispatcher("miren")
		fs := mflags.NewFlagSet("version")
		d.Dispatch("version", mflags.NewCommand(fs, func(fs *mflags.FlagSet, args []string) error {
			return nil
		}, mflags.WithUsage("Print version")))
		fs2 := mflags.NewFlagSet("app list")
		d.Dispatch("app list", mflags.NewCommand(fs2, func(fs *mflags.FlagSet, args []string) error {
			return nil
		}, mflags.WithUsage("List apps")))
		return d
	}

	t.Run("no args returns unchanged", func(t *testing.T) {
		d := newDispatcher()
		got, err := expandAlias(d, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("no config returns unchanged", func(t *testing.T) {
		d := newDispatcher()
		args := []string{"foo", "bar"}
		got, err := expandAlias(d, nil, args)
		require.NoError(t, err)
		assert.Equal(t, args, got)
	})

	ac := &appconfig.AppConfig{Aliases: map[string]string{
		"ls":      "app list",
		"ls all":  "app list --all",
		"console": `app exec -i "bin/rails console"`,
		"version": "app list",
		"bad":     `app exec "unclosed`,
	}}

	t.Run("expands and keeps trailing args", func(t *testing.T) {
		got, err := expandAlias(newDispatcher(), ac, []string{"ls", "-f", "json"})
		require.NoError(t, err)
		assert.Equal(t, []string{"app", "list", "-f", "json"}, got)
	})

	t.Run("longest prefix wins", func(t *testing.T) {
		got, err := expandAlias(newDispatcher(), ac, []string{"ls", "all"})
		require.NoError(t, err)
		assert.Equal(t, []string{"app", "list", "--all"}, got)
	})

	t.Run("quoted target tokens survive", func(t *testing.T) {
		got, err := expandAlias(newDispatcher(), ac, []string{"console"})
		require.NoError(t, err)
		assert.Equal(t, []string{"app", "exec", "-i", "bin/rails console"}, got)
	})

	t.Run("no match returns unchanged", func(t *testing.T) {
		args := []string{"app", "list"}
		got, err := expandAlias(newDispatcher(), ac, args)
		require.NoError(t, err)
		assert.Equal(t, args, got)
	})

	t.Run("alias shadowing a built-in is an error", func(t *testing.T) {
		_, err := expandAlias(newDispatcher(), ac, []string{"version"})
		require.ErrorContains(t, err, "shadows built-in command")
	})

	t.Run("untokenizable target is an error", func(t *testing.T) {
		_, err := expandAlias(newDispatcher(), ac, []string{"bad"})
		require.ErrorContains(t, err, `invalid alias "bad"`)
	})
}
