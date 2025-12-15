package commands

import "testing"

func TestBuildFilterWithService(t *testing.T) {
	tests := []struct {
		name       string
		userFilter string
		service    string
		want       string
	}{
		{
			name:       "no service, no filter",
			userFilter: "",
			service:    "",
			want:       "",
		},
		{
			name:       "service only",
			userFilter: "",
			service:    "web",
			want:       `service:"web"`,
		},
		{
			name:       "filter only",
			userFilter: "error",
			service:    "",
			want:       "error",
		},
		{
			name:       "service and filter",
			userFilter: "error",
			service:    "web",
			want:       `service:"web" error`,
		},
		{
			name:       "service with complex filter",
			userFilter: `error -debug /timeout/`,
			service:    "worker",
			want:       `service:"worker" error -debug /timeout/`,
		},
		{
			name:       "service with spaces needs quoting",
			userFilter: "",
			service:    "my service",
			want:       `service:"my service"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterWithService(tt.userFilter, tt.service)
			if got != tt.want {
				t.Errorf("buildFilterWithService(%q, %q) = %q, want %q",
					tt.userFilter, tt.service, got, tt.want)
			}
		})
	}
}

func TestNormalizeSandboxID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc123", "sandbox/abc123"},
		{"sandbox/abc123", "sandbox/abc123"},
		{"", "sandbox/"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeSandboxID(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSandboxID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
