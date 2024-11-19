package discovery

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPEndpoint(t *testing.T) {
	t.Run("sends the request to a server at the host", func(t *testing.T) {
		r := require.New(t)

		var (
			hit bool
			ff  string
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			hit = true
			ff = req.Header.Get("X-Forwarded-For")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "hello from the handler")
		})

		serv := httptest.NewServer(handler)

		defer serv.Close()

		var he HTTPEndpoint

		he.Host = serv.URL

		rw := httptest.NewRecorder()

		req, err := http.NewRequest("GET", "/", nil)
		r.NoError(err)

		req.RemoteAddr = "1.1.1.1:3333"

		he.ServeHTTP(rw, req)

		r.True(hit)
		r.Equal(http.StatusOK, rw.Code)

		r.Equal("hello from the handler\n", rw.Body.String())

		r.Equal("1.1.1.1", ff)
	})
}
