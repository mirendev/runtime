package ephemeral

import (
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/pkg/units"
)

// DefaultTTL is how long a preview lives when the deploy doesn't say. A week
// outlasts a PR that sits over a weekend before anyone reviews it.
const DefaultTTL = "7d"

// ParseTTL parses a preview TTL. It accepts Go durations ("48h", "90m") plus
// day and week units ("7d", "2w"), and rejects anything that isn't positive.
func ParseTTL(s string) (time.Duration, error) {
	d, err := units.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("must be greater than 0")
	}
	return d, nil
}

// WireTTL renders d as a plain Go duration ("168h" rather than "7d"). Servers
// that predate day units parse the TTL with time.ParseDuration, and so do
// consumers of the deploy summary, so this is the spelling that travels.
func WireTTL(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}
