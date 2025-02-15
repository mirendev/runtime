package ui

import (
	"fmt"
	"io"
)

type List struct {
	w io.Writer
}

func NewList(w io.Writer) *List {
	return &List{
		w: w,
	}
}

func Completed(str string, args ...any) string {
	return fmt.Sprintf(Checkmark+" "+str, args...)
}
