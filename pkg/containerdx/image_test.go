package containerdx

import "testing"

func TestNormalizeImageReference(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short name with tag",
			input:    "postgres:15",
			expected: "docker.io/library/postgres:15",
		},
		{
			name:     "short name without tag",
			input:    "postgres",
			expected: "docker.io/library/postgres",
		},
		{
			name:     "user/repo with tag",
			input:    "user/image:tag",
			expected: "docker.io/user/image:tag",
		},
		{
			name:     "user/repo without tag",
			input:    "user/image",
			expected: "docker.io/user/image",
		},
		{
			name:     "gcr.io fully qualified",
			input:    "gcr.io/project/image:tag",
			expected: "gcr.io/project/image:tag",
		},
		{
			name:     "localhost with port",
			input:    "localhost:5000/image:tag",
			expected: "localhost:5000/image:tag",
		},
		{
			name:     "localhost without port",
			input:    "localhost/image:tag",
			expected: "localhost/image:tag",
		},
		{
			name:     "custom registry with domain",
			input:    "registry.example.com/image:tag",
			expected: "registry.example.com/image:tag",
		},
		{
			name:     "docker.io explicitly specified",
			input:    "docker.io/library/postgres:15",
			expected: "docker.io/library/postgres:15",
		},
		{
			name:     "registry with port",
			input:    "registry.example.com:5000/image:tag",
			expected: "registry.example.com:5000/image:tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeImageReference(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeImageReference(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
