package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

func main() {
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		log.Fatal(err)
	}
	identity := hex.EncodeToString(seed[:])
	file, err := os.OpenFile("/journal", os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	count := 0
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		count++
		if _, err := file.Write([]byte{byte(count)}); err != nil {
			log.Fatal(err)
		}
		offset, err := file.Seek(0, 1)
		if err != nil {
			log.Fatal(err)
		}
		journal, err := os.ReadFile("/journal")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, "request", count)
		w.Header().Set("Connection", "close")
		_ = json.NewEncoder(w).Encode(map[string]any{"identity": identity, "count": count, "offset": offset, "journal": hex.EncodeToString(journal)})
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
