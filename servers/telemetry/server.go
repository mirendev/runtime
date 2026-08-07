package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"miren.dev/runtime/api/telemetry/telemetry_v1alpha"
)

type Server struct {
	log      *slog.Logger
	endpoint string
	headers  map[string]string
	client   *http.Client
}

var _ telemetry_v1alpha.Telemetry = (*Server)(nil)

func NewServer(log *slog.Logger) *Server {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	var headers map[string]string
	if h := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"); h != "" {
		headers = parseOTLPHeaders(h)
	}

	if endpoint != "" {
		log.Info("telemetry proxy enabled", "endpoint", endpoint)
	}

	return &Server{
		log:      log.With("module", "telemetry"),
		endpoint: endpoint,
		headers:  headers,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *Server) ReportSpans(ctx context.Context, state *telemetry_v1alpha.TelemetryReportSpans) error {
	if s.endpoint == "" {
		return nil
	}

	args := state.Args()
	data := args.SpanData()
	if len(data) == 0 {
		return nil
	}

	url := strings.TrimRight(s.endpoint, "/") + "/v1/traces"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		s.log.Warn("failed to create OTLP request", "error", err)
		return nil
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("failed to forward spans to collector", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.log.Warn("collector returned error", "status", resp.StatusCode)
	}

	return nil
}

// parseOTLPHeaders parses the OTEL_EXPORTER_OTLP_HEADERS env var format:
// key1=value1,key2=value2
func parseOTLPHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	for pair := range strings.SplitSeq(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok {
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return headers
}
