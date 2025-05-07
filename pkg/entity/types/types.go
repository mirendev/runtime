package types

import (
	"slices"
	"strings"
)

type (
	Id      string
	Keyword string
)

var tr = strings.NewReplacer("/", "__", ":", "___")

func (i Id) String() string {
	return string(i)
}

func (i Id) PathSafe() string {
	return tr.Replace(string(i))
}

type Label struct {
	Key   string `json:"key" cbor:"key"`
	Value string `json:"value" cbor:"value"`
}

type Labels []Label

func (l Labels) Get(key string) (string, bool) {
	for _, label := range l {
		if label.Key == key {
			return label.Value, true
		}
	}

	return "", false
}

func (l Labels) Equal(o Labels) bool {
	if len(l) != len(o) {
		return false
	}

	for _, a := range l {
		if !slices.Contains(o, a) {
			return false
		}
	}

	return true
}

func LabelSet(vals ...string) Labels {
	if len(vals)%2 != 0 {
		panic("LabelSet must have even number of values")
	}

	var labels Labels

	for i := 0; i < len(vals); i += 2 {
		labels = append(labels, Label{Key: vals[i], Value: vals[i+1]})
	}

	return labels
}
