package cond

import "fmt"

type ErrNotFonud struct {
	Element string
}

func (e *ErrNotFonud) Error() string {
	return "not found: " + e.Element
}

func NotFound(element any) error {
	return &ErrNotFonud{Element: fmt.Sprint(element)}
}
