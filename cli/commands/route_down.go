package commands

import (
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteDown(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Default bool   `long:"default" description:"Take the default route down (instead of a hostname)"`
	Reason  string `long:"reason" description:"Explanation shown to visitors on the holding page"`
	BackAt  string `long:"back-at" description:"When the route is expected back: a clock time in your own timezone (15:00), a duration (30m), or an RFC 3339 timestamp"`
	Yes     bool   `long:"yes" short:"y" description:"Skip the confirmation prompt for --default"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}

	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}

	backAt, err := parseBackAt(opts.BackAt, time.Now())
	if err != nil {
		return err
	}

	// The default route catches every hostname that doesn't match another
	// route, so this is the widest blast radius available in one command. JSON
	// output can't carry a prompt, but it mustn't be a way around one either:
	// there the caller has to say --yes out loud.
	if opts.Default && !opts.Yes {
		if opts.IsJSON() {
			return fmt.Errorf("taking the default route down affects every hostname without its own route; pass --yes to confirm")
		}

		confirmed, err := ui.Confirm(
			ui.WithMessage("Take the default route down? Every hostname without its own route gets the holding page."),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Cancelled\n")
			return nil
		}
	}

	client, err := ctx.RPCClient("dev.miren.runtime/routes")
	if err != nil {
		return err
	}

	routes := ingress_v1alpha.NewRoutesClient(client)

	res, err := routes.SetMaintenance(ctx, opts.Host, opts.Default, opts.Reason, backAt)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		type RouteDownJSON struct {
			Route       string `json:"route"`
			Maintenance bool   `json:"maintenance"`
			Reason      string `json:"reason,omitempty"`
			BackAt      string `json:"back_at,omitempty"`
			StartedAt   string `json:"started_at,omitempty"`
			StartedBy   string `json:"started_by,omitempty"`
		}

		return PrintJSON(RouteDownJSON{
			Route:       res.Route(),
			Maintenance: true,
			Reason:      res.Reason(),
			BackAt:      res.BackAt(),
			StartedAt:   res.StartedAt(),
			StartedBy:   res.StartedBy(),
		})
	}

	ctx.Printf("Route %s is in maintenance. Visitors get a holding page; the app itself keeps running.\n", res.Route())
	if res.Reason() != "" {
		ctx.Printf("Reason: %s\n", res.Reason())
	}
	if res.BackAt() != "" {
		ctx.Printf("Expected back: %s\n", res.BackAt())
	}
	ctx.Printf("Bring it back with: miren route up %s\n", upTarget(opts.Host, opts.Default))

	return nil
}

func upTarget(host string, isDefault bool) string {
	if isDefault {
		return "--default"
	}
	return host
}

// parseBackAt normalizes the operator's expected-return time to RFC 3339 UTC.
// It accepts a duration from now ("30m"), a clock time resolved to its next
// occurrence ("15:00"), or a full RFC 3339 timestamp.
//
// A bare clock time is read in the caller's own timezone, since that is what an
// operator typing "15:00" means. now carries that location, so the conversion
// to UTC falls out of it.
func parseBackAt(in string, now time.Time) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", nil
	}

	if t, err := time.Parse(time.RFC3339, in); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}

	if d, err := time.ParseDuration(in); err == nil {
		if d <= 0 {
			return "", fmt.Errorf("--back-at duration must be in the future: %s", in)
		}
		return now.Add(d).UTC().Format(time.RFC3339), nil
	}

	for _, layout := range []string{"15:04", "15:04:05"} {
		clock, err := time.Parse(layout, in)
		if err != nil {
			continue
		}

		at := time.Date(now.Year(), now.Month(), now.Day(),
			clock.Hour(), clock.Minute(), clock.Second(), 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}

		// The hour a spring-forward skips does not exist locally, and time.Date
		// silently slides it to one that does. Rather than quietly meaning an
		// hour the operator did not type, say so and ask for a timestamp.
		if at.Hour() != clock.Hour() || at.Minute() != clock.Minute() || at.Second() != clock.Second() {
			return "", fmt.Errorf("%s does not exist on that date in %s, which is the hour daylight saving time skips: give an RFC 3339 timestamp instead", in, at.Location())
		}

		return at.UTC().Format(time.RFC3339), nil
	}

	return "", fmt.Errorf("could not read --back-at %q: use a clock time (15:00), a duration (30m), or an RFC 3339 timestamp", in)
}
