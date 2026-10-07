package metricspush

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// reservedPrefix is the label namespace the runtime stamps. A push that uses it
// at all is refused rather than overridden.
//
// Refusing is not just stricter than overriding, it is the only option that
// holds. vmagent's extra_label does replace a label the payload also carries,
// but it cannot remove one, so a payload's miren_sandbox would survive on an
// app-scoped push. And relabel rules see both copies of a duplicated label, so
// no rule keyed on a stamped label can trust what it matches.
const reservedPrefix = "miren_"

// reservedLabel reports whether name would land in the runtime's namespace once
// stored. OTLP attribute keys are normalized to Prometheus names on the way in,
// so "miren.app" is checked as the miren_app it becomes.
func reservedLabel(name string) bool {
	return strings.HasPrefix(normalizeLabelName(name), reservedPrefix)
}

func normalizeLabelName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// counterSuffixes are the name endings Prometheus conventions reserve for
// cumulative series. An untyped sample carrying one is almost certainly a
// counter or part of a histogram, and app scope refuses it on that evidence.
var counterSuffixes = []string{"_total", "_count", "_sum", "_bucket", "_created"}

func counterShapedName(name string) bool {
	for _, suffix := range counterSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// preparePrometheus decodes a Pushgateway-style body, validates it for scope,
// and returns it re-encoded as the text format vmagent imports.
//
// Re-encoding is what lets one import path take both of the formats Pushgateway
// clients send. The Go client defaults to delimited protobuf; most others send
// text.
func preparePrometheus(body []byte, contentType string, scope Scope) ([]byte, error) {
	format := expfmt.ResponseFormat(http.Header{"Content-Type": {contentType}})
	if format.FormatType() == expfmt.TypeUnknown {
		// Pushgateway treats an unlabeled body as text, and so do the clients
		// that send one.
		format = expfmt.NewFormat(expfmt.TypeTextPlain)
	}

	var (
		out    bytes.Buffer
		series int
	)
	decoder := expfmt.NewDecoder(bytes.NewReader(body), format)
	for {
		var family dto.MetricFamily
		err := decoder.Decode(&family)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errorf(http.StatusBadRequest, "decoding metrics: %v", err)
		}

		name := family.GetName()
		// vmagent's text import reads classic names only, and so does every
		// query anyone will write against these series.
		if !model.LegacyValidation.IsValidMetricName(name) {
			return nil, errorf(http.StatusBadRequest, "metric name %q is not a valid Prometheus name", name)
		}
		if scope == ScopeApp {
			if err := appScopeAllowsFamily(&family); err != nil {
				return nil, err
			}
		}
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				if err := checkLabelName(label.GetName()); err != nil {
					return nil, err
				}
			}
			// A timestamp on a push would let a workload write into the past
			// of a series. Pushgateway refuses them for the same reason.
			if m.TimestampMs != nil {
				return nil, errorf(http.StatusBadRequest, "metric %q carries a timestamp; pushed samples are stamped on arrival", name)
			}
		}

		series += familySeries(&family)
		if series > maxSeriesPerPush {
			return nil, errorf(http.StatusRequestEntityTooLarge, "push exceeds %d series", maxSeriesPerPush)
		}

		if _, err := expfmt.MetricFamilyToText(&out, &family); err != nil {
			return nil, errorf(http.StatusBadRequest, "encoding metric %q: %v", name, err)
		}
	}
	return out.Bytes(), nil
}

// familySeries counts the series a family becomes once stored, which is what
// the scrape path's sample_limit counts. A histogram is a series per bucket
// plus _sum and _count, and a summary a series per quantile plus the same two.
func familySeries(family *dto.MetricFamily) int {
	n := 0
	for _, m := range family.GetMetric() {
		switch {
		case m.GetHistogram() != nil:
			n += len(m.GetHistogram().GetBucket()) + 2
		case m.GetSummary() != nil:
			n += len(m.GetSummary().GetQuantile()) + 2
		default:
			n++
		}
	}
	return n
}

func appScopeAllowsFamily(family *dto.MetricFamily) error {
	name := family.GetName()
	switch family.GetType() {
	case dto.MetricType_GAUGE:
		return nil
	case dto.MetricType_UNTYPED:
		if counterShapedName(name) {
			return errorf(http.StatusBadRequest,
				"metric %q looks like a counter; app scope takes only gauges, so push it at sandbox scope", name)
		}
		return nil
	case dto.MetricType_COUNTER, dto.MetricType_SUMMARY, dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
		return errorf(http.StatusBadRequest,
			"metric %q is a %s; app scope takes only gauges, so push it at sandbox scope",
			name, strings.ToLower(family.GetType().String()))
	default:
		return errorf(http.StatusBadRequest, "metric %q has unknown type %d", name, family.GetType())
	}
}

