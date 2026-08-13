package oncalendar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestParseAndNext(t *testing.T) {
	tests := []struct {
		name string
		expr string
		from string
		want string
	}{
		{
			name: "weekday at a fixed time",
			expr: "Mon *-*-* 09:00:00",
			from: "2026-08-04T12:00:00Z", // a Tuesday
			want: "2026-08-10T09:00:00Z",
		},
		{
			name: "same day, later",
			expr: "Mon *-*-* 09:00:00",
			from: "2026-08-10T08:59:59Z",
			want: "2026-08-10T09:00:00Z",
		},
		{
			name: "strictly after, so an exact match rolls forward a week",
			expr: "Mon *-*-* 09:00:00",
			from: "2026-08-10T09:00:00Z",
			want: "2026-08-17T09:00:00Z",
		},
		{
			name: "six-hourly step",
			expr: "*-*-* 00/6:00:00",
			from: "2026-08-04T07:30:00Z",
			want: "2026-08-04T12:00:00Z",
		},
		{
			name: "six-hourly step wraps the day",
			expr: "*-*-* 00/6:00:00",
			from: "2026-08-04T18:00:01Z",
			want: "2026-08-05T00:00:00Z",
		},
		{
			name: "half-hourly step",
			expr: "*-*-* *:00/30:00",
			from: "2026-08-04T07:29:00Z",
			want: "2026-08-04T07:30:00Z",
		},
		{
			name: "month rollover",
			expr: "*-*-01 00:00:00",
			from: "2026-08-04T00:00:00Z",
			want: "2026-09-01T00:00:00Z",
		},
		{
			name: "year rollover",
			expr: "*-01-01 00:00:00",
			from: "2026-08-04T00:00:00Z",
			want: "2027-01-01T00:00:00Z",
		},
		{
			name: "leap day skips non-leap years",
			expr: "*-02-29 00:00:00",
			from: "2026-03-01T00:00:00Z",
			want: "2028-02-29T00:00:00Z",
		},
		{
			name: "keyword daily",
			expr: "daily",
			from: "2026-08-04T13:00:00Z",
			want: "2026-08-05T00:00:00Z",
		},
		{
			name: "keyword hourly",
			expr: "hourly",
			from: "2026-08-04T13:00:01Z",
			want: "2026-08-04T14:00:00Z",
		},
		{
			name: "keyword weekly is Monday midnight",
			expr: "weekly",
			from: "2026-08-04T13:00:00Z",
			want: "2026-08-10T00:00:00Z",
		},
		{
			name: "keyword monthly",
			expr: "monthly",
			from: "2026-08-04T13:00:00Z",
			want: "2026-09-01T00:00:00Z",
		},
		{
			name: "time only means every day",
			expr: "12:30",
			from: "2026-08-04T13:00:00Z",
			want: "2026-08-05T12:30:00Z",
		},
		{
			name: "day-of-week range",
			expr: "Mon..Fri *-*-* 09:00:00",
			from: "2026-08-08T00:00:00Z", // Saturday
			want: "2026-08-10T09:00:00Z", // Monday
		},
		{
			name: "day-of-week range wraps the week",
			expr: "Fri..Mon *-*-* 09:00:00",
			from: "2026-08-05T12:00:00Z", // Wednesday
			want: "2026-08-07T09:00:00Z", // Friday
		},
		{
			name: "day-of-week list",
			expr: "Mon,Thu *-*-* 09:00:00",
			from: "2026-08-04T12:00:00Z", // Tuesday
			want: "2026-08-06T09:00:00Z", // Thursday
		},
		{
			name: "explicit far-future year is reachable",
			expr: "2040-01-01 00:00:00",
			from: "2026-08-04T00:00:00Z",
			want: "2040-01-01T00:00:00Z",
		},
		{
			name: "MM-DD form leaves the year open",
			expr: "12-25 08:00:00",
			from: "2026-08-04T00:00:00Z",
			want: "2026-12-25T08:00:00Z",
		},
		{
			name: "minute list",
			expr: "*-*-* *:00,15,30,45:00",
			from: "2026-08-04T07:16:00Z",
			want: "2026-08-04T07:30:00Z",
		},
		{
			name: "hour range",
			expr: "*-*-* 09..17:00:00",
			from: "2026-08-04T18:00:00Z",
			want: "2026-08-05T09:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.expr)
			require.NoError(t, err)

			got, ok := e.Next(at(tt.from))
			require.True(t, ok, "expected a firing time")
			assert.Equal(t, at(tt.want), got)
		})
	}
}

