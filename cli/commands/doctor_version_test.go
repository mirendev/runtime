package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/version"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		cli, server string
		want        versionSkew
	}{
		{"v0.14.0", "v0.14.0", skewNone},
		{"v0.14.0", "v0.13.2", skewServerBehind},
		{"v0.13.2", "v0.14.0", skewCLIBehind},
		{"v0.14.0", "v0.14.0-rc1", skewServerBehind},
		{"main:abc1234", "main:def5678", skewUnordered},
		{"main:abc1234", "v0.14.0", skewUnordered},
		{"unknown", "v0.14.0", skewUnordered},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s vs %s", tt.cli, tt.server), func(t *testing.T) {
			if got := compareVersions(tt.cli, tt.server); got != tt.want {
				t.Fatalf("compareVersions(%q, %q) = %v, want %v", tt.cli, tt.server, got, tt.want)
			}
		})
	}
}

func withCLIVersion(t *testing.T, v string) {
	t.Helper()
	prev := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = prev })
}

func versionEnv(server *serverVersion, err error) *doctorEnv {
	return &doctorEnv{
		cfg:              &clientconfig.Config{},
		cluster:          &clientconfig.ClusterConfig{Hostname: remoteHost},
		clusterName:      "prod",
		clusterCount:     1,
		serverVersion:    server,
		serverVersionErr: err,
	}
}

func TestVersionCheckTable(t *testing.T) {
	withCLIVersion(t, "v0.14.0")

	lookupErr := fmt.Errorf("%w: %w", errServerVersionUnsupported,
		rpc.NewResolveLookupError(serverInfoService, remoteHost, "unknown service"))

	tests := []struct {
		name        string
		env         *doctorEnv
		wantStatus  checkStatus
		wantSummary string
		wantAction  string
	}{
		{
			name:        "matching versions",
			env:         versionEnv(&serverVersion{Version: "v0.14.0", Ready: true}, nil),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.14.0, server v0.14.0",
		},
		{
			name:        "server predates version reporting",
			env:         versionEnv(nil, lookupErr),
			wantStatus:  checkWarn,
			wantSummary: "server does not report its version",
			wantAction:  "sudo miren server upgrade",
		},
		{
			name:        "server behind",
			env:         versionEnv(&serverVersion{Version: "v0.9.0", Ready: true}, nil),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server v0.9.0",
			wantAction:  "sudo miren server upgrade",
		},
		{
			name:        "cli behind",
			env:         versionEnv(&serverVersion{Version: "v0.15.0", Ready: true}, nil),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server v0.15.0",
			wantAction:  "miren upgrade",
		},
		{
			name:        "still starting is a fact not a fault",
			env:         versionEnv(&serverVersion{Version: "v0.14.0", Ready: false}, nil),
			wantStatus:  checkOK,
			wantSummary: "server still starting",
		},
		{
			name:        "dev builds differ",
			env:         versionEnv(&serverVersion{Version: "main:def5678", Ready: true}, nil),
			wantStatus:  checkOK,
			wantSummary: "different builds",
		},
		{
			name:        "other query failure is skipped",
			env:         versionEnv(nil, errors.New("boom")),
			wantStatus:  checkSkip,
			wantSummary: "could not read server version",
		},
		{
			name: "unreachable server defers to the server check",
			env: func() *doctorEnv {
				env := versionEnv(nil, errors.New("boom"))
				env.connErr = unreachableErr(remoteHost)
				return env
			}(),
			wantStatus:  checkSkip,
			wantSummary: "not reachable",
		},
		{
			name:        "no cluster",
			env:         &doctorEnv{},
			wantStatus:  checkSkip,
			wantSummary: "no cluster selected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkVersion(tt.env)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %v (summary %q)", got.Status, tt.wantStatus, got.Summary)
			}
			if !strings.Contains(got.Summary, tt.wantSummary) {
				t.Fatalf("summary = %q, want it to contain %q", got.Summary, tt.wantSummary)
			}
			if tt.wantAction == "" {
				return
			}
			if got.Problem == nil {
				t.Fatalf("expected a problem naming %q, got none", tt.wantAction)
			}
			found := false
			for _, a := range got.Problem.Actions {
				if a.Command == tt.wantAction {
					found = true
				}
			}
			if !found {
				t.Fatalf("actions %+v do not include %q", got.Problem.Actions, tt.wantAction)
			}
		})
	}
}

func TestServerVersionJSONOmitsAbsentTimestamps(t *testing.T) {
	data, err := json.Marshal(serverVersion{Version: "v0.14.0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "0001-01-01") {
		t.Fatalf("zero timestamps leaked into JSON: %s", data)
	}
}
