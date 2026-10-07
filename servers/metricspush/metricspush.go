// Package metricspush lets a workload export metrics by pushing them, for
// workloads that have nothing a scraper could reach, such as a background worker
// with no HTTP listener.
//
// A push travels in two hops. The workload sends it to a relay on its own node's
// bridge router, which identifies the sandbox the same way the token server
// does and attaches a workload token minted for it. The relay hands the push to
// the coordinator, which verifies that token, derives every miren_* label from
// the sandbox it names, and forwards the samples to the managed-metrics vmagent.
// Nothing the workload sends can choose which app its samples are filed under.
package metricspush

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	// Audience scopes the token a relay attaches to a push. A sandbox token
	// minted for anything else, including the default "miren" audience that
	// every sandbox has on disk, does not verify here.
	Audience = "miren-metrics-push"

	// TokenHeader carries the sandbox's workload token from relay to
	// coordinator. Not Authorization, for the same reason runnertelemetry
	// avoids it: on a cloud-registered cluster the listener's authenticator
	// treats an Authorization header as a cloud JWT and fails hard on anything
	// else, which would reject a relay's otherwise-valid mTLS request.
	TokenHeader = "Miren-Workload-Token"

	// ScopeHeader, FormatHeader and GroupingHeader describe the push on the
	// relay-to-coordinator hop. The workload never sets them; the relay derives
	// them from the path the workload pushed to.
	ScopeHeader    = "Miren-Push-Scope"
	FormatHeader   = "Miren-Push-Format"
	GroupingHeader = "Miren-Push-Grouping"

	// IngestPath is where a coordinator accepts pushes from relays.
	IngestPath = "/_push/metrics"

	// maxPushBytes bounds a single push, after decompression. The scrape path
	// caps a response at 2MiB; a push gets more room because it may batch what
	// several scrapes would have collected.
	maxPushBytes = 8 << 20

	// maxSeriesPerPush matches the scrape path's sample_limit, so pushing is
	// not a way around the cardinality bound scraping enforces.
	maxSeriesPerPush = 10000
)

// IngestPattern is the ServeMux pattern the coordinator mounts. It pins one
// method and one path, so the route cannot be used to reach anything else
// vmagent serves.
var IngestPattern = http.MethodPost + " " + IngestPath

// StatusPattern answers whether this cluster accepts pushes at all, so a
// runner can decide whether to advertise push to the sandboxes it starts.
var StatusPattern = http.MethodGet + " " + IngestPath

// Scope says which series a push lands on.
type Scope string

const (
	// ScopeSandbox files samples under the sandbox that pushed them, with the
	// same labels a scrape of that sandbox would carry. It is correct for every
	// kind of metric.
	ScopeSandbox Scope = "sandbox"

	// ScopeApp files samples under the app and service alone, with no
	// per-sandbox labels, so every writer lands on the same series. It is for
	// values every writer agrees on, like a row count read from a shared
	// database: any number of replicas can push one and sum() still reads it
	// once. Counters and histograms are refused, because interleaving
	// cumulative values from different processes on one series is garbage.
	ScopeApp Scope = "app"
)

func (s Scope) valid() bool {
	return s == ScopeSandbox || s == ScopeApp
}

// Format is the wire format of a push body.
type Format string

const (
	// FormatPrometheus is the Prometheus exposition format a Pushgateway
	// accepts, as text or as delimited protobuf.
	FormatPrometheus Format = "prometheus"

	// FormatOTLP is an OTLP/HTTP metrics export request in protobuf.
	FormatOTLP Format = "otlp"
)

func (f Format) valid() bool {
	return f == FormatPrometheus || f == FormatOTLP
}

// Push is one batch of samples on its way from a relay to the coordinator.
type Push struct {
	// Token is the sandbox's workload token, minted for Audience.
	Token string

	Scope  Scope
	Format Format

	// Grouping is the Pushgateway grouping key from the push path, job
	// included. Every sample in the push gets these labels. Prometheus only.
	Grouping map[string]string

	// ContentType and ContentEncoding are the workload's, passed through.
	ContentType     string
	ContentEncoding string

	Body []byte
}

// Error is a push failure with the HTTP status it should surface as, so a
// relay can hand the workload the coordinator's verdict rather than a generic
// failure.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s", e.Status, e.Message)
}

func errorf(status int, format string, args ...any) error {
	return &Error{Status: status, Message: fmt.Sprintf(format, args...)}
}

// writeError reports err to an HTTP caller. A push error carries the status and
// message meant for the workload; anything else is an internal failure whose
// detail belongs in our logs, not in the workload's.
func writeError(w http.ResponseWriter, err error) {
	if pe, ok := errors.AsType[*Error](err); ok {
		http.Error(w, pe.Message, pe.Status)
		return
	}
	http.Error(w, "metrics push failed", http.StatusBadGateway)
}
