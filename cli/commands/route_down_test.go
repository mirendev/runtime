package commands

import (
	"strings"
	"testing"
	"time"

	// Embed the timezone database so the daylight-saving case below runs
	// everywhere rather than silently skipping where tzdata is absent.
	_ "time/tzdata"
)

func TestParseBackAt(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty stays empty", in: "", want: ""},
		{name: "whitespace stays empty", in: "   ", want: ""},
		{name: "rfc3339 passes through", in: "2026-08-12T15:00:00Z", want: "2026-08-12T15:00:00Z"},
		{name: "rfc3339 normalizes to UTC", in: "2026-08-12T08:00:00-07:00", want: "2026-08-12T15:00:00Z"},
		{name: "duration", in: "30m", want: "2026-08-12T11:00:00Z"},
		{name: "compound duration", in: "1h30m", want: "2026-08-12T12:00:00Z"},
		{name: "clock time later today", in: "15:00", want: "2026-08-12T15:00:00Z"},
		{name: "clock time with seconds", in: "15:00:30", want: "2026-08-12T15:00:30Z"},
		{name: "clock time already passed rolls to tomorrow", in: "09:00", want: "2026-08-13T09:00:00Z"},
		{name: "clock time equal to now rolls to tomorrow", in: "10:30", want: "2026-08-13T10:30:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBackAt(tt.in, now)
			if err != nil {
				t.Fatalf("parseBackAt(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseBackAt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseBackAtRejectsUnreadableInput(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)

	for _, in := range []string{"soon", "25:00", "-30m", "0s", "tomorrow at 3"} {
		if _, err := parseBackAt(in, now); err == nil {
			t.Errorf("parseBackAt(%q) should have returned an error", in)
		}
	}
}

func TestUpTarget(t *testing.T) {
	if got := upTarget("example.com", false); got != "example.com" {
		t.Errorf("upTarget for a host = %q, want example.com", got)
	}
	if got := upTarget("", true); got != "--default" {
		t.Errorf("upTarget for the default route = %q, want --default", got)
	}
}

// TestParseBackAtUsesTheCallersTimezone pins what a bare clock time means. An
// operator typing "15:00" means three in the afternoon where they are, not in
// UTC, and every other case in this file runs with a UTC "now" so the
// conversion would otherwise never be exercised.
func TestParseBackAtUsesTheCallersTimezone(t *testing.T) {
	eastern := time.FixedZone("EST", -5*60*60)

	tests := []struct {
		name string
		now  time.Time
		in   string
		want string
	}{
		{
			name: "clock time later today",
			now:  time.Date(2026, 8, 12, 10, 30, 0, 0, eastern),
			in:   "15:00",
			want: "2026-08-12T20:00:00Z",
		},
		{
			name: "clock time already passed rolls to tomorrow",
			now:  time.Date(2026, 8, 12, 16, 0, 0, 0, eastern),
			in:   "15:00",
			want: "2026-08-13T20:00:00Z",
		},
		{
			// A clock time late in the local evening lands on the next UTC day.
			name: "local evening crosses the UTC date line",
			now:  time.Date(2026, 8, 12, 20, 0, 0, 0, eastern),
			in:   "22:00",
			want: "2026-08-13T03:00:00Z",
		},
		{
			// Durations and absolute timestamps are zone-independent.
			name: "duration is unaffected by the caller's zone",
			now:  time.Date(2026, 8, 12, 10, 30, 0, 0, eastern),
			in:   "30m",
			want: "2026-08-12T16:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBackAt(tt.in, tt.now)
			if err != nil {
				t.Fatalf("parseBackAt(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseBackAt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseBackAtRejectsSkippedClockTimes covers the one hour a year that does
// not exist locally. time.Date slides it to an hour that does, which would mean
// the holding page quietly announces a time the operator never typed.
func TestParseBackAtRejectsSkippedClockTimes(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading tzdata: %v", err)
	}

	// 2026-03-08 is a US spring-forward date: 02:00 jumps straight to 03:00.
	now := time.Date(2026, 3, 8, 0, 30, 0, 0, ny)

	if _, err := parseBackAt("02:30", now); err == nil {
		t.Error("02:30 does not exist on a spring-forward date and should be refused")
	}

	// The neighbouring hours are ordinary and must still work.
	for _, in := range []string{"01:30", "03:30"} {
		if _, err := parseBackAt(in, now); err != nil {
			t.Errorf("parseBackAt(%q) should be accepted, got %v", in, err)
		}
	}
}

// TestParseBackAtAlwaysReturnsUTC pins the wire contract: whatever zone the
// operator's machine is in, and whichever input form they use, what leaves the
// CLI is UTC. The server stores it verbatim and the router does date arithmetic
// against it, so a value carrying a local offset would put the whole window an
// hour or more off for everyone reading it.
func TestParseBackAtAlwaysReturnsUTC(t *testing.T) {
	zones := []*time.Location{
		time.UTC,
		time.FixedZone("EST", -5*60*60),
		time.FixedZone("IST", 5*60*60+30*60), // a half-hour offset, which trips naive math
		time.FixedZone("NPT", 5*60*60+45*60), // and a quarter-hour one
	}

	for _, zone := range zones {
		now := time.Date(2026, 8, 12, 10, 30, 0, 0, zone)

		for _, in := range []string{"15:00", "15:00:30", "30m", "1h30m", "2027-03-04T15:00:00Z", "2027-03-04T08:00:00-07:00"} {
			got, err := parseBackAt(in, now)
			if err != nil {
				t.Fatalf("parseBackAt(%q) in %s: %v", in, zone, err)
			}

			if !strings.HasSuffix(got, "Z") {
				t.Errorf("parseBackAt(%q) in %s = %q, want a UTC timestamp ending in Z", in, zone, got)
			}

			parsed, err := time.Parse(time.RFC3339, got)
			if err != nil {
				t.Fatalf("parseBackAt(%q) in %s produced unparseable %q", in, zone, got)
			}
			if _, offset := parsed.Zone(); offset != 0 {
				t.Errorf("parseBackAt(%q) in %s carries offset %d, want 0", in, zone, offset)
			}
			if !parsed.After(now) {
				t.Errorf("parseBackAt(%q) in %s = %q, which is not after now", in, zone, got)
			}
		}
	}
}

// TestParseBackAtPreservesTheInstant checks the conversion is a change of
// representation and not a change of moment: the same wall-clock request from
// two zones names two different instants, an hour apart, exactly as it should.
func TestParseBackAtPreservesTheInstant(t *testing.T) {
	east := time.FixedZone("UTC-5", -5*60*60)
	west := time.FixedZone("UTC-6", -6*60*60)

	fromEast, err := parseBackAt("15:00", time.Date(2026, 8, 12, 10, 0, 0, 0, east))
	if err != nil {
		t.Fatal(err)
	}
	fromWest, err := parseBackAt("15:00", time.Date(2026, 8, 12, 10, 0, 0, 0, west))
	if err != nil {
		t.Fatal(err)
	}

	if fromEast != "2026-08-12T20:00:00Z" {
		t.Errorf("15:00 in UTC-5 = %q, want 2026-08-12T20:00:00Z", fromEast)
	}
	if fromWest != "2026-08-12T21:00:00Z" {
		t.Errorf("15:00 in UTC-6 = %q, want 2026-08-12T21:00:00Z", fromWest)
	}
}
