// Package oncalendar parses the subset of systemd's OnCalendar syntax that
// Miren accepts for scheduled tasks, and evaluates the firing times it
// describes.
//
// OnCalendar is used in preference to cron because it reads left to right:
// "Mon *-*-* 09:00:00" is legible as "Monday at 9am" without knowing which
// position means what, where cron's "0 9 * * 1" is not.
//
// Everything is evaluated in UTC. A calendar expression is a pure function of
// its own text, so every replica derives the same firing times from the stored
// config without coordinating.
package oncalendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// searchHorizon bounds Next's forward scan. An expression that matches nothing
// within five years is treated as unsatisfiable rather than scanned forever.
const searchHorizon = 5

// Expr is a parsed calendar expression.
type Expr struct {
	text string

	dow    field // 0 = Sunday .. 6 = Saturday
	year   field
	month  field
	day    field
	hour   field
	minute field
	second field
}

// String returns the expression's canonical text, which is what gets stored and
// displayed.
func (e *Expr) String() string { return e.text }

// keywords are the shorthand forms systemd defines. They expand to ordinary
// expressions, so there is only one evaluator.
var keywords = map[string]string{
	"minutely":  "*-*-* *:*:00",
	"hourly":    "*-*-* *:00:00",
	"daily":     "*-*-* 00:00:00",
	"weekly":    "Mon *-*-* 00:00:00",
	"monthly":   "*-*-01 00:00:00",
	"yearly":    "*-01-01 00:00:00",
	"annually":  "*-01-01 00:00:00",
	"quarterly": "*-01,04,07,10-01 00:00:00",
}

var dayNames = map[string]int{
	"sun": 0, "sunday": 0,
	"mon": 1, "monday": 1,
	"tue": 2, "tuesday": 2, "tues": 2,
	"wed": 3, "wednesday": 3,
	"thu": 4, "thursday": 4, "thur": 4, "thurs": 4,
	"fri": 5, "friday": 5,
	"sat": 6, "saturday": 6,
}

// Parse parses a calendar expression. Accepted forms:
//
//	daily                       (and the other keywords)
//	HH:MM[:SS]                  every day at that time
//	YYYY-MM-DD HH:MM[:SS]
//	MM-DD HH:MM[:SS]            any year
//	DOW YYYY-MM-DD HH:MM[:SS]
//	DOW HH:MM[:SS]
//
// Every numeric field accepts `*`, a value, a comma list, an `a..b` range, and
// an `a/step` or `*/step` repetition. Day-of-week accepts names, comma lists,
// and `Mon..Fri` ranges.
func Parse(expr string) (*Expr, error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return nil, fmt.Errorf("empty calendar expression")
	}

	text := raw
	if expanded, ok := keywords[strings.ToLower(raw)]; ok {
		text = expanded
	}

	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > 3 {
		return nil, fmt.Errorf("invalid calendar expression %q: expected at most three parts (day-of-week, date, time)", expr)
	}

	var dowPart, datePart, timePart string
	switch len(fields) {
	case 3:
		dowPart, datePart, timePart = fields[0], fields[1], fields[2]
	case 2:
		// Two parts is either "DOW time" or "date time". A day-of-week always
		// begins with a letter; a date never does.
		if startsWithLetter(fields[0]) {
			dowPart, timePart = fields[0], fields[1]
		} else {
			datePart, timePart = fields[0], fields[1]
		}
	case 1:
		timePart = fields[0]
	}

	e := &Expr{text: text}

	var err error
	if e.dow, err = parseDOW(dowPart); err != nil {
		return nil, fmt.Errorf("invalid calendar expression %q: %w", expr, err)
	}
	if e.year, e.month, e.day, err = parseDate(datePart); err != nil {
		return nil, fmt.Errorf("invalid calendar expression %q: %w", expr, err)
	}
	if e.hour, e.minute, e.second, err = parseTime(timePart); err != nil {
		return nil, fmt.Errorf("invalid calendar expression %q: %w", expr, err)
	}

	// An expression that can never fire — "*-02-30", or a day-of-week that
	// never coincides with its day-of-month — is a config error, not a runtime
	// surprise. Probe from the epoch so the check is independent of when the
	// config is parsed: a config that validates today must not start failing
	// next year.
	if _, ok := e.Next(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)); !ok {
		return nil, fmt.Errorf("calendar expression %q never fires", expr)
	}

	return e, nil
}

