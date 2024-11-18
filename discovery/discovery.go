package discovery

import "net/http"

type Endpoint interface {
	ServeHTTP(w http.ResponseWriter, req *http.Request)
}
