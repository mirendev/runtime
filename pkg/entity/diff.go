package entity

func (e *Entity) Clone() *Entity {
	clonedAttrs := make([]Attr, len(e.attrs))
	for i, attr := range e.attrs {
		clonedAttrs[i] = Attr{
			ID:    attr.ID,
			Value: attr.Value.Clone(),
		}
	}
	return &Entity{attrs: clonedAttrs}
}

// Diff returns the difference between two entities.
// Returns attributes that are in entity 'a' but not in entity 'b'
func Diff(a, b *Entity) []Attr {
	var diff []Attr

	for _, aAttr := range a.attrs {
		found := false
		for _, bAttr := range b.attrs {
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