// Next returns the first firing time strictly after t, and whether one exists.
func (e *Expr) Next(after time.Time) (time.Time, bool) {
	t := after.UTC().Truncate(time.Second).Add(time.Second)

	// With a wildcard year, every pattern that can recur does so within five
	// years (the longest cycle is Feb 29's four). With explicit years, the
	// horizon has to reach them or a far-future date would read as unsatisfiable.
	limit := t.AddDate(searchHorizon, 0, 0)
	if !e.year.any {
		if end := time.Date(e.year.max()+1, time.January, 1, 0, 0, 0, 0, time.UTC); end.After(limit) {
			limit = end
		}
	}

	for t.Before(limit) {
		if !e.year.matches(t.Year()) {
			t = time.Date(t.Year()+1, time.January, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !e.month.matches(int(t.Month())) {
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
			continue
		}
		// Day-of-month and day-of-week are both restrictions: a date must
		// satisfy both. (systemd treats them as an intersection, not a union
		// like cron does for some field combinations.)
		if !e.day.matches(t.Day()) || !e.dow.matches(int(t.Weekday())) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
			continue
		}
		if !e.hour.matches(t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC).Add(time.Hour)
			continue
		}
		if !e.minute.matches(t.Minute()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC).Add(time.Minute)
			continue
		}
		if !e.second.matches(t.Second()) {
			t = t.Add(time.Second)
			continue
		}
		return t, true
	}

	return time.Time{}, false
}

// DesugarEvery converts an `every = "6h"` interval into the equivalent calendar
// expression. `every` is pure sugar: only the calendar form is ever stored, so
// there is exactly one scheduling mechanism.
//
// The expansion is anchored to midnight UTC rather than to the deploy. An
// interval measured from "whenever this was deployed" would move every time the
// app ships — a daily job on an app that deploys ten times a day would fire ten
// times a day — and tick-based dedup could not catch it, because a new anchor
// produces a new tick and therefore a new entity ID. A day-aligned schedule
// doesn't move when you deploy, and every replica derives the same firing times
// from the config alone.
//
// The cost is that only durations tiling a day evenly are accepted.
func DesugarEvery(d time.Duration) (string, error) {
	if d <= 0 {
		return "", fmt.Errorf("every must be a positive duration")
	}

	if d < time.Minute {
		return "", fmt.Errorf("every must be at least 1m; use schedule for finer control")
	}

	if d < time.Hour {
		if d%time.Minute != 0 {
			return "", fmt.Errorf("every %s is not a whole number of minutes; use schedule for arbitrary times", d)
		}
		mins := int(d / time.Minute)
		if 60%mins != 0 {
			return "", fmt.Errorf("every %s does not divide an hour evenly, so it has no day-aligned reading; "+
				"use a value dividing 60m (1m, 2m, 5m, 10m, 15m, 20m, 30m) or set schedule instead", d)
		}
		return fmt.Sprintf("*-*-* *:%s:00", stepField(mins, 2)), nil
	}

	if d%time.Hour != 0 {
		return "", fmt.Errorf("every %s is not a whole number of hours; use schedule for arbitrary times", d)
	}
	hours := int(d / time.Hour)
	if hours > 24 {
		return "", fmt.Errorf("every %s is longer than a day, so it has no day-aligned reading; use schedule instead", d)
	}
	if 24%hours != 0 {
		return "", fmt.Errorf("every %s does not divide a day evenly, so it has no day-aligned reading; "+
			"use a value dividing 24h (1h, 2h, 3h, 4h, 6h, 8h, 12h, 24h) or set schedule instead", d)
	}
	if hours == 24 {
		// A step of 24 within a 0..23 field is a needless way to write "once".
		return "*-*-* 00:00:00", nil
	}
	return fmt.Sprintf("*-*-* %s:00:00", stepField(hours, 2)), nil
}

// stepField renders the repetition form for a step, collapsing a step of 1 to
// `*` since "every value" is what it means.
func stepField(step, width int) string {
	if step == 1 {
		return "*"
	}
	return fmt.Sprintf("%0*d/%d", width, 0, step)
}

// field is one parsed component of an expression.
type field struct {
	any    bool
	values map[int]bool
}

func anyField() field { return field{any: true} }

func (f field) matches(v int) bool {
	if f.any {
		return true
	}
	return f.values[v]
}

// max returns the largest value the field admits. Only meaningful for a field
// that isn't a wildcard.
func (f field) max() int {
	m := 0
	for v := range f.values {
		if v > m {
			m = v
		}
	}
	return m
}

