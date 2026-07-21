// Package theme centralizes the miren CLI's color palette and adapts it to the
// terminal it's running in.
//
// Historically colors were hard-coded as bright 256-color ANSI indices tuned for
// dark terminals, which washed out (or dropped below readable contrast) on light
// backgrounds. This package instead exposes a small set of semantic roles
// (Header, Success, Warning, ...) as lipgloss.AdaptiveColor values whose light and
// dark variants are hand-tuned for contrast on their respective backgrounds.
//
// Init decides, once per process, which variant to use. It honors the de-facto
// terminal color conventions (NO_COLOR, FORCE_COLOR/CLICOLOR) and a manual
// override (MIREN_THEME env var or the `theme` client-config field), falling back
// to background auto-detection (COLORFGBG hint, then an OSC 11 query) and finally
// to dark, the safe historical default.
package theme

import (
	"hash/fnv"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"miren.dev/runtime/pkg/color"
)

// Variant is the resolved palette variant.
type Variant int

const (
	// VariantDark is the palette for dark-background terminals (the default).
	VariantDark Variant = iota
	// VariantLight is the palette for light-background terminals.
	VariantLight
	// VariantNoColor disables color entirely (NO_COLOR / `theme: no`).
	VariantNoColor
)

func (v Variant) String() string {
	switch v {
	case VariantLight:
		return "light"
	case VariantNoColor:
		return "no-color"
	case VariantDark:
		return "dark"
	default:
		return "dark"
	}
}

// Semantic color roles. Each is a lipgloss.CompleteAdaptiveColor, which carries
// exact values for the truecolor, ANSI256, and ANSI profiles, split by light and
// dark background. lipgloss selects the Light/Dark side from the background
// darkness we set via SetHasDarkBackground in Init, then reads the field matching
// the terminal's color profile with NO automatic degradation. This lets us serve
// designed jewel tones on truecolor terminals while pinning 256-color terminals
// to xterm anchor indices (so distinct roles never quantize into the same muddy
// bucket) and still degrade sanely to the basic 16 colors.
//
// Dark values stay close to the previous bright palette. Light values are darker,
// saturated tones tuned for contrast on white; the warm hues (gold/orange) run a
// little lower on contrast but are used bold or as status glyphs where that reads
// fine.
var (
	// Header styles section headings and table column headers (was bright "220").
	// On light backgrounds gold reads muddy, so headers borrow the Info blue there
	// (bold + underline still distinguish them from plain Info text); dark keeps
	// the gold.
	Header = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#FFD75F", ANSI256: "221", ANSI: "11"},
		Light: lipgloss.CompleteColor{TrueColor: "#2563EB", ANSI256: "26", ANSI: "4"},
	}
	// Success marks healthy/OK/added state (was bright green "10"/"2").
	Success = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#4EC94E", ANSI256: "77", ANSI: "10"},
		Light: lipgloss.CompleteColor{TrueColor: "#15803D", ANSI256: "28", ANSI: "2"},
	}
	// Warning marks degraded/pending/attention state (was yellow "11"/"3"/"208").
	Warning = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#E0B000", ANSI256: "178", ANSI: "11"},
		Light: lipgloss.CompleteColor{TrueColor: "#C2410C", ANSI256: "166", ANSI: "3"},
	}
	// Error marks failed/error state (was red "9"/"196").
	Error = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#FF6B6B", ANSI256: "203", ANSI: "9"},
		Light: lipgloss.CompleteColor{TrueColor: "#DC2626", ANSI256: "160", ANSI: "1"},
	}
	// Info marks informational accents and links (was blue "12"/"62").
	Info = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#5FAFFF", ANSI256: "75", ANSI: "12"},
		Light: lipgloss.CompleteColor{TrueColor: "#2563EB", ANSI256: "26", ANSI: "4"},
	}
	// Muted is low-emphasis secondary text. It replaces the pervasive Faint(true)
	// usage, which relied on the terminal's dim attribute and vanished on light
	// backgrounds (was gray "8"/"240"/"244"/"245").
	Muted = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#9A9A9A", ANSI256: "246", ANSI: "8"},
		Light: lipgloss.CompleteColor{TrueColor: "#6B7280", ANSI256: "243", ANSI: "8"},
	}
	// Highlight marks the selected/active item in interactive pickers.
	Highlight = lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: "#D7AFFF", ANSI256: "183", ANSI: "13"},
		Light: lipgloss.CompleteColor{TrueColor: "#7C3AED", ANSI256: "91", ANSI: "5"},
	}
)

