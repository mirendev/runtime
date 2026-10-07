package export

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func compatBase() Contract {
	return Contract{
		Version: Version1,
		Target:  "cloud",
		Marker:  "test/cloud",
		Kinds: []Kind{
			{
				ID:        "test/kind.app",
				Lifecycle: LifecycleMirror,
				Attributes: []Attribute{
					{ID: "test/app.name", Type: "string"},
					{ID: "test/app.owner", Type: "ref"},
					{ID: "test/app.labels", Type: "label", Many: true},
					{ID: "test/app.status", Type: "enum", EnumValues: []string{"test/status.ready", "test/status.failed"}},
					{ID: "test/app.actor", Type: "component"},
					{ID: "test/actor.subject", Type: "string", Parent: "test/app.actor"},
				},
			},
			{
				ID:         "test/kind.deployment",
				Lifecycle:  LifecycleArchive,
				Attributes: []Attribute{{ID: "test/deployment.message", Type: "string"}},
			},
		},
	}
}

// compile round-trips through JSON so the comparison sees exactly what a
// committed contract file would give it, and so Parse vets each fixture.
func compile(t *testing.T, contract Contract) *Contract {
	t.Helper()
	data, err := json.Marshal(contract)
	require.NoError(t, err)
	parsed, err := Parse(data)
	require.NoError(t, err)
	return parsed
}

func attribute(contract *Contract, kind, id string) *Attribute {
	k := slices.IndexFunc(contract.Kinds, func(candidate Kind) bool { return candidate.ID == kind })
	attrs := contract.Kinds[k].Attributes
	a := slices.IndexFunc(attrs, func(candidate Attribute) bool { return candidate.ID == id })
	return &attrs[a]
}

