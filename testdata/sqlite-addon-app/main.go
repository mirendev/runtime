// Exercises a sqlite-provider disk: the runtime mounts /data with a
// WAL-mode database already created, and litestream replicates it to the
// coordinator while this app writes.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

const dbPath = "/data/data.db"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("failed to open %s: %v\n", dbPath, err)
		os.Exit(1)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS notes (body TEXT)`); err != nil {
		fmt.Printf("failed to create table: %v\n", err)
		os.Exit(1)
	}

	// GET returns every note, newline separated; POST appends one.
	http.HandleFunc("/notes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rows, err := db.Query(`SELECT body FROM notes ORDER BY rowid`)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			var bodies []string
			for rows.Next() {
				var body string
				if err := rows.Scan(&body); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				bodies = append(bodies, body)
			}
			if err := rows.Err(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, strings.Join(bodies, "\n"))
		case http.MethodPost:
			body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
			if err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if _, err := db.Exec(`INSERT INTO notes (body) VALUES (?)`, string(body)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Reports the journal mode so the test can confirm the runtime set up WAL.
	http.HandleFunc("/journal-mode", func(w http.ResponseWriter, r *http.Request) {
		var mode string
		if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, mode)
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
