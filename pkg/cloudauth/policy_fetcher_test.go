package cloudauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDebugPolicyFetcher(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"policy", 200, `{"rules":[{"name":"debug-rule"}]}`, false},
		{"server error", 500, `unavailable`, true},
		{"invalid policy", 200, `{`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/self/rbac-rules", r.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			pf := NewPolicyFetcher(srv.URL, nil)
			err := pf.Fetch(t.Context())
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, pf.GetPolicy())
			} else {
				require.NoError(t, err)
				require.Equal(t, "debug-rule", pf.GetPolicy().Rules[0].Name)
			}
		})
	}
}
