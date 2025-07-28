package main

import (
	"fmt"
	"net/http"
)

func main() {
	// Intentional compilation error: undefined variable
	fmt.Println("Starting server on port", port)
	
	// Another error: wrong number of arguments
	http.HandleFunc("/", handler, "extra argument")
	
	// Missing return statement in function that should return error
	if err := startServer(); err != nil {
		fmt.Println("Server failed:", err)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	// Type mismatch error
	var count int = "not a number"
	
	w.Write([]byte(fmt.Sprintf("Hello! Count: %d", count)))
}

func startServer() error {
	// Missing return statement
	http.ListenAndServe(":8080", nil)
}