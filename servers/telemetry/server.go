package telemetry

import (
	"context"
	"log/slog"
	"net/http"

	"miren.dev/runtime/api/telemetry/telemetry_v1alpha"
	"miren.dev/runtime/pkg/otlpexport"
)

// Server relays spans that CLI commands report over RPC to the cluster's
// traces collector, so a command can be traced without the operator's machine
// knowing where the collector is or holding its credentials.
type Server struct {
	log         *slog.Logger
	destination *otlpexport.Destination
	client      *http.Client
}

var _ telemetry_v1alpha.Telemetry = (*Server)(nil)

// NewServer returns a relay to destination. A nil destination accepts and
// drops spans, which is what a cluster that exports no traces should do.
func NewServer(log *slog.Logger, destination *otlpexport.Destination) *Server {
	s := &Server{
		log:         log.With("module", "telemetry"),
		destination: destination,
	}
	if destination != nil {
		s.client = destination.Client()
		log.Info("telemetry proxy enabled", "endpoint", destination.Endpoint)
	}
	return s
}

func (s *Server) ReportSpans(ctx context.Context, state *telemetry_v1alpha.TelemetryReportSpans) error {
	if s.destination == nil {
		return nil
	}

	args := state.Args()
	data := args.SpanData()
	if len(data) == 0 {
		return nil
	}

	status, err := s.destination.Forward(ctx, s.client, data, "application/x-protobuf", "")
	if err != nil {
		s.log.Warn("failed to forward spans to collector", "error", err)
		return nil
	}
	if status >= 400 {
		s.log.Warn("collector returned error", "status", status)
	}

	return nil
}
