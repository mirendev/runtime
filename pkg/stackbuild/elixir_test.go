package stackbuild

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/tarx"
)

// copyFixture copies testdata/<name> into a fresh directory and returns it.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", name)
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		dst := filepath.Join(dir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	require.NoError(t, err)
	return dir
}

func initElixir(t *testing.T, dir string) *ElixirStack {
	t.Helper()
	s := &ElixirStack{MetaStack: MetaStack{dir: dir}}
	require.True(t, s.Detect())
	s.Init(BuildOptions{})
	return s
}

func envByName(vars []EnvVarRequirement) map[string]EnvVarRequirement {
	m := map[string]EnvVarRequirement{}
	for _, v := range vars {
		m[v.Name] = v
	}
	return m
}

func TestElixirDetect(t *testing.T) {
	t.Run("mix project", func(t *testing.T) {
		dir := copyFixture(t, "elixir")
		s := initElixir(t, dir)
		assert.Equal(t, "hello", s.releaseName)
		assert.False(t, s.hasPhoenix)
		assert.False(t, s.hasAssets)
		assert.Equal(t, "/app/bin/hello start", s.WebCommand())
	})

	t.Run("phoenix", func(t *testing.T) {
		dir := copyFixture(t, "phoenix")
		s := initElixir(t, dir)
		assert.Equal(t, "hopwatch", s.releaseName)
		assert.True(t, s.hasPhoenix)
		assert.True(t, s.hasAssets)
		assert.Equal(t, "/app/bin/hopwatch start", s.WebCommand())
	})

	t.Run("no mix.exs", func(t *testing.T) {
		s := &ElixirStack{MetaStack: MetaStack{dir: t.TempDir()}}
		assert.False(t, s.Detect())
	})

	t.Run("wins over a root package.json", func(t *testing.T) {
		dir := copyFixture(t, "phoenix")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644))
		stack, err := DetectStack(dir, BuildOptions{})
		require.NoError(t, err)
		assert.Equal(t, "elixir", stack.Name())
	})
}

func TestElixirReleaseName(t *testing.T) {
	cases := []struct {
		name   string
		mixExs string
		want   string
	}{
		{
			name:   "app name",
			mixExs: `def project, do: [app: :my_app, version: "0.1.0"]`,
			want:   "my_app",
		},
		{
			name: "explicit release wins",
			mixExs: `def project do
  [app: :my_app, releases: [web: [applications: [my_app: :permanent]]]]
end`,
			want: "web",
		},
		{
			name:   "umbrella with a release",
			mixExs: `def project, do: [apps_path: "apps", releases: [platform: [applications: [a: :permanent]]]]`,
			want:   "platform",
		},
		{
			name:   "umbrella without a release",
			mixExs: `def project, do: [apps_path: "apps", deps: [{:credo, "~> 1.7", app: false}]]`,
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mix.exs"), []byte(tc.mixExs), 0o644))
			s := initElixir(t, dir)
			assert.Equal(t, tc.want, s.releaseName)
		})
	}

	t.Run("missing release name fails the build clearly", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "mix.exs"), []byte(`def project, do: [apps_path: "apps"]`), 0o644))
		s := initElixir(t, dir)
		_, err := s.GenerateLLB(context.Background(), dir, BuildOptions{})
		assert.ErrorContains(t, err, "releases:")
		assert.Empty(t, s.WebCommand())
	})
}

