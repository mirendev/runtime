package query

import (
	"strings"
	"testing"
)

func TestSymbolRequestValidate(t *testing.T) {
	valid := []SymbolRequest{
		{Target: "kernel", Addresses: []uint64{1}},
		{Target: "kernel", Name: "schedule*", Limit: 4096},
		{Target: "binary", Path: "/bin/app", Name: "*main"},
		{Target: "process", PID: 1, Addresses: []uint64{1}},
		{Target: "process", PID: 1, Name: "handle*"},
	}
	for _, request := range valid {
		if err := request.Validate(); err != nil {
			t.Errorf("%+v: %v", request, err)
		}
	}

	tooMany := make([]uint64, 257)
	invalid := []SymbolRequest{
		{},
		{Target: "unknown", Addresses: []uint64{1}},
		{Target: "kernel"},
		{Target: "kernel", Name: "foo", Addresses: []uint64{1}},
		{Target: "kernel", Name: "*"},
		{Target: "kernel", Name: "a*b"},
		{Target: "kernel", Name: strings.Repeat("x", 257)},
		{Target: "kernel", Addresses: tooMany},
		{Target: "kernel", Name: "foo", Limit: 4097},
		{Target: "binary", Path: "relative", Name: "foo"},
		{Target: "process", Addresses: []uint64{1}},
		{Target: "process", Name: "foo"},
		{Target: "process", PID: 1},
		{Target: "process", PID: 1, Name: "foo", Addresses: []uint64{1}},
	}
	for _, request := range invalid {
		if err := request.Validate(); err == nil {
			t.Errorf("expected %+v to be rejected", request)
		}
	}
}
