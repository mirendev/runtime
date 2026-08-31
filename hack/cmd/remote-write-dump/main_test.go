package main

import (
	"math"
	"testing"
)

func TestSnapshotSampleUsesJSONSafeSpecialValues(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  any
	}{
		{name: "finite", value: 42, want: float64(42)},
		{name: "stale marker", value: math.NaN(), want: "NaN"},
		{name: "positive infinity", value: math.Inf(1), want: "+Inf"},
		{name: "negative infinity", value: math.Inf(-1), want: "-Inf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotSample(sample{Value: tt.value}).Value
			if got != tt.want {
				t.Fatalf("snapshot value = %#v, want %#v", got, tt.want)
			}
		})
	}
}
