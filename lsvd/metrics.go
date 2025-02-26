package lsvd

import (
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

var (
	blocksWritten = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_blocks_written",
		Help: "The total number of blocks written",
	})

	blocksRead = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_blocks_read",
		Help: "The total number of blocks read",
	})

	blocksReadLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lsvd_blocks_read_time",
		Help:    "The total number of blocks read",
		Buckets: prometheus.DefBuckets,
	})

	blocksWriteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lsvd_blocks_write_time",
		Help:    "The latency of block writes",
		Buckets: prometheus.DefBuckets,
	})

	iops = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_iops",
		Help: "The total number of iops",
	})

	segmentsWritten = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_segments_written",
		Help: "The total number of segments written",
	})

	writtenBytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_extent_bytes_written",
		Help: "The total number of bytes written for extents",
	})

	segmentsBytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_segments_bytes_written",
		Help: "The total number of segments bytes written",
	})

	segmentTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "lsvd_segments_upload_time",
		Help:    "The time taken to upload segments",
		Buckets: prometheus.DefBuckets,
	})

	segmentTotalTime = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_segments_processing_time",
		Help: "The total time spend processing segments",
	})

	openSegments = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lsvd_segments_open",
		Help: "The total number of open segments",
	})

	extentCacheMiss = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_extent_cache_miss",
		Help: "Number of times the extent cache did not contain the entry",
	})

	extentCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_extent_cache_hits",
		Help: "Number of times the extent cache contained the entry",
	})

	readProcessing = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_read_processing",
		Help: "How many additional seconds is used by processing read requests",
	})

	compressionOverhead = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_compression_read_overhead",
		Help: "How many additional seconds is added by decompressing on reads",
	})

	sendfileResponses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_responses_sendfile",
		Help: "How many responses are replied to with sendfile",
	})

	writeResponses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_responses_write",
		Help: "How many responses are replied to with write",
	})

	inflateCache = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_cache_inflate",
		Help: "How often values from the cache are inflated to memory",
	})

	extents = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lsvd_active_extents",
		Help: "How many entries are in the extent map",
	})

	extentUpdates = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_extent_updates",
		Help: "How many times the extent map has been updated",
	})

	dataDensity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lsvd_data_density",
		Help: "What percent of the stored data is used",
	})

	gcCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_gc_cycles",
		Help: "How many times the GC has run",
	})

	gcTime = promauto.NewCounter(prometheus.CounterOpts{
		Name: "lsvd_gc_time",
		Help: "How many seconds the GC has run for",
	})
)

func counterValue(c prometheus.Counter) int64 {
	var m dto.Metric
	c.Write(&m)
	return int64(m.Counter.GetValue())
}

func counterValueFloat(c prometheus.Counter) float64 {
	var m dto.Metric
	c.Write(&m)
	return m.Counter.GetValue()
}

func gaugeValue(c prometheus.Gauge) float64 {
	var m dto.Metric
	c.Write(&m)
	return m.Gauge.GetValue()
}

func counterAsDuration(c prometheus.Counter) time.Duration {
	var m dto.Metric
	c.Write(&m)
	return time.Duration(m.Counter.GetValue() * float64(time.Second))
}

func counterAsSeconds(c prometheus.Counter) float64 {
	var m dto.Metric
	c.Write(&m)
	return m.Counter.GetValue()
}

func timeTotalValue(c prometheus.Histogram) time.Duration {
	var m dto.Metric
	c.Write(&m)

	return time.Duration(m.Histogram.GetSampleSum() * float64(time.Second))
}

func timeAvgValue(c prometheus.Histogram) time.Duration {
	var m dto.Metric
	c.Write(&m)

	samples := m.Histogram.GetSampleCount()
	if samples == 0 {
		return 0
	}

	return time.Duration(m.Histogram.GetSampleSum()*float64(time.Second)) / time.Duration(samples)
}

func LogMetrics(log *slog.Logger) {
	log.Info("disk stats",
		"written-bytes", counterValue(writtenBytes),
		"segment-bytes", counterValue(segmentsBytes),
		"segments", counterValue(segmentsWritten),
		"total-segment-process-time", counterAsDuration(segmentTotalTime),
		"extent-cache-hits", counterValue(extentCacheHits),
		"extent-cache-misses", counterValue(extentCacheMiss),
		"sendfile-responses", counterValue(sendfileResponses),
		"write-responses", counterValue(writeResponses),
		"cache-inflates", counterValue(inflateCache),
		"data-density", gaugeValue(dataDensity),
	)

	log.Info("client stats",
		"iops", counterValue(iops),
		"blocks-written", counterValue(blocksWritten),
		"blocks-read", counterValue(blocksRead),
		"block-write-latency", timeAvgValue(blocksWriteLatency),
		"block-read-latency", timeAvgValue(blocksReadLatency),
		"compression-overhead", counterAsSeconds(compressionOverhead),
		"read-processing", counterAsSeconds(readProcessing),
	)
}
