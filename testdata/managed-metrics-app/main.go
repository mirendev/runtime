package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

var scrapes atomic.Uint64

func main() {
	port := envOr("PORT", "3000")
	mode := envOr("SMOKE_METRICS_MODE", "valid")
	instance := envOr("MIREN_RUNTIME_INSTANCE_NUM", "unknown")

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "managed metrics smoke app\nmode=%s\ninstance=%s\n", mode, instance)
	})
	http.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		switch mode {
		case "valid":
			writeMetrics(w, instance)
		case "error":
			http.Error(w, "deliberate metrics endpoint failure", http.StatusServiceUnavailable)
		case "malformed":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			fmt.Fprintln(w, "this is deliberately not valid metrics exposition")
		case "oversized":
			writeOversizedMetrics(w, instance)
		default:
			http.Error(w, "unknown SMOKE_METRICS_MODE", http.StatusInternalServerError)
		}
	})

	log.Printf("managed metrics smoke app listening on :%s (mode=%s, instance=%s)", port, mode, instance)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func writeMetrics(w http.ResponseWriter, instance string) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# HELP managed_metrics_smoke_value A recognizable value emitted by the managed metrics smoke app.")
	fmt.Fprintln(w, "# TYPE managed_metrics_smoke_value gauge")
	fmt.Fprintf(w, "managed_metrics_smoke_value{source=\"app\",instance_hint=%q,miren_app=\"spoofed\",miren_sandbox=\"spoofed\",miren_cluster=\"spoofed\"} 42\n", instance)
	fmt.Fprintln(w, "# HELP managed_metrics_smoke_scrapes_total Number of times this endpoint has been scraped.")
	fmt.Fprintln(w, "# TYPE managed_metrics_smoke_scrapes_total counter")
	fmt.Fprintf(w, "managed_metrics_smoke_scrapes_total{instance_hint=%q} %d\n", instance, scrapes.Add(1))
}

func writeOversizedMetrics(w http.ResponseWriter, instance string) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for i := 0; i < 35_000; i++ {
		fmt.Fprintf(bw, "managed_metrics_smoke_oversized{instance_hint=%q,series=%q,padding=\"0123456789012345678901234567890123456789\"} %d\n", instance, fmt.Sprintf("%05d", i), i)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
