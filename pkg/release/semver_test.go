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
		{
			input:   "main:abc123",
			wantErr: true,
		},
		{
			input:   "invalid",
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
