package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCertSerial(t *testing.T) {
	const hex = "4f1a9c3e7b2d8a6f0c5e1d3b9a7f2e64"
	const decimal = "105147180391496216627680129114540289636"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"uppercase hex, as in openssl serial output", "4F1A9C3E7B2D8A6F0C5E1D3B9A7F2E64", hex},
		{"lowercase hex", hex, hex},
		{"colon-separated hex, as in openssl -text", "4f:1a:9c:3e:7b:2d:8a:6f:0c:5e:1d:3b:9a:7f:2e:64", hex},
		{"0x prefix", "0x" + hex, hex},
		{"decimal, as in the audit log", decimal, hex},
		{"surrounding whitespace from a paste", "  " + hex + "\n", hex},
		{"0x forces hex on an all-digit serial", "0x1234", "1234"},
		{"all digits read as decimal", "1234", "4d2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCertSerial(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	for _, bad := range []string{"", "   ", "0", "-1", "xyz", "0x"} {
		_, err := parseCertSerial(bad)
		require.Error(t, err, "input %q", bad)
	}
}

func TestSerialDecimal(t *testing.T) {
	require.Equal(t, "1234", serialDecimal("4d2"))
}
