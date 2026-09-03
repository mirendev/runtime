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
	// a person: picking from a list, or confirming an overwrite.
	codeInteractiveRequired = "interactive_required"

	// codeNoIdentities means nobody is logged in. It is separate from
	// codeIdentityError because the fix is a person running `miren login`,
	// not a different argument.
	codeNoIdentities = "no_identities"

	// codeIdentityError is an identity that was named and not found, or one
	// that has to be named and was not. Either way the caller passes a
	// different --identity, and the message says which.
	codeIdentityError = "identity_error"

	// codeCloudRequestFailed is cloud failing to answer, as opposed to
	// answering with something unwelcome.
	codeCloudRequestFailed = "cloud_request_failed"

	// codeClusterNotFound covers both a name that matched nothing and an
	// account with no clusters at all. The distinction changes the message,
	// not what the caller can do about it.
	codeClusterNotFound = "cluster_not_found"

	// codeAmbiguousCluster means the name matched clusters in more than one
	// organization, cleared by --organization.
	codeAmbiguousCluster = "ambiguous_cluster"

	codeUnknownOrganization = "unknown_organization"

	// codeClusterUnreachable is a cluster that exists and could not be reached,
	// which is the one failure worth retrying later.
	codeClusterUnreachable = "cluster_unreachable"

	// codeClusterExists is a local name already in use, cleared by --force or
	// by choosing another name with --as.
	codeClusterExists = "cluster_exists"

	// codeConfigError is the local config failing to load or to save. The
	// caller cannot fix either one by asking differently.
	codeConfigError = "config_error"

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

// clusterAddJSONDoc is the extended description the docs generator renders on
// the command's page. The codes are a contract callers write against, so they
// belong somewhere a caller can read without opening the source.
const clusterAddJSONDoc = "With `--format json`, the command prints one result document and nothing else " +
	"on stdout; progress and warnings go to stderr. A failure is reported both as a " +
	"document and as a non-zero exit status.\n\n" +
	"```json\n" +
	"{\"ok\": true, \"cluster\": {\"name\": \"prod\", \"xid\": \"cluster-...\", \"organization\": \"Acme\",\n" +
	"                          \"address\": \"10.0.0.1:8443\", \"via_cloud\": false,\n" +
	"                          \"identity\": \"cloud\", \"active\": true,\n" +
	"                          \"config_file\": \"~/.config/miren/clientconfig.d/prod.yaml\"}}\n" +
	"\n" +
	"{\"ok\": false, \"error\": {\"code\": \"cluster_not_found\", \"message\": \"no cluster named ...\"}}\n" +
	"```\n\n" +
	"`name` is the local name, which is what every other command takes. `cloud_name` " +
	"appears alongside it only when `--as` stored the cluster under a different name than " +
	"it has in Miren Cloud, and `address` is absent for a cluster reached through cloud.\n\n" +
	"Messages are written for people and will be reworded. The code is the stable part:\n\n" +
	"| Code | Meaning |\n" +
	"|------|---------|\n" +
	"| `invalid_flags` | The flags given can't mean anything together. |\n" +
	"| `interactive_required` | Answering needs a person. Name a cluster with `--cluster`, or use `--force` to overwrite. |\n" +
	"| `no_identities` | Nobody is logged in. Run `miren login`. |\n" +
	"| `identity_error` | The identity named wasn't found, or one has to be named with `--identity`. |\n" +
	"| `cloud_request_failed` | Miren Cloud didn't answer. Worth retrying. |\n" +
	"| `cluster_not_found` | No cluster by that name, or none on the account. |\n" +
	"| `ambiguous_cluster` | The name exists in more than one organization. Add `--organization`. |\n" +
	"| `unknown_organization` | No organization by that name. |\n" +
	"| `cluster_unreachable` | The cluster exists and couldn't be reached. Worth retrying. |\n" +
	"| `cluster_exists` | That local name is taken. Use `--force`, or `--as` to pick another. |\n" +
	"| `config_error` | The local config couldn't be read or written. |\n" +
	"| `unknown` | A failure with no code. Treat it as uninterpretable. |"