func TestElixirEnvPatterns(t *testing.T) {
	cases := []struct {
		line     string
		want     string // "" means no match
		optional bool
	}{
		{`url = System.fetch_env!("API_URL")`, "API_URL", false},
		{`System.fetch_env("API_URL")`, "API_URL", true},
		{`System.get_env("DATABASE_URL") ||`, "DATABASE_URL", false},
		{`System.get_env("DATABASE_URL") || raise "missing"`, "DATABASE_URL", false},
		{`pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10"),`, "POOL_SIZE", true},
		{`port: String.to_integer(System.get_env("PORT_X", "4000"))`, "PORT_X", true},
		{`if System.get_env("ECTO_IPV6") in ~w(true 1), do: [:inet6], else: []`, "ECTO_IPV6", true},
		{`config :app, :q, System.get_env("DNS_CLUSTER_QUERY")`, "DNS_CLUSTER_QUERY", true},
		{`token = System.get_env("SLACK_WEBHOOK_URL")`, "SLACK_WEBHOOK_URL", true},
		{`msg = "#{System.get_env("GREETING")}"`, "GREETING", true},
		{`  #   keyfile: System.get_env("SOME_APP_SSL_KEY_PATH"),`, "", false},
		{`x = 1 # System.fetch_env!("TRAILING_COMMENT")`, "", false},
		{`System.get_env("lowercase")`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			var got string
			for _, p := range elixirEnvPatterns {
				if m := p.FindStringSubmatch(tc.line); m != nil {
					got = m[1]
				}
			}
			assert.Equal(t, tc.want, got)
			if tc.want != "" {
				assert.Equal(t, tc.optional, isOptionalEnvUsageGeneric(tc.line, tc.want, elixirOptionalEnvPatterns))
			}
		})
	}
}

func TestElixirEnvVars(t *testing.T) {
	t.Run("phoenix", func(t *testing.T) {
		dir := copyFixture(t, "phoenix")
		vars := envByName(initElixir(t, dir).RequiredEnvVars())

		assert.Equal(t, "required", vars["SECRET_KEY_BASE"].Confidence)
		assert.True(t, vars["SECRET_KEY_BASE"].CanGenerate)
		assert.Equal(t, "recommended", vars["PHX_HOST"].Confidence)
		// postgrex is in the lock, and runtime.exs raises without it.
		assert.Equal(t, "required", vars["DATABASE_URL"].Confidence)
		assert.Equal(t, "optional", vars["POOL_SIZE"].Confidence)
		assert.Equal(t, "optional", vars["ECTO_IPV6"].Confidence)

		for _, name := range []string{"PORT", "PHX_SERVER", "SOME_APP_SSL_KEY_PATH", "MAILGUN_API_KEY"} {
			assert.NotContains(t, vars, name)
		}
	})

	t.Run("plain mix", func(t *testing.T) {
		dir := copyFixture(t, "elixir")
		vars := envByName(initElixir(t, dir).RequiredEnvVars())
		assert.Equal(t, "required", vars["API_TOKEN"].Confidence)
		assert.Equal(t, "optional", vars["GREETING"].Confidence)
		assert.NotContains(t, vars, "SECRET_KEY_BASE")
	})

	t.Run("dev-only config is ignored", func(t *testing.T) {
		dir := copyFixture(t, "elixir")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "dev.exs"),
			[]byte(`import Config
config :hello, dev_token: System.fetch_env!("DEV_ONLY_TOKEN")
config :hello, shared: System.fetch_env!("API_TOKEN")
`), 0o644))
		vars := envByName(initElixir(t, dir).RequiredEnvVars())
		assert.NotContains(t, vars, "DEV_ONLY_TOKEN")
		assert.Contains(t, vars, "API_TOKEN")
	})

	t.Run("deps and _build are not scanned", func(t *testing.T) {
		dir := copyFixture(t, "elixir")
		for _, d := range []string{"deps/somedep/lib", "_build/prod/lib"} {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, d, "x.ex"),
				[]byte(`System.fetch_env!("FROM_DEPS")`), 0o644))
		}
		vars := envByName(initElixir(t, dir).RequiredEnvVars())
		assert.NotContains(t, vars, "FROM_DEPS")
	})
}

func TestElixir(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	dir := copyFixture(t, "elixir")
	s := initElixir(t, dir)
	state, err := s.GenerateLLB(context.Background(), dir, BuildOptions{})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		require.Contains(t, m, "app/bin/hello")
		require.Contains(t, m, "app/releases/0.1.0/sys.config")
		for name := range m {
			// The release bundles ERTS; the Elixir toolchain stays behind.
			require.NotContains(t, name, "usr/local/lib/elixir")
		}
	})

	cfg := s.Image().Config
	assert.Contains(t, cfg.Env, "MIX_ENV=prod")
	assert.Contains(t, cfg.Env, "LANG=C.UTF-8")
	assert.NotContains(t, cfg.Env, "PHX_SERVER=true")
	assert.Equal(t, "2010", cfg.User)
}