func TestCompare(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *Contract)
		want   []Change
	}{
		{
			name:   "identical",
			mutate: func(c *Contract) {},
		},
		{
			name: "additions pass",
			mutate: func(c *Contract) {
				c.Kinds[0].Attributes = append(c.Kinds[0].Attributes,
					Attribute{ID: "test/app.region", Type: "string"},
					Attribute{ID: "test/actor.email", Type: "string", Parent: "test/app.actor"},
				)
				c.Kinds = append(c.Kinds, Kind{ID: "test/kind.node", Lifecycle: LifecycleMirror})
				status := attribute(c, "test/kind.app", "test/app.status")
				status.EnumValues = append(status.EnumValues, "test/status.paused")
			},
		},
		{
			name: "rename is a removal",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.deployment", "test/deployment.message").ID = "test/deployment.summary"
			},
			want: []Change{{
				Severity:  Breaking,
				Remedy:    ShipCloudFirst,
				Kind:      "test/kind.deployment",
				Attribute: "test/deployment.message",
				Detail:    "is no longer exported, so whatever cloud projects from it goes blank",
			}},
		},
		{
			name: "kind removed",
			mutate: func(c *Contract) {
				c.Kinds = c.Kinds[:1]
			},
			want: []Change{{
				Severity: Breaking,
				Remedy:   ShipCloudFirst,
				Kind:     "test/kind.deployment",
				Detail:   "is no longer exported, so every projection of it stops updating",
			}},
		},
		{
			name: "wire type change",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.app", "test/app.name").Type = "keyword"
			},
			want: []Change{{
				Severity:  Breaking,
				Remedy:    Replace,
				Kind:      "test/kind.app",
				Attribute: "test/app.name",
				Detail:    "changed type from string to keyword; cloud rejects every entity that carries it",
			}},
		},
		{
			name: "enum to ref keeps its wire kind",
			mutate: func(c *Contract) {
				status := attribute(c, "test/kind.app", "test/app.status")
				status.Type = "ref"
				status.EnumValues = nil
			},
			want: []Change{{
				Severity:  Advisory,
				Kind:      "test/kind.app",
				Attribute: "test/app.status",
				Detail:    "changed type from enum to ref; they share a wire kind, so cloud keeps accepting it",
			}},
		},
		{
			name: "moved into a component",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.app", "test/app.name").Parent = "test/app.actor"
			},
			want: []Change{{
				Severity:  Breaking,
				Remedy:    Replace,
				Kind:      "test/kind.app",
				Attribute: "test/app.name",
				Detail:    "moved from the top level to inside test/app.actor; cloud rejects every entity that carries it",
			}},
		},
		{
			name: "single becomes many",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.app", "test/app.owner").Many = true
			},
			want: []Change{{
				Severity:  Breaking,
				Remedy:    ShipCloudFirst,
				Kind:      "test/kind.app",
				Attribute: "test/app.owner",
				Detail:    "became many-valued; cloud rejects any entity that repeats it",
			}},
		},
		{
			name: "many becomes single",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.app", "test/app.labels").Many = false
			},
			want: []Change{{
				Severity:  Advisory,
				Kind:      "test/kind.app",
				Attribute: "test/app.labels",
				Detail:    "is no longer many-valued; cloud still accepts a single value",
			}},
		},
		{
			name: "enum loses a value",
			mutate: func(c *Contract) {
				attribute(c, "test/kind.app", "test/app.status").EnumValues = []string{"test/status.ready"}
			},
			want: []Change{{
				Severity:  Breaking,
				Remedy:    ShipCloudFirst,
				Kind:      "test/kind.app",
				Attribute: "test/app.status",
				Detail:    "no longer has enum values test/status.failed; projections matching them quietly stop matching",
			}},
		},
		{
			name: "lifecycle change",
			mutate: func(c *Contract) {
				c.Kinds[1].Lifecycle = LifecycleMirror
			},
			want: []Change{{
				Severity: Advisory,
				Kind:     "test/kind.deployment",
				Detail:   "changed lifecycle from archive to mirror; cloud applies the lifecycle in its own copy and never sees this one, so it takes effect only once that copy changes",
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := compatBase()
			tc.mutate(&next)
			changes, err := Compare(compile(t, compatBase()), compile(t, next))
			require.NoError(t, err)
			require.Equal(t, tc.want, changes)

			// ShipCloudFirst promises that once cloud adopts the change, a
			// runtime still on the old contract passes. Check that by swapping
			// the sides: cloud holds next, the runtime still exports the base.
			// Cloud takes only the change, not next's additions, which it
			// learns after the runtime ships them.
			if slices.ContainsFunc(tc.want, func(c Change) bool { return c.Remedy == ShipCloudFirst }) {
				cloud := withoutAdditions(next, compatBase())
				reversed, err := Compare(compile(t, cloud), compile(t, compatBase()))
				require.NoError(t, err)
				for _, change := range reversed {
					require.NotEqual(t, Breaking, change.Severity, "cloud-first still fails: %s", change)
				}
			}
		})
	}
}

// withoutAdditions drops the kinds and attributes next has that base lacks.
func withoutAdditions(next, base Contract) Contract {
	out := next
	out.Kinds = nil
	for _, kind := range next.Kinds {
		k := slices.IndexFunc(base.Kinds, func(candidate Kind) bool { return candidate.ID == kind.ID })
		if k < 0 {
			continue
		}
		kind.Attributes = slices.DeleteFunc(slices.Clone(kind.Attributes), func(attr Attribute) bool {
			return !slices.ContainsFunc(base.Kinds[k].Attributes, func(candidate Attribute) bool { return candidate.ID == attr.ID })
		})
		out.Kinds = append(out.Kinds, kind)
	}
	return out
}

func TestCompareRefusesDifferentTargets(t *testing.T) {
	other := compatBase()
	other.Target = "elsewhere"
	_, err := Compare(compile(t, compatBase()), compile(t, other))
	require.Error(t, err)
}

// The runtime's own contract must be compatible with itself; this catches a
// Compare that misreads a real, generated contract.
func TestCompareGeneratedContractAgainstItself(t *testing.T) {
	data, err := os.ReadFile("../../../api/core/core_v1alpha/cloud-export.gen.json")
	require.NoError(t, err)
	changes, err := Compare(MustParse(data), MustParse(data))
	require.NoError(t, err)
	require.Empty(t, changes)
}
