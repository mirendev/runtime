package stackbuild

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/imagerefs"
)

// execOp marshals a single run step and returns its exec op, so a test can see
// exactly what the step mounts and sets without solving it.
func execOp(t *testing.T, opts ...llb.RunOption) *pb.ExecOp {
	t.Helper()
	opts = append([]llb.RunOption{llb.Shlex("true")}, opts...)
	st := llb.Image("docker.io/library/busybox:latest").Run(opts...).Root()
	def, err := st.Marshal(context.Background())
	require.NoError(t, err)

	var exec *pb.ExecOp
	for _, dt := range def.Def {
		var op pb.Op
		require.NoError(t, op.Unmarshal(dt))
		if e := op.GetExec(); e != nil {
			require.Nil(t, exec, "expected a single exec op")
			exec = e
		}
	}
	require.NotNil(t, exec)
	return exec
}

func secretMounts(exec *pb.ExecOp) map[string]*pb.Mount {
	mounts := map[string]*pb.Mount{}
	for _, m := range exec.Mounts {
		if m.MountType == pb.MountType_SECRET {
			mounts[m.Dest] = m
		}
	}
	return mounts
}

func TestDepAuthMountsSecretsAtTheirTargets(t *testing.T) {
	h := &highlevelBuilder{BuildOptions{Secrets: []Secret{
		{ID: "npm", Env: "NPM_TOKEN"},
		{ID: "netrc", File: "~/.netrc"},
		{ID: "pipconf", File: "/etc/pip.conf"},
	}}}

	t.Run("as root", func(t *testing.T) {
		r := require.New(t)
		exec := execOp(t, h.rootDepAuth())

		r.Len(exec.Secretenv, 1)
		r.Equal("npm", exec.Secretenv[0].ID)
		r.Equal("NPM_TOKEN", exec.Secretenv[0].Name)

		mounts := secretMounts(exec)
		r.Len(mounts, 2)
		r.Equal("netrc", mounts["/root/.netrc"].SecretOpt.ID)
		r.Equal(uint32(0), mounts["/root/.netrc"].SecretOpt.Uid)
		r.Equal(uint32(0o400), mounts["/root/.netrc"].SecretOpt.Mode)
		r.Equal("pipconf", mounts["/etc/pip.conf"].SecretOpt.ID)

		env := exec.Meta.Env
		r.Contains(env, "GIT_CONFIG_COUNT=6")
		r.Contains(env, "GIT_CONFIG_KEY_0=url.https://github.com/.insteadOf")
		r.Contains(env, "GIT_CONFIG_VALUE_0=git@github.com:")
		r.Contains(env, "GIT_CONFIG_VALUE_1=ssh://git@github.com/")
		r.Contains(env, "CARGO_NET_GIT_FETCH_WITH_CLI=true")
		r.Contains(env, "POETRY_SYSTEM_GIT_CLIENT=true")
	})

	t.Run("as the app user", func(t *testing.T) {
		r := require.New(t)
		mounts := secretMounts(execOp(t, h.appDepAuth()))

		// "~/" follows the step's user, and the file is theirs to read.
		r.Contains(mounts, "/home/app/.netrc")
		r.Equal(uint32(2010), mounts["/home/app/.netrc"].SecretOpt.Uid)
		r.Equal(uint32(2011), mounts["/home/app/.netrc"].SecretOpt.Gid)
		r.Contains(mounts, "/etc/pip.conf")
	})
}

func TestDepAuthAddsNothingWithoutSecrets(t *testing.T) {
	r := require.New(t)
	h := &highlevelBuilder{}

	// A build with no secrets must produce the same step it always did, or
	// every app's dependency layer would rebuild on upgrade.
	plain := execOp(t)
	withAuth := execOp(t, h.rootDepAuth())
	r.Equal(plain.Meta.Env, withAuth.Meta.Env)
	r.Equal(len(plain.Mounts), len(withAuth.Mounts))
	r.Empty(withAuth.Secretenv)
}

// TestBuildSecretsReachInstallStepOnly builds a Node app whose preinstall
// script checks that its declared secrets and the forge URL rewrite are there
// while dependencies install, then checks that neither secret made it into
// the image.
func TestBuildSecretsReachInstallStepOnly(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	root := t.TempDir()
	dir := setupTestDir(root, t)

	const (
		token = "tok-8f3a1c"
		netrc = "machine github.com login x-access-token password netrc-5d2e7b\n"
	)

	// Writes a marker into /app rather than echoing, since npm may not show a
	// lifecycle script's output. The marker holds no secret.
	check := func(cond, failure string) string {
		return cond + " || { echo '" + failure + "' >&2; exit 1; }; "
	}
	// The expected values are split across shell quotes so package.json, which
	// lands in the image, never holds them whole for the leak check to find.
	preinstall := check(`test "$NPM_TOKEN" = "tok-8f"'3a1c'`, "NPM_TOKEN not set") +
		check(`grep -q "netrc-5d"'2e7b' /root/.netrc`, "netrc not mounted") +
		// The slim Node image has no git to ask, so look at the env git reads.
		check(`test "$GIT_CONFIG_VALUE_0" = git@github.com:`, "forge rewrite missing") +
		check("test -f /app/.npmrc", ".npmrc not copied") +
		"echo secrets-present > /app/secret-proof"

	pkgJSON, err := json.Marshal(map[string]any{
		"name":    "secret-test",
		"version": "1.0.0",
		"scripts": map[string]string{"preinstall": preinstall},
	})
	require.NoError(t, err)

	files := map[string]string{
		"package.json":      string(pkgJSON),
		"package-lock.json": "{}",
		// Holds a reference, not the token: the committed .npmrc pattern the
		// env target exists for.
		".npmrc":   "//registry.example.invalid/:_authToken=${NPM_TOKEN}\n" + runNonce("#"),
		"index.js": "console.log('hi')",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	opts := BuildOptions{
		Version: "20",
		Secrets: []Secret{
			{ID: "npm", Env: "NPM_TOKEN"},
			{ID: "netrc", File: "~/.netrc"},
		},
	}
	stack := &NodeStack{MetaStack: MetaStack{dir: dir}}
	stack.Init(opts)
	state, err := stack.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	secrets := map[string][]byte{"npm": []byte(token), "netrc": []byte(netrc)}
	solveLLBWithSecrets(t, startBuildkit(t), "", dir, state, secrets, func(f io.Reader) {
		var proof bool
		tr := tar.NewReader(f)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)

			data, err := io.ReadAll(tr)
			require.NoError(t, err)
			require.False(t, bytes.Contains(data, []byte(token)), "token leaked into %s", hdr.Name)
			require.False(t, bytes.Contains(data, []byte("netrc-5d2e7b")), "netrc leaked into %s", hdr.Name)
			if hdr.Name == "app/secret-proof" {
				proof = true
			}
			require.NotEqual(t, "root/.netrc", hdr.Name, "netrc mount left a file in the image")
		}
		require.True(t, proof, "preinstall did not see the secrets")
	})
}

