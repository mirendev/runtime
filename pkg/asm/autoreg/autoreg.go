package autoreg

import (
	"reflect"
	"sync"

	"miren.dev/runtime/pkg/set"
)

type autoReg struct {
	mu sync.Mutex

	funcs set.Set[reflect.Value]
}

var defReg = &autoReg{
	funcs: set.New[reflect.Value](),
}

func Register[T any]() bool {
	defReg.mu.Lock()
	defer defReg.mu.Unlock()

	defReg.funcs.Add(reflect.ValueOf(func() *T {
		var v T
		return &v
	}))

	return true
}

func All() []reflect.Value {
	defReg.mu.Lock()
	defer defReg.mu.Unlock()

	return defReg.funcs.Values()
}