// laneColors form a categorical *identity* palette: they give each service a
// stable color so interleaved log lines can be followed by eye. They are
// deliberately disjoint from the semantic roles above, because identity answers
// "who is this?" not "how is it going?". Two rules shape the set:
//
//   - Cool/neutral hues only. Warm hues (orange, red) read as "alert" wherever
//     they land, so they stay reserved for status; a service should never look
//     alarmed just because of its name's hash.
//   - CVD-safe and bounded. Hues are drawn from the colorblind-safe qualitative
//     range (Okabe-Ito / Paul Tol) and capped at a handful; past that, two
//     services share a color and the printed name disambiguates them.
//
// Each lane pins a distinct xterm-256 anchor, but all lanes share one ANSI-16
// value, so on 16-color terminals identity color collapses to a single accent
// (identity is then carried by the service-name text instead).
var laneColors = []lipgloss.CompleteAdaptiveColor{
	{ // cyan
		Dark:  lipgloss.CompleteColor{TrueColor: "#56CFE1", ANSI256: "80", ANSI: "14"},
		Light: lipgloss.CompleteColor{TrueColor: "#0E7490", ANSI256: "30", ANSI: "6"},
	},
	{ // periwinkle
		Dark:  lipgloss.CompleteColor{TrueColor: "#8AA0F5", ANSI256: "105", ANSI: "14"},
		Light: lipgloss.CompleteColor{TrueColor: "#4F46E5", ANSI256: "62", ANSI: "6"},
	},
	{ // mint
		Dark:  lipgloss.CompleteColor{TrueColor: "#5FD0A8", ANSI256: "79", ANSI: "14"},
		Light: lipgloss.CompleteColor{TrueColor: "#0F766E", ANSI256: "29", ANSI: "6"},
	},
	{ // lavender
		Dark:  lipgloss.CompleteColor{TrueColor: "#B99BF0", ANSI256: "141", ANSI: "14"},
		Light: lipgloss.CompleteColor{TrueColor: "#7C3AED", ANSI256: "91", ANSI: "6"},
	},
	{ // magenta
		Dark:  lipgloss.CompleteColor{TrueColor: "#E879C7", ANSI256: "176", ANSI: "14"},
		Light: lipgloss.CompleteColor{TrueColor: "#BE185D", ANSI256: "125", ANSI: "6"},
	},
}

// Router is the reserved lane for router/access-log lines: a cool slate that is
// distinct from the rotating identity hues, so router traffic reads as its own
// lane without being dimmed into noise.
var Router = lipgloss.CompleteAdaptiveColor{
	Dark:  lipgloss.CompleteColor{TrueColor: "#7C8AA8", ANSI256: "103", ANSI: "8"},
	Light: lipgloss.CompleteColor{TrueColor: "#64748B", ANSI256: "60", ANSI: "8"},
}

