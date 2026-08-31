package commands

import "testing"

func TestFormatSource(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		value string
		want  string
	}{
		{name: "image", kind: "image", value: "docker.io/library/nginx:alpine", want: "image docker.io/library/nginx:alpine"},
		{name: "dockerfile", kind: "dockerfile", want: "dockerfile"},
		{name: "detected stack", kind: "stack", value: "python", want: "python (auto-detected)"},
		{name: "unknown kind", kind: "archive", value: "source.tar.gz", want: "archive source.tar.gz"},
		{name: "unknown kind without value", kind: "archive", want: "archive"},
		{name: "missing", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSource(tt.kind, tt.value); got != tt.want {
				t.Fatalf("formatSource(%q, %q) = %q, want %q", tt.kind, tt.value, got, tt.want)
			}
		})
	}
}

func TestAnalysisSourceFallsBackToStack(t *testing.T) {
	tests := []struct {
		stack     string
		kind      string
		value     string
		wantKind  string
		wantValue string
	}{
		{stack: "python", wantKind: "stack", wantValue: "python"},
		{stack: "dockerfile", wantKind: "dockerfile"},
		{stack: "image", wantKind: "image"},
		{stack: "python", kind: "image", value: "example.com/app:v1", wantKind: "image", wantValue: "example.com/app:v1"},
	}

	for _, tt := range tests {
		gotKind, gotValue := analysisSource(tt.stack, tt.kind, tt.value)
		if gotKind != tt.wantKind || gotValue != tt.wantValue {
			t.Fatalf("analysisSource(%q, %q, %q) = (%q, %q), want (%q, %q)",
				tt.stack, tt.kind, tt.value, gotKind, gotValue, tt.wantKind, tt.wantValue)
		}
	}
}
