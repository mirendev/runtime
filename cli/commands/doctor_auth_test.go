package commands

import (
	"errors"
	"testing"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
)

func authEnv(res authResult) *doctorEnv {
	return &doctorEnv{
		cfg:          &clientconfig.Config{},
		cluster:      &clientconfig.ClusterConfig{Hostname: remoteHost, Identity: "prod"},
		clusterName:  "prod",
		clusterCount: 1,
		auth:         res,
	}
}

func TestAuthenticationCheckTable(t *testing.T) {
	tests := []struct {
		name       string
		res        authResult
		wantStatus checkStatus
	}{
		{
			name:       "token with claims",
			res:        authResult{Method: "token", IdentityName: "prod", Claims: &auth.ExtendedClaims{}},
			wantStatus: checkOK,
		},
		{
			// No bearer token, so no claims, and signed in all the same.
			name:       "certificate",
			res:        authResult{Method: "certificate", IdentityName: "prod"},
			wantStatus: checkOK,
		},
		{
			name:       "token could not be obtained",
			res:        authResult{Method: "none", IdentityName: "prod", Err: errors.New("token expired")},
			wantStatus: checkWarn,
		},
		{
			name:       "identity missing",
			res:        authResult{Method: "none", Err: errors.New(`identity "prod" is not configured`)},
			wantStatus: checkWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkAuthentication(authEnv(tt.res))
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %v (summary %q)", got.Status, tt.wantStatus, got.Summary)
			}
		})
	}
}
