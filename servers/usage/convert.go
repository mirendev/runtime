package usage

import (
	"strings"
	"time"

	"miren.dev/runtime/api/usage/usage_v1alpha"
)

// This file is the seam between the two shapes the API is offered in.
//
// The RPC surface takes grouped types so a Go caller cannot transpose two
// arguments; the REST surface takes flat scalars because that is what a query
// string can carry, and duration strings because that is what a URL should look
// like. Both funnel into the same internal filter/window/ordering values, so
// there is one implementation and two front doors.

// ordering is the resolved sort and truncation for a listing.
type ordering struct {
	sort  string
	order string
	limit int
}

func (o ordering) truncate(n int) int {
	if o.limit > 0 && o.limit < n {
		return o.limit
	}
	return n
}

// --- from the RPC surface ---

func selectorToFilter(sel *usage_v1alpha.Selector) filter {
	if sel == nil {
		return filter{}
	}

	f := filter{
		app:           sel.App(),
		service:       sel.Service(),
		node:          sel.Node(),
		kind:          sel.Kind(),
		status:        sel.Status(),
		includeSystem: sel.IncludeSystem(),
	}

	// Addons count toward an app's total unless the caller says otherwise, so
	// the absent case has to be distinguished from an explicit false. The
	// generated getter cannot do that on its own.
	f.includeAddons = !sel.HasIncludeAddons() || sel.IncludeAddons()

	return f
}

func windowFrom(w *usage_v1alpha.Window) window {
	if w == nil {
		return resolveWindow(nil, nil, "")
	}
	return resolveWindow(w.Start(), w.End(), w.Aggregate())
}

func orderingFrom(o *usage_v1alpha.Ordering) ordering {
	if o == nil {
		return ordering{}
	}
	return ordering{sort: o.Sort(), order: o.Order(), limit: int(o.Limit())}
}

// --- from the REST surface ---

// restWindow converts the duration strings a URL carries into a span.
//
// since and until are both measured backwards from now, so "?since=1h" is the
// last hour and "?since=2h&until=1h" is the hour before that. An unparseable
// value is ignored rather than rejected: this is a diagnostic endpoint, and
// answering a typo'd duration with the default beats a 400.
func restWindow(since, until, aggregate string) window {
	end := time.Now()
	if d, ok := parseRESTDuration(until); ok {
		end = end.Add(-d)
	}

	start := end.Add(-defaultWindow)
	if d, ok := parseRESTDuration(since); ok {
		start = end.Add(-d)
	}

	return window{start: start, end: end, aggregate: resolveAggregate(aggregate)}
}

func parseRESTDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}

	return d, true
}