// Next must be a pure function of the expression and the instant, so every
// replica derives the same ticks without coordinating.
func TestNextIsDeterministicAndUTC(t *testing.T) {
	e, err := Parse("Mon *-*-* 09:00:00")
	require.NoError(t, err)

	// The same instant expressed in a different zone must yield the same tick.
	utc := at("2026-08-04T12:00:00Z")
	tokyo := utc.In(time.FixedZone("JST", 9*60*60))

	a, ok := e.Next(utc)
	require.True(t, ok)
	b, ok := e.Next(tokyo)
	require.True(t, ok)

	assert.True(t, a.Equal(b))
	assert.Equal(t, time.UTC, a.Location())
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"too many parts", "Mon *-*-* 09:00:00 extra"},
		{"unknown keyword", "fortnightly"},
		{"unknown day", "Funday *-*-* 09:00:00"},
		{"hour out of range", "*-*-* 24:00:00"},
		{"minute out of range", "*-*-* 09:60:00"},
		{"month out of range", "*-13-01 00:00:00"},
		{"day out of range", "*-01-32 00:00:00"},
		{"time missing minutes", "*-*-* 09"},
		{"too many time parts", "*-*-* 09:00:00:00"},
		{"non-numeric", "*-*-* ab:00:00"},
		{"zero step", "*-*-* 00/0:00:00"},
		{"backwards range", "*-*-* 17..09:00:00"},
		{"never fires", "*-02-30 00:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			assert.Error(t, err)
		})
	}
}

func TestDesugarEvery(t *testing.T) {
	tests := []struct {
		every string
		want  string
	}{
		{"1m", "*-*-* *:*:00"},
		{"5m", "*-*-* *:00/5:00"},
		{"30m", "*-*-* *:00/30:00"},
		{"1h", "*-*-* *:00:00"},
		{"60m", "*-*-* *:00:00"},
		{"2h", "*-*-* 00/2:00:00"},
		{"6h", "*-*-* 00/6:00:00"},
		{"12h", "*-*-* 00/12:00:00"},
		{"24h", "*-*-* 00:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.every, func(t *testing.T) {
			d, err := time.ParseDuration(tt.every)
			require.NoError(t, err)

			got, err := DesugarEvery(d)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// The desugared form is what gets stored, so it must round-trip
			// through the parser: there is only one scheduling mechanism.
			_, err = Parse(got)
			assert.NoError(t, err)
		})
	}
}

// The desugared expression must actually fire at the requested interval,
// anchored to midnight rather than to whenever it was parsed.
func TestDesugarEveryFiresOnInterval(t *testing.T) {
	expr, err := DesugarEvery(6 * time.Hour)
	require.NoError(t, err)
	e, err := Parse(expr)
	require.NoError(t, err)

	want := []string{
		"2026-08-04T06:00:00Z",
		"2026-08-04T12:00:00Z",
		"2026-08-04T18:00:00Z",
		"2026-08-05T00:00:00Z",
	}

	cur := at("2026-08-04T00:00:00Z")
	for _, w := range want {
		next, ok := e.Next(cur)
		require.True(t, ok)
		assert.Equal(t, at(w), next)
		cur = next
	}
}

func TestDesugarEveryRejectsNonTiling(t *testing.T) {
	tests := []struct {
		name  string
		every string
	}{
		{"does not divide a day", "7h"},
		{"does not divide an hour", "45m"},
		{"not whole hours", "90m"},
		{"longer than a day", "48h"},
		{"sub-minute", "30s"},
		{"not whole minutes", "90s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := time.ParseDuration(tt.every)
			require.NoError(t, err)

			_, err = DesugarEvery(d)
			require.Error(t, err)
			// Every rejection has to point somewhere actionable.
			assert.Contains(t, err.Error(), "schedule")
		})
	}
}

func TestDesugarEveryRejectsNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour} {
		_, err := DesugarEvery(d)
		assert.Error(t, err, "duration %s", d)
	}
}

// Intersecting a leap day with a weekday stretches far past the four-year leap
// cycle: the Gregorian century rule drops leap days that would otherwise land
// mid-cycle, so consecutive hits can be decades apart. A horizon sized for leap
// years alone rejects these as "never fires".
func TestParseAcceptsWeekdayOnLeapDay(t *testing.T) {
	for _, expr := range []string{
		"Mon *-02-29 00:00:00",
		"Sun *-02-29 12:00:00",
		"Thu *-02-29 09:30:00",
	} {
		t.Run(expr, func(t *testing.T) {
			e, err := Parse(expr)
			require.NoError(t, err, "a satisfiable expression must not be rejected")

			next, ok := e.Next(at("2026-08-04T00:00:00Z"))
			require.True(t, ok)
			assert.Equal(t, 29, next.Day())
			assert.Equal(t, time.February, next.Month())
		})
	}
}

func TestNextFindsADistantWeekdayLeapDay(t *testing.T) {
	e, err := Parse("Mon *-02-29 00:00:00")
	require.NoError(t, err)

	// 2044-02-29 is a Monday; the preceding Monday leap day is 2016.
	next, ok := e.Next(at("2026-08-04T00:00:00Z"))
	require.True(t, ok)
	assert.Equal(t, at("2044-02-29T00:00:00Z"), next)
	assert.Equal(t, time.Monday, next.Weekday())
}

// Genuinely impossible dates must still be rejected, or the wider horizon would
// just be trading one wrong answer for a slower one.
func TestParseStillRejectsImpossibleDates(t *testing.T) {
	for _, expr := range []string{"*-02-30 00:00:00", "*-04-31 00:00:00", "*-06-31 12:00:00"} {
		_, err := Parse(expr)
		assert.Error(t, err, "%s can never occur", expr)
	}
}
