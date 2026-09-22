package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/release"
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

var (
	releaseDay = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	latestMeta = &release.Metadata{Version: "v0.15.0", Commit: "aaaaaaa", BuildDate: releaseDay}
)

// versionEnv is a reachable remote cluster with latest known. The CLI is one
// release behind latest by default, so most cases override it.
func versionEnv(cli string, server *serverVersion, err error) *doctorEnv {
	return &doctorEnv{
		cliVersion:       version.Info{Version: cli},
		latest:           latestMeta,
		cfg:              &clientconfig.Config{},
		cluster:          &clientconfig.ClusterConfig{Hostname: remoteHost},
		clusterName:      "prod",
		clusterCount:     1,
		serverVersion:    server,
		serverVersionErr: err,
	}
}

func withoutLatest(env *doctorEnv, err error) *doctorEnv {
	env.latest = nil
	env.latestErr = err
	return env
}

func TestVersionCheckTable(t *testing.T) {
	lookupErr := fmt.Errorf("%w: %w", errServerVersionUnsupported,
		rpc.NewResolveLookupError(serverInfoService, remoteHost, "unknown service"))
	ready := func(v string) *serverVersion { return &serverVersion{Version: v, Ready: true} }

	tests := []struct {
		name       string
		env        *doctorEnv
		wantStatus checkStatus
		// wantSummary is the whole line unless partial is set, so that a
		// case pins the complete shape of the row and not just a fragment.
		wantSummary string
		partial     bool
		wantActions []string
	}{
		{
			name:        "everything current",
			env:         versionEnv("v0.15.0", ready("v0.15.0"), nil),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.15.0, server v0.15.0, latest v0.15.0",
		},
		{
			// The acceptance case: a remote client against an old server.
			name:        "server behind latest",
			env:         versionEnv("v0.15.0", ready("v0.9.0"), nil),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.15.0, server v0.9.0, latest v0.15.0",
			wantActions: []string{"sudo miren upgrade"},
		},
		{
			name:        "cli behind latest",
			env:         versionEnv("v0.14.0", ready("v0.15.0"), nil),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server v0.15.0, latest v0.15.0",
			wantActions: []string{"miren upgrade"},
		},
		{
			name:        "both behind latest",
			env:         versionEnv("v0.14.0", ready("v0.9.0"), nil),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server v0.9.0, latest v0.15.0",
			wantActions: []string{"miren upgrade", "sudo miren upgrade"},
		},
		{
			// Ahead of latest is not behind anything, and `miren upgrade`
			// would do nothing, so skew alone is not a warning.
			name:        "prerelease cli ahead of a current server",
			env:         versionEnv("v0.16.0-rc1", ready("v0.15.0"), nil),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.16.0-rc1, server v0.15.0, latest v0.15.0",
		},
		{
			// Main builds order by build date, the same way `miren upgrade`
			// decides. One from after the release is not behind it.
			name: "main build newer than the release",
			env: func() *doctorEnv {
				env := versionEnv("main:bbbbbbb", ready("v0.15.0"), nil)
				env.cliVersion.Commit = "bbbbbbb"
				env.cliVersion.BuildDate = releaseDay.Add(24 * time.Hour)
				return env
			}(),
			wantStatus:  checkOK,
			wantSummary: "CLI main:bbbbbbb, server v0.15.0, latest v0.15.0",
		},
		{
			name: "main build older than the release",
			env: versionEnv("v0.15.0", &serverVersion{
				Version: "main:ccccccc", Commit: "ccccccc", BuildDate: releaseDay.Add(-24 * time.Hour), Ready: true,
			}, nil),
			wantStatus:  checkWarn,
			wantSummary: "server main:ccccccc, latest v0.15.0",
			partial:     true,
			wantActions: []string{"sudo miren upgrade"},
		},
		{
			name:        "unversioned cli build is not placed",
			env:         versionEnv("unknown", ready("v0.15.0"), nil),
			wantStatus:  checkOK,
			wantSummary: "CLI unknown, server v0.15.0, latest v0.15.0",
		},
		{
			name:        "still starting is a fact not a fault",
			env:         versionEnv("v0.15.0", &serverVersion{Version: "v0.15.0"}, nil),
			wantStatus:  checkOK,
			wantSummary: "server v0.15.0 (still starting)",
			partial:     true,
		},
		{
			name:        "server predates version reporting",
			env:         versionEnv("v0.15.0", nil, lookupErr),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.15.0, server does not report its version, latest v0.15.0",
			wantActions: []string{"sudo miren upgrade"},
		},
		{
			name:        "server predates version reporting and the cli is behind too",
			env:         versionEnv("v0.14.0", nil, lookupErr),
			wantStatus:  checkWarn,
			wantSummary: "server does not report its version",
			partial:     true,
			wantActions: []string{"miren upgrade", "sudo miren upgrade"},
		},
		{
			name:        "other query failure still judges the cli",
			env:         versionEnv("v0.14.0", nil, errors.New("boom")),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server version unknown (boom), latest v0.15.0",
			wantActions: []string{"miren upgrade"},
		},
		{
			name: "unreachable server still judges the cli",
			env: func() *doctorEnv {
				env := versionEnv("v0.15.0", nil, errors.New("boom"))
				env.connErr = unreachableErr(remoteHost)
				return env
			}(),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.15.0, server unreachable, latest v0.15.0",
		},
		{
			name:        "no cluster still judges the cli",
			env:         &doctorEnv{cliVersion: version.Info{Version: "v0.14.0"}, latest: latestMeta},
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, latest v0.15.0",
			wantActions: []string{"miren upgrade"},
		},

		// Without latest, CLI-versus-server skew is the only evidence.
		{
			name:        "latest unreachable is a fact",
			env:         withoutLatest(versionEnv("v0.15.0", ready("v0.15.0"), nil), fmt.Errorf("failed to fetch metadata: %w", fmt.Errorf("Get \"http://x/latest/version.json\": %w", errors.New("connection refused")))),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.15.0, server v0.15.0, latest unknown (connection refused)",
		},
		{
			name:        "latest timed out",
			env:         withoutLatest(versionEnv("v0.15.0", ready("v0.15.0"), nil), fmt.Errorf("fetch: %w", context.DeadlineExceeded)),
			wantStatus:  checkOK,
			wantSummary: "latest unknown (asset service did not answer)",
			partial:     true,
		},
		{
			name:        "server behind cli without latest",
			env:         withoutLatest(versionEnv("v0.15.0", ready("v0.9.0"), nil), errors.New("offline")),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.15.0, server v0.9.0, latest unknown (offline)",
			wantActions: []string{"sudo miren upgrade"},
		},
		{
			name:        "cli behind server without latest",
			env:         withoutLatest(versionEnv("v0.14.0", ready("v0.15.0"), nil), errors.New("offline")),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.14.0, server v0.15.0",
			partial:     true,
			wantActions: []string{"miren upgrade"},
		},
		{
			// `miren upgrade` targets stable, so it can't close a gap to or
			// from a prerelease; the skew is a fact, not advice.
			name:        "prerelease server ahead without latest",
			env:         withoutLatest(versionEnv("v0.15.0", ready("v0.16.0-rc1"), nil), errors.New("offline")),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.15.0, server v0.16.0-rc1, latest unknown (offline) (prerelease skew)",
		},
		{
			name:        "prerelease cli ahead without latest",
			env:         withoutLatest(versionEnv("v0.16.0-rc1", ready("v0.15.0"), nil), errors.New("offline")),
			wantStatus:  checkOK,
			wantSummary: "CLI v0.16.0-rc1, server v0.15.0, latest unknown (offline) (prerelease skew)",
		},
		{
			// A prerelease behind the stable release is what `miren upgrade`
			// exists for, so that gap keeps its advice.
			name:        "prerelease cli behind a stable server without latest",
			env:         withoutLatest(versionEnv("v0.15.0-rc1", ready("v0.15.0"), nil), errors.New("offline")),
			wantStatus:  checkWarn,
			wantSummary: "CLI v0.15.0-rc1, server v0.15.0, latest unknown (offline)",
			wantActions: []string{"miren upgrade"},
		},
		{
			name:        "dev builds differ without latest",
			env:         withoutLatest(versionEnv("main:abc1234", ready("main:def5678"), nil), errors.New("offline")),
			wantStatus:  checkOK,
			wantSummary: "CLI main:abc1234, server main:def5678 (different build), latest unknown (offline)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkVersion(tt.env)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %v (summary %q)", got.Status, tt.wantStatus, got.Summary)
			}
			switch {
			case tt.partial && !strings.Contains(got.Summary, tt.wantSummary):
				t.Fatalf("summary = %q, want it to contain %q", got.Summary, tt.wantSummary)
			case !tt.partial && got.Summary != tt.wantSummary:
				t.Fatalf("summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if len(tt.wantActions) == 0 {
				if got.Problem != nil {
					t.Fatalf("unexpected problem: %+v", got.Problem)
				}
				return
			}
			if got.Problem == nil {
				t.Fatalf("expected a problem naming %v, got none", tt.wantActions)
			}
			var commands []string
			for _, a := range got.Problem.Actions {
				commands = append(commands, a.Command)
			}
			if strings.Join(commands, "|") != strings.Join(tt.wantActions, "|") {
				t.Fatalf("actions = %v, want %v", commands, tt.wantActions)
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