func checkLabelName(name string) error {
	if !model.LegacyValidation.IsValidLabelName(name) {
		return errorf(http.StatusBadRequest, "label name %q is not a valid Prometheus name", name)
	}
	if reservedLabel(name) {
		return errorf(http.StatusBadRequest, "label %q is reserved: the runtime sets %s* labels from the workload's identity", name, reservedPrefix)
	}
	return nil
}

// validateOTLP checks an OTLP export request for scope. The body is forwarded
// untouched, so this only has to decide whether it may pass.
func validateOTLP(body []byte, scope Scope) error {
	var req colmetrics.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		return errorf(http.StatusBadRequest, "decoding OTLP metrics: %v", err)
	}

	series := 0
	for _, rm := range req.GetResourceMetrics() {
		// vmagent turns resource attributes into labels on every series the
		// resource carries, so they are checked like any other label.
		if err := checkAttributes(rm.GetResource().GetAttributes()); err != nil {
			return err
		}
		for _, sm := range rm.GetScopeMetrics() {
			if err := checkAttributes(sm.GetScope().GetAttributes()); err != nil {
				return err
			}
			for _, m := range sm.GetMetrics() {
				if scope == ScopeApp {
					if err := appScopeAllowsOTLP(m); err != nil {
						return err
					}
				}
				n, err := checkOTLPPoints(m)
				if err != nil {
					return err
				}
				series += n
				if series > maxSeriesPerPush {
					return errorf(http.StatusRequestEntityTooLarge, "push exceeds %d series", maxSeriesPerPush)
				}
			}
		}
	}
	return nil
}

func checkAttributes(attrs []*common.KeyValue) error {
	for _, kv := range attrs {
		if reservedLabel(kv.GetKey()) {
			return errorf(http.StatusBadRequest, "attribute %q is reserved: the runtime sets %s* labels from the workload's identity", kv.GetKey(), reservedPrefix)
		}
	}
	return nil
}

// appScopeAllowsOTLP admits what can safely share one series across writers.
// OTLP says outright what a metric is, so unlike the Prometheus path this needs
// no guessing from names. A non-monotonic cumulative sum (an UpDownCounter)
// reports an absolute level, which is a gauge in all but name.
func appScopeAllowsOTLP(m *metrics.Metric) error {
	switch data := m.GetData().(type) {
	case *metrics.Metric_Gauge:
		return nil
	case *metrics.Metric_Sum:
		if !data.Sum.GetIsMonotonic() &&
			data.Sum.GetAggregationTemporality() == metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
			return nil
		}
		return errorf(http.StatusBadRequest,
			"metric %q is a counter; app scope takes only gauges, so push it at sandbox scope", m.GetName())
	default:
		return errorf(http.StatusBadRequest,
			"metric %q is a histogram or summary; app scope takes only gauges, so push it at sandbox scope", m.GetName())
	}
}

// checkOTLPPoints checks every data point's attributes and returns the series
// the metric becomes once stored, counted the way familySeries counts them.
func checkOTLPPoints(m *metrics.Metric) (int, error) {
	var points int
	check := func(attrs []*common.KeyValue, series int) error {
		points += series
		return checkAttributes(attrs)
	}
	switch data := m.GetData().(type) {
	case *metrics.Metric_Gauge:
		for _, p := range data.Gauge.GetDataPoints() {
			if err := check(p.GetAttributes(), 1); err != nil {
				return 0, err
			}
		}
	case *metrics.Metric_Sum:
		for _, p := range data.Sum.GetDataPoints() {
			if err := check(p.GetAttributes(), 1); err != nil {
				return 0, err
			}
		}
	case *metrics.Metric_Histogram:
		for _, p := range data.Histogram.GetDataPoints() {
			if err := check(p.GetAttributes(), len(p.GetBucketCounts())+2); err != nil {
				return 0, err
			}
		}
	case *metrics.Metric_ExponentialHistogram:
		for _, p := range data.ExponentialHistogram.GetDataPoints() {
			// vmagent stores these as vmrange buckets, at most one per
			// populated bucket on either side of zero.
			buckets := len(p.GetPositive().GetBucketCounts()) + len(p.GetNegative().GetBucketCounts())
			if err := check(p.GetAttributes(), buckets+2); err != nil {
				return 0, err
			}
		}
	case *metrics.Metric_Summary:
		for _, p := range data.Summary.GetDataPoints() {
			if err := check(p.GetAttributes(), len(p.GetQuantileValues())+2); err != nil {
				return 0, err
			}
		}
	}
	return points, nil
}
