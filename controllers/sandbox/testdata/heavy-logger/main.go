package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Configuration from environment variables
var (
	port            = getEnv("PORT", "3000")
	linesPerRequest = getEnvInt("LINES_PER_REQUEST", 20)
	logLineSize     = getEnvInt("LOG_LINE_SIZE", 200)
)

var requestCounter atomic.Uint64

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// generateLogLine creates a log line of approximately the specified size
func generateLogLine(requestID uint64, lineNum int) string {
	timestamp := time.Now().Format(time.RFC3339)
	padding := strings.Repeat("x", max(0, logLineSize-100))
	return fmt.Sprintf("[%s] Request %d Line %d: This is a heavy log message %s",
		timestamp, requestID, lineNum, padding)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type Response struct {
	RequestID      uint64 `json:"requestId"`
	Message        string `json:"message"`
	LinesLogged    int    `json:"linesLogged"`
	Duration       string `json:"duration"`
	TotalRequests  uint64 `json:"totalRequests"`
	Config         Config `json:"config"`
}

type Config struct {
	LinesPerRequest int `json:"linesPerRequest"`
	LogLineSize     int `json:"logLineSize"`
}

func main() {
	// Create logger (similar to cloud server's slog usage)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("===========================================")
	slog.Info("Heavy Logger Test App Started")
	slog.Info("===========================================",
		"port", port,
		"lines_per_request", linesPerRequest,
		"log_line_size", logLineSize,
		"estimated_bytes_per_request", linesPerRequest*logLineSize)
	slog.Info("Ready to receive requests!")
	slog.Info("===========================================")

	// Background heartbeat (like cloud server's periodic logging)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			slog.Info("HEARTBEAT",
				"timestamp", time.Now().Format(time.RFC3339),
				"requests_served", requestCounter.Load())
		}
	}()

	// Main request handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestID := requestCounter.Add(1)
		start := time.Now()

		// Log request start (mimics cloud server's "Request began")
		slog.Info("Request began",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr)

		// Generate heavy logs for this request
		for i := 1; i <= linesPerRequest; i++ {
			logLine := generateLogLine(requestID, i)
			slog.Info(logLine)
		}

		// Simulate some work (like cloud server processing)
		time.Sleep(time.Duration(10+requestID%40) * time.Millisecond)

		duration := time.Since(start)

		// Log request end (mimics cloud server's "Request ended")
		slog.Info("Request ended",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", 200,
			"duration", duration.String())

		// Log stats (mimics cloud server's detailed logging)
		slog.Info("STATS",
			"total_requests", requestCounter.Load(),
			"lines_per_request", linesPerRequest)

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", fmt.Sprintf("%d", requestID))
		w.WriteHeader(http.StatusOK)

		response := Response{
			RequestID:     requestID,
			Message:       "Heavy Logger Test App",
			LinesLogged:   linesPerRequest,
			Duration:      duration.String(),
			TotalRequests: requestCounter.Load(),
			Config: Config{
				LinesPerRequest: linesPerRequest,
				LogLineSize:     logLineSize,
			},
		}

		json.NewEncoder(w).Encode(response)
	})

	// Health endpoint (no logging to avoid noise)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})

	// Start server
	addr := ":" + port
	slog.Info("Server starting", "addr", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
