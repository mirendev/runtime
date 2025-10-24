package core_v1alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsServiceConcurrencyEmpty(t *testing.T) {
	tests := []struct {
		name     string
		sc       ServiceConcurrency
		expected bool
	}{
		{
			name:     "completely empty",
			sc:       ServiceConcurrency{},
			expected: true,
		},
		{
			name: "has mode",
			sc: ServiceConcurrency{
				Mode: "auto",
			},
			expected: false,
		},
		{
			name: "has requests per instance",
			sc: ServiceConcurrency{
				RequestsPerInstance: 10,
			},
			expected: false,
		},
		{
			name: "has num instances",
			sc: ServiceConcurrency{
				NumInstances: 1,
			},
			expected: false,
		},
		{
			name: "has scale down delay",
			sc: ServiceConcurrency{
				ScaleDownDelay: "15m",
			},
			expected: false,
		},
		{
			name: "fully populated auto mode",
			sc: ServiceConcurrency{
				Mode:                "auto",
				RequestsPerInstance: 10,
				ScaleDownDelay:      "15m",
			},
			expected: false,
		},
		{
			name: "fully populated fixed mode",
			sc: ServiceConcurrency{
				Mode:         "fixed",
				NumInstances: 2,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServiceConcurrencyEmpty(&tt.sc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
