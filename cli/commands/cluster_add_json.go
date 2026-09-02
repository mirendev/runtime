package commands

import (
	"errors"
	"fmt"
)

// The codes a caller can branch on. They exist because the message is written
// for a person and will be reworded; the code is the part a script is allowed
// to depend on, so each one names a distinct situation with a distinct fix.
const (
	// codeInvalidFlags is a combination of flags that cannot mean anything,
	// caught before any work is done.
	codeInvalidFlags = "invalid_flags"

	// codeInteractiveRequired is a request that can only be answered by asking
	// a person — picking from a list, or confirming an overwrite.
	codeInteractiveRequired = "interactive_required"

	codeNoIdentities       = "no_identities"
	codeMultipleIdentities = "multiple_identities"
	codeIdentityNotFound   = "identity_not_found"

	// codeCloudRequestFailed is cloud failing to answer, as opposed to
	// answering with something unwelcome.
	codeCloudRequestFailed = "cloud_request_failed"

	codeNoClusters          = "no_clusters"
	codeUnknownOrganization = "unknown_organization"
	codeClusterNotFound     = "cluster_not_found"
	codeAmbiguousCluster    = "ambiguous_cluster"

	// codeClusterUnreachable is a cluster that exists and could not be reached,
	// which is the one failure worth retrying later.
	codeClusterUnreachable = "cluster_unreachable"

	// codeClusterExists is a local name already in use, cleared by --force or
	// by choosing another name with --as.
	codeClusterExists = "cluster_exists"

	codeConfigLoadFailed  = "config_load_failed"
	codeConfigWriteFailed = "config_write_failed"
	codeCancelled         = "cancelled"

	// codeUnknown is what an error that was never given a code reports as.
	// Callers should treat it as a failure they cannot interpret.
	codeUnknown = "unknown"
)

// codedError carries a machine-readable code alongside an ordinary error, so
// the same failure can be rendered as a sentence for a person or as a code for
// a script without the two being written separately and drifting apart.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func codedErrorf(code, format string, args ...any) error {
	return &codedError{code: code, err: fmt.Errorf(format, args...)}
}

// errorCode reports the code an error carries, looking through wrapping.
func errorCode(err error) string {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return codeUnknown
}

// commandError is a failure as a caller reads it.
type commandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// addedCluster describes what was written to the config.
type addedCluster struct {
	// Name is the local name, which is what every other command takes.
	Name string `json:"name"`

	// CloudName is its name in Miren Cloud, present when it was looked up
	// there and different from the local name.
	CloudName    string `json:"cloud_name,omitempty"`
	XID          string `json:"xid,omitempty"`
	Organization string `json:"organization,omitempty"`

	// Address is empty for a cluster reached through cloud, which is dialed at
	// no address of its own.
	Address  string `json:"address,omitempty"`
	ViaCloud bool   `json:"via_cloud"`

	Identity string `json:"identity,omitempty"`

	// Insecure records that commands to this cluster send credentials over an
	// unencrypted connection, which happens with a cloud reached over http.
	Insecure bool `json:"insecure,omitempty"`

	// Active reports that this is now the cluster commands use by default.
	Active     bool   `json:"active"`
	ConfigFile string `json:"config_file,omitempty"`
}

// clusterAddResult is the document `cluster add --format json` prints. Exactly
// one of Cluster and Error is set, and OK says which without the caller having
// to check for a key's absence.
type clusterAddResult struct {
	OK      bool          `json:"ok"`
	Cluster *addedCluster `json:"cluster,omitempty"`
	Error   *commandError `json:"error,omitempty"`
}

// reportClusterAdd prints the result document.
//
// A failure is reported both ways: as a document on stdout and as a non-zero
// exit status, because a caller that only checks the exit code and one that
// only reads the document should reach the same conclusion. The error is not
// returned, since returning it would print the human-readable line too and
// leave the caller holding a document plus a stray sentence.
func reportClusterAdd(ctx *Context, added *addedCluster, err error) error {
	if err != nil {
		ctx.SetExitCode(1)
		return PrintJSON(clusterAddResult{
			Error: &commandError{Code: errorCode(err), Message: err.Error()},
		})
	}

	return PrintJSON(clusterAddResult{OK: true, Cluster: added})
}
