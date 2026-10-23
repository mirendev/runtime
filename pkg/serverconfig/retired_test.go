package serverconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A server still configured for the retired distributed topology must refuse
// to start, whichever source the setting comes from, so it never boots empty
// embedded stores in place of the external ones it was pointed at.
func TestLoad_RefusesRetiredTopology(t *testing.T) {
	cases := []struct {
		name         string
		toml         string
		env          map[string]string
		flags        func() *CLIFlags
		wantContains string
	}{
		{name: "distributed mode in file", toml: `mode = "distributed"`, wantContains: `mode = "distributed"`},
		{name: "distributed mode from env", env: map[string]string{"MIREN_MODE": "distributed"}, wantContains: `mode = "distributed"`},
		{name: "external etcd", toml: "[etcd]\nstart_embedded = false", wantContains: "etcd.start_embedded = false"},
		{
			name: "external etcd from flag",
			flags: func() *CLIFlags {
				f := NewCLIFlags()
				off := false
				f.EtcdConfigStartEmbedded = &off
				return f
			},
			wantContains: "etcd.start_embedded = false",
		},
		{name: "external containerd", env: map[string]string{"MIREN_CONTAINERD_START_EMBEDDED": "false"}, wantContains: "containerd.start_embedded = false"},
		{name: "buildkit disabled", toml: "[buildkit]\nstart_embedded = false", wantContains: "buildkit.start_embedded = false"},
		{name: "embedded victoriametrics disabled", toml: "[victoriametrics]\nstart_embedded = false", wantContains: "victoriametrics.start_embedded = false"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "server.toml")
			if err := os.WriteFile(configPath, []byte(tc.toml), 0o644); err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			var flags *CLIFlags
			if tc.flags != nil {
				flags = tc.flags()
			}

			_, err := Load(configPath, flags, nil)
			if err == nil {
				t.Fatalf("Load succeeded, want an error naming %q", tc.wantContains)
			}
			if !strings.Contains(err.Error(), tc.wantContains) || !strings.Contains(err.Error(), "no longer supported") {
				t.Errorf("Load error = %q, want it to name %q as no longer supported", err, tc.wantContains)
			}
		})
	}
}

// Settings that already describe the only supported topology still load, so
// existing `--mode standalone` invocations and explicit embedded flags keep working.
func TestLoad_AcceptsStandaloneTopologySettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.toml")
	body := `mode = "standalone"
[etcd]
start_embedded = true
[containerd]
start_embedded = true
[buildkit]
start_embedded = true
[victorialogs]
start_embedded = true
[victoriametrics]
start_embedded = true
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	flags := NewCLIFlags()
	mode := "standalone"
	flags.Mode = &mode

	if _, err := Load(configPath, flags, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := DefaultConfig().ValidateRetiredTopology(); err != nil {
		t.Errorf("default config: %v", err)
	}
}

// The external addresses only took effect alongside a refused setting, and
// earlier releases' `config generate --defaults` wrote the Victoria ones into
// every config. That generated file must keep loading.
func TestLoad_AcceptsIgnoredExternalAddresses(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "generated-defaults-before-mir-2064.toml"), nil, nil); err != nil {
		t.Fatalf("Load of a config from the previous `config generate --defaults`: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "server.toml")
	body := `[etcd]
endpoints = ["http://127.0.0.1:12379"]
[buildkit]
socket_path = "/run/buildkit/buildkitd.sock"
[victorialogs]
address = "victorialogs:9428"
[victoriametrics]
address = "victoriametrics:8428"
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath, nil, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestValidateRetiredTopologyReportsEverySetting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetMode("distributed")
	cfg.Etcd.SetStartEmbedded(false)
	cfg.Buildkit.SetStartEmbedded(false)

	err := cfg.ValidateRetiredTopology()
	if err == nil {
		t.Fatal("ValidateRetiredTopology succeeded, want an error")
	}
	for _, key := range []string{`mode = "distributed"`, "etcd.start_embedded = false", "buildkit.start_embedded = false"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error = %q, want it to name %s", err, key)
		}
	}
}
