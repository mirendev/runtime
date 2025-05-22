package caauth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateCommonName(t *testing.T) {
	tests := []struct {
		name        string
		commonName  string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid simple name",
			commonName:  "myservice",
			shouldError: false,
		},
		{
			name:        "valid name with hyphen",
			commonName:  "my-service",
			shouldError: false,
		},
		{
			name:        "valid name with numbers",
			commonName:  "service123",
			shouldError: false,
		},
		{
			name:        "valid name with hyphen and numbers",
			commonName:  "my-service-123",
			shouldError: false,
		},
		{
			name:        "valid name with max length",
			commonName:  "abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz-12345", // 63 chars
			shouldError: false,
		},
		{
			name:        "valid domain name",
			commonName:  "test.example.com",
			shouldError: false,
		},
		{
			name:        "valid complex domain name",
			commonName:  "my-service.example-domain.co.uk",
			shouldError: false,
		},
		{
			name:        "invalid name starting with hyphen",
			commonName:  "-myservice",
			shouldError: true,
			errorMsg:    "invalid name format",
		},
		{
			name:        "invalid name ending with hyphen",
			commonName:  "myservice-",
			shouldError: true,
			errorMsg:    "invalid name format",
		},
		{
			name:        "invalid name with special characters",
			commonName:  "my_service",
			shouldError: true,
			errorMsg:    "invalid name format",
		},
		{
			name:        "invalid name with spaces",
			commonName:  "my service",
			shouldError: true,
			errorMsg:    "invalid name format",
		},
		{
			name:        "invalid name with uppercase",
			commonName:  "MyService",
			shouldError: false, // Uppercase is allowed in DNS-1123
		},
		{
			name:        "invalid empty name",
			commonName:  "",
			shouldError: true,
			errorMsg:    "invalid name format",
		},
		{
			name:        "reserved name - admin",
			commonName:  "admin",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
		{
			name:        "reserved name - root",
			commonName:  "root",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
		{
			name:        "reserved name - system",
			commonName:  "system",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
		{
			name:        "reserved name - kubernetes",
			commonName:  "kubernetes",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
		{
			name:        "reserved name - kube-system",
			commonName:  "kube-system",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
		{
			name:        "reserved name - runtime",
			commonName:  "runtime",
			shouldError: true,
			errorMsg:    "reserved and cannot be used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommonName(tt.commonName)
			
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIssueCertificateValidatesCommonName verifies that IssueCertificate
// properly validates the common name before issuing a certificate
func TestIssueCertificateValidatesCommonName(t *testing.T) {
	// Create a CA for testing
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * 365 * 24 * time.Hour,
	})
	assert.NoError(t, err)

	// Test with invalid common names
	invalidNames := []string{
		"",                // Empty name
		"-invalid",        // Starts with hyphen
		"invalid-",        // Ends with hyphen
		"inv@lid",         // Contains special character
		"admin",           // Reserved name
		"my name",         // Contains space
	}

	for _, name := range invalidNames {
		_, err := ca.IssueCertificate(Options{
			CommonName:   name,
			Organization: "Test Org",
			Country:      "US",
			ValidFor:     24 * time.Hour,
		})
		assert.Error(t, err, "IssueCertificate should reject invalid common name: %s", name)
	}

	// Test with valid common name
	validNames := []string{
		"valid",
		"valid-name",
		"valid123",
		"v",
		"123valid",
	}

	for _, name := range validNames {
		_, err := ca.IssueCertificate(Options{
			CommonName:   name,
			Organization: "Test Org",
			Country:      "US",
			ValidFor:     24 * time.Hour,
		})
		assert.NoError(t, err, "IssueCertificate should accept valid common name: %s", name)
	}
}