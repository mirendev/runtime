package app

import "testing"

func TestIsReservedEnvVar(t *testing.T) {
	cases := map[string]bool{
		EnvRuntimeApp:         true,
		EnvRuntimeVersion:     true,
		EnvRuntimeInstanceNum: true,
		"MIREN_APP":           true,
		"MIREN_":              true,
		"MIREN_ANYTHING":      true,
		"DATABASE_URL":        false,
		"PORT":                false,
		"MIRENISH":            false,
		"":                    false,
	}

	for key, want := range cases {
		if got := IsReservedEnvVar(key); got != want {
			t.Errorf("IsReservedEnvVar(%q) = %v, want %v", key, got, want)
		}
	}

	// Every injected name must be reserved, or user config could shadow it.
	for _, name := range RuntimeEnvNames {
		if !IsReservedEnvVar(name) {
			t.Errorf("injected var %q is not reserved", name)
		}
	}

	// The deprecated aliases are injected too, so they must be reserved as well.
	// They are covered today by the MIREN_ prefix, but assert it explicitly so a
	// future alias that escapes the prefix can't slip through unshadowed.
	for _, name := range LegacyRuntimeEnvNames {
		if !IsReservedEnvVar(name) {
			t.Errorf("legacy injected alias %q is not reserved", name)
		}
	}
}
