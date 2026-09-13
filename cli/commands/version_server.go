package commands

import (
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

// Mirrors components/server.ServerInfoService; that package is linux-only.
const serverInfoService = "dev.miren.runtime/server-info"

type serverVersion struct {
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	BuildDate   time.Time `json:"build_date,omitzero"`
	InstanceID  string    `json:"runtime_instance_id"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	Ready       bool      `json:"ready"`
	InstallKind string    `json:"install_kind"`
}

// errServerVersionUnsupported: the server answered but predates the version
// RPC, which itself means it is older than this CLI.
var errServerVersionUnsupported = errors.New("server does not report its version")

func fetchServerVersion(ctx *Context) (*serverVersion, error) {
	client, err := ctx.RPCClient(serverInfoService)
	if err != nil {
		if re, ok := errors.AsType[*rpc.ResolveError](err); ok && re.Kind == rpc.ResolveLookupError {
			return nil, fmt.Errorf("%w: %w", errServerVersionUnsupported, err)
		}
		return nil, err
	}
	defer client.Close()

	results, err := server_v1alpha.NewServerInfoClient(client).Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("query server version: %w", err)
	}
	info := &serverVersion{
		Version:     results.Version(),
		Commit:      results.Commit(),
		InstanceID:  results.RuntimeInstanceId(),
		Ready:       results.Ready(),
		InstallKind: results.InstallKind(),
	}
	if results.HasBuildDate() {
		info.BuildDate = standard.FromTimestamp(results.BuildDate())
	}
	if results.HasStartedAt() {
		info.StartedAt = standard.FromTimestamp(results.StartedAt())
	}
	return info, nil
}

type versionSkew int

const (
	skewNone versionSkew = iota
	skewServerBehind
	skewCLIBehind
	// skewUnordered: different builds that are not both semver releases, such
	// as two main-branch commits.
	skewUnordered
)

func compareVersions(cli, server string) versionSkew {
	if cli == server {
		return skewNone
	}
	cliSem, cliErr := release.ParseSemVer(cli)
	serverSem, serverErr := release.ParseSemVer(server)
	if cliErr != nil || serverErr != nil {
		return skewUnordered
	}
	switch {
	case cliSem.IsNewer(serverSem):
		return skewServerBehind
	case serverSem.IsNewer(cliSem):
		return skewCLIBehind
	default:
		return skewNone
	}
}
