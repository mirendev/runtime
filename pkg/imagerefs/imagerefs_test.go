package imagerefs

import "testing"

func TestGetRubyImage(t *testing.T) {
	cases := map[string]string{
		// With our pull-through caching registry, patch-level versions
		// are fully preserved instead of being truncated to major.minor.
		"3.3.7": "oci.miren.cloud/ruby:3.3.7-slim",
		"3.3.0": "oci.miren.cloud/ruby:3.3.0-slim",
		"3.4":   "oci.miren.cloud/ruby:3.4-slim",
		"4.0":   "oci.miren.cloud/ruby:4.0-slim",
	}
	for in, want := range cases {
		if got := GetRubyImage(in); got != want {
			t.Errorf("GetRubyImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetGolangImage(t *testing.T) {
	cases := map[string]string{
		// Go builds on the glibc/bookworm variant (MIR-1248), and the
		// pull-through registry preserves full patch-level versions instead
		// of truncating to major.minor.
		"1.21.5": "oci.miren.cloud/golang:1.21.5-bookworm",
		"1.21.0": "oci.miren.cloud/golang:1.21.0-bookworm",
		"1.22":   "oci.miren.cloud/golang:1.22-bookworm",
		"1.23":   "oci.miren.cloud/golang:1.23-bookworm",
	}
	for in, want := range cases {
		if got := GetGolangImage(in); got != want {
			t.Errorf("GetGolangImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetElixirImage(t *testing.T) {
	// hexpm has no floating version tags, so the full tag passes through.
	want := "oci.miren.cloud/hexpm/elixir:" + ElixirDefaultTag
	if got := GetElixirImage(ElixirDefaultTag); got != want {
		t.Errorf("GetElixirImage(%q) = %q, want %q", ElixirDefaultTag, got, want)
	}
}

func TestElixirTag(t *testing.T) {
	if ElixirDefaultTag != "1.19.6-erlang-28.5.0.7-debian-bookworm-20260918-slim" {
		t.Errorf("ElixirDefaultTag = %q", ElixirDefaultTag)
	}
	if got := ElixirTag("1.20", "29"); got != "1.20.4-erlang-29.1.1-debian-bookworm-20260918-slim" {
		t.Errorf("ElixirTag(1.20, 29) = %q", got)
	}
	if got := ElixirTag("1.20", "24"); got != "" {
		t.Errorf("unsupported OTP should be empty, got %q", got)
	}
	if got := ElixirTag("1.9", "22"); got != "" {
		t.Errorf("unknown minor should be empty, got %q", got)
	}
	for minor, rel := range ElixirReleases {
		if _, ok := rel.OTP[rel.DefaultOTP]; !ok {
			t.Errorf("Elixir %s default OTP %s isn't in its OTP map", minor, rel.DefaultOTP)
		}
	}
}
