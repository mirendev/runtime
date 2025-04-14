package entity

func Is(ent AttrGetter, kind Id) bool {
	for _, a := range ent.GetAll(EntityKind) {
		if a.Value.Id() == kind {
			return true
		}
	}

	return false
}

type EntityAs interface {
	Is(ag AttrGetter) bool
	Decode(ag AttrGetter)
}

func TryAs(ent AttrGetter, ea EntityAs) bool {
	if !ea.Is(ent) {
		return false
	}

	ea.Decode(ent)

	return true
}

func As[
	T any,
	P interface {
		*T
		EntityAs
	},
](ent AttrGetter) (P, bool) {
	var obj P = new(T)

	if !obj.Is(ent) {
		return nil, false
	}

	obj.Decode(ent)

	return obj, true
}

func Empty[T comparable](x T) bool {
	if iz, ok := any(x).(interface{ IsZero() bool }); ok {
		return iz.IsZero()
	}

	var t T

	return t == x
}
