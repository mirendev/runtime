package workloadidentity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubjectRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		build    func() (Subject, error)
		expected string
	}{
		{
			name: "sandbox with all metadata",
			build: func() (Subject, error) {
				return newSandboxSubject("org-1", "app-1", "sandbox/sb-1")
			},
			expected: "org:org-1:app:app-1:sandbox:sandbox/sb-1",
		},
		{
			name: "sandbox without organization",
			build: func() (Subject, error) {
				return newSandboxSubject("", "app-1", "sandbox/sb-1")
			},
			expected: "app:app-1:sandbox:sandbox/sb-1",
		},
		{
			name: "sandbox without app",
			build: func() (Subject, error) {
				return newSandboxSubject("org-1", "", "sandbox/sb-1")
			},
			expected: "org:org-1:sandbox:sandbox/sb-1",
		},
		{
			name: "sandbox without optional metadata",
			build: func() (Subject, error) {
				return newSandboxSubject("", "", "sandbox/sb-1")
			},
			expected: "sandbox:sandbox/sb-1",
		},
		{
			name: "system workload with all metadata",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("org-1", "cluster-1", SystemWorkloadSandboxController)
			},
			expected: "org:org-1:cluster:cluster-1:system:sandboxcontroller",
		},
		{
			name: "system workload without organization",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("", "cluster-1", SystemWorkloadSandboxController)
			},
			expected: "cluster:cluster-1:system:sandboxcontroller",
		},
		{
			name: "system workload without cluster",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("org-1", "", SystemWorkloadSandboxController)
			},
			expected: "org:org-1:system:sandboxcontroller",
		},
		{
			name: "system workload without optional metadata",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("", "", SystemWorkloadSandboxController)
			},
			expected: "system:sandboxcontroller",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, err := tt.build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, subject.String())

			parsed, err := parseSubject(subject.String())
			require.NoError(t, err)
			assert.Equal(t, subject, parsed)
		})
	}
}

func TestSubjectRejectsReservedDelimiter(t *testing.T) {
	tests := []struct {
		name  string
		build func() (Subject, error)
		field string
	}{
		{
			name:  "sandbox organization",
			build: func() (Subject, error) { return newSandboxSubject("org:1", "app-1", "sandbox/sb-1") },
			field: "org",
		},
		{
			name: "sandbox app",
			build: func() (Subject, error) {
				return newSandboxSubject("org-1", "evil:sandbox:sb-victim", "sandbox/sb-mine")
			},
			field: "app",
		},
		{
			name:  "sandbox id",
			build: func() (Subject, error) { return newSandboxSubject("org-1", "evil", "sb-victim:sandbox:sb-mine") },
			field: "sandbox",
		},
		{
			name: "system organization",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("org:1", "cluster-1", SystemWorkloadSandboxController)
			},
			field: "org",
		},
		{
			name: "system cluster",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("org-1", "cluster:1", SystemWorkloadSandboxController)
			},
			field: "cluster",
		},
		{
			name: "system workload",
			build: func() (Subject, error) {
				return newSystemWorkloadSubject("org-1", "cluster-1", SystemWorkload("bad:name"))
			},
			field: "system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.build()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "subject "+tt.field+" value")
		})
	}
}

func TestParseSubjectRejectsMalformedEncoding(t *testing.T) {
	for _, encoded := range []string{
		"",
		"org:org-1:app",
		":value",
	} {
		t.Run(encoded, func(t *testing.T) {
			_, err := parseSubject(encoded)
			require.Error(t, err)
		})
	}
}
