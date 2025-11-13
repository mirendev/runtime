package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
)

func initDB(db *sql.DB) error {
	// Create a simple key-value table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kv_store (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func main() {
	fmt.Println("Sleeping to let DB warm up also...")
	time.Sleep(5 * time.Second)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	// Get database connection from DATABASE_URL
	dbURL := os.Getenv("DATABASE_URL")
	var db *sql.DB
	var dbErr error

	if dbURL == "" {
		dbErr = fmt.Errorf("DATABASE_URL environment variable not set")
		fmt.Printf("WARNING: %v\n", dbErr)
	} else {
		db, dbErr = sql.Open("postgres", dbURL)
		if dbErr != nil {
			fmt.Printf("Error opening database connection: %v\n", dbErr)
		} else {
			// Test the connection
			if err := db.Ping(); err != nil {
				fmt.Printf("Error pinging database: %v\n", err)
				dbErr = err
			} else {
				fmt.Println("Successfully connected to database")
				defer db.Close()

				// Initialize database schema
				if err := initDB(db); err != nil {
					fmt.Printf("Error initializing database: %v\n", err)
					dbErr = err
				} else {
					fmt.Println("Database initialized successfully")
				}
			}
		}
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if dbErr != nil {
			http.Error(w, fmt.Sprintf("Database not available: %v\n\nPlease set DATABASE_URL environment variable", dbErr), http.StatusServiceUnavailable)
			return
		}
		if db == nil {
			http.Error(w, "Database connection not initialized", http.StatusServiceUnavailable)
			return
		}

		var version string
		err := db.QueryRow("SELECT version()").Scan(&version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error querying database: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "PostgreSQL Version:\n%s\n", version)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if dbErr != nil {
			http.Error(w, fmt.Sprintf("Database not configured: %v", dbErr), http.StatusServiceUnavailable)
			return
		}
		if db == nil {
			http.Error(w, "Database connection not initialized", http.StatusServiceUnavailable)
			return
		}

		err := db.Ping()
		if err != nil {
			http.Error(w, fmt.Sprintf("Database connection failed: %v", err), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK - Database connected\n")
	})

	// GET /data - List all key-value pairs
	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		if dbErr != nil || db == nil {
			http.Error(w, "Database not available", http.StatusServiceUnavailable)
			return
		}

		rows, err := db.Query("SELECT key, value, created_at FROM kv_store ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, fmt.Sprintf("Error querying data: %v", err), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		fmt.Fprintf(w, "Key-Value Store:\n")
		fmt.Fprintf(w, "================\n\n")

		count := 0
		for rows.Next() {
			var key, value, createdAt string
			if err := rows.Scan(&key, &value, &createdAt); err != nil {
				http.Error(w, fmt.Sprintf("Error scanning row: %v", err), http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "%s = %s (created: %s)\n", key, value, createdAt)
			count++
		}

		if count == 0 {
			fmt.Fprintf(w, "(no data stored yet)\n\nUse POST /set?key=mykey&value=myvalue to store data\n")
		} else {
			fmt.Fprintf(w, "\nTotal entries: %d\n", count)
		}
	})

	// POST /set?key=X&value=Y - Set a value
	http.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed, use POST", http.StatusMethodNotAllowed)
			return
		}

		if dbErr != nil || db == nil {
			http.Error(w, "Database not available", http.StatusServiceUnavailable)
			return
		}

		key := r.URL.Query().Get("key")
		value := r.URL.Query().Get("value")

		if key == "" || value == "" {
			http.Error(w, "Missing key or value parameter", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("INSERT INTO kv_store (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = $2", key, value)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error storing data: %v", err), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "OK - Stored %s = %s\n", key, value)
	})

	fmt.Printf("Server starting on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}
