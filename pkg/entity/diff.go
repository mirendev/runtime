package entity

import "slices"

func (e *Entity) Clone() *Entity {
	f := *e

	f.Attrs = slices.Clone(f.Attrs)

	return &f
}

// Diff returns the difference between two entities.
// Returns attributes that are in entity 'a' but not in entity 'b'
func Diff(a, b *Entity) []Attr {
	var diff []Attr

	for _, aAttr := range a.Attrs {
		found := false
		for _, bAttr := range b.Attrs {
			if aAttr.Equal(bAttr) {
				found = true
				break
			}
		}
		if !found {
			diff = append(diff, aAttr)
		}
	}

	return diff
}
