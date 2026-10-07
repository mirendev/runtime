//go:build linux

package query

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestDockerContainersSnapshot(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "1" {
			t.Errorf("wrong Docker request: %s", r.URL)
		}
		fmt.Fprint(w, `[{"Id":"b2","Names":["/db"],"Image":"postgres:17","State":"exited"},{"Id":"a1","Names":["/web-api"],"Image":"app:1","State":"running"}]`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	all, err := dockerContainers(context.Background(), socket, "")
	if err != nil || len(all) != 2 || all[0].Name != "db" || all[1].Image != "app:1" || all[0].State != "exited" {
		t.Fatalf("Docker inventory: %+v, %v", all, err)
	}
	filtered, err := dockerContainers(context.Background(), socket, "web*")
	if err != nil || len(filtered) != 1 || filtered[0].ID != "a1" {
		t.Fatalf("Docker name filter: %+v, %v", filtered, err)
	}
}
