package types

import "strings"

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
