package sval

import (
	"fmt"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func Each[T any](fn func(v T) error) validation.Rule {
	return validation.Each(Struct[T](fn))
}

type Struct[T any] func(v T) error

func (s Struct[T]) Validate(value any) error {
	sv, ok := value.(T)
	if !ok {
		return fmt.Errorf("unable to convert to types %T => %T", value, sv)
	}

	return s(sv)
}
