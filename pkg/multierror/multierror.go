package multierror

import (
	"errors"
	"slices"
)

type MultiError struct {
	errors []error
}

func (m *MultiError) Error() string {
	var s string
	for _, err := range m.errors {
		s += err.Error() + "\n"
	}
	return s
}

func (m *MultiError) Unwrap() []error {
	return m.errors
}

func (m *MultiError) Errors() []error {
	return m.errors
}

func (m *MultiError) Is(err error) bool {
	for _, e := range m.errors {
		if e == err {
			return true
		}
	}
	return false
}

func (m *MultiError) As(target any) bool {
	for _, e := range m.errors {
		if errors.As(e, target) {
			return true
		}
	}
	return false
}

func Append(err error, errs ...error) error {
	if err == nil {
		return nil
	}

	if len(errs) == 0 {
		return err
	}

	me, ok := err.(*MultiError)
	if ok {
		return &MultiError{
			errors: append(slices.Clone(me.errors), errs...),
		}
	}

	return &MultiError{
		errors: errs,
	}
}