func TestDetectGitDeps(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{
			name:  "registry deps only",
			files: map[string]string{"package.json": `{"dependencies":{"express":"^4.18.2","@scope/x":"npm:@other/y@1"}}`},
		},
		{
			// Nearly every published package.json says this, so it must not
			// count.
			name: "repository field",
			files: map[string]string{"package.json": `{"repository":{"type":"git","url":"git+https://github.com/o/r.git"},` +
				`"dependencies":{"express":"^4.18.2"}}`},
		},
		{
			name:  "local path dep",
			files: map[string]string{"package.json": `{"dependencies":{"lib":"../lib","other":"./vendor/other","w":"workspace:*"}}`},
		},
		{
			name:  "git+ssh dep",
			files: map[string]string{"package.json": `{"dependencies":{"x":"git+ssh://git@github.com/o/x.git"}}`},
			want:  true,
		},
		{
			name:  "github shorthand",
			files: map[string]string{"package.json": `{"devDependencies":{"x":"o/x#v1.2.0"}}`},
			want:  true,
		},
		{
			name: "git dep only in the lockfile",
			files: map[string]string{
				"package.json":      `{"dependencies":{"x":"^1.0.0"}}`,
				"package-lock.json": `{"packages":{"node_modules/x":{"resolved":"git+ssh://git@github.com/o/x.git#abc"}}}`,
			},
			want: true,
		},
		{
			name:  "requirements direct reference",
			files: map[string]string{"requirements.txt": "flask==3.0\nlib @ git+https://github.com/o/lib.git@v1\n"},
			want:  true,
		},
		{
			name:  "poetry git source",
			files: map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\nlib = { git = \"https://github.com/o/lib.git\" }\n"},
			want:  true,
		},
		{
			name:  "pyproject without git",
			files: map[string]string{"pyproject.toml": "[project]\ndependencies = [\"flask\"]\nurls = { repository = \"https://github.com/o/r\" }\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}
			ms := &MetaStack{dir: dir}
			ms.detectGitDeps()
			require.Equal(t, tc.want, ms.gitDeps)
		})
	}
}

// TestNodeGitDependency installs a public GitHub dependency on the Node stack,
// whose slim image has no git, so the build has to install it first.
func TestNodeGitDependency(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"package.json": `{"name":"git-dep-test","version":"1.0.0","dependencies":` +
			`{"is-plain-obj":"git+ssh://git@github.com/sindresorhus/is-plain-obj.git#v4.1.0"}}`,
		"package-lock.json": "{}",
		"index.js":          "console.log('hi')" + runNonce("//"),
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	opts := BuildOptions{Version: "20"}
	stack := &NodeStack{MetaStack: MetaStack{dir: dir}}
	stack.Init(opts)
	state, err := stack.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	solveLLB(t, startBuildkit(t), "", dir, state, func(f io.Reader) {
		var installed bool
		tr := tar.NewReader(f)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if hdr.Name == "app/node_modules/is-plain-obj/package.json" {
				installed = true
			}
		}
		require.True(t, installed, "is-plain-obj was not installed")
	})
}

// TestDepAuthRewritesForgeSSHURLs runs real git against an SSH URL on a step
// with depAuth. The build has no SSH key (this image has no ssh client at
// all), so the fetch only works if git took the rewrite to https.
func TestDepAuthRewritesForgeSSHURLs(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	h := &highlevelBuilder{BuildOptions{Secrets: []Secret{{ID: "netrc", File: "~/.netrc"}}}}
	state := llb.Image(imagerefs.GetGolangImage("1.23")).Run(
		llb.Args([]string{"sh", "-c",
			"rm -f /usr/bin/ssh && git ls-remote git@github.com:sindresorhus/is-plain-obj.git v4.1.0" + runNonce("#")}),
		h.rootDepAuth(),
	).Root()

	secrets := map[string][]byte{"netrc": []byte("machine example.invalid login x password y\n")}
	solveLLBWithSecrets(t, startBuildkit(t), "", t.TempDir(), &state, secrets)
}