func startsWithLetter(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// parseNumeric parses a numeric field spec bounded by [min, max].
func parseNumeric(spec, name string, min, max int) (field, error) {
	if spec == "*" {
		return anyField(), nil
	}

	values := make(map[int]bool)
	for _, term := range strings.Split(spec, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			return field{}, fmt.Errorf("%s: empty value in %q", name, spec)
		}

		body, step := term, 1
		if idx := strings.Index(term, "/"); idx >= 0 {
			body = term[:idx]
			s, err := strconv.Atoi(term[idx+1:])
			if err != nil || s <= 0 {
				return field{}, fmt.Errorf("%s: invalid step in %q", name, term)
			}
			step = s
		}

		lo, hi := min, max
		switch {
		case body == "*":
			// Full range, already set.
		case strings.Contains(body, ".."):
			parts := strings.SplitN(body, "..", 2)
			var err error
			if lo, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
				return field{}, fmt.Errorf("%s: invalid range start in %q", name, term)
			}
			if hi, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
				return field{}, fmt.Errorf("%s: invalid range end in %q", name, term)
			}
			if lo > hi {
				return field{}, fmt.Errorf("%s: range %q starts after it ends", name, body)
			}
		default:
			v, err := strconv.Atoi(body)
			if err != nil {
				return field{}, fmt.Errorf("%s: %q is not a number", name, body)
			}
			lo = v
			// A bare value with a step repeats from that value to the field's
			// maximum, which is what "00/6" means.
			if step == 1 {
				hi = v
			} else {
				hi = max
			}
		}

		if lo < min || hi > max {
			return field{}, fmt.Errorf("%s: %q is out of range %d-%d", name, term, min, max)
		}
		for v := lo; v <= hi; v += step {
			values[v] = true
		}
	}

	if len(values) == 0 {
		return field{}, fmt.Errorf("%s: %q matches nothing", name, spec)
	}
	return field{values: values}, nil
}

func parseDOW(spec string) (field, error) {
	if spec == "" || spec == "*" {
		return anyField(), nil
	}

	values := make(map[int]bool)
	for _, term := range strings.Split(spec, ",") {
		term = strings.TrimSpace(term)
		if strings.Contains(term, "..") {
			parts := strings.SplitN(term, "..", 2)
			lo, ok := dayNames[strings.ToLower(strings.TrimSpace(parts[0]))]
			if !ok {
				return field{}, fmt.Errorf("day-of-week: unknown day %q", parts[0])
			}
			hi, ok := dayNames[strings.ToLower(strings.TrimSpace(parts[1]))]
			if !ok {
				return field{}, fmt.Errorf("day-of-week: unknown day %q", parts[1])
			}
			// Ranges wrap, so Fri..Mon is Fri, Sat, Sun, Mon.
			for d := lo; ; d = (d + 1) % 7 {
				values[d] = true
				if d == hi {
					break
				}
			}
			continue
		}

		d, ok := dayNames[strings.ToLower(term)]
		if !ok {
			return field{}, fmt.Errorf("day-of-week: unknown day %q", term)
		}
		values[d] = true
	}

	return field{values: values}, nil
}

func parseDate(spec string) (year, month, day field, err error) {
	if spec == "" {
		return anyField(), anyField(), anyField(), nil
	}

	parts := strings.Split(spec, "-")
	switch len(parts) {
	case 3:
		if year, err = parseNumeric(parts[0], "year", 1970, 2200); err != nil {
			return
		}
		if month, err = parseNumeric(parts[1], "month", 1, 12); err != nil {
			return
		}
		day, err = parseNumeric(parts[2], "day", 1, 31)
		return
	case 2:
		year = anyField()
		if month, err = parseNumeric(parts[0], "month", 1, 12); err != nil {
			return
		}
		day, err = parseNumeric(parts[1], "day", 1, 31)
		return
	default:
		err = fmt.Errorf("date: %q is not YYYY-MM-DD or MM-DD", spec)
		return
	}
}

func parseTime(spec string) (hour, minute, second field, err error) {
	if spec == "" {
		err = fmt.Errorf("a time of day is required")
		return
	}

	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		err = fmt.Errorf("time: %q is not HH:MM or HH:MM:SS", spec)
		return
	}

	if hour, err = parseNumeric(parts[0], "hour", 0, 23); err != nil {
		return
	}
	if minute, err = parseNumeric(parts[1], "minute", 0, 59); err != nil {
		return
	}
	if len(parts) == 3 {
		second, err = parseNumeric(parts[2], "second", 0, 59)
		return
	}

	// HH:MM means the top of that minute, not every second within it.
	second, err = parseNumeric("0", "second", 0, 59)
	return
}
