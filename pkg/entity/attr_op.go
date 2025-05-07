package entity

type AttrOpCode int

const (
	AttrOpCodeNone AttrOpCode = iota
	AttrOpCodeAdd
	AttrOpCodeRemove
)

type AttrOp struct {
	Op AttrOpCode

	Attr Attr
}

func AttrAdd(a Attr) AttrOp {
	return AttrOp{
		Op:   AttrOpCodeAdd,
		Attr: a,
	}
}

func AttrRemove(a Attr) AttrOp {
	return AttrOp{
		Op:   AttrOpCodeRemove,
		Attr: a,
	}
}

type AttrOps []AttrOp
