package httpingress

import "testing"

func TestJoinSandboxURL(t *testing.T) {
	const sandbox = "http://10.0.0.5:8080"

	cases := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "root", path: "/", want: "http://10.0.0.5:8080/"},
		{name: "path and query", path: "/.well-known/miren/admin?x=1", want: "http://10.0.0.5:8080/.well-known/miren/admin?x=1"},
		{name: "escaped path kept", path: "/a%2Fb", want: "http://10.0.0.5:8080/a%2Fb"},
		{name: "double slash stays a path", path: "//evil.example/x", want: "http://10.0.0.5:8080//evil.example/x"},
		{name: "userinfo trick", path: "@169.254.169.254/latest", wantErr: true},
		{name: "port trick", path: ":2379/v3", wantErr: true},
		{name: "absolute form", path: "http://evil.example/x", wantErr: true},
		{name: "relative", path: "admin", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := joinSandboxURL(sandbox, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("joinSandboxURL(%q) = %q, want error", tc.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("joinSandboxURL(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsMirenInternalPath(t *testing.T) {
	cases := map[string]bool{
		"/.well-known/miren/admin":        true,
		"/.well-known/miren":              true,
		"/./.well-known/miren/admin":      true,
		"/app/../.well-known/miren/admin": true,
		"//.well-known/miren/admin":       true,
		"/.well-known/acme-challenge/x":   false,
		"/.well-known/mirenx":             false,
		"/":                               false,
		"/ws":                             false,
	}

	for p, want := range cases {
		if got := isMirenInternalPath(p); got != want {
			t.Errorf("isMirenInternalPath(%q) = %v, want %v", p, got, want)
		}
	}
}
