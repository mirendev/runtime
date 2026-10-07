package export

import (
	"fmt"
	"slices"
	"strings"
)

// Severity says what a contract change does to a consumer that already knows
// the old contract.
type Severity int

const (
	// Breaking changes either make the consumer reject sync or quietly
	// diverge from what it projects. The quiet ones matter most here, since
	// nothing at sync time will notice them.
	Breaking Severity = iota
	// Advisory changes alter the contract without changing anything the
	// consumer sees or projects, so they deserve a look but not a failure.
	Advisory
)

func (s Severity) String() string {
	if s == Breaking {
		return "breaking"
	}
	return "advisory"
}

// Remedy says how a breaking change can land without breaking sync.
type Remedy int

const (
	// ShipCloudFirst works when runtimes still sending the old shape satisfy
	// the new contract too, so the consumer can adopt it before any runtime
	// release does.
	ShipCloudFirst Remedy = iota + 1
	// Replace is for changes no ordering makes safe: whichever shape the
	// consumer expects, it rejects runtimes still sending the other. The new
	// shape needs a new attribute, and the old one retires once nothing reads
	// it.
	Replace
)

// Change is one difference between a known contract and its successor.
// Attribute is empty when the change is to the kind itself. Remedy is set only
// on breaking changes.
type Change struct {
	Severity  Severity
	Remedy    Remedy
	Kind      string
	Attribute string
	Detail    string
}

func (c Change) String() string {
	if c.Attribute == "" {
		return fmt.Sprintf("kind %s %s", c.Kind, c.Detail)
	}
	return fmt.Sprintf("%s (kind %s) %s", c.Attribute, c.Kind, c.Detail)
}

// Compare reports how next differs from known, from the point of view of a
// consumer that validates against known the way cloud does: an attribute it
// knows must keep its wire kind, parent and cardinality, while anything it
// does not know is accepted as an addition. Additions are therefore not
// reported at all.
func Compare(known, next *Contract) ([]Change, error) {
	if known.Target != next.Target {
		return nil, fmt.Errorf("cannot compare contracts for different targets %q and %q", known.Target, next.Target)
	}

	var changes []Change
	for _, kind := range known.Kinds {
		idx := slices.IndexFunc(next.Kinds, func(k Kind) bool { return k.ID == kind.ID })
		if idx < 0 {
			changes = append(changes, Change{
				Severity: Breaking,
				Remedy:   ShipCloudFirst,
				Kind:     kind.ID,
				Detail:   "is no longer exported, so every projection of it stops updating",
			})
			continue
		}
		nextKind := next.Kinds[idx]
		if kind.Lifecycle != nextKind.Lifecycle {
			changes = append(changes, Change{
				Severity: Advisory,
				Kind:     kind.ID,
				Detail: fmt.Sprintf("changed lifecycle from %s to %s; cloud applies the lifecycle in its own copy and never sees this one, so it takes effect only once that copy changes",
					kind.Lifecycle, nextKind.Lifecycle),
			})
		}
		for _, attr := range kind.Attributes {
			idx := slices.IndexFunc(nextKind.Attributes, func(a Attribute) bool { return a.ID == attr.ID })
			if idx < 0 {
				changes = append(changes, Change{
					Severity:  Breaking,
					Remedy:    ShipCloudFirst,
					Kind:      kind.ID,
					Attribute: attr.ID,
					Detail:    "is no longer exported, so whatever cloud projects from it goes blank",
				})
				continue
			}
			changes = append(changes, compareAttribute(kind.ID, attr, nextKind.Attributes[idx])...)
		}
	}
	return changes, nil
}

func compareAttribute(kind string, known, next Attribute) []Change {
	var changes []Change
	add := func(severity Severity, remedy Remedy, format string, args ...any) {
		changes = append(changes, Change{
			Severity:  severity,
			Remedy:    remedy,
			Kind:      kind,
			Attribute: known.ID,
			Detail:    fmt.Sprintf(format, args...),
		})
	}
	breaking := func(remedy Remedy, format string, args ...any) { add(Breaking, remedy, format, args...) }
	advisory := func(format string, args ...any) { add(Advisory, 0, format, args...) }

	if known.Type != next.Type {
		// Parse has already rejected unknown types, so both lookups succeed.
		knownWire, _ := valueKind(known.Type)
		nextWire, _ := valueKind(next.Type)
		if knownWire == nextWire {
			advisory("changed type from %s to %s; they share a wire kind, so cloud keeps accepting it",
				known.Type, next.Type)
		} else {
			breaking(Replace, "changed type from %s to %s; cloud rejects every entity that carries it",
				known.Type, next.Type)
		}
	}
	if known.Parent != next.Parent {
		breaking(Replace, "moved from %s to %s; cloud rejects every entity that carries it",
			placement(known.Parent), placement(next.Parent))
	}
	switch {
	case !known.Many && next.Many:
		breaking(ShipCloudFirst, "became many-valued; cloud rejects any entity that repeats it")
	case known.Many && !next.Many:
		advisory("is no longer many-valued; cloud still accepts a single value")
	}
	if known.Type == "enum" && next.Type == "enum" {
		var dropped []string
		for _, value := range known.EnumValues {
			if !slices.Contains(next.EnumValues, value) {
				dropped = append(dropped, value)
			}
		}
		if len(dropped) > 0 {
			breaking(ShipCloudFirst, "no longer has enum values %s; projections matching them quietly stop matching",
				strings.Join(dropped, ", "))
		}
	}
	return changes
}

func placement(parent string) string {
	if parent == "" {
		return "the top level"
	}
	return "inside " + parent
}
