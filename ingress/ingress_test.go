package ingress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/pkg/testutils"
)

func TestIngressHTTP(t *testing.T) {
	t.Run("returns 404 when there is no ability to answer the request", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusNotFound, resp.StatusCode)
	})

	t.Run("invokes ServeHTTP on the discovered value", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		h.Lookup.(*discovery.Memory).Register(
			"test", discovery.EndpointFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}))

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "test.miren.test"

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusTeapot, resp.StatusCode)
	})

	t.Run("can derive the application from the host", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		_, ok := h.DeriveApp(req)
		r.False(ok)

		req.Host = "test.notarealdomain.xyz"

		_, ok = h.DeriveApp(req)
		r.False(ok)

		req.Host = "test.miren.test"

		app, ok := h.DeriveApp(req)
		r.True(ok)

		r.Equal("test", app)
	})

	t.Run("can deal with lookup being slow", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		h.Lookup.(*discovery.Memory).Register(
			"test", discovery.EndpointFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}))

		h.Lookup = discovery.SlowLookup(h.Lookup, 500*time.Millisecond)

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "test.miren.test"

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusTeapot, resp.StatusCode)
	})

	t.Run("returns an error if the lookup times out", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		h.Lookup.(*discovery.Memory).Register(
			"test", discovery.EndpointFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}))

		h.Lookup = discovery.SlowLookup(h.Lookup, 2*time.Second)
		h.LookupTimeout = 100 * time.Millisecond

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "test.miren.test"

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusGatewayTimeout, resp.StatusCode)
	})

	t.Run("returns an error if the lookup fails", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		h.Lookup = discovery.FailLookup(false, errors.New("this is a failure song"))

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "test.miren.test"

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusInternalServerError, resp.StatusCode)
	})

	t.Run("returns an error if the lookup fails in the background", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject, discovery.TestInject)

		var h HTTP
		err := reg.Populate(&h)
		r.NoError(err)

		h.Lookup = discovery.FailLookup(true, errors.New("this is a failure song"))

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "test.miren.test"

		var rw httptest.ResponseRecorder

		h.ServeHTTP(&rw, req)

		resp := rw.Result()

		defer resp.Body.Close()

		r.Equal(http.StatusInternalServerError, resp.StatusCode)
	})
}
