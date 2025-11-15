package cond

import (
	"context"
	"errors"
	"fmt"
	"io"
)

type ErrNotFound struct {
	Category string
	Element  string
}

func (e ErrNotFound) Error() string {
	return "not found: " + e.Element
}

func (e ErrNotFound) ErrorCategory() string {
	return e.Category
}

func (e ErrNotFound) ErrorCode() string {
	return "not-found"
}

func (e ErrNotFound) Is(target error) bool {
	if target == nil {
		return false
	}

	_, ok := target.(ErrNotFound)
	return ok
}

func NotFound(category string, element any) error {
	return ErrNotFound{Category: category, Element: fmt.Sprint(element)}
}

type ErrConflict struct {
	Category string
	Element  string
}

func (e ErrConflict) Error() string {
	return fmt.Sprintf("conflict in %s: %s", e.Category, e.Element)
}

func (e ErrConflict) ErrorCategory() string {
	return e.Category
}

func (e ErrConflict) ErrorCode() string {
	return "conflict"
}

func (e ErrConflict) Is(target error) bool {
	if target == nil {
		return false
	}

	_, ok := target.(ErrConflict)
	return ok
}

func Conflict(category string, element any) error {
	return ErrConflict{Category: category, Element: fmt.Sprint(element)}
}

type ErrCorruption struct {
	Category string
	Message  string
}

func (e ErrCorruption) Error() string {
	return fmt.Sprintf("corruption in %s: %s", e.Category, e.Message)
}

func (e ErrCorruption) ErrorMessage() string {
	return e.Message
}

func (e ErrCorruption) ErrorCategory() string {
	return e.Category
}

func (e ErrCorruption) ErrorCode() string {
	return "corruption"
}

func (e ErrCorruption) Is(target error) bool {
	if target == nil {
		return false
	}

	_, ok := target.(ErrCorruption)
	return ok
}

func Corruption(category string, message string, args ...any) error {
	return ErrCorruption{Category: category, Message: fmt.Sprintf(message, args...)}
}

type ErrGeneric struct {
	Message string
	inner   error
}

func (e ErrGeneric) Error() string {
	return e.Message
}

func (e ErrGeneric) ErrorCategory() string {
	return "generic"
}

func (e ErrGeneric) ErrorCode() string {
	return "unknown"
}

func (e ErrGeneric) Unwrap() error {
	return e.inner
}

func Error(str string) error {
	return ErrGeneric{Message: str}
}

type ErrRemote struct {
	Category string
	Code     string
	Message  string
}

func (e ErrRemote) ErrorCategory() string {
	return e.Category
}

func (e ErrRemote) ErrorCode() string {
	return e.Code
}

func (e ErrRemote) ErrorMessage() string {
	return e.Message
}

func (e ErrRemote) Error() string {
	cat := e.Category
	code := e.Code

	if cat == "" {
		cat = "generic"
	}

	if code == "" {
		code = "unknown"
	}

	return fmt.Sprintf("remote error: %s %s: %s", cat, code, e.Message)
}

func RemoteError(category, code, message string) error {
	switch code {
	case "closed":
		return ErrClosed{Message: message}
	case "validation-failure":
		return ValidationFailure(category, message)
	case "not-found":
		return NotFound(category, message)
	case "conflict":
		return Conflict(category, message)
	case "corruption":
		return ErrCorruption{Category: category, Message: message}
	}

	return ErrRemote{
		Category: category,
		Code:     code,
		Message:  message,
	}
}

func Panic(message string) error {
	return ErrPanic{Message: message}
}

func Errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	inner := errors.Unwrap(err)
	return ErrGeneric{Message: err.Error(), inner: inner}
}

type ErrPanic struct {
	Message string
}

func (e ErrPanic) Error() string {
	return "panic: " + e.Message
}

type ErrClosed struct {
	Message string
}

func (e ErrClosed) Error() string {
	return "closed: " + e.Message
}

func (e ErrClosed) ErrorCategory() string {
	return "closed"
}

func (e ErrClosed) ErrorCode() string {
	return "closed"
}

func (e ErrClosed) ErrorMessage() string {
	return e.Message
}

func (e ErrClosed) Is(target error) bool {
	if target == nil {
		return false
	}

	switch target.(type) {
	case ErrClosed:
		return true
	}

	return false
}

func Closed(message string) error {
	return ErrClosed{Message: message}
}

func Wrap(err error) error {
	if err == nil {
		return nil
	}

	// Return existing cond errors unchanged
	switch err.(type) {
	case ErrNotFound, ErrConflict, ErrCorruption, ErrGeneric, ErrRemote, ErrPanic, ErrClosed, ErrValidationFailure:
		return err
	}

	switch {
	case errors.Is(err, io.EOF):
		return ErrClosed{Message: err.Error()}
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return err
	}

	return Error(err.Error())
}

type ErrValidationFailure struct {
	Message  string
	Category string
}

func (e ErrValidationFailure) Error() string {
	return "validation failure: " + e.Message
}

func (e ErrValidationFailure) ErrorCategory() string {
	return e.Category
}

func (e ErrValidationFailure) ErrorCode() string {
	return "validation-failure"
}

func (e ErrValidationFailure) ErrorMessage() string {
	return e.Message
}

func ValidationFailure(category, message string) error {
	return ErrValidationFailure{
		Category: category,
		Message:  message,
	}
}
