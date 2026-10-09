//go:build !linux

package query

import (
	"context"
	"errors"
)

func inspectSymbols(context.Context, SymbolRequest) (SymbolResult, error) {
	return SymbolResult{}, errors.New("symbol inspection is unsupported on this platform")
}
