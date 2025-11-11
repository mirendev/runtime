package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {

	r, err := http.Get("http://ipconfig.io/json")
	if err != nil {
		fmt.Printf("Error fetching IP config: %v\n", err)
	} else {
		defer r.Body.Close()
		fmt.Println("Successfully fetched IP config")
		data, _ := io.ReadAll(r.Body)
		fmt.Println(string(data))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from Go!\n")
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})

	fmt.Printf("Server starting on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}
