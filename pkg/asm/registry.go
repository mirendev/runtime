package asm

import (
	"fmt"
	"reflect"

	"github.com/pkg/errors"
)

type builderKey struct {
	typ  reflect.Type
	name string
}

type builtValue struct {
	val  reflect.Value
	name string
}

type Registry struct {
	components map[string]interface{}

	built    []builtValue
	builders map[builderKey]reflect.Value
}

func (r *Registry) Register(name string, component interface{}) {
	if r.components == nil {
		r.components = make(map[string]interface{})
	}
	r.components[name] = component
}

func canPopulate(rv reflect.Value) bool {
	return reflect.Indirect(rv).Kind() == reflect.Struct
}

func (r *Registry) buildByType(field reflect.Value, tag string) (reflect.Value, error) {
	for _, v := range r.built {
		if isAssignableTo(v.val.Type(), field.Type()) {
			return v.val, nil
		}
	}

	var (
		builder reflect.Value
		ok      bool
	)

	for k, v := range r.builders {
		if tag != "" && k.name != tag {
			continue
		}

		if isAssignableTo(field.Type(), k.typ) {
			ok = true
			builder = v
			break
		}
	}

	if !ok {
		return reflect.Value{}, fmt.Errorf("no builder for %q", field.Type())
	}

	result := builder.Call(nil)
	if len(result) == 2 {
		if !result[1].IsNil() {
			return reflect.Value{}, result[1].Interface().(error)
		}
	}

	ret := result[0]

	r.built = append(r.built, builtValue{ret, tag})

	if canPopulate(ret) {
		err := r.Populate(ret.Interface())
		if err != nil {
			return reflect.Value{}, err
		}
	}

	return ret, nil
}

func isAssignableTo(a, b reflect.Type) bool {
	if a == b {
		return true
	}

	if a.Kind() == reflect.Interface {
		return b.Implements(a)
	}

	return a.AssignableTo(b)
}

func (r *Registry) populateByType(field reflect.Value, tag string) error {
	if tag == "" {
		for _, v := range r.components {
			cv := reflect.ValueOf(v)

			if isAssignableTo(cv.Type(), field.Type()) {
				field.Set(cv)
				return nil
			}
		}
	}

	ret, err := r.buildByType(field, tag)
	if err == nil {
		field.Set(ret)
		return nil
	}

	return errors.Wrapf(err, "error building component of type %s available", field.Type())
}

type HasPopulated interface {
	Populated() error
}

func (r *Registry) RunHooks(s any) error {
	if pop, ok := s.(HasPopulated); ok {
		return pop.Populated()
	}

	return nil
}

func (r *Registry) Resolve(s any) error {
	rv := reflect.ValueOf(s)

	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("expected a pointer, got %T", s)
	}

	err := r.populateByType(rv.Elem(), "")
	if err != nil {
		return err
	}

	return nil
}

func (r *Registry) ResolveNamed(s any, name string) error {
	rv := reflect.ValueOf(s)

	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("expected a pointer, got %T", s)
	}

	err := r.populateByType(rv.Elem(), name)
	if err != nil {
		return err
	}

	return nil
}

var injectFuncType = reflect.TypeFor[func(*Registry) error]()

func (r *Registry) Init(values ...any) error {
	for _, v := range values {
		err := r.Resolve(v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Registry) Populate(s interface{}) error {
	rv := reflect.ValueOf(s)

	rv = reflect.Indirect(rv)

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct, got %T", s)
	}

fields:
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rv.Type().Field(i)

		if !fieldType.IsExported() {
			continue
		}

		tag, ok := fieldType.Tag.Lookup("asm")
		if !ok {
			err := r.populateByType(field, "")
			if err != nil {
				return err
			}
			continue
		}

		component, ok := r.components[tag]
		if !ok {
			err := r.populateByType(field, tag)
			if err != nil {
				return err
			}
			continue fields
		}

		if !isAssignableTo(reflect.TypeOf(component), field.Type()) {
			return fmt.Errorf("component %q is of type %T, expected %s", tag, component, field.Type())
		}

		field.Set(reflect.ValueOf(component))
	}

	return r.RunHooks(s)
}

func (r *Registry) ProvideName(name string, f interface{}) {
	v := reflect.ValueOf(f)
	t := v.Type()

	if t.Kind() != reflect.Func {
		panic("expected a function")
	}

	if t.NumOut() != 1 && t.NumOut() != 2 {
		panic("expected a function with one return value")
	}

	if r.builders == nil {
		r.builders = make(map[builderKey]reflect.Value)
	}

	if _, ok := r.builders[builderKey{t.Out(0), name}]; ok {
		panic("existing builder for type: " + t.Out(0).String())
	}

	r.builders[builderKey{t.Out(0), name}] = v
}

func (r *Registry) Provide(f interface{}) {
	r.ProvideName("", f)
}

func Pick[T any](r *Registry) (T, error) {
	var t T
	err := r.Resolve(&t)
	return t, err
}