func TestElixirWithNpm(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	// A root package.json triggers the npm augmentation, which installs as
	// the app user; assets/package.json gets the Elixir stack's own install.
	dir := copyFixture(t, "elixir")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	pkg := []byte(`{"name": "x", "private": true, "version": "0.0.0"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), pkg, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "package.json"), pkg, 0o644))

	stack, err := DetectStack(dir, BuildOptions{})
	require.NoError(t, err)
	require.Equal(t, "elixir", stack.Name())
	require.True(t, stack.(*ElixirStack).assetsNpm)

	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		require.Contains(t, m, "app/bin/hello")
	})
}

func TestElixirBuildVersion(t *testing.T) {
	cases := []struct {
		version string
		want    string // tag prefix; "" means an error
		errHas  string
	}{
		{"1.18", "1.18.5-erlang-27.3.4.18-", ""},
		{"1.18.4", "1.18.5-erlang-27.3.4.18-", ""},
		{"1.18-otp-26", "1.18.5-erlang-26.2.5.21-", ""},
		{"1.20.1-otp-29", "1.20.4-erlang-29.1.1-", ""},
		{"1.19.6-erlang-28.5.0.7-debian-bookworm-20260918-slim", "1.19.6-erlang-28.5.0.7-debian-bookworm-", ""},
		{"1.9", "", "no Miren build for Elixir 1.9"},
		{"1.18-otp-29", "", "available: OTP 25, 26, 27"},
		{"latest", "", "unrecognized Elixir version"},
		{"1.19.6-erlang-28.5.0.7-debian-trixie-20260918-slim", "", "not a hexpm/elixir Debian bookworm tag"},
		{"1.19.6-erlang-28.5.0.7-alpine-3.22.1", "", "not a hexpm/elixir Debian bookworm tag"},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			dir := copyFixture(t, "elixir")
			s := initElixir(t, dir)
			b, err := s.builderImage(BuildOptions{Version: tc.version})
			if err == nil && !strings.Contains(b.tag, "-debian-bookworm-") {
				err = fmt.Errorf("not a hexpm/elixir Debian bookworm tag")
			}
			if tc.want == "" {
				assert.ErrorContains(t, err, tc.errHas)
				return
			}
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(b.tag, tc.want), b.tag)
		})
	}

	t.Run("GenerateLLB refuses a non-bookworm full tag", func(t *testing.T) {
		dir := copyFixture(t, "elixir")
		s := initElixir(t, dir)
		_, err := s.GenerateLLB(context.Background(), dir, BuildOptions{Version: "1.19.6-erlang-28.5.0.7-alpine-3.22.1"})
		assert.ErrorContains(t, err, "not a hexpm/elixir Debian bookworm tag")
	})
}

func TestElixirPinnedVersion(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		opts  BuildOptions
		want  string // tag prefix; "" means an error
		note  string // expected substring of an elixir-version event
	}{
		{
			name:  "tool-versions with an otp suffix",
			files: map[string]string{".tool-versions": "erlang 26.2.5\nelixir 1.18.4-otp-26 # pinned\n"},
			want:  "1.18.5-erlang-26.2.5.21-",
		},
		{
			name:  "tool-versions erlang line picks the otp",
			files: map[string]string{".tool-versions": "elixir 1.17.3\nerlang 25.3.2\n"},
			want:  "1.17.3-erlang-25.3.2.21-",
		},
		{
			name:  "mise string entry",
			files: map[string]string{"mise.toml": "[tools]\nelixir = \"1.20\"\n"},
			want:  "1.20.4-erlang-28.5.0.7-",
		},
		{
			name:  "mise list and table entries",
			files: map[string]string{".mise.toml": "[tools]\nelixir = [\"1.18.4-otp-27\", \"1.17\"]\nerlang = { version = \"27.3\" }\n"},
			want:  "1.18.5-erlang-27.3.4.18-",
		},
		{
			name:  "tool-versions wins over mise",
			files: map[string]string{".tool-versions": "elixir 1.17\n", "mise.toml": "[tools]\nelixir = \"1.20\"\n"},
			want:  "1.17.3-",
		},
		{
			name:  "an otp the minor doesn't build falls back to its default",
			files: map[string]string{".tool-versions": "elixir 1.18.4\nerlang 29.1\n"},
			want:  "1.18.5-erlang-27.3.4.18-",
			note:  "isn't built on OTP 29; building Elixir 1.18.5 on OTP 27.3.4.18",
		},
		{
			name:  "an unknown minor is an error, naming the file",
			files: map[string]string{".tool-versions": "elixir 1.12.3\n"},
			note:  ".tool-versions: no Miren build for Elixir 1.12",
		},
		{
			name:  "build.version wins over a pin",
			files: map[string]string{".tool-versions": "elixir 1.17\n"},
			opts:  BuildOptions{Version: "1.20"},
			want:  "1.20.4-",
		},
		{
			name: "no pin uses the default",
			want: "1.19.6-erlang-28.5.0.7-",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyFixture(t, "elixir")
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
			}
			s := initElixir(t, dir)
			b, err := s.builderImage(tc.opts)
			if tc.want == "" {
				assert.ErrorContains(t, err, tc.note)
				return
			}
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(b.tag, tc.want), b.tag)
			if tc.note != "" {
				var found bool
				for _, ev := range s.Events() {
					found = found || strings.Contains(ev.Message, tc.note)
				}
				assert.True(t, found, "expected an event containing %q", tc.note)
			}
		})
	}
}

func TestElixirWithoutConfigUsesBuildEnv(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	// mix new hasn't generated config/ since Elixir 1.9, and this module
	// reads a user env var at compile time, so the build needs both a
	// config-less dep stage and the env vars in place before mix compile.
	dir := copyFixture(t, "elixir")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "config")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", "token.ex"), []byte(`defmodule Hello.Token do
  @token System.fetch_env!("BUILD_TOKEN")
  def token, do: @token
end
`), 0o644))

	s := initElixir(t, dir)
	opts := BuildOptions{EnvVars: map[string]string{"BUILD_TOKEN": "from-miren"}}
	state, err := s.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		require.Contains(t, m, "app/bin/hello")
	})
}

func TestElixirUmbrellaPhoenix(t *testing.T) {
	// mix phx.new --umbrella declares phoenix in apps/<name>_web/mix.exs; only
	// the root mix.lock shows it at the top level.
	dir := t.TempDir()
	files := map[string]string{
		"mix.exs": `defmodule Shop.Umbrella.MixProject do
  use Mix.Project
  def project do
    [apps_path: "apps", releases: [shop: [applications: [shop: :permanent, shop_web: :permanent]]]]
  end
end
`,
		"mix.lock": `%{
  "phoenix": {:hex, :phoenix, "1.8.14", "abc", [:mix], [], "hexpm", "def"},
}
`,
		"apps/shop_web/mix.exs": `defmodule ShopWeb.MixProject do
  use Mix.Project
  def project, do: [app: :shop_web, deps: [{:phoenix, "~> 1.8"}]]
end
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	s := initElixir(t, dir)
	assert.True(t, s.umbrella)
	assert.True(t, s.hasPhoenix, "phoenix in mix.lock should count")
	assert.Equal(t, "shop", s.releaseName)
	assert.Equal(t, "/app/bin/shop start", s.WebCommand())
	assert.Equal(t, "required", envByName(s.RequiredEnvVars())["SECRET_KEY_BASE"].Confidence)

	var warned bool
	for _, ev := range s.Events() {
		warned = warned || ev.Name == "umbrella-assets"
	}
	assert.True(t, warned, "umbrella Phoenix apps should be told assets aren't built")
}