// Lane returns a stable identity color for seed (typically a service name) by
// hashing it into the lane palette. The same seed always maps to the same color
// within a build, so a service keeps its color across restarts and throughout a
// session. An empty seed returns Muted.
func Lane(seed string) lipgloss.CompleteAdaptiveColor {
	if seed == "" {
		return Muted
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return laneColors[h.Sum32()%uint32(len(laneColors))]
}

// Lanes returns the identity lane palette in order, for the `miren colors` debug
// command. It does not include the reserved Router lane.
func Lanes() []lipgloss.CompleteAdaptiveColor {
	return slices.Clone(laneColors)
}

// Role pairs a human name with its color, for the `miren colors` debug command.
type Role struct {
	Name  string
	Color lipgloss.CompleteAdaptiveColor
}

// Roles returns the semantic roles in display order.
func Roles() []Role {
	return []Role{
		{"Header", Header},
		{"Success", Success},
		{"Warning", Warning},
		{"Error", Error},
		{"Info", Info},
		{"Muted", Muted},
		{"Highlight", Highlight},
	}
}

var (
	once       sync.Once
	current    Variant
	detectedBG string
)

// Current returns the resolved palette variant. It is only meaningful after Init.
func Current() Variant { return current }

// DetectedBackground returns the terminal background hex observed during
// detection, or "" if detection didn't run (override set, not a TTY, or the
// terminal didn't answer). Used by the `miren colors` debug command.
func DetectedBackground() string { return detectedBG }

// Init resolves the palette variant once per process and applies it to lipgloss's
// default renderer. configured is the `theme` value from client config (may be
// empty). It is safe to call multiple times; only the first call takes effect.
func Init(configured string) {
	once.Do(func() {
		v, force := resolve(os.LookupEnv, configured, detectBackground, isTTY())
		current = v

		switch {
		case v == VariantNoColor:
			lipgloss.SetColorProfile(termenv.Ascii)
		case force:
			// Emit color even when stdout isn't a TTY (piped, CI). Floor at
			// ANSI256 so forcing never degrades to no color, but honor COLORTERM
			// so a truecolor environment still gets truecolor.
			lipgloss.SetColorProfile(forcedProfile(os.LookupEnv))
		default:
			// Color is on and this is (or should be) an interactive terminal.
			// Some modern terminals report a TERM that older termenv builds don't
			// recognize (e.g. TERM=xterm-ghostty forwarded over SSH, with no
			// COLORTERM to fall back on), which makes lipgloss degrade all the way
			// to Ascii — monochrome output on a perfectly capable terminal. Floor
			// to ANSI256 so we always render at least the anchor palette. Truecolor
			// still requires the terminal to advertise it (COLORTERM or a TERM
			// termenv knows); this only rescues the "would've been monochrome" case.
			if isTTY() && lipgloss.ColorProfile() == termenv.Ascii && colorCapableTerm(os.LookupEnv) {
				lipgloss.SetColorProfile(termenv.ANSI256)
			}
		}

		lipgloss.SetHasDarkBackground(v != VariantLight)
	})
}

// detectBackground runs the active OSC 11 query and records the result for the
// debug command. Kept as a named function so resolve stays pure and testable.
func detectBackground() string {
	detectedBG = color.Background()
	return detectedBG
}

// resolve is the pure decision function: given the environment, the configured
// override, a background probe, and whether stdout is a TTY, it returns the
// variant and whether color output should be forced on. It performs no I/O beyond
// calling detectBG (only when it actually needs the background).
func resolve(lookup func(string) (string, bool), configured string, detectBG func() string, isTTY bool) (Variant, bool) {
	override := configured
	if v, ok := lookup("MIREN_THEME"); ok && v != "" {
		override = v
	}
	override = strings.ToLower(strings.TrimSpace(override))

	force := truthyEnv(lookup, "FORCE_COLOR") || truthyEnv(lookup, "CLICOLOR_FORCE")

	switch override {
	case "no", "none", "off", "never":
		return VariantNoColor, false
	case "light":
		return VariantLight, force
	case "dark":
		return VariantDark, force
	}

	// NO_COLOR (present and non-empty) or CLICOLOR=0 disables color unless the
	// user explicitly forces it back on.
	if !force {
		if v, ok := lookup("NO_COLOR"); ok && v != "" {
			return VariantNoColor, false
		}
		if v, ok := lookup("CLICOLOR"); ok && v == "0" {
			return VariantNoColor, false
		}
	}

	// Auto-detect the background. Prefer the passive COLORFGBG hint (no terminal
	// round-trip); fall back to the active OSC 11 probe only on a TTY; default to
	// dark, matching the historical palette.
	if hint, ok := lookup("COLORFGBG"); ok {
		if v, matched := variantFromColorFGBG(hint); matched {
			return v, force
		}
	}
	if isTTY {
		if isLightHex(detectBG()) {
			return VariantLight, force
		}
	}
	return VariantDark, force
}

func truthyEnv(lookup func(string) (string, bool), key string) bool {
	v, ok := lookup(key)
	return ok && v != "" && v != "0"
}

// forcedProfile picks the color profile to use when color is forced on (piped,
// CI). It floors at ANSI256 so forcing never yields no color, and upgrades to
// truecolor when the environment advertises it via COLORTERM.
func forcedProfile(lookup func(string) (string, bool)) termenv.Profile {
	if ct, _ := lookup("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		return termenv.TrueColor
	}
	return termenv.ANSI256
}

// colorCapableTerm reports whether TERM names a real terminal that can render
// color, as opposed to a genuine no-color signal ("dumb" or an unset TERM). It's
// used to decide whether an Ascii profile on a TTY is a real "no color" request
// or just termenv failing to recognize a modern terminal's name.
func colorCapableTerm(lookup func(string) (string, bool)) bool {
	t, ok := lookup("TERM")
	return ok && t != "" && t != "dumb"
}

// variantFromColorFGBG interprets a COLORFGBG value like "15;0" (fg;bg) or
// "15;default;0". The background is the last field; ANSI 7 and 9-15 are light.
func variantFromColorFGBG(v string) (Variant, bool) {
	fields := strings.Split(v, ";")
	if len(fields) == 0 {
		return VariantDark, false
	}
	bg := strings.TrimSpace(fields[len(fields)-1])
	switch bg {
	case "7", "9", "10", "11", "12", "13", "14", "15":
		return VariantLight, true
	case "0", "1", "2", "3", "4", "5", "6", "8":
		return VariantDark, true
	}
	return VariantDark, false
}

// isLightHex reports whether a "#rrggbb" background color is light. Empty or
// unparseable input is treated as dark (the safe default).
func isLightHex(hex string) bool {
	c, err := colorful.Hex(hex)
	if err != nil {
		return false
	}
	_, _, l := c.Hsl()
	return l > 0.5
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
