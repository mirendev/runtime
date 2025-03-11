package rpc

import (
	"reflect"
	"sync"

	"github.com/mr-tron/base58"
)

type IfaceReg struct {
	mu     sync.RWMutex
	reg    map[reflect.Type]reflect.Type
	byName map[string]reflect.Type
}

func NewIfaceReg() *IfaceReg {
	return &IfaceReg{
		reg: make(map[reflect.Type]reflect.Type),
	}
}

var defaultIfaceReg = NewIfaceReg()

func CanBe[T, I any]() bool {
	defaultIfaceReg.mu.RLock()
	defer defaultIfaceReg.mu.RUnlock()

	t := reflect.TypeFor[T]()

	defaultIfaceReg.reg[t] = reflect.TypeFor[I]()

	name := t.PkgPath() + "." + t.Name()
	h := base58.Encode([]byte(name))
	defaultIfaceReg.byName[h] = t
	return true
}

func typeByHash(h string) (reflect.Type, bool) {
	defaultIfaceReg.mu.RLock()
	defer defaultIfaceReg.mu.RUnlock()

	t, ok := defaultIfaceReg.byName[h]
	return t, ok
}

type InterfaceCreators struct {
	mu       sync.Mutex
	creators map[reflect.Type]reflect.Value
}

func NewInterfaceCreators() *InterfaceCreators {
	return &InterfaceCreators{
		creators: make(map[reflect.Type]reflect.Value),
	}
}

var defaultInterfaceCreators = NewInterfaceCreators()

func RegisterInterface[T any](fn any) bool {
	rv := reflect.ValueOf(fn)

	if rv.Kind() != reflect.Func {
		panic("fn must be a function")
	}

	defaultInterfaceCreators.mu.Lock()
	defer defaultInterfaceCreators.mu.Unlock()

	defaultInterfaceCreators.creators[reflect.TypeFor[T]()] = rv

	return true
}

type InterfaceDescriptor struct {
}
