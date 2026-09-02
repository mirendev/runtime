// Package cloudapitest provides a stand-in for Miren Cloud's HTTP API, so
// tests that need cloud to answer something can have it answer without a cloud.
package cloudapitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"

	"miren.dev/runtime/pkg/cloudapi"
)

// Server is a fake Miren Cloud serving the endpoints cloudapi.Client calls. It
// records which clusters were asked about, which is how a test checks that a
// caller asked about the clusters it should have and left the rest alone.
type Server struct {
	*httptest.Server

	mu    sync.Mutex
	asked []string
}

// NewServer returns a fake cloud holding the given clusters, reporting online
// for the cluster ids set to true in online. Close it when the test is done.
//
// Pass a non-OK status to make every request fail, which is how a test tells
// "cloud said no" apart from "cloud would not answer".
func NewServer(clusters []cloudapi.Cluster, online map[string]bool, status int) *Server {
	fake := &Server{}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/users/clusters", func(w http.ResponseWriter, r *http.Request) {
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, map[string]any{"clusters": clusters})
	})

	mux.HandleFunc("/api/v1/clusters/{xid}/online", func(w http.ResponseWriter, r *http.Request) {
		xid := r.PathValue("xid")

		fake.mu.Lock()
		fake.asked = append(fake.asked, xid)
		fake.mu.Unlock()

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, map[string]any{"cluster_xid": xid, "online": online[xid]})
	})

	fake.Server = httptest.NewServer(mux)

	return fake
}

// Asked returns the cluster ids whose presence was checked, in request order.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
