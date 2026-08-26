package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
)

const dataPath = "/data/value"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	// POST /data writes the (possibly large) request body to the disk.
	// GET  /data returns its sha256 so we can verify a restore without
	// streaming the whole payload back.
	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f, err := os.Open(dataPath)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			defer f.Close()
			h := sha256.New()
			n, err := io.Copy(h, f)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "%x %d\n", h.Sum(nil), n)
		case http.MethodPost:
			f, err := os.Create(dataPath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer f.Close()
			if _, err := io.Copy(f, r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := f.Sync(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK\n")
	})

	fmt.Printf("Server starting on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}
