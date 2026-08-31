// remote-write-dump is a small inspection receiver for managed metrics smoke
// tests. It validates the telemetry-writer claims, decodes Prometheus Remote
// Write requests, and keeps a queryable in-memory sample window.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/snappy"

	"miren.dev/runtime/internal/remotewrite"
)

const maxRememberedSamples = 500

type sample struct {
	Metric      string            `json:"metric"`
	Labels      map[string]string `json:"labels"`
	Value       float64           `json:"value"`
	TimestampMS int64             `json:"timestamp_ms"`
	ReceivedAt  time.Time         `json:"received_at"`
}

type sampleSnapshot struct {
	Metric      string            `json:"metric"`
	Labels      map[string]string `json:"labels"`
	Value       any               `json:"value"`
	TimestampMS int64             `json:"timestamp_ms"`
	ReceivedAt  time.Time         `json:"received_at"`
}

type receiver struct {
	mu               sync.Mutex
	mode             string
	expectedAudience string
	writes           int
	rejections       int
	lastClaims       map[string]any
	samples          []sample
}

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "address to listen on")
	audience := flag.String("audience", "managed-metrics-smoke", "required JWT audience")
	flag.Parse()

	r := &receiver{mode: "accept", expectedAudience: *audience}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/write", r.write)
	mux.HandleFunc("GET /api/v1/samples", r.snapshot)
	mux.HandleFunc("POST /control", r.control)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	log.Printf("remote-write dump listening on http://%s (audience=%q)", *listen, *audience)
	log.Printf("JWT signatures are not verified by this developer tool; the integration test covers signature verification")
	log.Fatal(http.ListenAndServe(*listen, mux))
}

func (r *receiver) write(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	mode := r.mode
	r.mu.Unlock()
	switch mode {
	case "unauthorized":
		r.reject(w, http.StatusUnauthorized, "deliberate authorization failure")
		return
	case "unavailable":
		r.reject(w, http.StatusServiceUnavailable, "deliberate receiver outage")
		return
	}

	claims, err := parseAndCheckClaims(req.Header.Get("Authorization"), r.expectedAudience)
	if err != nil {
		r.reject(w, http.StatusUnauthorized, err.Error())
		return
	}
	compressed, err := io.ReadAll(io.LimitReader(req.Body, 16<<20))
	if err != nil {
		r.reject(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := snappy.Decode(nil, compressed)
	if err != nil {
		r.reject(w, http.StatusBadRequest, "snappy: "+err.Error())
		return
	}
	samples, err := parseWriteRequest(body, time.Now())
	if err != nil {
		r.reject(w, http.StatusBadRequest, "protobuf: "+err.Error())
		return
	}

	r.mu.Lock()
	r.writes++
	r.lastClaims = claims
	r.samples = append(r.samples, samples...)
	if len(r.samples) > maxRememberedSamples {
		r.samples = slices.Clone(r.samples[len(r.samples)-maxRememberedSamples:])
	}
	writes := r.writes
	r.mu.Unlock()

	metrics := make(map[string]bool)
	for _, sample := range samples {
		metrics[sample.Metric] = true
	}
	if len(samples) > 0 {
		log.Printf("accepted write=%d series=%d metrics=%v", writes, len(samples), sortedKeys(metrics))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *receiver) snapshot(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	metric := req.URL.Query().Get("metric")
	samples := make([]sampleSnapshot, 0, len(r.samples))
	for _, sample := range r.samples {
		if metric == "" || sample.Metric == metric {
			samples = append(samples, snapshotSample(sample))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mode":        r.mode,
		"writes":      r.writes,
		"rejections":  r.rejections,
		"last_claims": r.lastClaims,
		"samples":     samples,
	})
}

func snapshotSample(sample sample) sampleSnapshot {
	value := any(sample.Value)
	switch {
	case math.IsNaN(sample.Value):
		value = "NaN"
	case math.IsInf(sample.Value, 1):
		value = "+Inf"
	case math.IsInf(sample.Value, -1):
		value = "-Inf"
	}
	return sampleSnapshot{
		Metric:      sample.Metric,
		Labels:      sample.Labels,
		Value:       value,
		TimestampMS: sample.TimestampMS,
		ReceivedAt:  sample.ReceivedAt,
	}
}

func (r *receiver) control(w http.ResponseWriter, req *http.Request) {
	mode := req.URL.Query().Get("mode")
	if mode != "accept" && mode != "unauthorized" && mode != "unavailable" {
		http.Error(w, "mode must be accept, unauthorized, or unavailable", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.mode = mode
	if req.URL.Query().Get("clear") == "true" {
		r.writes = 0
		r.rejections = 0
		r.lastClaims = nil
		r.samples = nil
	}
	r.mu.Unlock()
	log.Printf("receiver mode changed to %s", mode)
	fmt.Fprintf(w, "mode=%s\n", mode)
}

func (r *receiver) reject(w http.ResponseWriter, status int, message string) {
	r.mu.Lock()
	r.rejections++
	rejections := r.rejections
	r.mu.Unlock()
	log.Printf("rejected write=%d status=%d reason=%s", rejections, status, message)
	http.Error(w, message, status)
}

func parseAndCheckClaims(header, audience string) (map[string]any, error) {
	token := strings.TrimPrefix(header, "Bearer ")
	if token == header || token == "" {
		return nil, fmt.Errorf("missing bearer token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed bearer token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding token claims: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("decoding token claims: %w", err)
	}
	if claims["identity_type"] != "system" || claims["system_workload"] != "telemetrywriter" {
		return nil, fmt.Errorf("token is not the telemetry-writer system workload")
	}
	if !hasAudience(claims["aud"], audience) {
		return nil, fmt.Errorf("token does not contain audience %q", audience)
	}
	return claims, nil
}

func hasAudience(claim any, expected string) bool {
	switch claim := claim.(type) {
	case string:
		return claim == expected
	case []any:
		return slices.ContainsFunc(claim, func(value any) bool { return value == expected })
	default:
		return false
	}
}

func parseWriteRequest(data []byte, receivedAt time.Time) ([]sample, error) {
	decoded, err := remotewrite.Decode(data)
	if err != nil {
		return nil, err
	}
	result := make([]sample, len(decoded))
	for i, decodedSample := range decoded {
		result[i] = sample{
			Metric:      decodedSample.Metric,
			Labels:      decodedSample.Labels,
			Value:       decodedSample.Value,
			TimestampMS: decodedSample.TimestampMS,
			ReceivedAt:  receivedAt,
		}
	}
	return result, nil
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
