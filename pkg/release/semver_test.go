package release

import "testing"

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		want    *SemVer
	}{
		{
			input: "v1.2.3",
			want:  &SemVer{Major: 1, Minor: 2, Patch: 3, Original: "v1.2.3"},
		},
		{
			input: "v0.1.0",
			want:  &SemVer{Major: 0, Minor: 1, Patch: 0, Original: "v0.1.0"},
		},
		{
			input: "v0.0.0-test.1",
			want:  &SemVer{Major: 0, Minor: 0, Patch: 0, Prerelease: "test.1", Original: "v0.0.0-test.1"},
		},
		{
			input: "v1.0.0-rc.1",
			want:  &SemVer{Major: 1, Minor: 0, Patch: 0, Prerelease: "rc.1", Original: "v1.0.0-rc.1"},
		},
		// Without 'v' prefix (also valid per regex)
		{
			input: "1.2.3",
			want:  &SemVer{Major: 1, Minor: 2, Patch: 3, Original: "1.2.3"},
		},
		{
			input: "0.1.0-alpha.1",
			want:  &SemVer{Major: 0, Minor: 1, Patch: 0, Prerelease: "alpha.1", Original: "0.1.0-alpha.1"},
		},
		{
			input:   "main:abc123",
			wantErr: true,
		},
		{
			input:   "invalid",
			wantErr: true,
		},
		// Invalid prerelease formats (should be rejected)
		{
			input:   "v1.0.0-test..1",
			wantErr: true,
		},
		{
			input:   "v1.0.0-.test",
			wantErr: true,
		},
		{
			input:   "v1.0.0-test.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSemVer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Major != tt.want.Major || got.Minor != tt.want.Minor ||
					got.Patch != tt.want.Patch || got.Prerelease != tt.want.Prerelease {
					t.Errorf("ParseSemVer() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestSemVerIsNewer(t *testing.T) {
	tests := []struct {
		name  string
		this  string
		other string
		want  bool
	}{
		{"major version", "v2.0.0", "v1.9.9", true},
		{"minor version", "v1.2.0", "v1.1.9", true},
		{"patch version", "v1.0.1", "v1.0.0", true},
		{"same version", "v1.0.0", "v1.0.0", false},
		{"prerelease older", "v1.0.0-rc.1", "v1.0.0", false},
		{"prerelease newer", "v1.0.0", "v1.0.0-rc.1", true},
		{"preview progression", "v0.2.0", "v0.1.0", true},
		{"test versions", "v0.0.0-test.2", "v0.0.0-test.1", true},
		// Numeric prerelease comparison (SemVer 2.0.0 spec compliance)
		{"numeric prerelease 10 > 2", "v0.0.0-test.10", "v0.0.0-test.2", true},
		{"numeric prerelease 2 < 10", "v0.0.0-test.2", "v0.0.0-test.10", false},
		{"numeric prerelease 100 > 99", "v1.0.0-rc.100", "v1.0.0-rc.99", true},
		// Alphanumeric vs numeric (numeric has lower precedence per spec)
		{"alphanumeric > numeric", "v1.0.0-alpha.1", "v1.0.0-1", true},
		{"numeric < alphanumeric", "v1.0.0-1", "v1.0.0-alpha.1", false},
		// Length differences (shorter has lower precedence)
		{"longer prerelease newer", "v1.0.0-rc.1.1", "v1.0.0-rc.1", true},
		{"shorter prerelease older", "v1.0.0-rc.1", "v1.0.0-rc.1.1", false},
		// Complex mixed cases
		{"rc.2 > rc.1", "v1.0.0-rc.2", "v1.0.0-rc.1", true},
		{"beta > alpha", "v1.0.0-beta.1", "v1.0.0-alpha.1", true},
		{"same prerelease", "v1.0.0-rc.1", "v1.0.0-rc.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			this, err := ParseSemVer(tt.this)
			if err != nil {
				t.Fatal(err)
			}
			other, err := ParseSemVer(tt.other)
			if err != nil {
				t.Fatal(err)
			}

			if got := this.IsNewer(other); got != tt.want {
				t.Errorf("%s.IsNewer(%s) = %v, want %v", tt.this, tt.other, got, tt.want)
			}
		})
	}
}
