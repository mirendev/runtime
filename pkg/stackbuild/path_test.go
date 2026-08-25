package stackbuild

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestPathFromEnv(t *testing.T) {
	require.Equal(t, "/opt/bin:/usr/bin", pathFromEnv([]string{"NODE_ENV=production", "PATH=/opt/bin:/usr/bin"}))
	require.Equal(t, "", pathFromEnv([]string{"NODE_ENV=production"}))
	require.Equal(t, "", pathFromEnv(nil))
	// First PATH wins.
	require.Equal(t, "/first", pathFromEnv([]string{"PATH=/first", "PATH=/second"}))
}

func TestSetResultEnv(t *testing.T) {
	var s MetaStack
	s.setupResult() // seeds a default PATH

	// Replacing PATH keeps a single PATH entry.
	s.setResultEnv("PATH", "/opt/bin:/usr/bin")
	require.Equal(t, "/opt/bin:/usr/bin", pathFromEnv(s.result.Config.Env))
	require.Equal(t, 1, countPrefix(s.result.Config.Env, "PATH="))

	// A new key is appended without disturbing PATH.
	s.setResultEnv("NODE_ENV", "production")
	require.Contains(t, s.result.Config.Env, "NODE_ENV=production")
	require.Equal(t, "/opt/bin:/usr/bin", pathFromEnv(s.result.Config.Env))
}

// fakeMetaResolver returns a fixed image config, or an error, for any ref.
type fakeMetaResolver struct {
	cfg []byte
	err error
}

func (f fakeMetaResolver) ResolveImageConfig(ctx context.Context, ref string, opt sourceresolver.Opt) (string, digest.Digest, []byte, error) {
	if f.err != nil {
		return "", "", nil, f.err
	}
	return ref, "", f.cfg, nil
}

func imageConfigJSON(t *testing.T, env []string) []byte {
	t.Helper()
	var img ocispecs.Image
	img.Config.Env = env
	data, err := json.Marshal(img)
	require.NoError(t, err)
	return data
}

// envValue returns the value for key in an OCI-style env slice, or "" if absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			return e[len(prefix):]
		}
	}
	return ""
}

func TestInheritBaseEnv(t *testing.T) {
	t.Run("adopts upstream PATH over the default", func(t *testing.T) {
		var s MetaStack
		s.setupResult()
		mr := fakeMetaResolver{cfg: imageConfigJSON(t, []string{"PATH=/usr/local/node/bin:/usr/local/bin:/usr/bin"})}
		s.inheritBaseEnv(context.Background(), mr, "bun:1", BuildOptions{})
		require.Equal(t, "/usr/local/node/bin:/usr/local/bin:/usr/bin", pathFromEnv(s.result.Config.Env))
		require.Equal(t, 1, countPrefix(s.result.Config.Env, "PATH="))
	})

	t.Run("inherits other env vars, not just PATH", func(t *testing.T) {
		var s MetaStack
		s.setupResult()
		mr := fakeMetaResolver{cfg: imageConfigJSON(t, []string{
			"LANG=C.UTF-8",
			"NODE_VERSION=20.11.0",
			"GEM_HOME=/usr/local/bundle",
		})}
		s.inheritBaseEnv(context.Background(), mr, "node:20", BuildOptions{})
		require.Equal(t, "C.UTF-8", envValue(s.result.Config.Env, "LANG"))
		require.Equal(t, "20.11.0", envValue(s.result.Config.Env, "NODE_VERSION"))
		require.Equal(t, "/usr/local/bundle", envValue(s.result.Config.Env, "GEM_HOME"))
	})

	t.Run("stack's deliberate env wins over the base", func(t *testing.T) {
		var s MetaStack
		s.setupResult()
		// Stack sets a value before inheritance runs.
		s.AddEnv("BUNDLE_WITHOUT", "development")
		mr := fakeMetaResolver{cfg: imageConfigJSON(t, []string{"BUNDLE_WITHOUT=test", "GEM_HOME=/usr/local/bundle"})}
		s.inheritBaseEnv(context.Background(), mr, "ruby:3.4", BuildOptions{})
		// Base does not clobber the stack's deliberate choice, but still fills in
		// vars the stack didn't set.
		require.Equal(t, "development", envValue(s.result.Config.Env, "BUNDLE_WITHOUT"))
		require.Equal(t, 1, countPrefix(s.result.Config.Env, "BUNDLE_WITHOUT="))
		require.Equal(t, "/usr/local/bundle", envValue(s.result.Config.Env, "GEM_HOME"))
	})

	t.Run("keeps default when base declares no PATH", func(t *testing.T) {
		var s MetaStack
		s.setupResult()
		def := pathFromEnv(s.result.Config.Env)
		mr := fakeMetaResolver{cfg: imageConfigJSON(t, []string{"NODE_ENV=production"})}
		s.inheritBaseEnv(context.Background(), mr, "bun:1", BuildOptions{})
		require.Equal(t, def, pathFromEnv(s.result.Config.Env))
	})

	t.Run("keeps defaults on resolve error", func(t *testing.T) {
		var s MetaStack
		s.setupResult()
		def := pathFromEnv(s.result.Config.Env)
		mr := fakeMetaResolver{err: context.DeadlineExceeded}
		s.inheritBaseEnv(context.Background(), mr, "bun:1", BuildOptions{})
		require.Equal(t, def, pathFromEnv(s.result.Config.Env))
	})
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}
